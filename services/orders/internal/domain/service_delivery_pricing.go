package domain

import (
	"context"
	"log/slog"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/geo"
	"bakehouse/pkg/tenant"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/orders/ordersapi"
)

/*
 * Delivery pricing at the till, the cart and checkout.
 *
 * The rules live with the store (franchiseapi.Branch.QuoteDelivery); this
 * file supplies what they need: the address's ZIP, how far it is from the
 * store (radius stores only), the item subtotal and the slot.
 *
 * Distance is straight-line between two geocoded points. The customer's
 * point is located once when the address is set and kept on the cart; the
 * store's comes from its saved coordinates, or is located here (and cached
 * by the geocoder) for a store saved before coordinates were kept.
 */

// UseAddressGeocoder wires street-address geocoding for radius delivery.
func (s *Service) UseAddressGeocoder(g geo.AddressGeocoder) { s.addrGeo = g }

// branchPoint returns where the store is.
func (s *Service) branchPoint(ctx context.Context, b *franchiseapi.Branch) (float64, float64, bool) {
	if b.Latitude != nil && b.Longitude != nil {
		return *b.Latitude, *b.Longitude, true
	}
	return geo.LocateAddress(ctx, s.addrGeo, b.Street, b.City, b.State, b.Postcode)
}

// quoteDelivery prices a delivery from b to an address. lat/lng are the
// address's point when already known; otherwise it is located from the
// address when the store needs a distance.
func (s *Service) quoteDelivery(ctx context.Context, b *franchiseapi.Branch, street, city, state, postcode string,
	lat, lng *float64, subtotalCents int64, slotStart string) franchiseapi.DeliveryQuote {
	in := franchiseapi.DeliveryQuoteInput{Postcode: postcode, SubtotalCents: subtotalCents, SlotStart: slotStart}
	if b.NeedsDistance() {
		if lat == nil || lng == nil {
			if la, ln, ok := geo.LocateAddress(ctx, s.addrGeo, street, city, state, postcode); ok {
				lat, lng = &la, &ln
			}
		}
		if lat != nil && lng != nil {
			if bl, bg, ok := s.branchPoint(ctx, b); ok {
				d := geo.DistanceKm(bl, bg, *lat, *lng)
				in.DistanceKm = &d
			}
		}
	}
	return b.QuoteDelivery(in)
}

// quoteCartDelivery prices the cart's delivery. Never call it for pickup.
func (s *Service) quoteCartDelivery(ctx context.Context, b *franchiseapi.Branch, cart *ordersapi.Cart, subtotalCents int64) franchiseapi.DeliveryQuote {
	return s.quoteDelivery(ctx, b, derefStr(cart.DeliveryStreet), derefStr(cart.DeliveryCity), derefStr(cart.DeliveryState),
		derefStr(cart.DeliveryPostcode), cart.DeliveryLatitude, cart.DeliveryLongitude, subtotalCents, derefStr(cart.SlotStart))
}

// locateCartAddress stores the cart's delivery point when its store prices by
// radius. Best effort: an address that can't be located simply quotes as
// undeliverable, with a message saying so.
func (s *Service) locateCartAddress(ctx context.Context, cartID string, b *franchiseapi.Branch, street, city, state, postcode *string) {
	if !b.NeedsDistance() {
		return
	}
	lat, lng, ok := geo.LocateAddress(ctx, s.addrGeo, derefStr(street), derefStr(city), derefStr(state), derefStr(postcode))
	if !ok {
		return
	}
	if err := s.store.SetCartDeliveryPoint(ctx, cartID, &lat, &lng); err != nil {
		slog.Warn("could not save cart delivery point", "cart", cartID, "error", err)
	}
}

// errDeliveryUnavailable is checkout's refusal for an address the store
// doesn't deliver to; the reason is the quote's own sentence.
func errDeliveryUnavailable(reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "This store doesn't deliver to that address."
	}
	return apperr.New("DELIVERY_UNAVAILABLE", apperr.CategoryBusinessRule, reason).
		WithFields(apperr.FieldError{Field: "deliveryAddress", Code: "OUT_OF_AREA", Message: reason})
}

// --- POS --------------------------------------------------------------------

// DeliveryQuoteRequest is the till asking what delivery to an address costs.
type DeliveryQuoteRequest struct {
	BranchID      string
	Street        string
	City          string
	State         string
	Postcode      string
	SubtotalCents int64
	SlotStart     string
}

// QuoteDeliveryPOS prices a delivery for the till. It never refuses an
// out-of-area address: the till shows the reason and staff decide, typing
// their own fee if they take the order anyway.
func (s *Service) QuoteDeliveryPOS(ctx context.Context, claims *auth.StaffClaims, req DeliveryQuoteRequest) (*franchiseapi.DeliveryQuote, error) {
	if strings.TrimSpace(req.BranchID) == "" {
		return nil, apperr.Validation("An outlet is required.").
			WithFields(apperr.FieldError{Field: "branchId", Code: "REQUIRED", Message: "Required."})
	}
	b, err := s.branches.GetBranch(ctx, req.BranchID)
	if err != nil {
		return nil, apperr.NotFound("Outlet not found.")
	}
	if !tenant.CanAccessBranch(claims, "", b.ID) {
		return nil, apperr.PermissionDenied("You do not have access to this outlet.")
	}
	q := s.quoteDelivery(ctx, b, req.Street, req.City, firstNonEmpty(req.State, b.State), req.Postcode,
		nil, nil, req.SubtotalCents, req.SlotStart)
	return &q, nil
}

// posDeliveryFee is the fee a till order gets when the till didn't send one:
// the store's quote for the order's address, or 0 when the address is out of
// area or incomplete (the till warned about that through QuoteDeliveryPOS).
func (s *Service) posDeliveryFee(ctx context.Context, b *franchiseapi.Branch, o *ordersapi.Order, subtotalCents int64) int64 {
	street, city, state, postcode := derefStr(o.DeliveryStreet), derefStr(o.DeliveryCity), derefStr(o.DeliveryState), derefStr(o.DeliveryPostcode)
	if street == "" && postcode == "" {
		street, city, postcode = derefStr(o.GuestCustomerAddress), derefStr(o.GuestCustomerCity), derefStr(o.GuestCustomerPostcode)
	}
	q := s.quoteDelivery(ctx, b, street, city, firstNonEmpty(state, b.State), postcode, nil, nil, subtotalCents, o.SlotStart)
	if !q.Deliverable {
		return 0
	}
	return q.FeeCents
}
