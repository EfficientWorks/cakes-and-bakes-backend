package domain

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/orders/ordersapi"
)

// CartIdentity is either a logged-in customer id or a guest's opaque token
// (never both — resolved once at the handler layer from the bearer token /
// X-Cart-Token header).
type CartIdentity struct {
	CustomerID *string
	GuestToken string // raw token, hashed before any DB lookup
}

func (s *Service) resolveCart(ctx context.Context, id CartIdentity) (*ordersapi.Cart, error) {
	if id.CustomerID != nil {
		// Signed in but still holding a guest cart: it joins the account
		// (see mergeGuestCart). A no-op once merged.
		if id.GuestToken != "" {
			s.mergeGuestCart(ctx, *id.CustomerID, id.GuestToken)
		}
		return s.store.GetCartByCustomer(ctx, *id.CustomerID)
	}
	if id.GuestToken != "" {
		return s.store.GetCartByGuestTokenHash(ctx, auth.HashOpaqueToken(id.GuestToken))
	}
	return nil, ordersapi.ErrNotFound
}

// GetCart returns the caller's cart with a live price summary, or an empty
// "no cart yet" result — never an error, since an empty cart is a normal
// state, not a failure.
func (s *Service) GetCart(ctx context.Context, id CartIdentity) (*ordersapi.Cart, *ordersapi.PriceSummary, error) {
	cart, err := s.resolveCart(ctx, id)
	if errors.Is(err, ordersapi.ErrNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	summary, err := s.PriceCart(ctx, cart)
	if err != nil {
		return nil, nil, err
	}
	return cart, summary, nil
}

type AddItemRequest struct {
	BranchID       string
	ProductID      string
	VariantID      *string
	FlavourIDs     []string
	AddonIDs       []string
	PresentationID *string
	WrapColourID   *string
	Quantity       int
	IsGift         bool
	RecipientFirst *string
	RecipientLast  *string
	GiftMessage    *string
}

// AddItem prices and validates the request server-side (never trusting a
// client-supplied price), auto-creates the cart if this is the first item,
// and returns a fresh guestToken when one was newly minted (empty
// otherwise) so the handler knows whether to hand it back to the client.
func (s *Service) AddItem(ctx context.Context, id CartIdentity, req AddItemRequest) (*ordersapi.Cart, *ordersapi.PriceSummary, string, error) {
	if req.Quantity <= 0 {
		req.Quantity = 1
	}
	if s.branches != nil && req.BranchID != "" {
		if _, err := s.branchOpenByID(ctx, req.BranchID); err != nil {
			return nil, nil, "", err
		}
	}

	cart, err := s.resolveCart(ctx, id)
	newGuestToken := ""
	if errors.Is(err, ordersapi.ErrNotFound) || cart == nil {
		if req.BranchID == "" {
			return nil, nil, "", apperr.Validation("branchId is required to start a new cart.")
		}
		var guestHash *string
		if id.CustomerID == nil {
			// Use the token the caller already presented, if any (e.g. a
			// previously-issued X-Cart-Token whose cart expired, or the
			// synthetic per-session token CreateManualOrder reuses across
			// several AddItem/SetFulfilment/SetSchedule/CreateOrder calls).
			// Only mint a brand-new one when the caller truly has none yet
			// — otherwise the cart we create here would be unreachable by
			// the identity the caller is about to keep using.
			tok := id.GuestToken
			if tok == "" {
				var herr error
				tok, herr = auth.NewOpaqueToken()
				if herr != nil {
					return nil, nil, "", apperr.Internal(herr)
				}
				newGuestToken = tok
			}
			h := auth.HashOpaqueToken(tok)
			guestHash = &h
		}
		cart, err = s.store.CreateCart(ctx, req.BranchID, id.CustomerID, guestHash)
		if err != nil {
			return nil, nil, "", apperr.Internal(err)
		}
	} else if err != nil {
		return nil, nil, "", apperr.Internal(err)
	}

	/*
	 * A cart belongs to one outlet (SF-19), and choosing a different one moves
	 * the whole cart rather than refusing.
	 *
	 * This used to be a hard conflict: a customer who picked Edison, filled a
	 * basket, then decided to collect from Trappe was told to empty it and
	 * start over. The rule the conflict protected — one order, one branch — is
	 * kept by moving every line together, and each line is re-priced against
	 * the new outlet below and again at checkout, so nothing is sold at the
	 * old branch's prices.
	 *
	 * A line the new outlet does not carry is caught by the availability check
	 * that follows, which is where that belongs.
	 */
	if req.BranchID != "" && req.BranchID != cart.BranchID {
		if err := s.store.SetCartBranch(ctx, cart.ID, req.BranchID); err != nil {
			return nil, nil, "", apperr.Internal(err)
		}
		cart.BranchID = req.BranchID
	}

	priced, err := s.catalog.GetPricedItem(ctx, cart.BranchID, req.ProductID, req.VariantID, req.FlavourIDs, req.AddonIDs, req.PresentationID, req.WrapColourID, time.Now())
	if err != nil {
		return nil, nil, "", err
	}
	if !priced.Available {
		return nil, nil, "", apperr.New("PRODUCT_UNAVAILABLE", apperr.CategoryBusinessRule, "This product is currently unavailable: "+priced.AvailableReason)
	}

	// PricedItem.UnitPriceCents is base+variant+every modifier delta summed
	// together; ordersapi.CartItem.UnitPriceCents stores just the branch-rule
	// base price plus any variant delta, because ordersapi.CartItem.UnitTotalCents()
	// re-adds the modifier deltas from the snapshotted selections.
	basePlusVariant := cartItemBasePrice(priced)

	item := ordersapi.CartItem{
		CartID: cart.ID, ProductID: priced.ProductID, ProductName: priced.ProductName, ProductImageURL: priced.ProductImageURL,
		VariantID: priced.VariantID, VariantName: priced.VariantName, UnitPriceCents: basePlusVariant,
		Quantity: req.Quantity, SelectedFlavours: toSelectedModifiers(priced.SelectedFlavours), SelectedAddons: toSelectedModifiers(priced.SelectedAddons),
		SelectedPresentation: toSelectedModifierPtr(priced.SelectedPresentation), SelectedWrapColour: toSelectedModifierPtr(priced.SelectedWrapColour),
		IsGift: req.IsGift, RecipientFirstName: req.RecipientFirst, RecipientLastName: req.RecipientLast, GiftMessage: req.GiftMessage,
	}

	// Adding what is already in the basket raises that line's quantity rather
	// than opening a second identical one. Two lines of the same thing are
	// indistinguishable to the customer, so a cart that shows them looks like
	// it has lost track of itself — and the kitchen has to reconcile them.
	//
	// The comparison runs on the *priced* selections rather than the request's
	// ids, so it sees what the line will actually store: ids the catalogue
	// rejected are already gone, and the ordering is the group's, not the
	// order the customer happened to tap them in.
	if existing := findMergeable(cart.Items, item); existing != nil {
		// Everything but the quantity is the line's own current value: a merge
		// raises the count, it does not restate what is being bought.
		if err := s.store.UpdateCartItem(ctx, cart.ID, existing.ID, existing.Quantity+item.Quantity,
			existing.SelectedFlavours, existing.SelectedAddons, existing.SelectedPresentation, existing.SelectedWrapColour,
			&existing.IsGift, existing.RecipientFirstName, existing.RecipientLastName, existing.GiftMessage); err != nil {
			return nil, nil, "", apperr.Internal(err)
		}
	} else if _, err := s.store.AddCartItem(ctx, item); err != nil {
		return nil, nil, "", apperr.Internal(err)
	}

	cart, err = s.store.GetCartByID(ctx, cart.ID)
	if err != nil {
		return nil, nil, "", apperr.Internal(err)
	}
	summary, err := s.PriceCart(ctx, cart)
	if err != nil {
		return nil, nil, "", err
	}
	return cart, summary, newGuestToken, nil
}

/*
 * Whether an incoming line is the same thing as one already in the cart.
 *
 * "The same thing" means the customer could not tell the two apart: same
 * product, same size, and the same set of every modifier. Sets, not lists —
 * picking vanilla then chocolate must match chocolate then vanilla, so the
 * ids are sorted before they are compared.
 *
 * A gift is deliberately never merged. Two bouquets going to two people with
 * two different cards are two lines however identical the cupcakes are, and
 * silently folding them together would send one card and lose a recipient.
 */
func findMergeable(existing []ordersapi.CartItem, incoming ordersapi.CartItem) *ordersapi.CartItem {
	if incoming.IsGift {
		return nil
	}
	for i := range existing {
		candidate := &existing[i]
		if candidate.IsGift ||
			candidate.ProductID != incoming.ProductID ||
			!sameStringPtr(candidate.VariantID, incoming.VariantID) ||
			!sameModifierSet(candidate.SelectedFlavours, incoming.SelectedFlavours) ||
			!sameModifierSet(candidate.SelectedAddons, incoming.SelectedAddons) ||
			!sameModifierPtr(candidate.SelectedPresentation, incoming.SelectedPresentation) ||
			!sameModifierPtr(candidate.SelectedWrapColour, incoming.SelectedWrapColour) {
			continue
		}
		return candidate
	}
	return nil
}

// modifierIDs pulls the ids back out of a stored selection, for re-pricing a
// line whose selections the caller did not restate. Non-nil even when empty,
// so the result still reads as "these are the selections" rather than as the
// absent value it is standing in for.
func modifierIDs(selected []ordersapi.SelectedModifier) []string {
	ids := make([]string, 0, len(selected))
	for _, m := range selected {
		ids = append(ids, m.ID)
	}
	return ids
}

// sameModifierSet compares by id, order-independently. Names and price deltas
// are snapshots of the same option, so an id match settles it.
func sameModifierSet(a, b []ordersapi.SelectedModifier) bool {
	if len(a) != len(b) {
		return false
	}
	left := make([]string, 0, len(a))
	right := make([]string, 0, len(b))
	for _, m := range a {
		left = append(left, m.ID)
	}
	for _, m := range b {
		right = append(right, m.ID)
	}
	sort.Strings(left)
	sort.Strings(right)
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func sameModifierPtr(a, b *ordersapi.SelectedModifier) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.ID == b.ID
}

func sameStringPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func sumModifierDeltas(p *catalogapi.PricedItem) int64 {
	var total int64
	for _, f := range p.SelectedFlavours {
		total += f.PriceDelta
	}
	for _, a := range p.SelectedAddons {
		total += a.PriceDelta
	}
	if p.SelectedPresentation != nil {
		total += p.SelectedPresentation.PriceDelta
	}
	if p.SelectedWrapColour != nil {
		total += p.SelectedWrapColour.PriceDelta
	}
	return total
}

func cartItemBasePrice(p *catalogapi.PricedItem) int64 {
	if p == nil {
		return 0
	}
	// Keep the stored cart unit price at the branch product rule base price, with
	// modifier deltas tracked separately as SelectedFlavours/Addons/etc. The API
	// exposes the total through UnitTotalCents() and LineTotalCents().
	return p.UnitPriceCents - sumModifierDeltas(p)
}

// UpdateItem re-prices the line from scratch against current catalogue
// state whenever selections change, rather than trusting stored deltas —
// keeps the same server-never-trusts-client-price rule as AddItem.
func (s *Service) UpdateItem(ctx context.Context, id CartIdentity, itemID string, quantity int, flavourIDs, addonIDs []string, presentationID, wrapColourID *string, isGift *bool, recipientFirst, recipientLast, giftMessage *string) (*ordersapi.Cart, *ordersapi.PriceSummary, error) {
	cart, err := s.resolveCart(ctx, id)
	if err != nil {
		return nil, nil, notFoundOrInternal(err, "Cart not found.")
	}
	var existing *ordersapi.CartItem
	for i := range cart.Items {
		if cart.Items[i].ID == itemID {
			existing = &cart.Items[i]
			break
		}
	}
	if existing == nil {
		return nil, nil, apperr.NotFound("Cart item not found.")
	}
	if quantity <= 0 {
		quantity = existing.Quantity
	}

	/*
	 * A field the caller did not mention keeps the value the line already has.
	 *
	 * This matters because the line is re-priced below, and re-pricing runs
	 * the full modifier validation. A quantity-only PATCH — which is exactly
	 * what the cart's own +/- stepper sends — used to arrive with empty
	 * selections, and a product whose flavour group requires a choice then
	 * failed with "requires at least 1 selection", on a line that had a
	 * perfectly good flavour on it all along.
	 *
	 * Nil means "leave alone" here, matching ProductPatch elsewhere in the
	 * codebase. Clearing a selection is done by sending the one that replaces
	 * it, which is all the storefront ever needs: these groups are pick-one,
	 * so there is no "none" to return to.
	 */
	if flavourIDs == nil {
		flavourIDs = modifierIDs(existing.SelectedFlavours)
	}
	if addonIDs == nil {
		addonIDs = modifierIDs(existing.SelectedAddons)
	}
	if presentationID == nil && existing.SelectedPresentation != nil {
		presentationID = &existing.SelectedPresentation.ID
	}
	if wrapColourID == nil && existing.SelectedWrapColour != nil {
		wrapColourID = &existing.SelectedWrapColour.ID
	}

	/*
	 * The gift card follows the same rule, with one addition: turning the
	 * gift off takes the card with it.
	 *
	 * A line that is not a gift has no card to letter, so leaving the message
	 * behind would keep printing it on a kitchen ticket for an order the
	 * customer had already said was for themselves. Turning the gift *on*
	 * without a message is fine — the customer may write it later.
	 */
	if isGift != nil && !*isGift {
		empty := ""
		giftMessage, recipientFirst, recipientLast = &empty, &empty, &empty
	}

	priced, err := s.catalog.GetPricedItem(ctx, cart.BranchID, existing.ProductID, existing.VariantID, flavourIDs, addonIDs, presentationID, wrapColourID, time.Now())
	if err != nil {
		return nil, nil, err
	}
	if !priced.Available {
		return nil, nil, apperr.New("PRODUCT_UNAVAILABLE", apperr.CategoryBusinessRule, "This product is currently unavailable: "+priced.AvailableReason)
	}

	flavours := toSelectedModifiers(priced.SelectedFlavours)
	addons := toSelectedModifiers(priced.SelectedAddons)
	presentation := toSelectedModifierPtr(priced.SelectedPresentation)
	wrap := toSelectedModifierPtr(priced.SelectedWrapColour)

	if err := s.store.UpdateCartItem(ctx, cart.ID, itemID, quantity, flavours, addons, presentation, wrap, isGift, recipientFirst, recipientLast, giftMessage); err != nil {
		return nil, nil, notFoundOrInternal(err, "Cart item not found.")
	}

	cart, err = s.store.GetCartByID(ctx, cart.ID)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	summary, err := s.PriceCart(ctx, cart)
	if err != nil {
		return nil, nil, err
	}
	return cart, summary, nil
}

func (s *Service) RemoveItem(ctx context.Context, id CartIdentity, itemID string) (*ordersapi.Cart, *ordersapi.PriceSummary, error) {
	cart, err := s.resolveCart(ctx, id)
	if err != nil {
		return nil, nil, notFoundOrInternal(err, "Cart not found.")
	}
	if err := s.store.RemoveCartItem(ctx, cart.ID, itemID); err != nil {
		return nil, nil, notFoundOrInternal(err, "Cart item not found.")
	}
	cart, err = s.store.GetCartByID(ctx, cart.ID)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	summary, err := s.PriceCart(ctx, cart)
	if err != nil {
		return nil, nil, err
	}
	return cart, summary, nil
}

// SetFulfilment sets pickup vs delivery and, for delivery, the address —
// re-validated against the branch's delivery zone at checkout time, not
// here, so the customer can fill the form in any order.
func (s *Service) SetFulfilment(
	ctx context.Context,
	id CartIdentity,
	fulfilmentType string,
	street, city, state, postcode, branchId *string,
) (*ordersapi.Cart, *ordersapi.PriceSummary, error) {

	cart, err := s.resolveCart(ctx, id)
	if err != nil {
		return nil, nil, notFoundOrInternal(err, "Cart not found.")
	}
	if len(cart.Items) == 0 {
		return nil, nil, apperr.Validation("cart is empty.")
	}

	if fulfilmentType != ordersapi.FulfilmentPickup &&
		fulfilmentType != ordersapi.FulfilmentDelivery {
		return nil, nil, apperr.Validation(
			"fulfilmentType must be 'pickup' or 'delivery'.",
		)
	}

	targetBranchID := cart.BranchID
	branchChanged := false
	if branchId != nil && strings.TrimSpace(*branchId) != "" {
		newBID := strings.TrimSpace(*branchId)
		if newBID != cart.BranchID {
			targetBranchID = newBID
			branchChanged = true
		}
	}

	branch, err := s.branches.GetBranch(ctx, targetBranchID)
	if err != nil {
		return nil, nil, apperr.NotFound("Branch not found.")
	}
	if err := assertBranchOpen(branch); err != nil {
		return nil, nil, err
	}

	// Update fulfilment details and branch ID on cart.
	if err := s.store.SetCartFulfilment(
		ctx,
		cart.ID,
		fulfilmentType,
		street,
		city,
		state,
		postcode,
		&targetBranchID,
	); err != nil {
		return nil, nil, apperr.Internal(err)
	}

	if branchChanged {
		_ = s.store.ClearCartSchedule(ctx, cart.ID)
	}
	// A radius store measures from where the address is: locate it once now
	// rather than on every price.
	if fulfilmentType == ordersapi.FulfilmentDelivery {
		s.locateCartAddress(ctx, cart.ID, branch, street, city, state, postcode)
	}

	// Re-price each line item against the branch.
	for _, item := range cart.Items {
		flavourIDs := idsOf(item.SelectedFlavours)
		addonIDs := idsOf(item.SelectedAddons)
		priced, err := s.catalog.GetPricedItem(ctx, targetBranchID, item.ProductID, item.VariantID,
			flavourIDs, addonIDs, idOf(item.SelectedPresentation), idOf(item.SelectedWrapColour), time.Now())
		if err != nil {
			_ = s.store.UpdateCartItemAvailable(ctx, item.ID, false)
			continue
		}
		if !priced.Available {
			_ = s.store.UpdateCartItemAvailable(ctx, item.ID, false)
			continue
		}

		basePlusVariant := cartItemBasePrice(priced)
		flavours := toSelectedModifiers(priced.SelectedFlavours)
		addons := toSelectedModifiers(priced.SelectedAddons)
		presentation := toSelectedModifierPtr(priced.SelectedPresentation)
		wrap := toSelectedModifierPtr(priced.SelectedWrapColour)

		if err := s.store.UpdateCartItemPricing(
			ctx,
			item.ID,
			basePlusVariant,
			flavours,
			addons,
			presentation,
			wrap,
			priced.Available,
		); err != nil {
			return nil, nil, apperr.Internal(err)
		}
	}

	// Reload cart after updates.
	cart, err = s.store.GetCartByID(ctx, cart.ID)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}

	summary, err := s.PriceCart(ctx, cart)
	if err != nil {
		return nil, nil, err
	}

	return cart, summary, nil
}

// SetSchedule books a specific slot for the cart's branch. It reserves
// nothing yet — capacity is only actually consumed at order creation
// (SCH-04) — but it does confirm the slot exists and currently has room, so
// the customer gets fast feedback rather than discovering a conflict only
// at checkout.
func (s *Service) SetSchedule(ctx context.Context, id CartIdentity, date time.Time, slotStart string) (*ordersapi.Cart, *ordersapi.PriceSummary, error) {
	cart, err := s.resolveCart(ctx, id)
	if err != nil {
		return nil, nil, notFoundOrInternal(err, "Cart not found.")
	}

	slots, _, err := s.GetAvailableSlots(ctx, cart.BranchID, cart.FulfilmentType, date, 1)
	if err != nil {
		return nil, nil, err
	}
	var matched *ordersapi.SlotCapacity
	for _, day := range slots {
		for i := range day.Slots {
			// "09:00" and "09:00:00" are the same slot.
			if hhmm5(day.Slots[i].SlotStart) == hhmm5(slotStart) {
				matched = &day.Slots[i]
			}
		}
	}
	if matched == nil {
		return nil, nil, apperr.New("SLOT_NO_LONGER_AVAILABLE", apperr.CategoryConflict, "This time slot is no longer available. Please choose another.")
	}
	// Seasonal-tag restrictions are not checked here. The pickup/delivery
	// popup already shows the unavailable dates, each cart line carries its
	// restriction once the date is set, and checkout refuses an order with a
	// restricted line (PRODUCT_RESTRICTED_DATE).

	if err := s.store.SetCartSchedule(ctx, cart.ID, date, matched.SlotStart, matched.SlotEnd); err != nil {
		return nil, nil, apperr.Internal(err)
	}
	cart, err = s.store.GetCartByID(ctx, cart.ID)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	summary, err := s.PriceCart(ctx, cart)
	if err != nil {
		return nil, nil, err
	}
	return cart, summary, nil
}

func (s *Service) SetTip(ctx context.Context, id CartIdentity, tipCents int64) (*ordersapi.PriceSummary, error) {
	cart, err := s.resolveCart(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Cart not found.")
	}
	if tipCents < 0 {
		tipCents = 0
	}
	if err := s.store.SetCartTip(ctx, cart.ID, tipCents); err != nil {
		return nil, apperr.Internal(err)
	}
	cart.TipCents = tipCents
	return s.PriceCart(ctx, cart)
}

// PriceCart totals a cart server-side: line prices, discount, delivery fee,
// tax and tip. The client never supplies a price.
//
// Tax is charged on the discounted subtotal, not the list one: a discount
// reduces what the customer actually pays for the goods, and that is the sum
// tax is owed on. Delivery is discounted separately, because a free-delivery
// coupon zeroes that line rather than reducing the goods.
func (s *Service) PriceCart(ctx context.Context, cart *ordersapi.Cart) (*ordersapi.PriceSummary, error) {
	summary, _, err := s.priceCartWithCoupon(ctx, cart)
	return summary, err
}

// priceCartWithCoupon is PriceCart plus the resolved discount, which order
// creation needs so it can record which coupon was used and count the
// redemption. Callers that only want the money use PriceCart.
func (s *Service) priceCartWithCoupon(ctx context.Context, cart *ordersapi.Cart) (*ordersapi.PriceSummary, *catalogapi.CouponDiscount, error) {
	// Lines a seasonal tag rules out for the chosen slot, flagged on every
	// cart response so the basket can say so.
	s.markRestrictedLines(ctx, cart)
	var subtotal int64
	productIDs := make([]string, 0, len(cart.Items))
	for _, item := range cart.Items {
		subtotal += item.LineTotalCents()
		productIDs = append(productIDs, item.ProductID)
	}

	branch, err := s.branches.GetBranch(ctx, cart.BranchID)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	if err := assertBranchOpen(branch); err != nil {
		return nil, nil, err
	}

	// Delivery is priced by the store's rules for this address, order size
	// and slot. An address the store doesn't cover prices at 0 and says so;
	// checkout refuses it.
	var deliveryFee int64
	deliveryAvailable, deliveryMessage := true, ""
	if cart.FulfilmentType == ordersapi.FulfilmentDelivery {
		q := s.quoteCartDelivery(ctx, branch, cart, subtotal)
		deliveryFee, deliveryAvailable, deliveryMessage = q.FeeCents, q.Deliverable, q.Reason
	}

	// A code that has stopped being valid since it was applied — expired
	// overnight, used up by someone else, or left behind when the basket
	// dropped below its minimum — is quietly dropped rather than failing the
	// whole cart read. The shopper sees the discount disappear from the
	// summary, which is the truth, instead of a cart that will not load.
	var discount *catalogapi.CouponDiscount
	if cart.DiscountCode != nil && *cart.DiscountCode != "" && s.coupons != nil && subtotal > 0 {
		categoryIDs, catErr := s.coupons.CategoryIDsForProducts(ctx, productIDs)
		if catErr != nil {
			slog.Warn("could not resolve categories for coupon", "error", catErr)
		}
		quoted, quoteErr := s.coupons.QuoteCoupon(ctx, catalogapi.CouponQuote{
			Code: *cart.DiscountCode, BranchID: cart.BranchID,
			SubtotalCents: subtotal, DeliveryFeeCents: deliveryFee,
			ProductIDs: productIDs, CategoryIDs: categoryIDs,
			CustomerID: cart.CustomerID,
		})
		if quoteErr == nil {
			discount = quoted
		}
	}

	var discountCents int64
	if discount != nil {
		if discount.FreeDelivery {
			deliveryFee -= discount.AmountCents
			if deliveryFee < 0 {
				deliveryFee = 0
			}
		} else {
			discountCents = discount.AmountCents
		}
	}

	taxable := subtotal - discountCents
	if taxable < 0 {
		taxable = 0
	}
	tax := taxOn(taxable, branch.TaxRatePct)

	total := taxable + deliveryFee + tax + cart.TipCents
	if total < 0 {
		total = 0
	}
	return &ordersapi.PriceSummary{
		SubtotalCents: subtotal, DeliveryFeeCents: deliveryFee, TaxCents: tax, TaxName: branchTaxName(branch),
		TipCents: cart.TipCents, DiscountCents: discountCents, TotalCents: total,
		DeliveryAvailable: deliveryAvailable, DeliveryMessage: deliveryMessage,
		Currency: branch.Currency,
	}, discount, nil
}

// ApplyCoupon attaches a discount code to the cart.
//
// The code is validated here rather than at checkout so the shopper finds out
// immediately, and stored on the cart rather than held in the browser so the
// price the server quotes is the price it will charge.
func (s *Service) ApplyCoupon(ctx context.Context, id CartIdentity, code string) (*ordersapi.Cart, *ordersapi.PriceSummary, error) {
	cart, err := s.resolveCart(ctx, id)
	if err != nil {
		return nil, nil, notFoundOrInternal(err, "Cart not found.")
	}
	if s.coupons == nil {
		return nil, nil, apperr.New("COUPON_INVALID", apperr.CategoryValidation, "That code is not valid.")
	}
	if len(cart.Items) == 0 {
		return nil, nil, apperr.Validation("Add something to your basket before applying a code.")
	}

	var subtotal int64
	productIDs := make([]string, 0, len(cart.Items))
	for _, item := range cart.Items {
		subtotal += item.LineTotalCents()
		productIDs = append(productIDs, item.ProductID)
	}

	branch, err := s.branches.GetBranch(ctx, cart.BranchID)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	var deliveryFee int64
	if cart.FulfilmentType == ordersapi.FulfilmentDelivery {
		deliveryFee = s.quoteCartDelivery(ctx, branch, cart, subtotal).FeeCents
	}

	categoryIDs, err := s.coupons.CategoryIDsForProducts(ctx, productIDs)
	if err != nil {
		return nil, nil, err
	}

	// Rejections travel to the shopper as they are: the catalogue service
	// already turned each rule into a sentence worth reading.
	// The signed-in customer, if any, travels with the quote: a new-customer
	// code is refused to a guest with a message that says to sign in, and
	// to a returning customer with one that says why.
	if _, err := s.coupons.QuoteCoupon(ctx, catalogapi.CouponQuote{
		Code: code, BranchID: cart.BranchID,
		SubtotalCents: subtotal, DeliveryFeeCents: deliveryFee,
		ProductIDs: productIDs, CategoryIDs: categoryIDs,
		CustomerID: firstCustomerID(id.CustomerID, cart.CustomerID),
	}); err != nil {
		return nil, nil, err
	}

	normalised := catalogapi.NormaliseCouponCode(code)
	if err := s.store.SetCartDiscount(ctx, cart.ID, &normalised); err != nil {
		return nil, nil, apperr.Internal(err)
	}
	cart.DiscountCode = &normalised

	summary, err := s.PriceCart(ctx, cart)
	if err != nil {
		return nil, nil, err
	}
	return cart, summary, nil
}

// firstCustomerID picks the caller's customer id from the token first and
// the cart second — the token is authoritative, the cart is what it was
// created under.
func firstCustomerID(ids ...*string) *string {
	for _, id := range ids {
		if id != nil && *id != "" {
			return id
		}
	}
	return nil
}

// RemoveCoupon clears the cart's discount code.
func (s *Service) RemoveCoupon(ctx context.Context, id CartIdentity) (*ordersapi.Cart, *ordersapi.PriceSummary, error) {
	cart, err := s.resolveCart(ctx, id)
	if err != nil {
		return nil, nil, notFoundOrInternal(err, "Cart not found.")
	}
	if err := s.store.SetCartDiscount(ctx, cart.ID, nil); err != nil {
		return nil, nil, apperr.Internal(err)
	}
	cart.DiscountCode = nil

	summary, err := s.PriceCart(ctx, cart)
	if err != nil {
		return nil, nil, err
	}
	return cart, summary, nil
}
