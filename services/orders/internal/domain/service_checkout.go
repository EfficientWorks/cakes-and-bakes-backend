package domain

import (
	"bakehouse/pkg/realtime"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/email"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/orders/internal/store"
	"bakehouse/services/orders/ordersapi"
	"bakehouse/services/payments/paymentsapi"
)

type ValidationWarning struct {
	Type    string
	Message string
}

type ValidationResult struct {
	Valid    bool
	Warnings []ValidationWarning
	// Summary is the cart priced now, delivery charge included, so the
	// storefront can show the figures the order will be charged at.
	Summary *ordersapi.PriceSummary
}

// ValidationExpectation is what the storefront is showing, so a change can
// be flagged rather than silently charged. Both optional.
type ValidationExpectation struct {
	DeliveryFeeCents *int64
	TotalCents       *int64
}

// ValidateCart re-checks price and availability for every line against
// current catalogue state, and slot capacity, without mutating anything —
// used by the storefront right before showing the payment step
// (checkout/validate in 05-ORDERS.md).
func (s *Service) ValidateCart(ctx context.Context, id CartIdentity, expect *ValidationExpectation) (*ValidationResult, error) {
	cart, err := s.resolveCart(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Cart not found.")
	}
	if len(cart.Items) == 0 {
		return nil, apperr.Validation("Cart is empty.")
	}

	result := &ValidationResult{Valid: true}
	for _, item := range cart.Items {
		priced, err := s.repriceItem(ctx, cart.BranchID, item)
		if err != nil {
			result.Valid = false
			result.Warnings = append(result.Warnings, ValidationWarning{Type: "PRODUCT_UNAVAILABLE", Message: err.Error()})
			continue
		}
		if !priced.available {
			result.Valid = false
			result.Warnings = append(result.Warnings, ValidationWarning{
				Type: "PRODUCT_UNAVAILABLE", Message: fmt.Sprintf("%s is no longer available: %s", item.ProductName, priced.reason),
			})
			continue
		}
		/*
		 * Both sides of this comparison have to mean the same thing.
		 *
		 * GetPricedItem returns a unit price with the chosen modifiers already
		 * folded in; the line stores the base price with its modifiers kept
		 * beside it. Comparing the first against the second flagged a price
		 * change on every line that had any modifier at all — and then printed
		 * the "new" price by adding the deltas a second time, so a $82 bouquet
		 * was reported as having changed to $88. Neither number had moved.
		 */
		if priced.unitPriceCents != item.UnitTotalCents() {
			result.Valid = false
			result.Warnings = append(result.Warnings, ValidationWarning{
				Type: "PRICE_CHANGED",
				Message: fmt.Sprintf("%s price changed from $%.2f to $%.2f", item.ProductName,
					float64(item.UnitTotalCents())/100, float64(priced.unitPriceCents)/100),
			})
		}
	}

	if cart.ScheduledDate == nil || cart.SlotStart == nil {
		result.Valid = false
		result.Warnings = append(result.Warnings, ValidationWarning{Type: "SCHEDULE_REQUIRED", Message: "Please choose a delivery or pickup time."})
	} else {
		slots, _, err := s.GetAvailableSlots(ctx, cart.BranchID, cart.FulfilmentType, *cart.ScheduledDate, 1)
		if err == nil {
			found := false
			for _, d := range slots {
				for _, sc := range d.Slots {
					if sc.SlotStart == *cart.SlotStart {
						found = true
					}
				}
			}
			if !found {
				result.Valid = false
				result.Warnings = append(result.Warnings, ValidationWarning{Type: "SLOT_FULL", Message: "Your selected slot is no longer available. Please choose another time."})
			}
		}
		// Seasonal tags' restricted dates (website carts).
		if !id.isManualCart() && s.markRestrictedLines(ctx, cart) > 0 {
			result.Valid = false
			for _, it := range cart.Items {
				if it.Restriction != nil {
					result.Warnings = append(result.Warnings, ValidationWarning{Type: "PRODUCT_RESTRICTED_DATE", Message: it.Restriction.Message})
				}
			}
		}
	}

	/*
	 * The delivery charge, priced now from the store's rules for this
	 * address, order size and slot: any of those may have changed since the
	 * customer last saw a total. Out of area blocks checkout (it would be
	 * refused at order creation anyway); a charge different from the one the
	 * storefront is showing is flagged so the customer sees the new total
	 * before paying it.
	 */
	summary, err := s.PriceCart(ctx, cart)
	if err != nil {
		return nil, err
	}
	result.Summary = summary
	if cart.FulfilmentType == ordersapi.FulfilmentDelivery {
		if !summary.DeliveryAvailable {
			result.Valid = false
			msg := summary.DeliveryMessage
			if msg == "" {
				msg = "This store doesn't deliver to that address."
			}
			result.Warnings = append(result.Warnings, ValidationWarning{Type: "DELIVERY_UNAVAILABLE", Message: msg})
		} else if expect != nil && expect.DeliveryFeeCents != nil && *expect.DeliveryFeeCents != summary.DeliveryFeeCents {
			result.Valid = false
			result.Warnings = append(result.Warnings, ValidationWarning{Type: "DELIVERY_FEE_CHANGED",
				Message: fmt.Sprintf("The delivery charge is now $%.2f (was $%.2f).",
					float64(summary.DeliveryFeeCents)/100, float64(*expect.DeliveryFeeCents)/100)})
		}
	}
	if expect != nil && expect.TotalCents != nil && *expect.TotalCents != summary.TotalCents && result.Valid {
		result.Valid = false
		result.Warnings = append(result.Warnings, ValidationWarning{Type: "TOTAL_CHANGED",
			Message: fmt.Sprintf("Your order total is now $%.2f (was $%.2f).",
				float64(summary.TotalCents)/100, float64(*expect.TotalCents)/100)})
	}

	return result, nil
}

type repricedItem struct {
	unitPriceCents int64
	available      bool
	reason         string
}

func (s *Service) repriceItem(ctx context.Context, branchID string, item ordersapi.CartItem) (*repricedItem, error) {
	priced, err := s.catalog.GetPricedItem(ctx, branchID, item.ProductID, item.VariantID,
		idsOf(item.SelectedFlavours), idsOf(item.SelectedAddons), idOf(item.SelectedPresentation), idOf(item.SelectedWrapColour), time.Now())
	if err != nil {
		return nil, err
	}
	return &repricedItem{unitPriceCents: priced.UnitPriceCents, available: priced.Available, reason: priced.AvailableReason}, nil
}

func idsOf(mods []ordersapi.SelectedModifier) []string {
	out := make([]string, 0, len(mods))
	for _, m := range mods {
		out = append(out, m.ID)
	}
	return out
}

func idOf(mod *ordersapi.SelectedModifier) *string {
	if mod == nil {
		return nil
	}
	return &mod.ID
}

// --- Checkout: order creation (ORD-01, SF-25 to SF-29) ----------------------

type Recipient struct {
	FirstName string
	LastName  string
	Phone     *string
	// Email is the receiver's own, kept apart from Purchaser.Email: the
	// receipt goes to whoever paid, this is for notifying the person the
	// order is for. Optional, and usually absent on a gift.
	Email *string
}

type Purchaser struct {
	FirstName string
	LastName  string
	Email     string
	Phone     *string
}

type CreateOrderRequest struct {
	Recipient   Recipient
	GiftMessage *string
	// GiftReason is why it is being sent. Presentational and optional — no
	// enum here, because the storefront owns the list of occasions.
	GiftReason           *string
	IsGift               bool
	Purchaser            Purchaser
	DeliveryInstructions *string
	// Notes is the customer's free-text note on the order; optional.
	Notes *string
	// BusinessName and DeliveryAddressType ("residence" | "business") describe
	// where the delivery is going. Optional; ignored for pickup.
	BusinessName        *string
	DeliveryAddressType *string
	// The two delivery preferences the checkout offers. Only meaningful on a
	// delivery order; a pickup order ignores both.
	PhotoOnDelivery  bool
	SurpriseDelivery bool
	MarketingOptIn   bool
	// PaymentMethod selects the settlement path: "card" (the default, and
	// every order before cash on delivery existed) or "cash_on_delivery".
	PaymentMethod   string
	PaymentMethodID string
	// PaymentIntentID is sent instead of PaymentMethodID when the customer
	// has just completed a 3-D Secure challenge: the intent already exists
	// and only needs verifying, so creating a second one would charge twice.
	PaymentIntentID string
	IdempotencyKey  string
	Status          string
	// SendKotTime, when the client sends one, is stamped on every order line
	// as send_kot_time. Nil leaves the lines unsent.
	SendKotTime *time.Time
	// EndTime, when the client sends one with a closing status, is stored
	// as the order's end_time; otherwise it is stamped when the order closes.
	EndTime *time.Time
}

// PaymentMethodCOD is the one method where the order exists before the money.
const PaymentMethodCOD = "cash_on_delivery"

// CreateOrder re-prices every line from scratch, reserves slot capacity
// atomically, captures payment, and writes the order — all inside one
// transaction at the store layer (SCH-04). A replayed idempotency key
// returns the original order instead of creating a duplicate (SF-28).
func (s *Service) CreateOrder(ctx context.Context, id CartIdentity, req CreateOrderRequest) (*ordersapi.Order, error) {
	// Validate the requested status up front, before a card is charged.
	requestedStatus := ordersapi.NormalizeStatus(req.Status)
	if requestedStatus != "" && !ordersapi.IsValidStatus(requestedStatus) {
		return nil, apperr.Validation("\"" + req.Status + "\" is not an order status. Valid values: " +
			strings.Join(ordersapi.AllStatuses(), ", ") + ".").
			WithFields(apperr.FieldError{Field: "status", Code: "INVALID", Message: "Not a status."})
	}
	if req.IdempotencyKey == "" {
		return nil, apperr.Validation("Idempotency-Key header is required.")
	}
	if req.Recipient.FirstName == "" || req.Recipient.LastName == "" {
		return nil, apperr.Validation("Recipient name is required.").
			WithFields(apperr.FieldError{Field: "recipient", Code: "REQUIRED", Message: "Who is this for?"})
	}
	if t := trimPtr(req.DeliveryAddressType); t != nil && !ordersapi.IsValidAddressType(*t) {
		return nil, apperr.Validation("deliveryAddressType must be \"residence\" or \"business\".")
	}
	if req.Purchaser.Email == "" {
		return nil, apperr.Validation("Purchaser email is required for the receipt.")
	}

	// Idempotency check comes before cart resolution: checkout deletes the
	// cart on success, so a client retrying after a network timeout (the
	// realistic double-submission case, not just a fast double-click) would
	// otherwise hit "cart not found" instead of getting back the order it
	// already placed (SF-28).
	if existing, err := s.store.FindOrderByIdempotencyKey(ctx, req.IdempotencyKey); err == nil {
		return existing, nil
	}

	cart, err := s.resolveCart(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Your cart could not be found. Please start again.")
	}
	if len(cart.Items) == 0 {
		return nil, apperr.Validation("Cart is empty.")
	}
	if cart.ScheduledDate == nil || cart.SlotStart == nil || cart.SlotEnd == nil {
		return nil, apperr.Validation("Please choose a delivery or pickup time before checking out.")
	}
	if !id.isManualCart() && s.markRestrictedLines(ctx, cart) > 0 {
		return nil, errRestrictedItems(cart)
	}
	if cart.FulfilmentType == ordersapi.FulfilmentDelivery && req.Recipient.Phone == nil {
		return nil, apperr.Validation("Recipient phone number is required for delivery.").
			WithFields(apperr.FieldError{Field: "recipient.phone", Code: "REQUIRED", Message: "Required for delivery."})
	}

	branch, err := s.branches.GetBranch(ctx, cart.BranchID)
	if err != nil {
		return nil, apperr.NotFound("Branch not found.")
	}
	// Nothing on this path used to ask whether the outlet was open. A branch
	// suspended with its franchise, closed, or shut for the day would take the
	// order and the payment regardless.
	if err := assertBranchOpen(branch); err != nil {
		return nil, err
	}
	/* if cart.FulfilmentType == ordersapi.FulfilmentDelivery && cart.DeliveryPostcode != nil && !zipInBranchZone(branch, *cart.DeliveryPostcode) {
		return nil, apperr.New("DELIVERY_ADDRESS_OUT_OF_ZONE", apperr.CategoryValidation, "This address is outside the selected branch's delivery zone.")
	} */

	// Re-price every line server-side — the client-supplied cart snapshot is
	// never trusted for the money-moving step.
	var orderItems []ordersapi.OrderItem
	var subtotal int64
	for _, item := range cart.Items {
		priced, err := s.catalog.GetPricedItem(ctx, cart.BranchID, item.ProductID, item.VariantID,
			idsOf(item.SelectedFlavours), idsOf(item.SelectedAddons), idOf(item.SelectedPresentation), idOf(item.SelectedWrapColour), time.Now())
		if err != nil {
			return nil, err
		}
		if !priced.Available {
			return nil, apperr.New("PRODUCT_UNAVAILABLE", apperr.CategoryBusinessRule, item.ProductName+" is no longer available: "+priced.AvailableReason)
		}
		lineTotal := priced.UnitPriceCents * int64(item.Quantity)
		subtotal += lineTotal
		orderItems = append(orderItems, ordersapi.OrderItem{
			ProductID: priced.ProductID, ProductName: priced.ProductName, VariantID: priced.VariantID, VariantName: priced.VariantName,
			Quantity: item.Quantity, UnitPriceCents: priced.UnitPriceCents, LineTotalCents: lineTotal,
			SelectedFlavours: toSelectedModifiers(priced.SelectedFlavours), SelectedAddons: toSelectedModifiers(priced.SelectedAddons),
			SelectedPresentation: toSelectedModifierPtr(priced.SelectedPresentation), SelectedWrapColour: toSelectedModifierPtr(priced.SelectedWrapColour),
			ProductionStatus: "pending",
			// The cart line's own gift message, which used to be dropped here.
			GiftMessage: item.GiftMessage,
			SendKotTime: req.SendKotTime,
		})
	}

	// Delivery priced by the store's rules against the re-priced subtotal,
	// and refused outright for an address the store doesn't cover.
	var deliveryFee int64
	if cart.FulfilmentType == ordersapi.FulfilmentDelivery {
		q := s.quoteCartDelivery(ctx, branch, cart, subtotal)
		if !q.Deliverable {
			return nil, errDeliveryUnavailable(q.Reason)
		}
		deliveryFee = q.FeeCents
	}

	// The discount is re-quoted here against the server's own re-priced
	// subtotal, never carried over from the cart summary: the summary was
	// computed from prices that could have changed since, and this is the
	// number the customer's card is charged.
	var discount *catalogapi.CouponDiscount
	if cart.DiscountCode != nil && *cart.DiscountCode != "" && s.coupons != nil {
		productIDs := make([]string, 0, len(orderItems))
		for _, item := range orderItems {
			productIDs = append(productIDs, item.ProductID)
		}
		categoryIDs, catErr := s.coupons.CategoryIDsForProducts(ctx, productIDs)
		if catErr != nil {
			slog.Warn("could not resolve categories for coupon at checkout", "error", catErr)
		}
		// A code that has stopped applying between the cart and this moment
		// is dropped rather than failing the order. The customer is charged
		// the honest total, which is the one they can see; refusing to take
		// the order because a coupon expired mid-checkout would be worse.
		if quoted, quoteErr := s.coupons.QuoteCoupon(ctx, catalogapi.CouponQuote{
			Code: *cart.DiscountCode, BranchID: cart.BranchID,
			SubtotalCents: subtotal, DeliveryFeeCents: deliveryFee,
			ProductIDs: productIDs, CategoryIDs: categoryIDs,
			CustomerID: firstCustomerID(id.CustomerID, cart.CustomerID),
		}); quoteErr == nil {
			discount = quoted
		} else {
			slog.Info("discount code no longer applies at checkout",
				"code", *cart.DiscountCode, "reason", quoteErr)
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

	slot, err := s.store.GetOrCreateSlot(ctx, cart.BranchID, *cart.ScheduledDate, *cart.SlotStart, *cart.SlotEnd, cart.FulfilmentType, 0)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if slot.Remaining() <= 0 {
		return nil, apperr.New("SLOT_NO_LONGER_AVAILABLE", apperr.CategoryConflict, "Your selected slot is now full. Please choose another time.")
	}

	reference, err := store.NewOrderReference()
	if err != nil {
		return nil, apperr.Internal(err)
	}

	// Cash on delivery inverts the usual order of events.
	//
	// Every other method captures the money first and only then creates the
	// order, so a failed payment never leaves a phantom order behind. COD
	// cannot: there is nothing to capture until someone hands over cash at
	// the door. So the order is created unpaid, carrying a genuine pending
	// payment row, and is settled at handover (RecordCashCollected).
	//
	// Nothing here pretends the money arrived. `payment_status` stays
	// 'pending', which is what makes "cash still owed" a query rather than a
	// guess — the risk COD introduces is uncollected cash, and it is only
	// manageable if it is visible.
	if req.PaymentMethod == PaymentMethodCOD {
		if err := checkCODAllowed(branch, total); err != nil {
			return nil, err
		}
	}

	// Two ways in. A first attempt creates and confirms an intent from the
	// payment method; a retry after 3-D Secure verifies the intent the
	// customer just authenticated. Both re-price against `total`, which this
	// server computed — never against anything the client sent.
	var payment *paymentsapi.PaymentResult
	switch {
	case req.PaymentMethod == PaymentMethodCOD:
		// No processor call at all. A zero-value result would misreport the
		// provider reference, so the COD path skips `payment` entirely and
		// the order is written with a pending status below.
	case req.PaymentIntentID != "":
		payment, err = s.payments.VerifyIntent(ctx, req.PaymentIntentID, total)
	default:
		payment, err = s.payments.Capture(ctx, total, branch.Currency, req.PaymentMethodID, req.IdempotencyKey)
	}
	if err != nil {
		// Authentication needed is not a decline: it is passed through with
		// its own code so the browser can run the challenge and come back,
		// rather than telling the customer to try a different card.
		var needsAuth *paymentsapi.AuthenticationRequired
		if errors.As(err, &needsAuth) {
			return nil, apperr.Wrap(err, "PAYMENT_REQUIRES_ACTION", apperr.CategoryValidation,
				"This card needs to be verified with your bank.").
				WithMeta(map[string]any{
					"paymentIntentId": needsAuth.PaymentIntentID,
					"clientSecret":    needsAuth.ClientSecret,
				})
		}
		return nil, apperr.Wrap(err, "PAYMENT_FAILED", apperr.CategoryUpstream, "Payment could not be completed. Please try a different card.")
	}

	order := ordersapi.Order{
		Reference: reference, BranchID: branch.ID, CustomerID: id.CustomerID,
		Channel: "web", FulfilmentType: cart.FulfilmentType,
		RecipientFirstName: req.Recipient.FirstName, RecipientLastName: req.Recipient.LastName, RecipientPhone: req.Recipient.Phone,
		RecipientEmail: req.Recipient.Email,
		GiftMessage:    req.GiftMessage, GiftReason: req.GiftReason, IsGift: req.IsGift,
		PurchaserFirstName: req.Purchaser.FirstName, PurchaserLastName: req.Purchaser.LastName,
		PurchaserEmail: req.Purchaser.Email, PurchaserPhone: req.Purchaser.Phone,
		DeliveryStreet: cart.DeliveryStreet, DeliveryCity: cart.DeliveryCity, DeliveryState: cart.DeliveryState,
		DeliveryPostcode: cart.DeliveryPostcode, DeliveryInstructions: req.DeliveryInstructions,
		BusinessName: trimPtr(req.BusinessName), DeliveryAddressType: trimPtr(req.DeliveryAddressType),
		PhotoOnDelivery: req.PhotoOnDelivery, SurpriseDelivery: req.SurpriseDelivery,
		ScheduledDate: *cart.ScheduledDate, SlotStart: *cart.SlotStart, SlotEnd: *cart.SlotEnd, SlotCapacityID: &slot.ID,
		SubtotalCents: subtotal, DeliveryFeeCents: deliveryFee, TaxCents: tax, TipCents: cart.TipCents,
		DiscountCents: discountCents, DiscountCode: cart.DiscountCode,
		TotalCents: total, Currency: branch.Currency,
		Status:         firstNonEmpty(requestedStatus, ordersapi.StatusPlaced),
		IdempotencyKey: req.IdempotencyKey, MarketingOptIn: req.MarketingOptIn,
		Notes:   trimPtr(req.Notes),
		EndTime: req.EndTime,
	}

	if payment != nil {
		order.PaymentMethod = "card"
		order.PaymentStatus = payment.Status
		order.ProviderPaymentID = &payment.ProviderRef
	} else {
		// Unpaid, and honestly so. Settled at handover.
		order.PaymentMethod = PaymentMethodCOD
		order.PaymentStatus = "pending"
	}

	orderID, err := s.store.CreateOrderTx(ctx, order, orderItems)
	isReplay := errors.Is(err, ordersapi.ErrIdempotentReplay)
	if err != nil && !isReplay {
		if errors.Is(err, ordersapi.ErrSlotFull) {
			return nil, apperr.New("SLOT_NO_LONGER_AVAILABLE", apperr.CategoryConflict, "Your selected slot filled up just now. Please choose another time.")
		}
		return nil, apperr.Internal(err)
	}

	created, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	if !isReplay {
		s.publishOrder(realtime.EventOrderCreated, created, "")
	}

	if !isReplay {
		if payment != nil {
			s.payments.LinkOrder(ctx, payment.ProviderRef, created.ID, branch.ID)
		} else {
			// COD still gets a payment row, pending rather than captured, so
			// the money owed is in the same ledger as everything else and the
			// day-close reconciliation has one place to look.
			s.payments.RecordPending(ctx, created.ID, branch.ID, "cash", total, branch.Currency)
		}
		// Counted once, and only once the order exists. Doing it at apply
		// time would let a shopper burn a limited coupon by typing the code
		// and walking away; doing it inside the order transaction would tie
		// a coupon-counter failure to the order succeeding, which is the
		// wrong trade — the sale matters more than the tally.
		if discount != nil && s.coupons != nil {
			if err := s.coupons.RedeemCoupon(ctx, discount.CouponID, created.ID,
				firstCustomerID(id.CustomerID, cart.CustomerID), discount.AmountCents); err != nil {
				slog.Warn("could not record coupon redemption",
					"coupon", discount.Code, "order", created.Reference, "error", err)
			}
		}
		_ = s.store.DeleteCart(ctx, cart.ID)
		/*
		 * Accepted on arrival.
		 *
		 * An order was created as "placed" and left there — nothing ever moved
		 * it on, so every order sat waiting for an acceptance no part of the
		 * system was going to give it, and the kitchen queue filled with work
		 * that never looked confirmed.
		 *
		 * There is nothing to decide at this point: the customer has chosen a
		 * slot the branch published, against stock the branch listed, and paid.
		 * A shop that wants to refuse it cancels it, which is a real action
		 * with a reason attached — not the absence of one.
		 *
		 * Best effort, and after the cart is gone: an order that exists and is
		 * paid for must not be undone because a status row would not write.
		 */
		if requestedStatus == "" {
			if _, err := s.store.UpdateOrderStatus(ctx, created.ID, ordersapi.StatusConfirmed,
				"system", nil, "Accepted automatically"); err != nil {
				slog.Warn("order could not be auto-accepted", "order", created.Reference, "error", err)
			} else {
				created.Status = ordersapi.StatusConfirmed
			}
		}

		s.sendOrderConfirmation(ctx, created)
	}

	return created, nil
}

func (s *Service) sendOrderConfirmation(ctx context.Context, o *ordersapi.Order) {
	if s.emailer == nil {
		return
	}
	_ = s.sendOrderConfirmationErr(ctx, o)
}

// sendOrderConfirmationErr is the same send, with the failure returned.
//
// Checkout ignores it deliberately — an order is placed and paid for whether
// or not the email lands, and failing the request would be worse. Resending by
// hand is the opposite case: somebody pressed a button to find out.
func (s *Service) sendOrderConfirmationErr(ctx context.Context, o *ordersapi.Order) error {
	if s.emailer == nil {
		return nil
	}
	items := make([]map[string]interface{}, 0, len(o.Items))
	for _, i := range o.Items {
		var variantName string
		if i.VariantName != nil {
			variantName = *i.VariantName
		}
		var flavours []map[string]interface{}
		for _, f := range i.SelectedFlavours {
			flavours = append(flavours, map[string]interface{}{
				"name":       f.Name,
				"priceDelta": f.PriceDelta,
			})
		}
		var addons []map[string]interface{}
		for _, a := range i.SelectedAddons {
			addons = append(addons, map[string]interface{}{
				"name":       a.Name,
				"priceDelta": a.PriceDelta,
			})
		}
		var presentation map[string]interface{}
		if i.SelectedPresentation != nil {
			presentation = map[string]interface{}{
				"name":       i.SelectedPresentation.Name,
				"priceDelta": i.SelectedPresentation.PriceDelta,
			}
		}
		var wrap map[string]interface{}
		if i.SelectedWrapColour != nil {
			wrap = map[string]interface{}{
				"name":       i.SelectedWrapColour.Name,
				"priceDelta": i.SelectedWrapColour.PriceDelta,
			}
		}

		var sumModifierDeltas int64
		for _, f := range i.SelectedFlavours {
			sumModifierDeltas += f.PriceDelta
		}
		for _, a := range i.SelectedAddons {
			sumModifierDeltas += a.PriceDelta
		}
		if i.SelectedPresentation != nil {
			sumModifierDeltas += i.SelectedPresentation.PriceDelta
		}
		if i.SelectedWrapColour != nil {
			sumModifierDeltas += i.SelectedWrapColour.PriceDelta
		}
		basePrice := i.UnitPriceCents - sumModifierDeltas
		if basePrice <= 0 && i.UnitPriceCents > 0 {
			basePrice = i.UnitPriceCents
		}

		items = append(items, map[string]interface{}{
			"name":         i.ProductName,
			"variant":      variantName,
			"quantity":     i.Quantity,
			"basePrice":    basePrice,
			"unitPrice":    i.UnitPriceCents,
			"total":        i.LineTotalCents,
			"flavours":     flavours,
			"addons":       addons,
			"presentation": presentation,
			"wrap":         wrap,
		})
	}

	totalCents := o.TotalCents
	if totalCents == 0 {
		totalCents = o.SubtotalCents - o.DiscountCents + o.DeliveryFeeCents + o.TaxCents + o.TipCents + o.ServiceChargeCents
		if totalCents < 0 {
			totalCents = 0
		}
	}

	data := map[string]interface{}{
		"reference":      o.Reference,
		"recipientName":  o.RecipientFirstName + " " + o.RecipientLastName,
		"purchaserName":  strings.TrimSpace(o.PurchaserFirstName + " " + o.PurchaserLastName),
		"scheduledDate":  o.ScheduledDate.Format("2006-01-02"),
		"slotStart":      o.SlotStart,
		"slotEnd":        o.SlotEnd,
		"items":          items,
		"subtotal":       o.SubtotalCents,
		"fulfilmentType": o.FulfilmentType,
		"deliveryFee":    o.DeliveryFeeCents,
		"tax":            o.TaxCents,
		"tip":            o.TipCents,
		"discount":       o.DiscountCents,
		"serviceCharge":  o.ServiceChargeCents,
		"total":          totalCents,
		// Absolute, because an email has no page to be relative to. A bare
		// "/track/BB…" was being resolved by the mail client against nothing,
		// which is how it arrived as "http:///track/BB…".
		"trackingUrl": email.ShopLink("/orders/" + o.Reference),
	}
	if o.DiscountCode != nil && *o.DiscountCode != "" {
		data["discountCode"] = *o.DiscountCode
	}

	return s.emailer.Send(ctx, email.Email{
		To:         o.PurchaserEmail,
		Template:   email.TemplateOrderConfirmation,
		BusinessID: businessOf(o),
		Data:       data,
	})
}

// zipInBranchZone mirrors tenancy's own zip_list matching (see
// franchise.zipInZone) — duplicated locally rather than exported from tenancy
// purely for this one check, since it is just a membership test over an
// already-exported field.
func zipInBranchZone(branch *franchiseapi.Branch, postcode string) bool {
	if branch.DeliveryZone.Type != "zip_list" {
		return false
	}
	for _, z := range branch.DeliveryZone.Zips {
		if z == postcode {
			return true
		}
	}
	return false
}

// checkCODAllowed decides whether this branch will let this order leave unpaid.
//
// Both conditions are the branch's to set, and both are checked here on the
// server rather than trusted from the storefront: the checkout hides the
// option when a branch does not accept cash, but hiding a control is not
// enforcing a rule (RBAC-08 — the server re-authorises every mutation).
func checkCODAllowed(branch *franchiseapi.Branch, totalCents int64) error {
	if !branch.AcceptsCashOnDelivery {
		return apperr.New("COD_NOT_AVAILABLE", apperr.CategoryBusinessRule,
			"This outlet does not accept cash on delivery. Please pay by card.")
	}
	if branch.CODMaxOrderCents != nil && totalCents > *branch.CODMaxOrderCents {
		return apperr.New("COD_LIMIT_EXCEEDED", apperr.CategoryBusinessRule,
			"This order is above the cash-on-delivery limit for this outlet. Please pay by card.")
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
