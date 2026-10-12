// Package ordering implements the storefront cart/checkout flow and the
// full order lifecycle (BRD Sections 7, 9.3, 9.4, 12.4, 13). It depends on
// tenancy (branch hours/timezone/routing) and catalog (product pricing and
// availability) through narrow read-only interfaces, and on a
// paymentsapi.Processor seam that the payments module fills in.
package domain

import (
	"context"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/audit"
	"bakehouse/pkg/email"
	"bakehouse/pkg/geo"
	"bakehouse/pkg/realtime"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/orders/internal/store"
	"bakehouse/services/orders/ordersapi"
	"bakehouse/services/payments/paymentsapi"
)

type BranchLookup interface {
	GetBranch(ctx context.Context, id string) (*franchiseapi.Branch, error)
	ListBranchExceptions(ctx context.Context, branchID string) ([]franchiseapi.BranchException, error)
}

type CatalogLookup interface {
	GetPricedItem(ctx context.Context, branchID, productID string, variantID *string, flavourIDs, addonIDs []string, presentationID, wrapColourID *string, date time.Time) (*catalogapi.PricedItem, error)
}

type Service struct {
	store      *store.Store
	branches   BranchLookup
	catalog    CatalogLookup
	coupons    catalogapi.CouponPricer
	sales      catalogapi.SalesRecorder
	payments   paymentsapi.Processor
	emailer    email.Emailer
	audit      *audit.Writer
	locationIQ *locationIQClient
	// KDS: central-kitchen coverage and per-location KDS settings (see
	// UseKDS), and the live-update hub.
	kds franchiseapi.KDSReader
	hub *realtime.Hub
	// cards: payment links and refunds through Stripe (see
	// UseCardPayments). Nil until wired; card then means manual only.
	cards paymentsapi.Cards
	// tagRestrictions: seasonal tags' restricted dates (see
	// service_restricted_dates.go). Nil: nothing is restricted.
	tagRestrictions catalogapi.TagRestrictions
	// Street-address geocoding for radius delivery (see UseAddressGeocoder).
	addrGeo geo.AddressGeocoder
}

func NewService(store *store.Store, branches BranchLookup, catalogSvc CatalogLookup, coupons catalogapi.CouponPricer, sales catalogapi.SalesRecorder, payments paymentsapi.Processor, emailer email.Emailer, auditWriter *audit.Writer, locationIQAccessToken string) *Service {
	if payments == nil {
		payments = paymentsapi.NoopProcessor{}
	}
	return &Service{
		store: store, branches: branches, catalog: catalogSvc, coupons: coupons, sales: sales,
		payments: payments, emailer: emailer, audit: auditWriter,
		locationIQ: newLocationIQClient(locationIQAccessToken),
	}
}

func toSelectedModifiers(in []catalogapi.SelectedModifier) []ordersapi.SelectedModifier {
	out := make([]ordersapi.SelectedModifier, 0, len(in))
	for _, m := range in {
		out = append(out, ordersapi.SelectedModifier{ID: m.ID, Name: m.Name, PriceDelta: m.PriceDelta})
	}
	return out
}

func toSelectedModifierPtr(in *catalogapi.SelectedModifier) *ordersapi.SelectedModifier {
	if in == nil {
		return nil
	}
	return &ordersapi.SelectedModifier{ID: in.ID, Name: in.Name, PriceDelta: in.PriceDelta}
}

// trimPtr trims a string pointer and turns an empty result into nil, so the
// database holds NULL rather than "" for "not supplied".
func trimPtr(p *string) *string {
	if p == nil {
		return nil
	}
	t := strings.TrimSpace(*p)
	if t == "" {
		return nil
	}
	return &t
}

// --- Storefront recipient book -------------------------------------------------

// RecipientOwner identifies who a recipient book belongs to. Every request
// carries the purchaser's email (guests have nothing else); a signed-in
// customer also carries their id, which widens what they can see to rows
// saved under the account.
type RecipientOwner struct {
	PurchaserEmail string
	CustomerID     *string
}

func (o RecipientOwner) validate() error {
	e := strings.TrimSpace(o.PurchaserEmail)
	if e == "" {
		return apperr.Validation("purchaserEmail is required.")
	}
	if !strings.Contains(e, "@") {
		return apperr.Validation("purchaserEmail must be a valid email address.")
	}
	return nil
}

// SaveRecipientRequest is the body of POST and PUT /storefront/recipients.
type SaveRecipientRequest struct {
	Email                string
	FirstName            string
	LastName             string
	Phone                *string
	BusinessName         *string
	DeliveryAddressType  *string
	DeliveryInstructions *string
	DeliveryStreet       *string
	DeliveryCity         *string
	DeliveryState        *string
	DeliveryPostcode     *string
}

func (r SaveRecipientRequest) toModel(owner RecipientOwner) (ordersapi.StorefrontRecipient, error) {
	// Update paths pass an owner with no email; the stored row keeps its own.
	email := strings.TrimSpace(r.Email)
	if email == "" || !strings.Contains(email, "@") {
		return ordersapi.StorefrontRecipient{}, apperr.Validation("Recipient email is required.")
	}
	first, last := strings.TrimSpace(r.FirstName), strings.TrimSpace(r.LastName)
	if first == "" || last == "" {
		return ordersapi.StorefrontRecipient{}, apperr.Validation("Recipient first and last name are required.")
	}
	addrType := trimPtr(r.DeliveryAddressType)
	if addrType != nil && !ordersapi.IsValidAddressType(*addrType) {
		return ordersapi.StorefrontRecipient{}, apperr.Validation("deliveryAddressType must be \"residence\" or \"business\".")
	}
	return ordersapi.StorefrontRecipient{
		PurchaserEmail: strings.TrimSpace(owner.PurchaserEmail), CustomerID: owner.CustomerID,
		RecipientEmail: email, FirstName: first, LastName: last, Phone: trimPtr(r.Phone),
		BusinessName: trimPtr(r.BusinessName), DeliveryAddressType: addrType,
		DeliveryInstructions: trimPtr(r.DeliveryInstructions),
		DeliveryStreet:       trimPtr(r.DeliveryStreet), DeliveryCity: trimPtr(r.DeliveryCity),
		DeliveryState: trimPtr(r.DeliveryState), DeliveryPostcode: trimPtr(r.DeliveryPostcode),
	}, nil
}

func (s *Service) ListStorefrontRecipients(ctx context.Context, owner RecipientOwner) ([]ordersapi.StorefrontRecipient, error) {
	if err := owner.validate(); err != nil {
		return nil, err
	}
	recipients, err := s.store.ListStorefrontRecipients(ctx, owner.PurchaserEmail, owner.CustomerID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return recipients, nil
}

func (s *Service) GetStorefrontRecipient(ctx context.Context, id string) (*ordersapi.StorefrontRecipient, error) {
	if !looksLikeUUID(id) {
		return nil, apperr.NotFound("Recipient not found.")
	}
	r, err := s.store.GetStorefrontRecipient(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Recipient not found.")
	}
	return r, nil
}

// CreateStorefrontRecipient saves a recipient ahead of (or instead of) an
// order. Saving an email already in the purchaser's book updates that entry.
func (s *Service) CreateStorefrontRecipient(ctx context.Context, owner RecipientOwner, req SaveRecipientRequest) (*ordersapi.StorefrontRecipient, error) {
	if err := owner.validate(); err != nil {
		return nil, err
	}
	m, err := req.toModel(owner)
	if err != nil {
		return nil, err
	}
	saved, err := s.store.CreateStorefrontRecipient(ctx, m)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return saved, nil
}

// UpdateStorefrontRecipient rewrites one saved recipient, found by id alone.
// customerID, when the caller is signed in, is stamped onto a row that has
// none so the recipient follows the account from then on.
func (s *Service) UpdateStorefrontRecipient(ctx context.Context, customerID *string, id string, req SaveRecipientRequest) (*ordersapi.StorefrontRecipient, error) {
	if !looksLikeUUID(id) {
		return nil, apperr.NotFound("Recipient not found.")
	}
	m, err := req.toModel(RecipientOwner{CustomerID: customerID})
	if err != nil {
		return nil, err
	}
	saved, err := s.store.UpdateStorefrontRecipient(ctx, id, customerID, m)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, apperr.Conflict("Another saved recipient already uses that email.")
		}
		return nil, notFoundOrInternal(err, "Recipient not found.")
	}
	return saved, nil
}

// DeleteStorefrontRecipient removes one saved recipient by id alone.
func (s *Service) DeleteStorefrontRecipient(ctx context.Context, id string) error {
	if !looksLikeUUID(id) {
		return apperr.NotFound("Recipient not found.")
	}
	if err := s.store.DeleteStorefrontRecipient(ctx, id); err != nil {
		return notFoundOrInternal(err, "Recipient not found.")
	}
	return nil
}

// looksLikeUUID is a cheap shape check so a malformed id reads as "not
// found" rather than surfacing Postgres's cast error as INTERNAL_ERROR.
func looksLikeUUID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range id {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}
