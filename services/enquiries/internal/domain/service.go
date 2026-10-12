package domain

import (
	"context"
	"errors"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/email"
	"bakehouse/pkg/ratelimit"
	"bakehouse/pkg/tenant"
	"bakehouse/services/enquiries/enquiriesapi"
	"bakehouse/services/enquiries/internal/store"
	"bakehouse/services/franchise/franchiseapi"
)

// BranchLookup is the franchise read surface this service needs: a custom-order
// enquiry names a store, and the enquiry has to inherit that branch's franchise
// so it lands in the right inbox (SF-34).
type BranchLookup interface {
	GetBranch(ctx context.Context, id string) (*franchiseapi.Branch, error)
}

// Public forms are unauthenticated, so they are throttled per address to keep
// the admin queue usable. The window is generous enough that a person
// correcting a typo is never blocked.
const (
	submitLimit  = 5
	submitWindow = time.Hour
)

// Service holds enquiry capture and the admin queue.
type Service struct {
	store    *store.Store
	branches BranchLookup
	emailer  email.Emailer
	// Where enquiries with no branch attached are sent.
	inbox   string
	limiter *ratelimit.Limiter
}

// NewService assembles the service. limiter and emailer may be nil — the
// forms still record, they simply lose throttling and notification.
func NewService(st *store.Store, branches BranchLookup, emailer email.Emailer, limiter *ratelimit.Limiter) *Service {
	return &Service{store: st, branches: branches, emailer: emailer, limiter: limiter}
}

// SubmitRequest is one submitted form, before type-specific validation.
type SubmitRequest struct {
	Type     string
	Name     string
	Email    string
	Phone    string
	Message  string
	BranchID string
	// Details carries the form's own fields.
	Details map[string]any
	// SourceIP is recorded for abuse triage, never returned to a client.
	SourceIP string
}

// Submit validates, routes and records an enquiry, then notifies the branch or
// head office best-effort. A notification failure never fails the submission —
// losing the enquiry would be worse than losing the email.
func (s *Service) Submit(ctx context.Context, req SubmitRequest) (*enquiriesapi.Enquiry, error) {
	switch req.Type {
	case enquiriesapi.TypeCustomOrder, enquiriesapi.TypeFranchise, enquiriesapi.TypeContact:
	default:
		return nil, apperr.Validation("Unknown enquiry type.")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, apperr.Validation("Your name is required.").
			WithFields(apperr.FieldError{Field: "name", Code: "REQUIRED", Message: "Please tell us your name."})
	}
	addr, err := normaliseEmail(req.Email)
	if err != nil {
		return nil, err
	}
	if err := s.checkRate(ctx, addr); err != nil {
		return nil, err
	}

	enquiry := enquiriesapi.Enquiry{
		Type:    req.Type,
		Name:    name,
		Email:   addr,
		Phone:   trimmedPtr(req.Phone),
		Message: trimmedPtr(req.Message),
		Details: req.Details,
	}
	if enquiry.Details == nil {
		enquiry.Details = map[string]any{}
	}

	// A named store routes the enquiry to that branch's staff.
	if req.BranchID != "" {
		branch, err := s.branches.GetBranch(ctx, req.BranchID)
		if err != nil {
			return nil, apperr.Validation("Select a store location we recognise.").
				WithFields(apperr.FieldError{Field: "branchId", Code: "UNKNOWN", Message: "Unknown store."})
		}
		enquiry.BranchID = &branch.ID
	} else if req.Type == enquiriesapi.TypeCustomOrder {
		return nil, apperr.Validation("Choose the store that should handle your order.").
			WithFields(apperr.FieldError{Field: "branchId", Code: "REQUIRED", Message: "Please select a store location."})
	}

	created, err := s.store.Create(ctx, enquiry, req.SourceIP)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	s.notify(ctx, created)
	return created, nil
}

// checkRate throttles submissions per email address. A limiter error is
// deliberately not fatal: an unavailable Redis should not stop a customer
// reaching the business.
func (s *Service) checkRate(ctx context.Context, addr string) error {
	if s.limiter == nil {
		return nil
	}
	ok, err := s.limiter.Allow(ctx, "enquiry:"+addr, submitLimit, submitWindow)
	if err != nil {
		return nil
	}
	if !ok {
		return apperr.RateLimited("You have sent several enquiries already. Please give us a little time to reply.")
	}
	return nil
}

// notify emails the handling branch, or head office when the enquiry is
// brand-level. Best-effort by design — see Submit.
func (s *Service) notify(ctx context.Context, e *enquiriesapi.Enquiry) {
	if s.emailer == nil {
		return
	}
	data := map[string]interface{}{
		"type": e.Type, "name": e.Name, "email": e.Email, "enquiryId": e.ID,
	}
	if e.Phone != nil {
		data["phone"] = *e.Phone
	}
	if e.Message != nil {
		data["message"] = *e.Message
	}
	for k, v := range e.Details {
		data[k] = v
	}

	to := s.brandInbox()
	if e.BranchID != nil {
		if branch, err := s.branches.GetBranch(ctx, *e.BranchID); err == nil {
			data["branchName"] = branch.Name
			to = branchInbox(branch)
		}
	}
	_ = s.emailer.Send(ctx, email.Email{To: to, Template: email.TemplateEnquiryReceived, Data: data})

	/*
	 * And the person who sent it.
	 *
	 * Only the shop was being told. Somebody who fills in a custom-order or
	 * franchise form gets no acknowledgement at all — so as far as they can
	 * tell it went nowhere, and the usual next step is to send it again.
	 */
	if strings.TrimSpace(e.Email) != "" {
		if err := s.emailer.Send(ctx, email.Email{
			To:       e.Email,
			Template: email.TemplateEnquiryAcknowledged,
			Data:     data,
		}); err != nil {
			slog.Error("enquiry acknowledgement failed to send",
				"enquiryId", e.ID, "to", e.Email, "cause", err)
		}
	}
}

// brandInbox is where franchise and general enquiries land.
//
// Configured rather than compiled in, so a deployment can point it at a
// mailbox somebody actually reads.
func (s *Service) brandInbox() string {
	if strings.TrimSpace(s.inbox) != "" {
		return s.inbox
	}
	return "hello@bakedbouquet.com"
}

// branchInbox derives a branch's contact address from its code, matching the
// convention head office already uses for store mailboxes.
func branchInbox(b *franchiseapi.Branch) string {
	return strings.ToLower(b.Code) + "@bakedbouquet.com"
}

// --- Admin queue -------------------------------------------------------------

// List returns the enquiry queue visible to the caller. Brand-scoped staff see
// everything; everyone else sees only their own branch's enquiries.
func (s *Service) List(ctx context.Context, claims *auth.StaffClaims, f enquiriesapi.EnquiryFilter) ([]enquiriesapi.Enquiry, int, error) {
	if !tenant.HasPermission(claims, tenant.PermCustomersView) {
		return nil, 0, apperr.PermissionDenied("You do not have permission to view enquiries.")
	}
	if !tenant.IsBrandScoped(claims) && f.BranchID == "" {
		// Scope is taken from the token, never from the query string.
		branchID, ok := singleBranch(claims)
		if !ok {
			return nil, 0, apperr.PermissionDenied("Select a branch you have access to.")
		}
		f.BranchID = branchID
	}
	if f.BranchID != "" && !tenant.IsBrandScoped(claims) {
		if !branchInScope(claims, f.BranchID) {
			return nil, 0, apperr.PermissionDenied("You do not have access to that branch.")
		}
	}

	list, total, err := s.store.List(ctx, f)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}
	return list, total, nil
}

// Get returns one enquiry, re-checking branch scope on the resolved record.
func (s *Service) Get(ctx context.Context, claims *auth.StaffClaims, id string) (*enquiriesapi.Enquiry, error) {
	if !tenant.HasPermission(claims, tenant.PermCustomersView) {
		return nil, apperr.PermissionDenied("You do not have permission to view enquiries.")
	}
	e, err := s.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, apperr.NotFound("Enquiry not found.")
		}
		return nil, apperr.Internal(err)
	}
	if err := s.assertScope(claims, e); err != nil {
		return nil, err
	}
	return e, nil
}

// UpdateStatus moves an enquiry through its lifecycle.
func (s *Service) UpdateStatus(ctx context.Context, claims *auth.StaffClaims, id, status string, note *string) (*enquiriesapi.Enquiry, error) {
	switch status {
	case enquiriesapi.StatusNew, enquiriesapi.StatusInProgress, enquiriesapi.StatusClosed:
	default:
		return nil, apperr.Validation("status must be new, in_progress or closed.")
	}
	existing, err := s.Get(ctx, claims, id)
	if err != nil {
		return nil, err
	}
	updated, err := s.store.UpdateStatus(ctx, existing.ID, status, claims.StaffID, note)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return updated, nil
}

// assertScope refuses an enquiry belonging to a branch the caller cannot see.
func (s *Service) assertScope(claims *auth.StaffClaims, e *enquiriesapi.Enquiry) error {
	if tenant.IsBrandScoped(claims) {
		return nil
	}
	if e.BranchID == nil {
		// Brand-level enquiries are head office's to answer.
		return apperr.PermissionDenied("You do not have access to this enquiry.")
	}
	if !tenant.CanAccessBranch(claims, *e.BranchID) {
		return apperr.PermissionDenied("You do not have access to this enquiry.")
	}
	return nil
}

func branchInScope(claims *auth.StaffClaims, branchID string) bool {
	for _, ra := range claims.RoleAssignments {
		if ra.ScopeType == tenant.ScopeBranch && ra.BranchID != nil && *ra.BranchID == branchID {
			return true
		}
	}
	return false
}

// singleBranch returns the caller's branch when they hold exactly one branch
// assignment, so the queue defaults to it without an explicit filter.
func singleBranch(claims *auth.StaffClaims) (string, bool) {
	var found string
	for _, ra := range claims.RoleAssignments {
		if ra.ScopeType == tenant.ScopeBranch && ra.BranchID != nil {
			if found != "" && found != *ra.BranchID {
				return "", false
			}
			found = *ra.BranchID
		}
	}
	return found, found != ""
}

// --- Newsletter --------------------------------------------------------------

// Subscribe records marketing consent with its timestamp (NOT-06).
func (s *Service) Subscribe(ctx context.Context, rawEmail, source string) (*enquiriesapi.NewsletterSubscriber, error) {
	addr, err := normaliseEmail(rawEmail)
	if err != nil {
		return nil, err
	}
	if source == "" {
		source = "footer"
	}
	sub, err := s.store.Subscribe(ctx, addr, source)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return sub, nil
}

// Unsubscribe withdraws consent. Unknown addresses succeed silently so the
// endpoint cannot be used to test whether someone is subscribed.
func (s *Service) Unsubscribe(ctx context.Context, rawEmail string) error {
	addr, err := normaliseEmail(rawEmail)
	if err != nil {
		return err
	}
	if err := s.store.Unsubscribe(ctx, addr); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

func normaliseEmail(raw string) (string, error) {
	addr := strings.ToLower(strings.TrimSpace(raw))
	if addr == "" {
		return "", apperr.Validation("Email address is required.").
			WithFields(apperr.FieldError{Field: "email", Code: "REQUIRED", Message: "Please enter your email."})
	}
	if _, err := mail.ParseAddress(addr); err != nil {
		return "", apperr.Validation("Enter a valid email address.").
			WithFields(apperr.FieldError{Field: "email", Code: "INVALID", Message: "That does not look like an email address."})
	}
	return addr, nil
}

func trimmedPtr(v string) *string {
	if v = strings.TrimSpace(v); v == "" {
		return nil
	}
	return &v
}

// UseBrandInbox points brand-level enquiries at a configured mailbox.
func (s *Service) UseBrandInbox(addr string) { s.inbox = addr }
