package domain

import (
	"bakehouse/pkg/realtime"
	"bakehouse/pkg/timex"
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/email"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/orders/internal/store"
	"bakehouse/services/orders/ordersapi"
)

// --- Public tracking & customer self-service (SF-32, ACC-08..ACC-12) --------

func (s *Service) GetOrderByReference(ctx context.Context, reference string) (*ordersapi.Order, error) {
	o, err := s.store.GetOrderByReference(ctx, reference)
	if err != nil {
		return nil, notFoundOrInternal(err, "Order not found.")
	}
	return o, nil
}

func (s *Service) ListOrdersForCustomer(ctx context.Context, customerID, status string, page, limit int) ([]ordersapi.Order, int, error) {
	orders, total, err := s.store.ListOrders(ctx, ordersapi.ListOrdersFilter{CustomerID: customerID, Status: status, Page: page, Limit: limit})
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}
	return orders, total, nil
}

func (s *Service) GetOrderForCustomer(ctx context.Context, customerID, orderID string) (*ordersapi.Order, error) {
	o, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, notFoundOrInternal(err, "Order not found.")
	}
	if o.CustomerID == nil || *o.CustomerID != customerID {
		return nil, apperr.NotFound("Order not found.")
	}
	return o, nil
}

// GetCustomerAddress fetches address details from the customer_addresses table
// for use in order response DTOs. All three return values are nil when no
// address row exists or customerID is blank.
func (s *Service) GetCustomerAddress(ctx context.Context, customerID string) (address, city, postcode *string) {
	return s.store.GetCustomerAddress(ctx, customerID)
}

// GetCustomerDetails fetches full customer details including address from customers and customer_addresses tables.
func (s *Service) GetCustomerDetails(ctx context.Context, customerID string) *store.CustomerDetails {
	return s.store.GetCustomerDetails(ctx, customerID)
}

// Reorder re-validates every line against current availability and pricing
// before adding to a fresh cart, rather than blindly replaying the old
// order (ACC-12) — items that are no longer available are skipped and
// reported back as warnings instead of failing the whole reorder.
func (s *Service) Reorder(ctx context.Context, customerID, orderID string) (*ordersapi.Cart, []string, error) {
	o, err := s.GetOrderForCustomer(ctx, customerID, orderID)
	if err != nil {
		return nil, nil, err
	}

	var warnings []string
	var cart *ordersapi.Cart
	for _, item := range o.Items {
		_, _, _, err := s.AddItem(ctx, CartIdentity{CustomerID: &customerID}, AddItemRequest{
			BranchID: o.BranchID, ProductID: item.ProductID, VariantID: item.VariantID,
			FlavourIDs: idsOf(item.SelectedFlavours), AddonIDs: idsOf(item.SelectedAddons),
			PresentationID: idOf(item.SelectedPresentation), WrapColourID: idOf(item.SelectedWrapColour),
			Quantity: item.Quantity, IsGift: o.IsGift,
			RecipientFirst: &o.RecipientFirstName, RecipientLast: &o.RecipientLastName, GiftMessage: o.GiftMessage,
		})
		if err != nil {
			warnings = append(warnings, item.ProductName+" is currently unavailable and was not added.")
			continue
		}
	}
	cart, err = s.store.GetCartByCustomer(ctx, customerID)
	if err != nil {
		return nil, warnings, notFoundOrInternal(err, "Could not build a cart from this order.")
	}
	return cart, warnings, nil
}

// ClaimGuestOrder links a guest order to a newly-created (or existing)
// account, once the purchaser email matches (ACC-14).
func (s *Service) ClaimGuestOrder(ctx context.Context, customerID, reference, email string) (*ordersapi.Order, error) {
	o, err := s.store.GetOrderByReference(ctx, reference)
	if err != nil {
		return nil, notFoundOrInternal(err, "Order not found.")
	}
	if o.CustomerID != nil {
		return nil, apperr.New("ORDER_ALREADY_CLAIMED", apperr.CategoryConflict, "This order is already linked to an account.")
	}
	if !equalFoldEmail(o.PurchaserEmail, email) {
		return nil, apperr.New("EMAIL_MISMATCH", apperr.CategoryAuthorization, "That email does not match the order's purchaser email.")
	}
	if err := s.store.ClaimOrder(ctx, o.ID, customerID); err != nil {
		return nil, apperr.Internal(err)
	}
	return s.store.GetOrderByID(ctx, o.ID)
}

func equalFoldEmail(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func noticeEmailForOrder(o *ordersapi.Order) string {
	if o == nil {
		return ""
	}
	return strings.TrimSpace(o.PurchaserEmail)
}

// --- Cancellation (ORD-16, ACC-10) -------------------------------------------

type CancellationOutcome struct {
	RefundCents int64
	RefundPct   int
}

// CancelOrder applies the branch's cancellation tiers: full refund before
// the same-day cut-off, a configurable partial refund in the middle
// window, no automatic refund once production has begun (blocked entirely
// — ordersapi.IsAmendable already excludes in_production and later).
func (s *Service) CancelOrder(ctx context.Context, actorType string, actorID *string, orderID, reason string) (*CancellationOutcome, error) {
	return s.cancelOrder(ctx, actorType, actorID, orderID, reason, false)
}

// cancelOrder is CancelOrder; fromCourier skips the in-production checks,
// because a delivery reaches the courier only once it is ready, and a
// cancellation made at the courier has to land on an order that far along.
func (s *Service) cancelOrder(ctx context.Context, actorType string, actorID *string, orderID, reason string, fromCourier bool) (*CancellationOutcome, error) {
	o, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, notFoundOrInternal(err, "Order not found.")
	}
	/*
	 * Order matters here, and it was the wrong way round.
	 *
	 * A cancelled order is not amendable, so the production check below caught
	 * it first and the already-cancelled branch was unreachable: cancelling
	 * something twice reported that it "has already entered production", which
	 * is both untrue and unactionable — the caller goes looking for a kitchen
	 * that never had it.
	 *
	 * The same went for anything else past production. An order that had been
	 * delivered, returned or refunded was told it was in production, when what
	 * the counter needs to hear is that it is finished and a return or a refund
	 * is the way back.
	 */
	if o.Status == ordersapi.StatusCancelled {
		return nil, apperr.New("ORDER_ALREADY_CANCELLED", apperr.CategoryConflict, "This order is already cancelled.")
	}
	switch o.Status {
	case ordersapi.StatusFulfilled, ordersapi.StatusReturned, ordersapi.StatusRefunded:
		return nil, apperr.New("ORDER_CLOSED", apperr.CategoryBusinessRule,
			"This order is already closed. Record a return or a refund instead.")
	}
	if !fromCourier && !ordersapi.IsAmendable(o.Status) {
		return nil, apperr.New("ORDER_IN_PRODUCTION", apperr.CategoryBusinessRule, "This order has already entered production and cannot be cancelled automatically. Contact the branch.")
	}
	// KDS: once the kitchen has started it, it is not cancellable here either.
	if !fromCourier && ordersapi.KOTInKitchen(o.KOTStatus) {
		return nil, apperr.New("ORDER_IN_PREPARATION", apperr.CategoryBusinessRule,
			"The kitchen is already working on this order ("+o.KOTStatus+"); it cannot be cancelled.").
			WithMeta(map[string]any{"kotStatus": o.KOTStatus})
	}

	pct := s.cancellationRefundPct(ctx, o)
	// The tip is never refunded, on a cancellation either.
	refundCents := refundableTotal(o) * int64(pct) / 100

	issuedBy := ""
	if actorID != nil {
		issuedBy = *actorID
	}
	if s.cards != nil {
		// Back to whichever cards paid — a website charge, or Stripe
		// payment links on a counter order. Cash is handed back at the counter.
		if _, err := s.refundToCards(ctx, o, "", refundCents, false, reason, issuedBy); err != nil {
			return nil, err
		}
		if _, err := s.cancelOpenLink(ctx, o.ID); err != nil {
			slog.Warn("cancel: could not expire payment link", "order", o.Reference, "error", err)
		}
	} else if refundCents > 0 && o.ProviderPaymentID != nil {
		// Same rule as an explicit refund: a placeholder payment id is not a
		// real charge, and cancelling such an order must not fail on Stripe
		// not knowing it.
		if refundableAtProvider(o.ProviderPaymentID) {
			if _, err := s.payments.Refund(ctx, o.ID, *o.ProviderPaymentID, refundCents, reason, issuedBy); err != nil {
				return nil, apperr.Wrap(err, "REFUND_FAILED", apperr.CategoryUpstream, "Refund could not be processed. Please contact support.")
			}
		}
	}

	/*
	 * Only money that was taken can be given back.
	 *
	 * The refund percentage says how much of the order the customer is owed,
	 * and it was being written as the payment status regardless of whether
	 * anything had ever been captured. So cancelling a cash-on-delivery order
	 * — where nothing is paid until the door — left it reading "Refunded" to
	 * a customer who had not paid a penny.
	 *
	 * An uncaptured order keeps the status it had. Cancelled is already on
	 * the order itself; the payment status should say what happened to the
	 * money, and for these the answer is "nothing did".
	 */
	paymentStatus := o.PaymentStatus
	if o.PaymentStatus == ordersapi.PaymentCaptured {
		switch {
		case pct >= 100:
			paymentStatus = ordersapi.PaymentRefunded
		case pct > 0:
			paymentStatus = ordersapi.PaymentPartialRefund
		}
	}
	if paymentStatus != o.PaymentStatus {
		_ = s.store.UpdatePaymentStatus(ctx, o.ID, paymentStatus)
	}

	if o.SlotCapacityID != nil {
		_ = s.releaseSlot(ctx, *o.SlotCapacityID)
	}
	if _, err := s.store.UpdateOrderStatus(ctx, o.ID, ordersapi.StatusCancelled, actorType, actorID, reason); err != nil {
		return nil, apperr.Internal(err)
	}

	if s.emailer != nil {
		s.sendSimpleNotice(ctx, o, email.TemplateOrderCancelled, reason)
	}
	s.publishOrderByID(ctx, realtime.EventOrderCancelled, o.ID)

	return &CancellationOutcome{RefundCents: refundCents, RefundPct: pct}, nil
}

// cancellationRefundPct: full refund if more than the branch's lead-time
// hours remain before the slot, otherwise a 50% partial refund. Branches
// with fully custom tiers are a later enhancement — this is the two-tier
// version the BRD's worked example describes.
func (s *Service) cancellationRefundPct(ctx context.Context, o *ordersapi.Order) int {
	cfg, err := s.store.GetScheduleConfig(ctx, o.BranchID)
	if err != nil {
		return 100
	}
	slotTime, err := time.Parse("15:04:05", normaliseTime(o.SlotStart))
	if err != nil {
		return 100
	}
	slotDateTime := time.Date(o.ScheduledDate.Year(), o.ScheduledDate.Month(), o.ScheduledDate.Day(), slotTime.Hour(), slotTime.Minute(), 0, 0, time.UTC)
	leadHours := cfg.DefaultLeadHours
	if leadHours <= 0 {
		leadHours = 2
	}
	if time.Until(slotDateTime) >= time.Duration(leadHours)*time.Hour {
		return 100
	}
	return 50
}

func (s *Service) releaseSlot(ctx context.Context, slotID string) error {
	tx, err := s.store.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := store.ReleaseSlot(ctx, tx, slotID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

/*
 * A notice about an order — cancelled, refunded, and so on.
 *
 * Best effort by design: an order is cancelled whether or not the customer's
 * mail server is reachable, and failing the cancellation because of the email
 * would be worse than the email not arriving.
 *
 * But it is no longer *silent*. The failure used to be dropped on the floor,
 * so "the customer never got the cancellation email" was unanswerable — there
 * was nothing anywhere saying whether it had even been attempted, let alone
 * why it failed.
 */
func (s *Service) sendSimpleNotice(ctx context.Context, o *ordersapi.Order, template, reason string) {
	if s.emailer == nil {
		return
	}
	to := noticeEmailForOrder(o)
	if to == "" {
		slog.Warn("order notice not sent: no purchaser email",
			"template", template, "reference", o.Reference)
		return
	}
	if err := s.emailer.Send(ctx, email.Email{
		To:         to,
		Template:   template,
		BusinessID: businessOf(o),
		Data: map[string]interface{}{
			"reference": o.Reference, "recipientName": o.RecipientFirstName + " " + o.RecipientLastName,
			"reason": reason,
		},
	}); err != nil {
		slog.Error("order notice failed to send",
			"template", template, "reference", o.Reference, "to", to, "cause", err)
		return
	}
	slog.Info("order notice sent",
		"template", template, "reference", o.Reference, "to", to)
}

// --- Status transitions & manual fulfilment (ORD-06, ADM-05) ----------------

func (s *Service) MarkFulfilled(ctx context.Context, actorID string, orderID, note string) error {
	previous, err := s.store.UpdateOrderStatus(ctx, orderID, ordersapi.StatusFulfilled, "staff", &actorID, note)
	if err != nil {
		return notFoundOrInternal(err, "Order not found.")
	}
	s.recordSalesOnFulfilment(ctx, orderID, previous, ordersapi.StatusFulfilled)
	s.publishOrderByID(ctx, realtime.EventOrderStatusChanged, orderID)
	return nil
}

func (s *Service) SetStatus(ctx context.Context, actorID string, orderID, status, note string) error {
	return s.setStatus(ctx, "staff", &actorID, orderID, status, note)
}

// setStatus is SetStatus for any actor: the courier webhook moves orders as
// "system", with no staff id.
func (s *Service) setStatus(ctx context.Context, actorType string, actorID *string, orderID, status, note string) error {
	// Checked here rather than left to the database's CHECK constraint. Until
	// this existed, asking for a status the workflow does not have — a
	// plausible "completed" or "preparing" — came back as a 500 with nothing
	// to say what was wrong, when it is plainly the caller's mistake and the
	// list of valid values is short enough to print.
	if !ordersapi.IsValidStatus(status) {
		return apperr.Validation("\"" + status + "\" is not an order status. Valid values: " +
			strings.Join(ordersapi.AllStatuses(), ", ") + ".")
	}

	previous, err := s.store.UpdateOrderStatus(ctx, orderID, status, actorType, actorID, note)
	if err != nil {
		return notFoundOrInternal(err, "Order not found.")
	}
	s.recordSalesOnFulfilment(ctx, orderID, previous, status)
	defer s.publishOrderByID(ctx, realtime.EventOrderStatusChanged, orderID)

	// Send customer notification on key status transitions.
	if s.emailer != nil {
		switch status {
		case ordersapi.StatusReady:
			if o, err := s.store.GetOrderByID(ctx, orderID); err == nil {
				branchName := ""
				branchAddress := ""
				if s.branches != nil {
					if b, err := s.branches.GetBranch(ctx, o.BranchID); err == nil && b != nil {
						branchName = b.Name
						branchAddress = b.Street + ", " + b.City + ", " + b.State + " " + b.Postcode
					}
				}
				to := noticeEmailForOrder(o)
				if to != "" {
					_ = s.emailer.Send(ctx, email.Email{
						To:         to,
						Template:   email.TemplateOrderReady,
						BusinessID: businessOf(o),
						Data: map[string]interface{}{
							"reference":     o.Reference,
							"recipientName": o.RecipientFirstName + " " + o.RecipientLastName,
							"branchName":    branchName,
							"branchAddress": branchAddress,
						},
					})
				}
			}
		case ordersapi.StatusFulfilled:
			if o, err := s.store.GetOrderByID(ctx, orderID); err == nil {
				to := noticeEmailForOrder(o)
				if to != "" {
					_ = s.emailer.Send(ctx, email.Email{
						To:         to,
						Template:   email.TemplateOrderDelivered,
						BusinessID: businessOf(o),
						Data: map[string]interface{}{
							"reference":     o.Reference,
							"recipientName": o.RecipientFirstName + " " + o.RecipientLastName,
						},
					})
				}
			}
		}
	}
	return nil
}

// recordSalesOnFulfilment feeds the catalogue's popularity rollup the moment
// an order actually reaches `fulfilled`.
//
// Guarded on the transition, not on the destination: staff can mark an
// already-delivered order as delivered again, and counting its products a
// second time would inflate the ranking every time someone re-saved a status.
//
// Failures are logged and swallowed. The order *is* fulfilled — the customer
// has their bouquet — and refusing that because a ranking column did not
// update would be the wrong trade. See catalogapi.SalesRecorder.
func (s *Service) recordSalesOnFulfilment(ctx context.Context, orderID, previous, next string) {
	if s.sales == nil || next != ordersapi.StatusFulfilled || previous == ordersapi.StatusFulfilled {
		return
	}

	order, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		slog.Error("could not load order for popularity rollup", "order", orderID, "error", err)
		return
	}

	sales := make([]catalogapi.ProductSale, 0, len(order.Items))
	for _, item := range order.Items {
		sales = append(sales, catalogapi.ProductSale{ProductID: item.ProductID, Quantity: item.Quantity})
	}
	if err := s.sales.RecordProductSales(ctx, sales); err != nil {
		slog.Error("could not record product sales", "order", orderID, "error", err)
	}
}

// --- Amendments (ORD-06 to ORD-15) -------------------------------------------

type AmendOrderRequest struct {
	RecipientFirstName   *string
	RecipientLastName    *string
	RecipientPhone       *string
	GiftMessage          *string
	DeliveryInstructions *string
	DeliveryStreet       *string
	DeliveryCity         *string
	DeliveryState        *string
	DeliveryPostcode     *string
	BusinessName         *string
	DeliveryAddressType  *string
	ScheduledDate        *time.Time
	SlotStart            *string
	Reason               string
	// KDS: flag the order "Amended" on the kitchen display.
	MarkAmended bool
}

func (s *Service) AmendOrder(ctx context.Context, actorID, actorRole string, orderID string, req AmendOrderRequest) (*ordersapi.Order, error) {
	if req.Reason == "" {
		return nil, apperr.Validation("A reason is required for every amendment.")
	}
	if req.DeliveryAddressType != nil && strings.TrimSpace(*req.DeliveryAddressType) != "" &&
		!ordersapi.IsValidAddressType(strings.TrimSpace(*req.DeliveryAddressType)) {
		return nil, apperr.Validation("deliveryAddressType must be \"residence\" or \"business\".")
	}

	before := map[string]interface{}{}
	after := map[string]interface{}{}
	var priceDelta int64

	var newSlotID *string
	err := s.store.AmendOrderTx(ctx, orderID, func(tx pgx.Tx, o *ordersapi.Order) error {
		if req.RecipientFirstName != nil {
			before["recipientFirstName"], after["recipientFirstName"] = o.RecipientFirstName, *req.RecipientFirstName
			o.RecipientFirstName = *req.RecipientFirstName
		}
		if req.RecipientLastName != nil {
			before["recipientLastName"], after["recipientLastName"] = o.RecipientLastName, *req.RecipientLastName
			o.RecipientLastName = *req.RecipientLastName
		}
		if req.RecipientPhone != nil {
			before["recipientPhone"], after["recipientPhone"] = o.RecipientPhone, *req.RecipientPhone
			o.RecipientPhone = req.RecipientPhone
		}
		if req.GiftMessage != nil {
			before["giftMessage"], after["giftMessage"] = o.GiftMessage, *req.GiftMessage
			o.GiftMessage = req.GiftMessage
		}
		if req.DeliveryInstructions != nil {
			before["deliveryInstructions"], after["deliveryInstructions"] = o.DeliveryInstructions, *req.DeliveryInstructions
			o.DeliveryInstructions = req.DeliveryInstructions
		}
		if req.DeliveryStreet != nil {
			o.DeliveryStreet = req.DeliveryStreet
		}
		if req.DeliveryCity != nil {
			o.DeliveryCity = req.DeliveryCity
		}
		if req.DeliveryState != nil {
			o.DeliveryState = req.DeliveryState
		}
		if req.DeliveryPostcode != nil {
			o.DeliveryPostcode = req.DeliveryPostcode
		}
		if req.BusinessName != nil {
			before["businessName"], after["businessName"] = o.BusinessName, *req.BusinessName
			o.BusinessName = trimPtr(req.BusinessName)
		}
		if req.DeliveryAddressType != nil {
			before["deliveryAddressType"], after["deliveryAddressType"] = o.DeliveryAddressType, *req.DeliveryAddressType
			o.DeliveryAddressType = trimPtr(req.DeliveryAddressType)
		}

		/*
		 * Either half of the schedule can be changed on its own.
		 *
		 * This used to require both, so "move it to Friday, same time" and
		 * "same day, but at four" both fell straight through and changed
		 * nothing — the amend returned success and the order kept its old
		 * slot. The half that was not sent is taken from the order, which is
		 * what "same time" means.
		 */
		if req.ScheduledDate != nil || req.SlotStart != nil {
			newDate := o.ScheduledDate
			if req.ScheduledDate != nil {
				newDate = *req.ScheduledDate
			}
			newStart := o.SlotStart
			if req.SlotStart != nil {
				newStart = *req.SlotStart
			}

			before["scheduledDate"], before["slotStart"] = o.ScheduledDate.Format("2006-01-02"), o.SlotStart
			after["scheduledDate"], after["slotStart"] = newDate.Format("2006-01-02"), newStart

			cfg, err := s.store.GetScheduleConfig(ctx, o.BranchID)
			if err != nil {
				return err
			}
			slotEnd := addMinutesToTime(newStart, fulfilmentSlotMinutes)
			newSlot, err := s.store.GetOrCreateSlot(ctx, o.BranchID, newDate, newStart, slotEnd, o.FulfilmentType, cfg.MaxOrdersPerSlot)
			if err != nil {
				return err
			}
			if err := store.ReserveSlot(ctx, tx, newSlot.ID); err != nil {
				return err
			}
			if o.SlotCapacityID != nil {
				if err := store.ReleaseSlot(ctx, tx, *o.SlotCapacityID); err != nil {
					return err
				}
			}
			newSlotID = &newSlot.ID
			o.ScheduledDate = newDate
			o.SlotStart = newStart
			o.SlotEnd = slotEnd
		}

		_, err := tx.Exec(ctx, `
			UPDATE orders SET recipient_first_name=$2, recipient_last_name=$3, recipient_phone=$4, gift_message=$5,
				delivery_instructions=$6, delivery_street=$7, delivery_city=$8, delivery_state=$9, delivery_postcode=$10,
				scheduled_date=$11, slot_start=$12, slot_end=$13, slot_capacity_id=COALESCE($14, slot_capacity_id),
				business_name=$15, delivery_address_type=$16, updated_at=NOW()
			WHERE id = $1`,
			o.ID, o.RecipientFirstName, o.RecipientLastName, o.RecipientPhone, o.GiftMessage,
			o.DeliveryInstructions, o.DeliveryStreet, o.DeliveryCity, o.DeliveryState, o.DeliveryPostcode,
			o.ScheduledDate, o.SlotStart, o.SlotEnd, newSlotID, o.BusinessName, o.DeliveryAddressType)
		return err
	}, ordersapi.OrderAmendment{ActorID: &actorID, ActorRole: actorRole, Reason: req.Reason, BeforeValue: before, AfterValue: after, PriceDeltaCents: priceDelta})
	if err != nil {
		if err == store.ErrAmendmentClosed {
			return nil, apperr.New("ORDER_IN_PRODUCTION", apperr.CategoryBusinessRule, "This order has entered production; the amendment window has closed.")
		}
		if err == ordersapi.ErrSlotFull {
			return nil, apperr.New("SLOT_UNAVAILABLE", apperr.CategoryConflict, "The requested slot is full. Please choose another time.")
		}
		return nil, notFoundOrInternal(err, "Order not found.")
	}

	event := realtime.EventOrderUpdated
	if req.MarkAmended {
		if err := s.store.MarkAmended(ctx, orderID, timex.EventTimeFrom(ctx)); err != nil {
			return nil, apperr.Internal(err)
		}
		event = realtime.EventOrderAmended
	}
	updated, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if s.emailer != nil {
		s.sendSimpleNotice(ctx, updated, email.TemplateOrderAmended, req.Reason)
	}
	s.publishOrder(event, updated, "")
	return updated, nil
}

func addMinutesToTime(hhmm string, mins int) string {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return hhmm
	}
	return t.Add(time.Duration(mins) * time.Minute).Format("15:04")
}

// businessOf is the business an order's emails go out as. Set explicitly
// rather than left to the request: some sends run from a payment webhook,
// which acts for no business.
func businessOf(o *ordersapi.Order) string {
	if o == nil || o.BusinessID == nil {
		return ""
	}
	return *o.BusinessID
}
