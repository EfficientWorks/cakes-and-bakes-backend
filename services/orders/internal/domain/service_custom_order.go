package domain

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/realtime"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/orders/internal/store"
	"bakehouse/services/orders/ordersapi"
)

/*
 * Custom orders as real orders (F3).
 *
 * The Custom Orders form is still recorded as an enquiry, and now also becomes
 * an order for the chosen store and date: price to be confirmed (£0), unpaid,
 * channel web, source custom_order. That is what puts it on the kitchen
 * display (planning and tickets) and in the admin order list like any other
 * order. Staff price it with the customer and take payment later.
 *
 * It reserves no slot capacity: the slot is the customer's preferred time, not
 * one of the website's bookable windows, and a request the shop has not yet
 * accepted should not crowd out paying customers.
 */

// UseCustomerPhotos wires the check that a photo url is a customer upload.
func (s *Service) UseCustomerPhotos(p catalogapi.CustomerPhotos) { s.photos = p }

// customOrderPlan is a validated request, ready to write.
type customOrderPlan struct {
	branch    *franchiseapi.Branch
	slotStart string
	slotEnd   string
	photoURLs []string
	message   string
	fulfil    string
}

// ValidateCustomOrder implements ordersapi.CustomOrderCreator.
func (s *Service) ValidateCustomOrder(ctx context.Context, req ordersapi.CustomOrderRequest) error {
	_, err := s.planCustomOrder(ctx, req, true)
	return err
}

// planCustomOrder validates a request. strict also requires the store to be
// taking the fulfilment on that date at the requested time (its opening hours,
// closures and lead time, via the slot service); creation runs it relaxed, so
// a slot filling up between the check and the write cannot lose the order.
func (s *Service) planCustomOrder(ctx context.Context, req ordersapi.CustomOrderRequest, strict bool) (*customOrderPlan, error) {
	branch, err := s.branches.GetBranch(ctx, req.BranchID)
	if err != nil || branch == nil {
		return nil, apperr.Validation("Select a store location we recognise.").
			WithFields(apperr.FieldError{Field: "branchId", Code: "UNKNOWN", Message: "Unknown store."})
	}
	if branch.Status != franchiseapi.StatusActive {
		return nil, apperr.New("BRANCH_UNAVAILABLE", apperr.CategoryBusinessRule,
			"This store location is not taking orders at the moment. Please choose another.")
	}

	plan := &customOrderPlan{branch: branch}
	plan.fulfil, err = customOrderFulfilment(req.FulfilmentType)
	if err != nil {
		return nil, err
	}
	if plan.fulfil == ordersapi.FulfilmentDelivery && strings.TrimSpace(req.Street) == "" && strings.TrimSpace(req.Postcode) == "" {
		return nil, apperr.Validation("Add the delivery address.").
			WithFields(apperr.FieldError{Field: "deliveryAddress", Code: "REQUIRED", Message: "Required for delivery."})
	}

	if req.Date.IsZero() {
		return nil, apperr.Validation("Choose the date you need the order.").
			WithFields(apperr.FieldError{Field: "date", Code: "REQUIRED", Message: "Required."})
	}
	if req.Date.Format("2006-01-02") < branchToday(branch) {
		return nil, apperr.Validation("Choose a date from today onwards.").
			WithFields(apperr.FieldError{Field: "date", Code: "PAST", Message: "Must be today or later."})
	}
	if plan.slotStart, plan.slotEnd, err = customOrderSlot(req.SlotStart, req.SlotEnd); err != nil {
		return nil, err
	}
	if err := s.fitCustomOrderSlot(ctx, plan, req, strict); err != nil {
		return nil, err
	}

	plan.message = strings.TrimSpace(req.CakeMessage)
	if utf8.RuneCountInString(plan.message) > ordersapi.CustomOrderMessageMax {
		msg := fmt.Sprintf("The message on the cake can be at most %d characters.", ordersapi.CustomOrderMessageMax)
		return nil, apperr.New("CAKE_MESSAGE_TOO_LONG", apperr.CategoryValidation, msg).
			WithFields(apperr.FieldError{Field: "cakeMessage", Code: "TOO_LONG", Message: msg})
	}

	refs := make([]string, 0, len(req.PhotoRefs))
	for _, r := range req.PhotoRefs {
		if r = strings.TrimSpace(r); r != "" {
			refs = append(refs, r)
		}
	}
	if len(refs) > ordersapi.CustomOrderMaxPhotos {
		return nil, apperr.New("TOO_MANY_PHOTOS", apperr.CategoryValidation,
			fmt.Sprintf("You can attach up to %d photos.", ordersapi.CustomOrderMaxPhotos)).
			WithFields(apperr.FieldError{Field: "photoUrls", Code: "TOO_MANY", Message: "Up to 3 photos."})
	}
	for _, ref := range refs {
		url, err := s.resolvePhoto(ctx, ref)
		if err != nil {
			return nil, err
		}
		plan.photoURLs = append(plan.photoURLs, url)
	}
	return plan, nil
}

// resolvePhoto turns an upload id or url into the stored url, refusing any
// link that is not one of our customer uploads.
func (s *Service) resolvePhoto(ctx context.Context, ref string) (string, error) {
	if s.photos == nil {
		return "", apperr.New("PHOTO_INVALID", apperr.CategoryValidation, "Photos can't be attached right now.")
	}
	url, err := s.photos.ResolveCustomerPhoto(ctx, ref)
	if errors.Is(err, catalogapi.ErrCustomerPhotoNotFound) {
		return "", apperr.New("PHOTO_INVALID", apperr.CategoryValidation,
			"One of the photos wasn't uploaded here. Please upload it again.").
			WithFields(apperr.FieldError{Field: "photoUrls", Code: "UNKNOWN", Message: "Upload the photo again."})
	}
	if err != nil {
		return "", apperr.Internal(err)
	}
	return url, nil
}

// CreateCustomOrder implements ordersapi.CustomOrderCreator.
func (s *Service) CreateCustomOrder(ctx context.Context, req ordersapi.CustomOrderRequest) (*ordersapi.Order, error) {
	if strings.TrimSpace(req.EnquiryID) == "" {
		return nil, apperr.Validation("A custom order needs its enquiry.")
	}
	plan, err := s.planCustomOrder(ctx, req, false)
	if err != nil {
		return nil, err
	}
	key := "custom_order_" + req.EnquiryID
	if existing, err := s.store.FindOrderByIdempotencyKey(ctx, key); err == nil {
		return existing, nil
	}

	reference, err := store.NewOrderReference()
	if err != nil {
		return nil, apperr.Internal(err)
	}
	first, last := splitName(req.Name)
	phone := trimPtr(&req.Phone)
	source, enquiryID := ordersapi.SourceCustomOrder, req.EnquiryID
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Custom cake"
	}
	priceNote := "Custom order: price to be confirmed with the customer. Not paid online."
	notes := strings.TrimSpace(req.Notes)

	order := ordersapi.Order{
		Reference: reference, BranchID: plan.branch.ID, Channel: "web", FulfilmentType: plan.fulfil,
		RecipientFirstName: first, RecipientLastName: last, RecipientPhone: phone,
		PurchaserFirstName: first, PurchaserLastName: last, PurchaserEmail: strings.TrimSpace(req.Email), PurchaserPhone: phone,
		ScheduledDate: req.Date, SlotStart: plan.slotStart, SlotEnd: plan.slotEnd,
		Currency: plan.branch.Currency, Status: ordersapi.StatusPlaced,
		PaymentStatus: "pending", PaymentMethod: ordersapi.PaymentMethodNone,
		IdempotencyKey: key, Notes: &priceNote,
		Source: &source, EnquiryID: &enquiryID,
	}
	if plan.fulfil == ordersapi.FulfilmentDelivery {
		order.DeliveryStreet = trimPtr(&req.Street)
		order.DeliveryCity = trimPtr(&req.City)
		order.DeliveryPostcode = trimPtr(&req.Postcode)
	}
	item := ordersapi.OrderItem{
		ProductName: title, Quantity: 1, ProductionStatus: "pending",
		CakeMessage: trimPtr(&plan.message), PhotoURLs: plan.photoURLs,
	}
	// The form's details ride on the line, where the kitchen reads them.
	item.Comments = trimPtr(&notes)

	orderID, err := s.store.CreateOrderTx(ctx, order, []ordersapi.OrderItem{item})
	isReplay := errors.Is(err, ordersapi.ErrIdempotentReplay)
	if err != nil && !isReplay {
		return nil, apperr.Internal(err)
	}
	if !isReplay {
		// Accepted on arrival, like a website order, so the kitchen sees it;
		// the price is settled with the customer separately.
		if _, err := s.store.UpdateOrderStatus(ctx, orderID, ordersapi.StatusConfirmed,
			"system", nil, "Custom order request: price to be confirmed"); err != nil {
			slog.Warn("custom order could not be accepted", "order", reference, "error", err)
		}
	}
	created, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if !isReplay {
		s.publishOrder(realtime.EventOrderCreated, created, "")
	}
	return created, nil
}

func customOrderFulfilment(v string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "pickup", "collection", "collect":
		return ordersapi.FulfilmentPickup, nil
	case "delivery":
		return ordersapi.FulfilmentDelivery, nil
	}
	return "", apperr.Validation("fulfilmentType must be pickup or delivery.").
		WithFields(apperr.FieldError{Field: "fulfilmentType", Code: "INVALID", Message: "pickup or delivery."})
}

// customOrderSlot reads "HH:MM" times: no start is 09:00, no end is 30
// minutes after the start.
func customOrderSlot(start, end string) (string, string, error) {
	start, end = strings.TrimSpace(start), strings.TrimSpace(end)
	if start == "" {
		start = "09:00"
	}
	st, err := parseClock(start)
	if err != nil {
		return "", "", apperr.Validation("slotStart must be a time like 14:00.").
			WithFields(apperr.FieldError{Field: "slotStart", Code: "INVALID", Message: "HH:MM."})
	}
	var en time.Time
	if end == "" {
		en = st.Add(30 * time.Minute)
		if en.Day() != st.Day() {
			en = st // a 23:45 start stays a point in time
		}
	} else if en, err = parseClock(end); err != nil || en.Before(st) {
		return "", "", apperr.Validation("slotEnd must be a time like 14:30, after slotStart.").
			WithFields(apperr.FieldError{Field: "slotEnd", Code: "INVALID", Message: "HH:MM after the start."})
	}
	return st.Format("15:04"), en.Format("15:04"), nil
}

func parseClock(v string) (time.Time, error) {
	if t, err := time.Parse("15:04", v); err == nil {
		return t, nil
	}
	return time.Parse("15:04:05", v)
}

// branchToday is the store's local calendar date.
func branchToday(b *franchiseapi.Branch) string {
	now := time.Now()
	if loc, err := time.LoadLocation(b.Timezone); err == nil && b.Timezone != "" {
		now = now.In(loc)
	}
	return now.Format("2006-01-02")
}

// splitName turns "Priya Shah Patel" into "Priya" and "Shah Patel".
func splitName(full string) (string, string) {
	parts := strings.Fields(full)
	switch len(parts) {
	case 0:
		return "Customer", ""
	case 1:
		return parts[0], ""
	}
	return parts[0], strings.Join(parts[1:], " ")
}

// fitCustomOrderSlot puts the order on one of the store's own slots for the
// day: the requested time when the store offers it, else the day's first.
// Strict: a day with no slots (closed, past the lead time, fully booked) or a
// time the store does not offer is refused. Relaxed: the parsed time stands.
func (s *Service) fitCustomOrderSlot(ctx context.Context, plan *customOrderPlan, req ordersapi.CustomOrderRequest, strict bool) error {
	days, _, err := s.GetAvailableSlots(ctx, plan.branch.ID, plan.fulfil, req.Date, 1)
	if err != nil {
		if strict {
			return err
		}
		return nil
	}
	want := req.Date.Format("2006-01-02")
	var slots []ordersapi.SlotCapacity
	for _, d := range days {
		if d.Date == want {
			slots = d.Slots
		}
	}
	if len(slots) == 0 {
		if !strict {
			return nil
		}
		return apperr.New("CUSTOM_ORDER_DATE_UNAVAILABLE", apperr.CategoryBusinessRule,
			"The store can't take a "+plan.fulfil+" order for that day. Please choose another date.").
			WithFields(apperr.FieldError{Field: "date", Code: "UNAVAILABLE", Message: "Choose another date."})
	}
	if strings.TrimSpace(req.SlotStart) == "" {
		plan.slotStart, plan.slotEnd = hhmm5(slots[0].SlotStart), hhmm5(slots[0].SlotEnd)
		return nil
	}
	for _, sc := range slots {
		if hhmm5(sc.SlotStart) == plan.slotStart {
			plan.slotStart, plan.slotEnd = hhmm5(sc.SlotStart), hhmm5(sc.SlotEnd)
			return nil
		}
	}
	if !strict {
		return nil
	}
	return apperr.New("SLOT_NO_LONGER_AVAILABLE", apperr.CategoryBusinessRule,
		"That time isn't available on that day. Please choose another time.").
		WithFields(apperr.FieldError{Field: "slotStart", Code: "UNAVAILABLE", Message: "Choose another time."})
}
