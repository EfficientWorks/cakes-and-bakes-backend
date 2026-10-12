package domain

import (
	"context"
	"errors"
	"log/slog"

	"bakehouse/pkg/auth"
	"bakehouse/services/orders/ordersapi"
)

/*
 * A guest's cart follows them when they sign in.
 *
 * A shopper fills a basket as a guest (the cart is found by the X-Cart-Token
 * header), then signs in (the cart is found by their customer id). The first
 * cart request that carries both moves the guest cart onto the account:
 *
 *   - no cart on the account yet, or an empty one: the guest cart simply
 *     becomes the customer's;
 *   - otherwise the guest lines are added to the customer's cart (the same
 *     product with the same choices raises that line's quantity, as adding it
 *     again would), the guest's fulfilment choices win as the most recent,
 *     and the guest cart is removed.
 *
 * The guest cart is claimed in one statement first, so two requests racing
 * after sign-in cannot both merge it. Best effort: a failure is logged and the
 * customer keeps the cart they had.
 */
func (s *Service) mergeGuestCart(ctx context.Context, customerID, guestToken string) {
	existing, err := s.store.GetCartByCustomer(ctx, customerID)
	if err != nil && !errors.Is(err, ordersapi.ErrNotFound) {
		slog.Warn("cart merge: could not read the customer's cart", "error", err)
		return
	}
	guestID, err := s.store.ClaimGuestCart(ctx, auth.HashOpaqueToken(guestToken), customerID)
	if errors.Is(err, ordersapi.ErrNotFound) {
		return // nothing to merge, or already merged
	}
	if err != nil {
		slog.Warn("cart merge: could not claim the guest cart", "error", err)
		return
	}
	// The claimed cart now belongs to the customer. With nothing of theirs to
	// merge into, that is the whole job.
	if existing == nil || existing.ID == guestID || len(existing.Items) == 0 {
		if existing != nil && existing.ID != guestID {
			_ = s.store.DeleteCart(ctx, existing.ID)
		}
		return
	}

	guest, err := s.store.GetCartByID(ctx, guestID)
	if err != nil {
		slog.Warn("cart merge: could not read the guest cart", "cart", guestID, "error", err)
		return
	}
	if len(guest.Items) == 0 {
		_ = s.store.DeleteCart(ctx, guest.ID)
		return
	}

	// The guest's lines into the customer's cart.
	lines := append([]ordersapi.CartItem(nil), existing.Items...)
	for _, it := range guest.Items {
		if same := findMergeable(lines, it); same != nil {
			if err := s.store.UpdateCartItem(ctx, existing.ID, same.ID, same.Quantity+it.Quantity,
				same.SelectedFlavours, same.SelectedAddons, same.SelectedPresentation, same.SelectedWrapColour,
				&same.IsGift, same.RecipientFirstName, same.RecipientLastName, same.GiftMessage); err != nil {
				slog.Warn("cart merge: could not raise a line", "error", err)
				continue
			}
			same.Quantity += it.Quantity
			continue
		}
		moved := it
		moved.ID, moved.CartID = "", existing.ID
		if added, err := s.store.AddCartItem(ctx, moved); err != nil {
			slog.Warn("cart merge: could not move a line", "error", err)
		} else if added != nil {
			lines = append(lines, *added)
		}
	}

	// Gone before the choices are copied: the claimed cart is the customer's
	// newest, so while it exists it is the one their identity resolves to.
	_ = s.store.DeleteCart(ctx, guest.ID)

	// The guest's choices are the latest: store, pickup or delivery, address
	// and slot, and a code they applied. SetFulfilment re-prices every line
	// for that store.
	id := CartIdentity{CustomerID: &customerID}
	if guest.BranchID != "" && guest.FulfilmentType != "" {
		branchID := guest.BranchID
		if _, _, err := s.SetFulfilment(ctx, id, guest.FulfilmentType, guest.DeliveryStreet, guest.DeliveryCity,
			guest.DeliveryState, guest.DeliveryPostcode, &branchID); err != nil {
			slog.Warn("cart merge: could not carry the fulfilment over", "error", err)
		}
	}
	if guest.ScheduledDate != nil && guest.SlotStart != nil && guest.SlotEnd != nil {
		_ = s.store.SetCartSchedule(ctx, existing.ID, *guest.ScheduledDate, *guest.SlotStart, *guest.SlotEnd)
	}
	if guest.DiscountCode != nil && *guest.DiscountCode != "" {
		_ = s.store.SetCartDiscount(ctx, existing.ID, guest.DiscountCode)
	}
	if guest.TipCents > 0 && existing.TipCents == 0 {
		_ = s.store.SetCartTip(ctx, existing.ID, guest.TipCents)
	}
}
