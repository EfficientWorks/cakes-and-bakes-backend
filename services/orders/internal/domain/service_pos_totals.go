package domain

import (
	"context"
	"fmt"
	"math"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/services/orders/ordersapi"
)

/* ------------------------------------------------------------- arithmetic */

/*
 * The total, computed from the lines.
 *
 * Charges arrive from the till because tax rate, delivery and service are
 * decisions made at the counter; the subtotal never does, because it is a fact
 * about the lines that were stored. A percentage discount is applied to the
 * subtotal here rather than trusted from the request, so what the customer was
 * shown and what the receipt says cannot come apart.
 */
func (s *Service) recalculatePOSTotals(ctx context.Context, orderID string, charges ordersapi.OrderTotals) error {
	lines, err := s.store.LinesForTotals(ctx, orderID)
	if err != nil {
		return apperr.Internal(err)
	}

	var subtotal int64
	for _, l := range lines {
		kept := l.Quantity - l.ReturnedQuantity
		if kept <= 0 {
			continue
		}
		// Per-unit, so a partly returned line contributes only what was kept,
		// and keeps only the kept units' share of its own discount (the
		// returned units' share went back with them, see returnPricer).
		perUnit := l.UnitPriceCents
		subtotal += perUnit * int64(kept)
		if l.DiscountCents == 0 && l.DiscountPct != nil && *l.DiscountPct > 0 {
			// A line discount kept only as a percentage, as the return prices it.
			subtotal -= int64(math.Round(float64(perUnit*int64(kept)) * *l.DiscountPct / 100))
		} else if l.Quantity > 0 && kept < l.Quantity {
			subtotal -= int64(math.Round(float64(l.DiscountCents) * float64(kept) / float64(l.Quantity)))
		} else {
			subtotal -= l.DiscountCents
		}
	}
	if subtotal < 0 {
		subtotal = 0
	}

	discount := charges.DiscountCents
	if charges.DiscountPct != nil && *charges.DiscountPct > 0 {
		pct := *charges.DiscountPct
		if pct > 100 {
			pct = 100
		}
		// Rounded to the nearest cent: a customer offered 10% off £9.99 is
		// owed a penny one way or the other, and truncating always favours
		// the shop.
		discount = int64(math.Round(float64(subtotal) * pct / 100))
	}
	if discount > subtotal {
		discount = subtotal
	}

	// Tax follows the rate unless the till set the cents itself this time.
	if !charges.TaxExplicit && charges.TaxPct != nil && *charges.TaxPct > 0 {
		charges.TaxCents = taxOn(subtotal-discount, charges.TaxPct)
	}

	total := subtotal - discount + charges.TaxCents + charges.DeliveryFeeCents +
		charges.ServiceChargeCents + charges.TipCents
	if total < 0 {
		total = 0
	}

	return s.applyTotals(ctx, orderID, ordersapi.OrderTotals{
		SubtotalCents:      subtotal,
		DiscountCents:      discount,
		DiscountPct:        charges.DiscountPct,
		TaxCents:           charges.TaxCents,
		TaxPct:             charges.TaxPct,
		DeliveryFeeCents:   charges.DeliveryFeeCents,
		ServiceChargeCents: charges.ServiceChargeCents,
		TipCents:           charges.TipCents,
		TotalCents:         total,
	})
}

// recalculatePOSTotalsFromStored recomputes using the charges already on the
// order — for a return, where nothing about tax or service has changed.
func (s *Service) recalculatePOSTotalsFromStored(ctx context.Context, orderID string) error {
	order, err := s.store.GetOrderByID(ctx, orderID)
	if err != nil {
		return apperr.Internal(err)
	}
	return s.recalculatePOSTotals(ctx, orderID, ordersapi.OrderTotals{
		DiscountPct:        order.DiscountPct,
		DiscountCents:      order.DiscountCents,
		TaxCents:           order.TaxCents,
		TaxPct:             s.orderTaxPct(ctx, order),
		DeliveryFeeCents:   order.DeliveryFeeCents,
		ServiceChargeCents: order.ServiceChargeCents,
		TipCents:           order.TipCents,
	})
}

func (s *Service) applyTotals(ctx context.Context, orderID string, t ordersapi.OrderTotals) error {
	if err := s.store.ApplyOrderTotals(ctx, orderID, t); err != nil {
		return notFoundOrInternal(err, "Order not found.")
	}
	return nil
}

func validatePOSItems(items []ordersapi.POSItem) error {
	for i, it := range items {
		where := fmt.Sprintf("items[%d]", i)
		/*
		 * A product is required to add a line, not to change one.
		 * For manual / counter items (e.g., custom candle, non-catalog item),
		 * productID can be empty as long as a product name is provided.
		 *
		 * An edit names the line by its id and sends what is changing — a
		 * quantity, a status, the round it is on. Demanding the product again
		 * would make the till echo back fields it is not touching, and the
		 * only thing that could do is let them drift from what is stored.
		 */
		if it.ID == "" && strings.TrimSpace(it.ProductID) == "" && strings.TrimSpace(it.ProductName) == "" {
			return apperr.Validation("Every new line needs a product or item name.").
				WithFields(apperr.FieldError{Field: where + ".productName", Code: "REQUIRED", Message: "Required."})
		}
		if it.ID == "" {
			if it.Quantity <= 0 {
				return apperr.Validation("A quantity must be at least one.").
					WithFields(apperr.FieldError{Field: where + ".quantity", Code: "MIN", Message: "At least 1."})
			}
		} else {
			if it.Quantity < 0 {
				return apperr.Validation("A quantity cannot be negative.").
					WithFields(apperr.FieldError{Field: where + ".quantity", Code: "MIN", Message: "Zero or more."})
			}
		}
		if it.UnitPriceCents < 0 || it.LineTotalCents < 0 || it.DiscountCents < 0 {
			return apperr.Validation("A price cannot be negative.").
				WithFields(apperr.FieldError{Field: where + ".unitPriceCents", Code: "MIN", Message: "Zero or more."})
		}
		if it.DiscountPct != nil && (*it.DiscountPct < 0 || *it.DiscountPct > 100) {
			return apperr.Validation("A discount is between 0 and 100 percent.").
				WithFields(apperr.FieldError{Field: where + ".discountPct", Code: "RANGE", Message: "0 to 100."})
		}
	}
	return nil
}

func posCharges(req POSOrderRequest) ordersapi.OrderTotals {
	return ordersapi.OrderTotals{
		DiscountPct:        req.DiscountPct,
		DiscountCents:      req.DiscountCents,
		TaxCents:           req.TaxCents,
		DeliveryFeeCents:   req.DeliveryFeeCents,
		ServiceChargeCents: req.ServiceChargeCents,
		TipCents:           req.TipCents,
	}
}

func posOrderShell(req POSOrderRequest) ordersapi.POSOrderShell {
	shell := ordersapi.POSOrderShell{
		FulfilmentType:        req.FulfilmentType,
		ScheduledDate:         req.ScheduledDate,
		SlotStart:             req.SlotStart,
		SlotEnd:               req.SlotEnd,
		Notes:                 req.Notes,
		GuestCustomerName:     req.GuestCustomerName,
		GuestCustomerMobile:   req.GuestCustomerMobile,
		GuestCustomerAddress:  req.GuestCustomerAddress,
		GuestCustomerCity:     req.GuestCustomerCity,
		GuestCustomerPostcode: req.GuestCustomerPostcode,
		GuestCustomerEmail:    req.GuestCustomerEmail,
		OrderDeliverDate:      req.OrderDeliverDate,
		OrderDeliverTime:      req.OrderDeliverTime,
	}
	if shell.FulfilmentType == "" {
		shell.FulfilmentType = "pickup"
	}
	shell.Recipient = req.Recipient
	shell.GiftMessage, shell.GiftReason, shell.IsGift = req.GiftMessage, req.GiftReason, req.IsGift
	if req.Customer != nil {
		shell.FirstName = req.Customer.FirstName
		shell.LastName = req.Customer.LastName
		shell.Phone = req.Customer.Phone
		shell.Email = req.Customer.Email
		shell.Address = req.Customer.Address
		if shell.GuestCustomerName == nil && (req.Customer.FirstName != "" || req.Customer.LastName != "") {
			name := strings.TrimSpace(req.Customer.FirstName + " " + req.Customer.LastName)
			if name != "" {
				shell.GuestCustomerName = &name
			}
		}
		if shell.GuestCustomerMobile == nil && req.Customer.Phone != "" {
			phone := req.Customer.Phone
			shell.GuestCustomerMobile = &phone
		}
		if shell.GuestCustomerEmail == nil && req.Customer.Email != "" {
			email := req.Customer.Email
			shell.GuestCustomerEmail = &email
		}
		if shell.GuestCustomerAddress == nil && req.Customer.Address != "" {
			addr := req.Customer.Address
			shell.GuestCustomerAddress = &addr
		}
		if shell.GuestCustomerCity == nil && req.Customer.City != "" {
			city := req.Customer.City
			shell.GuestCustomerCity = &city
		}
		if shell.GuestCustomerPostcode == nil && req.Customer.Postcode != "" {
			pc := req.Customer.Postcode
			shell.GuestCustomerPostcode = &pc
		}
		shell.CustomerStreet, shell.CustomerState = req.Customer.streetAndState()
	}
	return shell
}

// orderTaxPct is the rate an order's tax is computed at: the one stored on
// it, else (for orders from before the rate was kept) the outlet's current
// rate, else nil — in which case the stored cents are left alone.
func (s *Service) orderTaxPct(ctx context.Context, order *ordersapi.Order) *float64 {
	if order.TaxPct != nil {
		return order.TaxPct
	}
	if branch, err := s.branches.GetBranch(ctx, order.BranchID); err == nil && branch != nil {
		return branch.TaxRatePct
	}
	return nil
}

// chargesTouched reports whether a charges patch would change any amount.
func chargesTouched(c ordersapi.OrderTotalsPatch) bool {
	return c.DiscountPct != nil || c.DiscountCents != nil || c.TaxCents != nil || c.TaxPct != nil ||
		c.DeliveryFeeCents != nil || c.ServiceChargeCents != nil || c.TipCents != nil
}
