package domain

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/audit"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/geo"
	"bakehouse/pkg/tenant"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store"
)

type Service struct {
	store    *store.Store
	audit    *audit.Writer
	geocoder geo.Geocoder
	// addrGeo locates full addresses for radius delivery (see
	// UseAddressGeocoder). Nil: radius stores can't be quoted.
	addrGeo geo.AddressGeocoder
}

// UseAddressGeocoder wires street-address geocoding: stores are located from
// their address when saved, and customers when delivery is checked.
func (s *Service) UseAddressGeocoder(g geo.AddressGeocoder) { s.addrGeo = g }

func NewService(store *store.Store, auditWriter *audit.Writer, geocoder geo.Geocoder) *Service {
	if geocoder == nil {
		geocoder = geo.NoopGeocoder{}
	}
	return &Service{store: store, audit: auditWriter, geocoder: geocoder}
}

// --- Branches (GOV-04, GOV-05, GOV-08) --------------------------------------

// CreateBranch provisions a branch. It goes straight to
// franchiseapi.StatusActive: GOV-05/NFR-14 require a new branch to be able to trade the
// same working day via self-service configuration alone, with no separate
// activation step. Delivery-zone overlap with an existing active branch is
// reported as a warning, never a hard failure (GOV-08).
func (s *Service) CreateBranch(ctx context.Context, claims *auth.StaffClaims, b franchiseapi.Branch) (*franchiseapi.Branch, []string, error) {
	if !tenant.HasPermission(claims, tenant.PermBranchManage) {
		return nil, nil, apperr.PermissionDenied("You do not have permission to create a branch.")
	}
	if strings.TrimSpace(b.Name) == "" || strings.TrimSpace(b.Code) == "" {
		return nil, nil, apperr.Validation("Branch name and code are required.")
	}
	if strings.TrimSpace(b.Timezone) == "" {
		return nil, nil, apperr.Validation("Choose the store's timezone (for example Europe/London).").
			WithFields(apperr.FieldError{Field: "timezone", Code: "REQUIRED", Message: "Required."})
	}
	// The store belongs to the business of whoever creates it.
	if claims.BusinessID != "" {
		b.BusinessID = &claims.BusinessID
	}
	if len(b.FulfilmentTypes) == 0 {
		b.FulfilmentTypes = []string{"pickup", "delivery"}
	}
	if b.DeliveryZone.Type == "" {
		b.DeliveryZone.Type = "zip_list"
	}
	if strings.TrimSpace(b.OwnershipType) == "" {
		b.OwnershipType = franchiseapi.OwnershipFranchise
	}
	ownership, ok := franchiseapi.NormalizeOwnershipType(b.OwnershipType)
	if !ok {
		return nil, nil, invalidOwnershipType()
	}
	b.OwnershipType = ownership

	// A Location ID names one place: a store's cannot be a central kitchen's.
	if b.LocationCode != nil && strings.TrimSpace(*b.LocationCode) != "" {
		if err := s.assertBusinessIDFree(ctx, strings.TrimSpace(*b.LocationCode), "", ""); err != nil {
			return nil, nil, err
		}
	}

	if err := normalizeStoreHours(&b.PickupHours, &b.DeliveryHours); err != nil {
		return nil, nil, err
	}
	if b.CardProviders != nil {
		providers, err := franchiseapi.NormalizeCardProviders(b.CardProviders)
		if err != nil {
			return nil, nil, cardProvidersInvalid(err)
		}
		b.CardProviders = providers
	}

	if b.DeliveryPricing.Region != "" {
		if err := b.DeliveryPricing.Validate(b.DeliveryZone.Zips); err != nil {
			return nil, nil, apperr.Validation(err.Error()).
				WithFields(apperr.FieldError{Field: "deliveryPricing", Code: "INVALID", Message: err.Error()})
		}
	}

	warnings, err := s.overlapWarnings(ctx, "", b.DeliveryZone)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}

	out, err := s.store.CreateBranch(ctx, b)
	if err != nil {
		// The code is the outlet's human handle and the database enforces it as
		// unique. Saying which field clashed is the difference between a form
		// the user can fix and "an unexpected error occurred".
		if isDuplicateKey(err) {
			return nil, nil, apperr.New("BRANCH_CODE_TAKEN", apperr.CategoryConflict,
				"An outlet with the code \""+b.Code+"\" already exists. Choose another.").
				WithFields(apperr.FieldError{Field: "code", Code: "TAKEN", Message: "Already in use."})
		}
		return nil, nil, apperr.Internal(err)
	}
	if err := s.store.SetBranchStatus(ctx, out.ID, franchiseapi.StatusActive); err != nil {
		return nil, nil, apperr.Internal(err)
	}
	out.Status = franchiseapi.StatusActive

	// The code its till signs in with, allocated now rather than on demand: a
	// shop that opens on Monday needs the card printed on Friday, and asking
	// somebody to remember to generate one is how a till ends up locked out.
	businessID, err := s.store.EnsureBusinessID(ctx, out.ID, out.Code)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	out.LocationCode = &businessID

	// Both receipt kinds' print settings, at column defaults, right away —
	// same reasoning as the business ID above: GET must never 404 just
	// because nobody has opened the print settings screen yet.
	if err := s.store.EnsureDefaultPrintTemplates(ctx, out.ID,
		out.Name, printOutletAddress(out), derefStr(out.Phone)); err != nil {
		return nil, nil, apperr.Internal(err)
	}
	// KDS settings at their defaults, for the same reason: the store's KDS
	// must be able to read its settings the day the store opens.
	if err := s.store.EnsureKDSSettings(ctx, franchiseapi.KDSScope{
		Type: franchiseapi.LocationBranch, BranchID: out.ID}); err != nil {
		return nil, nil, apperr.Internal(err)
	}
	s.locateBranch(ctx, out, false)
	s.writeAudit(ctx, claims, "branch.create", "branch", &out.ID, &out.ID, nil, out, "")
	return out, warnings, nil
}

// overlapWarnings checks the given zip list against every other active
// branch's delivery zone and returns a human-readable warning per overlap
// found (GOV-08). excludeBranchID is used on update, so a branch does not
// "overlap" with its own prior zone.
func (s *Service) overlapWarnings(ctx context.Context, excludeBranchID string, zone franchiseapi.DeliveryZone) ([]string, error) {
	if zone.Type != "zip_list" || len(zone.Zips) == 0 {
		return nil, nil
	}
	others, err := s.store.ListActiveBranchesForRouting(ctx)
	if err != nil {
		return nil, err
	}
	newZips := map[string]bool{}
	for _, z := range zone.Zips {
		newZips[franchiseapi.NormalizePostcode(z)] = true
	}

	var warnings []string
	for _, other := range others {
		if other.ID == excludeBranchID || other.DeliveryZone.Type != "zip_list" {
			continue
		}
		var shared []string
		for _, z := range other.DeliveryZone.Zips {
			if newZips[franchiseapi.NormalizePostcode(z)] {
				shared = append(shared, z)
			}
		}
		if len(shared) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"Delivery zone overlaps with %s (%s) on postcode(s): %s. The routing tie-break (distance, then remaining capacity, then branch code) will decide which branch fulfils an order in the overlap.",
				other.Name, other.Code, strings.Join(shared, ", "),
			))
		}
	}
	return warnings, nil
}

func (s *Service) GetBranch(ctx context.Context, claims *auth.StaffClaims, id string) (*franchiseapi.Branch, error) {
	b, err := s.store.GetBranch(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Branch not found.")
	}
	if !tenant.CanAccessBranch(claims, "", b.ID) {
		return nil, apperr.PermissionDenied("You do not have access to this branch.")
	}
	return b, nil
}

func (s *Service) ListBranches(ctx context.Context, claims *auth.StaffClaims, status string) ([]franchiseapi.Branch, error) {
	all, err := s.store.ListBranches(ctx, status)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if tenant.IsBrandScoped(claims) {
		return all, nil
	}
	out := make([]franchiseapi.Branch, 0, len(all))
	for _, b := range all {
		if tenant.CanAccessBranch(claims, "", b.ID) {
			out = append(out, b)
		}
	}
	return out, nil
}

// UpdateBranch enforces field-level governance: commercial fields (delivery
// zone/fee, minimum order, tax) require PermBranchCommercial; operational
// fields (name, hours) require only PermBranchOperational — matching the
// governance_rules matrix (ADM-15: a field outside the caller's level is
// rejected with an explanatory message, not silently dropped).
// normalizeStoreHours checks and tidies the pickup and delivery weeks.
func normalizeStoreHours(pickup, delivery *[]franchiseapi.DayHours) error {
	p, err := franchiseapi.NormalizeHours(*pickup, "Pickup")
	if err != nil {
		return apperr.Validation(err.Error()).
			WithFields(apperr.FieldError{Field: "pickupHours", Code: "INVALID", Message: err.Error()})
	}
	d, err := franchiseapi.NormalizeHours(*delivery, "Delivery")
	if err != nil {
		return apperr.Validation(err.Error()).
			WithFields(apperr.FieldError{Field: "deliveryHours", Code: "INVALID", Message: err.Error()})
	}
	*pickup, *delivery = p, d
	return nil
}

func cardProvidersInvalid(err error) error {
	return apperr.Validation(err.Error()).
		WithFields(apperr.FieldError{Field: "cardProviders", Code: "INVALID", Message: err.Error()})
}

func (s *Service) UpdateBranch(ctx context.Context, claims *auth.StaffClaims, id string, patch franchiseapi.BranchPatch) (*franchiseapi.Branch, []string, error) {
	before, err := s.store.GetBranch(ctx, id)
	if err != nil {
		return nil, nil, notFoundOrInternal(err, "Branch not found.")
	}
	if !tenant.IsBrandScoped(claims) && !tenant.CanAccessBranch(claims, "", before.ID) {
		return nil, nil, apperr.PermissionDenied("You do not have access to this branch.")
	}

	touchesCommercial := patch.DeliveryZone != nil || patch.DeliveryFeeCents != nil || patch.MinOrderCents != nil ||
		patch.DeliveryPricing != nil ||
		patch.TaxName != nil || patch.TaxRatePct != nil
	touchesOperational := patch.Name != nil || patch.FulfilmentTypes != nil || patch.OpeningHours != nil || patch.OwnershipType != nil || patch.LocationCode != nil

	if touchesCommercial && !tenant.HasPermission(claims, tenant.PermBranchCommercial) {
		return nil, nil, apperr.New("GOVERNANCE_VIOLATION", apperr.CategoryAuthorization,
			"Delivery zone, fee, minimum order and tax fields are franchise-level — a branch manager cannot edit them.")
	}
	if touchesOperational && !tenant.HasPermission(claims, tenant.PermBranchOperational) {
		return nil, nil, apperr.PermissionDenied("You do not have permission to edit branch details.")
	}

	if patch.PickupHours != nil || patch.DeliveryHours != nil {
		var pickup, delivery []franchiseapi.DayHours
		if patch.PickupHours != nil {
			pickup = *patch.PickupHours
		}
		if patch.DeliveryHours != nil {
			delivery = *patch.DeliveryHours
		}
		if err := normalizeStoreHours(&pickup, &delivery); err != nil {
			return nil, nil, err
		}
		if patch.PickupHours != nil {
			patch.PickupHours = &pickup
		}
		if patch.DeliveryHours != nil {
			patch.DeliveryHours = &delivery
		}
	}
	if patch.CardProviders != nil {
		providers, err := franchiseapi.NormalizeCardProviders(*patch.CardProviders)
		if err != nil {
			return nil, nil, cardProvidersInvalid(err)
		}
		patch.CardProviders = &providers
	}

	if patch.DeliveryPricing != nil {
		zips := before.DeliveryZone.Zips
		if patch.DeliveryZone != nil {
			zips = patch.DeliveryZone.Zips
		}
		if err := patch.DeliveryPricing.Validate(zips); err != nil {
			return nil, nil, apperr.Validation(err.Error()).
				WithFields(apperr.FieldError{Field: "deliveryPricing", Code: "INVALID", Message: err.Error()})
		}
	}

	var warnings []string
	if patch.DeliveryZone != nil {
		warnings, err = s.overlapWarnings(ctx, id, *patch.DeliveryZone)
		if err != nil {
			return nil, nil, apperr.Internal(err)
		}
	}

	if patch.Type != nil && *patch.Type != franchiseapi.BranchTypeRetail && *patch.Type != franchiseapi.BranchTypeCentralKitchen {
		return nil, nil, apperr.Validation("An outlet is either retail or a central kitchen.").
			WithFields(apperr.FieldError{Field: "type", Code: "INVALID", Message: "retail or central_kitchen."})
	}
	if patch.OwnershipType != nil {
		ownership, ok := franchiseapi.NormalizeOwnershipType(*patch.OwnershipType)
		if !ok {
			return nil, nil, invalidOwnershipType()
		}
		patch.OwnershipType = &ownership
	}
	if patch.LocationCode != nil && strings.TrimSpace(*patch.LocationCode) != "" {
		if err := s.assertBusinessIDFree(ctx, strings.TrimSpace(*patch.LocationCode), id, ""); err != nil {
			return nil, nil, err
		}
	}
	after, err := s.store.UpdateBranch(ctx, id, patch)
	if err != nil {
		if isDuplicateKey(err) {
			// `after` is nil on error; name the code the caller asked for.
			code := before.Code
			if patch.Code != nil {
				code = *patch.Code
			}
			return nil, nil, apperr.New("BRANCH_CODE_TAKEN", apperr.CategoryConflict,
				"An outlet with the code \""+code+"\" or that Location ID already exists. Choose another.").
				WithFields(apperr.FieldError{Field: "code", Code: "TAKEN", Message: "Already in use."})
		}
		return nil, nil, notFoundOrInternal(err, "Branch not found.")
	}
	// Re-locate when the address moved, or the store has never been located.
	addressChanged := patch.Street != nil || patch.City != nil || patch.State != nil || patch.Postcode != nil
	s.locateBranch(ctx, after, addressChanged)
	s.writeAudit(ctx, claims, "branch.update", "branch", &id, &id, before, after, "")
	return after, warnings, nil
}

func (s *Service) SuspendBranch(ctx context.Context, claims *auth.StaffClaims, id, reason string) error {
	if !tenant.HasPermission(claims, tenant.PermBranchManage) {
		return apperr.PermissionDenied("You do not have permission to suspend a branch.")
	}
	before, err := s.store.GetBranch(ctx, id)
	if err != nil {
		return notFoundOrInternal(err, "Branch not found.")
	}
	if !tenant.CanAccessBranch(claims, "", before.ID) {
		return apperr.PermissionDenied("You do not have access to this branch.")
	}
	if err := s.store.SetBranchStatus(ctx, id, franchiseapi.StatusSuspended); err != nil {
		return apperr.Internal(err)
	}
	if err := s.store.DeactivateStaffUsersForBranch(ctx, id); err != nil {
		return apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "branch.suspend", "branch", &id, &id, before.Status, franchiseapi.StatusSuspended, reason)
	return nil
}

func (s *Service) ReactivateBranch(ctx context.Context, claims *auth.StaffClaims, id string) error {
	if !tenant.HasPermission(claims, tenant.PermBranchManage) {
		return apperr.PermissionDenied("You do not have permission to reactivate a branch.")
	}
	before, err := s.store.GetBranch(ctx, id)
	if err != nil {
		return notFoundOrInternal(err, "Branch not found.")
	}
	if !tenant.CanAccessBranch(claims, "", before.ID) {
		return apperr.PermissionDenied("You do not have access to this branch.")
	}
	if err := s.store.SetBranchStatus(ctx, id, franchiseapi.StatusActive); err != nil {
		return apperr.Internal(err)
	}
	// Reactivating a branch does not re-enable the people assigned to it.
	s.writeAudit(ctx, claims, "branch.reactivate", "branch", &id, &id, before.Status, franchiseapi.StatusActive, "")
	return nil
}

// EmergencyClose closes every remaining slot for the branch today
// (SCH-08). affectedOrders is always 0 until the ordering module exists —
// this is a known, documented gap, not a silent lie: there are no orders to
// affect yet.
func (s *Service) EmergencyClose(ctx context.Context, claims *auth.StaffClaims, id, reason string) (affectedOrders int, err error) {
	if !tenant.HasPermission(claims, tenant.PermBranchEmergencyClose) {
		return 0, apperr.PermissionDenied("You do not have permission to close this branch.")
	}
	if strings.TrimSpace(reason) == "" {
		return 0, apperr.Validation("A reason is required for an emergency close.")
	}
	b, err := s.store.GetBranch(ctx, id)
	if err != nil {
		return 0, notFoundOrInternal(err, "Branch not found.")
	}
	if !tenant.CanAccessBranch(claims, "", b.ID) {
		return 0, apperr.PermissionDenied("You do not have access to this branch.")
	}

	today := branchLocalToday(b.Timezone)
	if err := s.store.SetBranchEmergencyClosed(ctx, id, today); err != nil {
		return 0, apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "branch.emergency_close", "branch", &id, &id, false, true, reason)
	return 0, nil
}

func branchLocalToday(tz string) time.Time {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// --- Branch exceptions (SCH-07) ---------------------------------------------

func (s *Service) ListExceptions(ctx context.Context, claims *auth.StaffClaims, branchID string) ([]franchiseapi.BranchException, error) {
	b, err := s.store.GetBranch(ctx, branchID)
	if err != nil {
		return nil, notFoundOrInternal(err, "Branch not found.")
	}
	if !tenant.CanAccessBranch(claims, "", b.ID) {
		return nil, apperr.PermissionDenied("You do not have access to this branch.")
	}
	list, err := s.store.ListBranchExceptions(ctx, branchID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return list, nil
}

func (s *Service) AddException(ctx context.Context, claims *auth.StaffClaims, e franchiseapi.BranchException) (*franchiseapi.BranchException, error) {
	if !tenant.HasPermission(claims, tenant.PermBranchOperational) {
		return nil, apperr.PermissionDenied("You do not have permission to edit this branch's schedule.")
	}
	b, err := s.store.GetBranch(ctx, e.BranchID)
	if err != nil {
		return nil, notFoundOrInternal(err, "Branch not found.")
	}
	if !tenant.CanAccessBranch(claims, "", b.ID) {
		return nil, apperr.PermissionDenied("You do not have access to this branch.")
	}
	out, err := s.store.CreateBranchException(ctx, e)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "branch.exception.create", "branch_exception", &out.ID, &b.ID, nil, out, e.Reason)
	return out, nil
}

func (s *Service) DeleteException(ctx context.Context, claims *auth.StaffClaims, branchID, exceptionID string) error {
	if !tenant.HasPermission(claims, tenant.PermBranchOperational) {
		return apperr.PermissionDenied("You do not have permission to edit this branch's schedule.")
	}
	b, err := s.store.GetBranch(ctx, branchID)
	if err != nil {
		return notFoundOrInternal(err, "Branch not found.")
	}
	if !tenant.CanAccessBranch(claims, "", b.ID) {
		return apperr.PermissionDenied("You do not have access to this branch.")
	}
	if err := s.store.DeleteBranchException(ctx, branchID, exceptionID); err != nil {
		return notFoundOrInternal(err, "Exception not found.")
	}
	return nil
}

// --- Governance rules (GOV-02) -----------------------------------------------

func (s *Service) ListGovernanceRules(ctx context.Context) ([]franchiseapi.GovernanceRule, error) {
	rules, err := s.store.ListGovernanceRules(ctx)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return rules, nil
}

// --- Storefront: delivery eligibility & branch resolution (SF-01..SF-05, ORD-01) ---

type DeliveryCheckResult struct {
	Eligible bool
	// Branch is the recommended one — nearest, then by code. Kept as the
	// single answer for callers that only want one.
	Branch *franchiseapi.Branch
	// Branches is every outlet that delivers to this address, best first.
	//
	// Zones legitimately overlap: two shops can both cover a postcode, and a
	// customer with a preference between them was being shown only the one
	// the tie-break picked, with no way to know the other existed.
	Branches []franchiseapi.Branch
	// Ranked is Branches with each store's distance from the customer.
	Ranked             []franchiseapi.BranchWithDistance
	NearestBranches    []franchiseapi.BranchWithDistance
	EmailCapturePrompt bool
	// Quotes is each eligible branch's delivery price for an order of
	// unknown size, keyed by branch id.
	Quotes map[string]franchiseapi.DeliveryQuote
}

// CheckDelivery resolves a ZIP against every active branch's delivery zone.
// Exactly one match: eligible. Multiple matches (overlapping zones):
// documented tie-break — nearest by road distance (approximated as
// great-circle distance; a routing-distance provider is a later swap-in),
// then branch code (remaining-slot-capacity tie-break arrives with the
// ordering module). No match: the three nearest branches are returned so
// the customer is never shown a dead end (SF-03).
//
// timeSlot is the chosen slot's start, store-local; when given, stores that
// charge by delivery time quote that slot's fee. subtotalCents is the cart's
// item subtotal (0 when unknown), so free-over rules can apply.
func (s *Service) CheckDelivery(ctx context.Context, postcode, address, timeSlot string, subtotalCents int64) (*DeliveryCheckResult, error) {
	postcode = strings.TrimSpace(postcode)
	if postcode == "" {
		return nil, apperr.Validation("Enter a postcode.")
	}
	if subtotalCents < 0 {
		return nil, apperr.Validation("Subtotal can't be negative.").
			WithFields(apperr.FieldError{Field: "subtotalCents", Code: "INVALID", Message: "Can't be negative."})
	}
	timeSlot = strings.TrimSpace(timeSlot)
	if timeSlot != "" && !isClock(timeSlot) {
		return nil, apperr.Validation("Time slot must be HH:MM or HH:MM:SS.").
			WithFields(apperr.FieldError{Field: "timeSlot", Code: "INVALID", Message: "Use HH:MM or HH:MM:SS."})
	}

	branches, err := s.store.ListActiveBranchesForRouting(ctx)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	// The customer is located once — their address, else the ZIP's centre —
	// and every store is measured from that one point: radius stores to decide
	// whether they deliver, every store to order the list nearest first.
	custLat, custLng, custLocated := s.customerPoint(ctx, address, postcode)

	// Each store answers by its own pricing: its ZIP list, or its radius.
	var candidates []franchiseapi.Branch
	distances := map[string]*float64{}
	quotes := map[string]franchiseapi.DeliveryQuote{}
	for i := range branches {
		b := branches[i]
		if !hasFulfilmentType(b.FulfilmentTypes, "delivery") {
			continue
		}
		in := franchiseapi.DeliveryQuoteInput{Postcode: postcode, SlotStart: timeSlot, SubtotalCents: subtotalCents}
		miles := s.distanceFrom(ctx, &b, custLat, custLng, custLocated)
		if miles != nil && b.NeedsDistance() {
			km := franchiseapi.MilesToKm(*miles)
			in.DistanceKm = &km
		}
		if q := b.QuoteDelivery(in); q.Deliverable {
			candidates = append(candidates, b)
			distances[b.ID] = miles
			quotes[b.ID] = q
		}
	}

	if len(candidates) >= 1 {
		// Only the stores that deliver are sorted: nearest first, the first
		// being the recommendation.
		ranked := make([]franchiseapi.BranchWithDistance, 0, len(candidates))
		for _, b := range candidates {
			ranked = append(ranked, franchiseapi.BranchWithDistance{Branch: b, DistanceMiles: distances[b.ID]})
		}
		sortNearest(ranked)
		ordered := make([]franchiseapi.Branch, 0, len(ranked))
		for _, r := range ranked {
			ordered = append(ordered, r.Branch)
		}
		chosen := ordered[0]
		return &DeliveryCheckResult{Eligible: true, Branch: &chosen, Branches: ordered, Ranked: ranked, Quotes: quotes}, nil
	}

	// No store delivers here: the three nearest that offer pickup, so the
	// customer is never shown a dead end.
	var pickup []franchiseapi.Branch
	for _, b := range branches {
		if hasFulfilmentType(b.FulfilmentTypes, "pickup") {
			pickup = append(pickup, b)
		}
	}
	nearest := s.rankNearest(ctx, pickup, custLat, custLng, custLocated)
	if len(nearest) > 3 {
		nearest = nearest[:3]
	}
	return &DeliveryCheckResult{Eligible: false, NearestBranches: nearest, EmailCapturePrompt: true}, nil
}

// ListPickupBranches lists every store that offers pickup, nearest first from
// the customer's address (or ZIP). With neither, they come back by name with
// no distance.
// When branchType is empty or "all", all active branches are returned.
// When branchType is corporate_store (or "own"), only corporate stores are returned.
// When branchType is franchise_store (or "franchise"), only franchise stores are returned.
func (s *Service) ListPickupBranches(ctx context.Context, postcode, address, branchType string) ([]franchiseapi.BranchWithDistance, error) {
	branches, err := s.store.ListActiveBranchesForRouting(ctx)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	t := strings.ToLower(strings.TrimSpace(branchType))
	var want string
	if t != "" && t != "all" {
		w, ok := franchiseapi.NormalizeOwnershipType(t)
		if !ok {
			return nil, invalidOwnershipType()
		}
		want = w
	}
	pickup := make([]franchiseapi.Branch, 0, len(branches))
	for _, b := range branches {
		if want != "" && b.OwnershipType != want {
			continue
		}
		if hasFulfilmentType(b.FulfilmentTypes, "pickup") {
			pickup = append(pickup, b)
		}
	}

	lat, lng, located := s.customerPoint(ctx, address, postcode)
	return s.rankNearest(ctx, pickup, lat, lng, located), nil
}

func (s *Service) ZoneCoverageNotify(ctx context.Context, email, postcode string) error {
	if email == "" || postcode == "" {
		return apperr.Validation("Email and postcode are required.")
	}
	if err := s.store.CreateZoneCoverageRequest(ctx, email, postcode); err != nil {
		return apperr.Internal(err)
	}
	return nil
}

func hasFulfilmentType(types []string, want string) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}

// isClock reports whether s is "HH:MM" or "HH:MM:SS".
func isClock(s string) bool {
	for _, layout := range []string{"15:04:05", "15:04"} {
		if _, err := time.Parse(layout, s); err == nil {
			return true
		}
	}
	return false
}

func zipInZone(zone franchiseapi.DeliveryZone, postcode string) bool {
	if zone.Type != "zip_list" {
		return false
	}
	for _, z := range zone.Zips {
		if z == postcode {
			return true
		}
	}
	return false
}

func notFoundOrInternal(err error, msg string) error {
	if errors.Is(err, store.ErrNotFound) {
		return apperr.NotFound(msg)
	}
	return apperr.Internal(err)
}

func (s *Service) writeAudit(ctx context.Context, claims *auth.StaffClaims, action, resourceType string, resourceID, branchID *string, before, after interface{}, reason string) {
	if s.audit == nil || claims == nil {
		return
	}
	_ = s.audit.Write(ctx, audit.Entry{
		ActorID: &claims.StaffID, ActorType: "staff", Action: action,
		ResourceType: resourceType, ResourceID: resourceID,
		BranchID:    branchID,
		BeforeValue: before, AfterValue: after, Reason: reason,
	})
}

// isDuplicateKey asks the store whether an error was a unique clash.
func isDuplicateKey(err error) bool { return store.IsDuplicateKey(err) }

// --- Branch Requests --------------------------------------------------------

// CreateBranchRequest creates a new branch request on behalf of the caller.
//
// The caller must hold branch_request.manage (super_admin, franchise_owner, or
// branch_manager). Scope is also enforced: a branch_manager must own the target
// branch; a franchise_owner must own its franchise; a super_admin may target
// any branch.
func (s *Service) CreateBranchRequest(ctx context.Context, claims *auth.StaffClaims, req franchiseapi.BranchRequest) (*franchiseapi.BranchRequest, error) {
	if !tenant.HasPermission(claims, tenant.PermBranchRequestManage) {
		return nil, apperr.PermissionDenied("You do not have permission to create branch requests.")
	}
	if strings.TrimSpace(req.BranchID) == "" {
		return nil, apperr.Validation("branchId is required.")
	}

	_, err := s.store.GetBranch(ctx, req.BranchID)
	if err != nil {
		return nil, notFoundOrInternal(err, "Branch not found.")
	}
	if !tenant.CanAccessBranch(claims, "", req.BranchID) {
		return nil, apperr.PermissionDenied("You do not have access to this branch.")
	}
	if !isValidRequestType(req.Type) {
		return nil, apperr.Validation("Request type must be one of: product, category, modifier, other.")
	}

	// Capture the caller's role for the record.
	req.RequestedBy = claims.StaffID
	req.Role = callerRole(claims)

	out, err := s.store.CreateBranchRequest(ctx, req)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "branch_request.create", "branch_request", &out.ID, &out.BranchID, nil, out, "")
	return out, nil
}

// GetBranchRequest returns a single branch request, enforcing scope.
func (s *Service) GetBranchRequest(ctx context.Context, claims *auth.StaffClaims, id string) (*franchiseapi.BranchRequest, error) {
	if !tenant.HasPermission(claims, tenant.PermBranchRequestManage) {
		return nil, apperr.PermissionDenied("You do not have permission to view branch requests.")
	}
	req, err := s.store.GetBranchRequest(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Branch request not found.")
	}
	if !tenant.CanAccessBranch(claims, "", req.BranchID) {
		return nil, apperr.PermissionDenied("You do not have access to this branch request.")
	}
	return req, nil
}

// ListBranchRequests returns requests visible to the caller.
//
// Super admins see everything. Franchise owners see their franchise.
// Branch managers see their own branch. The filter is applied on top of scope.
func (s *Service) ListBranchRequests(ctx context.Context, claims *auth.StaffClaims, f franchiseapi.BranchRequestFilter) ([]franchiseapi.BranchRequest, int, franchiseapi.BranchRequestStats, error) {
	if !tenant.HasPermission(claims, tenant.PermBranchRequestManage) {
		return nil, 0, franchiseapi.BranchRequestStats{}, apperr.PermissionDenied("You do not have permission to list branch requests.")
	}

	if f.Type != "" && !isValidRequestType(f.Type) {
		return nil, 0, franchiseapi.BranchRequestStats{}, apperr.Validation("Invalid request type. Must be one of: product, modifier, category, other.")
	}

	// Scope-restrict the filter for non-brand-level callers.
	if !tenant.IsBrandScoped(claims) {
		// An explicit filter may narrow the caller's view, never widen it:
		// a branchId or franchiseId outside their scope is refused rather
		// than returning another outlet's requests.
		if f.BranchID != "" {
			b, err := s.store.GetBranch(ctx, f.BranchID)
			if err != nil {
				return nil, 0, franchiseapi.BranchRequestStats{}, notFoundOrInternal(err, "Branch not found.")
			}
			if !tenant.CanAccessBranch(claims, "", b.ID) {
				return nil, 0, franchiseapi.BranchRequestStats{}, apperr.PermissionDenied("You do not have access to this branch.")
			}
		}
		_, branchID := tenant.OwnScope(claims)
		// Branch-level: restrict to the branch.
		if branchID != "" && f.BranchID == "" {
			f.BranchID = branchID
		}
	}

	items, total, stats, err := s.store.ListBranchRequests(ctx, f)
	if err != nil {
		return nil, 0, franchiseapi.BranchRequestStats{}, apperr.Internal(err)
	}
	return items, total, stats, nil
}

// ApproveRequest approves a pending branch request. Only super admins may call this.
func (s *Service) ApproveRequest(ctx context.Context, claims *auth.StaffClaims, id string) (*franchiseapi.BranchRequest, error) {
	if !tenant.HasPermission(claims, tenant.PermBranchRequestApprove) {
		return nil, apperr.PermissionDenied("Only super admin can approve branch requests.")
	}
	existing, err := s.store.GetBranchRequest(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Branch request not found.")
	}
	if existing.Status != franchiseapi.RequestStatusPending {
		return nil, apperr.BusinessRule("BRANCH_REQUEST_INVALID_STATUS",
			fmt.Sprintf("Cannot approve a request with status %q — only pending requests can be approved.", existing.Status))
	}
	out, err := s.store.ApproveRequest(ctx, id, claims.StaffID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "branch_request.approve", "branch_request", &out.ID, &out.BranchID, existing, out, "")
	return out, nil
}

// RejectRequest rejects a pending branch request. Only super admins may call this.
func (s *Service) RejectRequest(ctx context.Context, claims *auth.StaffClaims, id, note string) (*franchiseapi.BranchRequest, error) {
	if !tenant.HasPermission(claims, tenant.PermBranchRequestApprove) {
		return nil, apperr.PermissionDenied("Only super admin can reject branch requests.")
	}
	existing, err := s.store.GetBranchRequest(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Branch request not found.")
	}
	if existing.Status != franchiseapi.RequestStatusPending {
		return nil, apperr.BusinessRule("BRANCH_REQUEST_INVALID_STATUS",
			fmt.Sprintf("Cannot reject a request with status %q — only pending requests can be rejected.", existing.Status))
	}
	out, err := s.store.RejectRequest(ctx, id, claims.StaffID, note)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "branch_request.reject", "branch_request", &out.ID, &out.BranchID, existing, out, note)
	return out, nil
}

// CancelRequest cancels a pending or approved request.
//
// The requester themselves, a franchise owner over the branch, or a super admin
// may cancel. Rejected or already-completed requests cannot be cancelled.
func (s *Service) CancelRequest(ctx context.Context, claims *auth.StaffClaims, id string) (*franchiseapi.BranchRequest, error) {
	if !tenant.HasPermission(claims, tenant.PermBranchRequestManage) {
		return nil, apperr.PermissionDenied("You do not have permission to cancel branch requests.")
	}
	existing, err := s.store.GetBranchRequest(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Branch request not found.")
	}
	// Scope check — non-brand callers must own the branch.
	if !tenant.CanAccessBranch(claims, "", existing.BranchID) {
		return nil, apperr.PermissionDenied("You do not have access to this branch request.")
	}
	if existing.Status != franchiseapi.RequestStatusPending && existing.Status != franchiseapi.RequestStatusApproved {
		return nil, apperr.BusinessRule("BRANCH_REQUEST_INVALID_STATUS",
			fmt.Sprintf("Cannot cancel a request with status %q.", existing.Status))
	}
	out, err := s.store.CancelRequest(ctx, id)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "branch_request.cancel", "branch_request", &out.ID, &out.BranchID, existing, out, "")
	return out, nil
}

// CompleteRequest marks an approved request as completed.
//
// Super admins and franchise owners (over the branch's franchise) may complete.
func (s *Service) CompleteRequest(ctx context.Context, claims *auth.StaffClaims, id string) (*franchiseapi.BranchRequest, error) {
	if !tenant.HasPermission(claims, tenant.PermBranchRequestManage) {
		return nil, apperr.PermissionDenied("You do not have permission to complete branch requests.")
	}
	existing, err := s.store.GetBranchRequest(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Branch request not found.")
	}
	if !tenant.CanAccessBranch(claims, "", existing.BranchID) {
		return nil, apperr.PermissionDenied("You do not have access to this branch request.")
	}
	if existing.Status != franchiseapi.RequestStatusApproved {
		return nil, apperr.BusinessRule("BRANCH_REQUEST_INVALID_STATUS",
			fmt.Sprintf("Cannot complete a request with status %q — only approved requests can be completed.", existing.Status))
	}
	out, err := s.store.CompleteRequest(ctx, id, claims.StaffID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "branch_request.complete", "branch_request", &out.ID, &out.BranchID, existing, out, "")
	return out, nil
}

// callerRole returns the first role found in the caller's assignments,
// or "unknown" when the token carries no assignments (should never happen
// after proper auth middleware, but the function must be safe regardless).
func callerRole(claims *auth.StaffClaims) string {
	if claims == nil || len(claims.RoleAssignments) == 0 {
		return "unknown"
	}
	return claims.RoleAssignments[0].Role
}

// isValidRequestType reports whether t is one of the four permitted request types.
func isValidRequestType(t string) bool {
	switch t {
	case franchiseapi.RequestTypeProduct,
		franchiseapi.RequestTypeCategory,
		franchiseapi.RequestTypeModifier,
		franchiseapi.RequestTypeOther:
		return true
	}
	return false
}

func invalidOwnershipType() error {
	return apperr.Validation("ownershipType must be corporate_store or franchise_store.").
		WithFields(apperr.FieldError{Field: "ownershipType", Code: "INVALID", Message: "corporate_store or franchise_store."})
}

// locateBranch makes sure b has coordinates, geocoding its address when it
// has none (or when force, because the address changed) and saving them.
// Best effort: a store that can't be located simply can't be quoted for
// radius delivery. Reports whether b has coordinates afterwards.
func (s *Service) locateBranch(ctx context.Context, b *franchiseapi.Branch, force bool) bool {
	if b == nil {
		return false
	}
	if !force && b.Latitude != nil && b.Longitude != nil {
		return true
	}
	lat, lng, ok := geo.LocateAddress(ctx, s.addrGeo, b.Street, b.City, b.State, b.Postcode)
	if !ok {
		return b.Latitude != nil && b.Longitude != nil
	}
	if err := s.store.SetBranchCoordinates(ctx, b.ID, lat, lng); err != nil {
		slog.Warn("could not save branch coordinates", "branch", b.ID, "error", err)
	}
	b.Latitude, b.Longitude = &lat, &lng
	return true
}
