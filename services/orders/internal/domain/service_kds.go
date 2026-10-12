package domain

import (
	"context"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/realtime"
	"bakehouse/pkg/tenant"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/orders/ordersapi"
)

/*
 * The Kitchen Display.
 *
 * A KDS works for one location: a store (its own orders) or a central kitchen
 * (every store assigned to it). The location comes from the signed-in staff
 * token — a central-kitchen staff member's token names their kitchen, a
 * store-scoped one names the store — so a KDS rarely has to say anything.
 * Brand-wide staff (a super admin setting a tablet up) name it explicitly.
 */

// UseKDS wires the franchise lookups the KDS needs (kitchen → stores, and
// each location's settings). Nil leaves every KDS endpoint answering 503.
func (s *Service) UseKDS(r franchiseapi.KDSReader) { s.kds = r }

// UseRealtime sets the live-update hub (defaults to realtime.Default).
func (s *Service) UseRealtime(h *realtime.Hub) { s.hub = h }

// KDSScopeQuery is how a request names its location.
type KDSScopeQuery struct {
	BranchID         string
	CentralKitchenID string
	// FilterBranchID narrows a central kitchen's view to one of its stores
	// (the KDS "Choose Store" picker).
	FilterBranchID string
}

// KDSScope is the resolved location and the stores it covers.
type KDSScope struct {
	Type             string
	BranchID         string
	CentralKitchenID string
	Name             string
	Timezone         string
	// Every store the location covers.
	AllBranchIDs []string
	// The stores this request reads (AllBranchIDs, or the picked one).
	BranchIDs []string
	Settings  *franchiseapi.KDSSettings
}

func errKDSUnavailable() error {
	return apperr.New("KDS_UNAVAILABLE", apperr.CategoryUpstream, "The Kitchen Display is not configured on this server.")
}

// ResolveKDSScope turns a request into a location the caller may use.
func (s *Service) ResolveKDSScope(ctx context.Context, claims *auth.StaffClaims, q KDSScopeQuery) (*KDSScope, error) {
	if s.kds == nil {
		return nil, errKDSUnavailable()
	}
	if !tenant.HasPermission(claims, tenant.PermKitchenViewOrders) && !tenant.HasPermission(claims, tenant.PermKDSAccess) {
		return nil, apperr.PermissionDenied("You do not have access to the Kitchen Display.")
	}
	branchID, kitchenID := strings.TrimSpace(q.BranchID), strings.TrimSpace(q.CentralKitchenID)
	if branchID != "" && kitchenID != "" {
		return nil, apperr.Validation("Send either branchId or centralKitchenId, not both.")
	}
	if branchID == "" && kitchenID == "" {
		if ks := tenant.CentralKitchenScopes(claims); len(ks) == 1 {
			kitchenID = ks[0]
		} else if len(ks) == 0 {
			branchID = soleBranch(claims)
		}
	}

	scope := &KDSScope{}
	switch {
	case kitchenID != "":
		if !tenant.IsBrandScoped(claims) && !containsString(tenant.CentralKitchenScopes(claims), kitchenID) {
			return nil, apperr.PermissionDenied("You do not have access to this corporate office.")
		}
		ck, err := s.kds.GetCentralKitchen(ctx, kitchenID)
		if err != nil {
			return nil, apperr.NotFound("Corporate office not found.")
		}
		scope.Type, scope.CentralKitchenID, scope.Name, scope.Timezone =
			franchiseapi.LocationCentralKitchen, ck.ID, ck.Name, ck.Timezone
		scope.AllBranchIDs = ck.BranchIDs
	case branchID != "":
		if !tenant.CanAccessBranch(claims, branchID) {
			return nil, apperr.PermissionDenied("You do not have access to this store.")
		}
		b, err := s.branches.GetBranch(ctx, branchID)
		if err != nil {
			return nil, apperr.NotFound("Store not found.")
		}
		scope.Type, scope.BranchID, scope.Name, scope.Timezone = franchiseapi.LocationBranch, b.ID, b.Name, b.Timezone
		scope.AllBranchIDs = []string{b.ID}
	default:
		return nil, apperr.Validation("Say which location: branchId or centralKitchenId.").
			WithFields(apperr.FieldError{Field: "branchId", Code: "REQUIRED", Message: "Required."})
	}

	scope.BranchIDs = scope.AllBranchIDs
	if f := strings.TrimSpace(q.FilterBranchID); f != "" && scope.Type == franchiseapi.LocationCentralKitchen {
		if !containsString(scope.AllBranchIDs, f) {
			return nil, apperr.Validation("That store is not assigned to this corporate office.").
				WithFields(apperr.FieldError{Field: "storeId", Code: "NOT_ASSIGNED", Message: "Not assigned."})
		}
		scope.BranchIDs = []string{f}
	}
	if scope.BranchIDs == nil {
		scope.BranchIDs = []string{}
	}

	settings, err := s.kds.KDSSettingsFor(ctx, franchiseapi.KDSScope{
		Type: scope.Type, BranchID: scope.BranchID, CentralKitchenID: scope.CentralKitchenID})
	if err != nil {
		return nil, apperr.Internal(err)
	}
	scope.Settings = settings
	return scope, nil
}

// kdsMayActOnBranch reports whether a KDS user may change an order of this
// store: store access as usual, or via a central kitchen that serves it.
func (s *Service) kdsMayActOnBranch(ctx context.Context, claims *auth.StaffClaims, branchID string) bool {
	if tenant.CanAccessBranch(claims, branchID) {
		return true
	}
	if s.kds == nil {
		return false
	}
	for _, ck := range tenant.CentralKitchenScopes(claims) {
		ids, err := s.kds.CentralKitchenBranchIDs(ctx, ck)
		if err == nil && containsString(ids, branchID) {
			return true
		}
	}
	return false
}

func soleBranch(claims *auth.StaffClaims) string {
	found := ""
	for _, ra := range claims.RoleAssignments {
		if ra.ScopeType != tenant.ScopeBranch || ra.BranchID == nil || *ra.BranchID == "" {
			continue
		}
		if found != "" && found != *ra.BranchID {
			return ""
		}
		found = *ra.BranchID
	}
	return found
}

func containsString(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func (sc *KDSScope) location() *time.Location {
	if loc, err := time.LoadLocation(sc.Timezone); err == nil && sc.Timezone != "" {
		return loc
	}
	return time.UTC
}

// ================================================================== live

// publishOrder tells live screens an order changed. Best effort.
func (s *Service) publishOrder(eventType string, o *ordersapi.Order, itemID string) {
	if o == nil {
		return
	}
	h := s.hub
	if h == nil {
		h = realtime.Default
	}
	e := realtime.Event{
		Type: eventType, OrderID: o.ID, OrderItemID: itemID, BranchID: o.BranchID,
		Reference: o.Reference, Status: o.Status, KOTStatus: o.KOTStatus,
	}
	if o.CustomerID != nil {
		e.CustomerID = *o.CustomerID
	}
	// What a till needs to announce the order without fetching it.
	e.Data = map[string]any{
		"fulfilmentType": o.FulfilmentType, "channel": o.Channel,
		"customerName": orderCustomerName(o), "totalCents": o.TotalCents,
	}
	if o.BillNo != nil {
		e.Data["billNo"] = *o.BillNo
	}
	h.Publish(e)
}

// orderCustomerName is who the order is for, as a till shows it.
func orderCustomerName(o *ordersapi.Order) string {
	if n := strings.TrimSpace(o.PurchaserFirstName + " " + o.PurchaserLastName); n != "" {
		return n
	}
	if o.GuestCustomerName != nil && strings.TrimSpace(*o.GuestCustomerName) != "" {
		return strings.TrimSpace(*o.GuestCustomerName)
	}
	return strings.TrimSpace(o.RecipientFirstName + " " + o.RecipientLastName)
}

// publishOrderByID loads and publishes; used by the existing POS/admin paths.
func (s *Service) publishOrderByID(ctx context.Context, eventType, orderID string) {
	if o, err := s.store.GetOrderByID(ctx, orderID); err == nil {
		s.publishOrder(eventType, o, "")
	}
}

// SubscribeKDS opens a live feed for the stores a KDS scope covers.
func (s *Service) SubscribeKDS(scope *KDSScope) (<-chan realtime.Event, func()) {
	h := s.hub
	if h == nil {
		h = realtime.Default
	}
	ids := scope.BranchIDs
	if len(ids) == 0 {
		// A kitchen with no stores listens to nothing rather than everything.
		ids = []string{"__none__"}
	}
	return h.Subscribe(ids)
}
