package pos

import (
	"context"
	"encoding/json"
	"strings"

	"bakehouse/pkg/timex"
	"bakehouse/services/orders/internal/store/customer"
	"bakehouse/services/orders/internal/store/order"
	"bakehouse/services/orders/internal/store/shared"
	"bakehouse/services/orders/ordersapi"
)

// CreatePOSOrder writes a counter order and its first batch of lines.
//
// One transaction, and no slot: a walk-in is served now, so there is nothing
// to reserve and nothing to decrement. That is the whole difference from the
// storefront path, and it is why this does not go through it.
//
// The order lands as 'confirmed' rather than 'placed'. A ticket sent to the
// kitchen has already been accepted by a person — there is nobody left to
// confirm it, and leaving it as 'placed' would put it in a queue waiting for
// an approval that is never coming.
func (s *Repo) CreatePOSOrder(
	ctx context.Context,
	branchID string,
	customerID *string,
	shell ordersapi.POSOrderShell,
	items []ordersapi.POSItem,
) (*ordersapi.Order, error) {
	if customerID == nil || strings.TrimSpace(*customerID) == "" {
		// If customerID is empty, use guest customer details to create customer and save that id in db
		custMobile := ""
		if shell.GuestCustomerMobile != nil && strings.TrimSpace(*shell.GuestCustomerMobile) != "" {
			custMobile = strings.TrimSpace(*shell.GuestCustomerMobile)
		} else if strings.TrimSpace(shell.Phone) != "" {
			custMobile = strings.TrimSpace(shell.Phone)
		}

		custName := ""
		if shell.GuestCustomerName != nil && strings.TrimSpace(*shell.GuestCustomerName) != "" {
			custName = strings.TrimSpace(*shell.GuestCustomerName)
		} else if strings.TrimSpace(shell.FirstName) != "" || strings.TrimSpace(shell.LastName) != "" {
			custName = strings.TrimSpace(shell.FirstName + " " + shell.LastName)
		}

		custEmail := ""
		if shell.GuestCustomerEmail != nil && strings.TrimSpace(*shell.GuestCustomerEmail) != "" {
			custEmail = strings.TrimSpace(*shell.GuestCustomerEmail)
		} else if strings.TrimSpace(shell.Email) != "" {
			custEmail = strings.TrimSpace(shell.Email)
		}

		custAddr := ""
		if shell.GuestCustomerAddress != nil && strings.TrimSpace(*shell.GuestCustomerAddress) != "" {
			custAddr = strings.TrimSpace(*shell.GuestCustomerAddress)
		} else if strings.TrimSpace(shell.Address) != "" {
			custAddr = strings.TrimSpace(shell.Address)
		}

		if custMobile != "" || custName != "" || custEmail != "" {
			first := custName
			last := ""
			parts := strings.Fields(custName)
			if len(parts) > 0 {
				first = parts[0]
				if len(parts) > 1 {
					last = strings.Join(parts[1:], " ")
				}
			}
			if first == "" {
				first = "Guest"
			}

			var newCID string
			// Look for an existing customer with the same mobile or email.
			// A match is refused, not updated: a customer is changed only when
			// the till names them by id.
			var existingID, matchedOn string
			//
			// Only among this store's own POS customers: a POS customer belongs
			// to one store, and the till's customer list shows only those. The
			// mobile is compared digits-only, the way it is kept unique.
			if custMobile != "" {
				_ = s.pool.QueryRow(ctx, `
					SELECT id FROM customers
					 WHERE branch_id = $2 AND customer_type = 'pos' AND deleted_at IS NULL
					   AND normalise_mobile(mobile_number) = normalise_mobile($1)
					 LIMIT 1`, custMobile, branchID).Scan(&existingID)
				if existingID != "" {
					matchedOn = "mobileNumber"
				}
			}
			if existingID == "" && custEmail != "" {
				_ = s.pool.QueryRow(ctx, `
					SELECT id FROM customers
					 WHERE branch_id = $2 AND customer_type = 'pos' AND deleted_at IS NULL
					   AND lower(email) = lower($1)
					 LIMIT 1`, custEmail, branchID).Scan(&existingID)
				if existingID != "" {
					matchedOn = "email"
				}
			}

			if existingID != "" {
				return nil, s.customers.CustomerExists(ctx, existingID, matchedOn)
			} else {
				var emailParam *string
				if custEmail != "" {
					emailParam = &custEmail
				}
				var mobileParam *string
				if custMobile != "" {
					mobileParam = &custMobile
				}
				if err := s.pool.QueryRow(ctx, `
					INSERT INTO customers (branch_id, customer_type, first_name, last_name, email, mobile_number, password_hash, is_verified, created_at, updated_at)
					VALUES ($1, 'pos', $2, $3, $4, $5, '', true, NOW(), NOW())
					RETURNING id`, branchID, first, last, emailParam, mobileParam).Scan(&newCID); err != nil {
					// An email that is another customer's is the till's to
					// fix, not something to drop without a word.
					if customer.IsEmailTaken(err) {
						return nil, customer.ErrCustomerContactTaken
					}
					// Otherwise a walk-in still gets its order; it just is not linked.
					newCID = ""
				}
				// Address (custAddr) is written below, once the final
				// customer id is known.
				_ = custAddr
			}

			if newCID != "" {
				customerID = &newCID
			}
		}
	}

	// The purchaser is the customer: whatever the till left blank comes from
	// their record, whether they were named by id, matched on mobile or email,
	// or created just now from the guest details.
	if customerID != nil && strings.TrimSpace(*customerID) != "" {
		if c, err := s.customers.GetCustomerContact(ctx, *customerID); err == nil {
			if shell.FirstName == "" {
				shell.FirstName = c.FirstName
			}
			if shell.LastName == "" {
				shell.LastName = c.LastName
			}
			if shell.Email == "" {
				shell.Email = c.Email
			}
			if shell.Phone == "" && c.Phone != nil {
				shell.Phone = *c.Phone
			}
		}
	}

	// Who it is for: the recipient the till named, else the customer.
	recFirst, recLast := shell.FirstName, shell.LastName
	recPhone, recEmail := shared.NilIfEmptyStr(shell.Phone), shared.NilIfEmptyStr(shell.Email)
	// The recipient's address, when the till gives one, is the order's
	// delivery address.
	var recStreet, recCity, recPostcode *string
	if r := shell.Recipient; r != nil {
		recStreet, recCity, recPostcode = shared.NilIfEmptyPtr(r.Address), shared.NilIfEmptyPtr(r.City), shared.NilIfEmptyPtr(r.Postcode)
	}
	// A recipient the till sends is taken as sent: a field it leaves out or
	// blank stays empty rather than being filled from the customer.
	if r := shell.Recipient; r != nil {
		recFirst, recLast = shared.DerefTrim(r.FirstName), shared.DerefTrim(r.LastName)
		recPhone, recEmail = shared.NilIfEmptyPtr(r.Phone), shared.NilIfEmptyPtr(r.Email)
	}

	// Whoever the order ended up attached to — new or existing — gets the
	// address/city/postcode the till typed. Previously city and postcode
	// were written as '' on a new customer and never written on an existing
	// one, so the order GET (which reads customer_addresses) showed blanks.
	if customerID != nil && strings.TrimSpace(*customerID) != "" {
		addr := shared.NilIfEmptyPtr(shell.GuestCustomerAddress)
		if addr == nil {
			addr = shared.NilIfEmptyStr(shell.Address)
		}
		city := shared.NilIfEmptyPtr(shell.GuestCustomerCity)
		postcode := shared.NilIfEmptyPtr(shell.GuestCustomerPostcode)
		street, state := shared.NilIfEmptyPtr(shell.CustomerStreet), shared.NilIfEmptyPtr(shell.CustomerState)
		if addr != nil || city != nil || postcode != nil || street != nil || state != nil {
			if err := s.orders.UpsertCustomerAddress(ctx, strings.TrimSpace(*customerID), addr, street, city, state, postcode); err != nil {
				return nil, err
			}
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	reference, err := order.NewOrderReference()
	if err != nil {
		return nil, err
	}
	billNo, err := order.NextBillNo(ctx, tx, branchID)
	if err != nil {
		return nil, err
	}

	var orderID string
	err = tx.QueryRow(ctx, `
		INSERT INTO orders (
			reference, branch_id, customer_id, channel, fulfilment_type,
			recipient_first_name, recipient_last_name, recipient_phone, recipient_email,
			purchaser_first_name, purchaser_last_name, purchaser_email, purchaser_phone,
			scheduled_date, slot_start, slot_end,
			subtotal_cents, delivery_fee_cents, tax_cents, tip_cents, discount_cents,
			service_charge_cents, total_cents, currency,
			status, payment_status, payment_method, notes, idempotency_key,
			guest_customer_name, guest_customer_mobile, guest_customer_address, guest_customer_email,
			order_deliver_date, order_deliver_time, bill_no, biller_id, updated_biller_id, start_time,
			guest_customer_city, guest_customer_postcode,
			delivery_street, delivery_city, delivery_postcode,
			gift_message, gift_reason, is_gift,
			guest_customer_street, guest_customer_state)
		VALUES ($1,$2,$3,'pos',$4,
			$5,$6,$7,$8,
			-- The purchaser columns are not nullable: a website order always
			-- has an email, a walk-in never does. Empty rather than absent,
			-- which is the honest answer for somebody who paid at a counter.
			$24,$25,$10,$26,
			-- Scheduled date and slot from the request; otherwise today and now at
			-- the store (its own timezone, not the database server's).
			COALESCE($11, (NOW() AT TIME ZONE (SELECT timezone FROM branches WHERE id = $2::uuid))::date),
			COALESCE(NULLIF($12, '')::time, (NOW() AT TIME ZONE (SELECT timezone FROM branches WHERE id = $2::uuid))::time(0)),
			COALESCE(NULLIF($13, '')::time, (COALESCE(NULLIF($12, '')::time, (NOW() AT TIME ZONE (SELECT timezone FROM branches WHERE id = $2::uuid))::time(0)) + interval '30 minutes')),
			0,0,0,0,0,
			0,0,'USD',
			-- No payment method yet: the till does not know how the order
			-- will be paid until it takes the money. Written by the payment
			-- path when the order is settled.
			COALESCE(NULLIF($22, ''), 'confirmed'),'pending',NULL,$9,
			-- Unique per ticket rather than supplied by the caller: a till has
			-- no retry story to protect — the operator can see whether the
			-- order appeared — and the column will not take a null.
			gen_random_uuid()::text,
			$14, $15, $16, $17,
			NULLIF($18, '')::date, NULLIF($19, '')::time, $20,
			-- Who rang it up. The same person is also the last to touch it
			-- until an update says otherwise.
			$21, $21,
			-- Opened when the till says it was, else now.
			COALESCE($23::timestamptz, NOW()),
			$27, $28,
			$29, $30, $31,
			-- is_gift keeps its column default (true) unless the till says.
			$32, $33, COALESCE($34::boolean, true),
			$35, $36)
		RETURNING id`,
		reference, branchID, customerID, shell.FulfilmentType,
		recFirst, recLast, recPhone, recEmail,
		shared.NilIfEmptyStr(shell.Notes), shell.Email, shell.ScheduledDate, shell.SlotStart, shell.SlotEnd,
		shared.NilIfEmptyPtr(shell.GuestCustomerName), shared.NilIfEmptyPtr(shell.GuestCustomerMobile),
		shared.NilIfEmptyPtr(shell.GuestCustomerAddress), shared.NilIfEmptyPtr(shell.GuestCustomerEmail),
		shared.NilIfEmptyPtr(shell.OrderDeliverDate), shared.NilIfEmptyPtr(shell.OrderDeliverTime), billNo,
		shared.NilIfEmptyPtr(shell.BillerID), shell.Status, timex.EventTimeFrom(ctx),
		shell.FirstName, shell.LastName, shared.NilIfEmptyStr(shell.Phone),
		shared.NilIfEmptyPtr(shell.GuestCustomerCity), shared.NilIfEmptyPtr(shell.GuestCustomerPostcode),
		recStreet, recCity, recPostcode,
		shared.NilIfEmptyPtr(shell.GiftMessage), shared.NilIfEmptyPtr(shell.GiftReason), shell.IsGift,
		shared.NilIfEmptyPtr(shell.CustomerStreet), shared.NilIfEmptyPtr(shell.CustomerState)).Scan(&orderID)
	if err != nil {
		return nil, err
	}

	// Everything sent together is batch 1 — the first trip to the kitchen.
	for _, it := range items {
		flavours, _ := json.Marshal(it.Flavours)
		addons, _ := json.Marshal(it.Addons)
		if _, err := tx.Exec(ctx, `
			INSERT INTO order_items (order_id, product_id, product_name, variant_id, variant_name,
				quantity, unit_price_cents, line_total_cents, selected_flavours, selected_addons,
				batch_no, discount_pct, discount_cents, production_status, send_kot_time, comments, cancelled_at,
				production_started_at, production_done_at, kot_status, gift_message, gift_reason)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,
				COALESCE(NULLIF($20, ''), CASE WHEN $14 IN ('cancel', 'cancelled') THEN 'cancelled' ELSE 'queue' END),
				$21, $22)`,
			orderID, shared.NilIfEmptyStr(it.ProductID), it.ProductName, shared.NilIfEmptyStr(it.VariantID), shared.NilIfEmptyStr(it.VariantName),
			it.Quantity, it.UnitPriceCents, it.LineTotalCents, flavours, addons,
			batchOrFirst(it.BatchNo), it.DiscountPct, it.DiscountCents,
			statusOrPending(it.Status), it.SendKotTime, shared.NilIfEmptyStr(it.Comments), cancelTimeFor(ctx, it),
			productionStartedFor(ctx, it), productionDoneFor(ctx, it), it.KOTStatus,
			shared.NilIfEmptyPtr(it.GiftMessage), shared.NilIfEmptyPtr(it.GiftReason)); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.orders.GetOrderByID(ctx, orderID)
}

// batchOrFirst defaults a line to the opening round. A till that tracks its
// own batches sends one; a simple one does not have to.
func batchOrFirst(n int) int {
	if n <= 0 {
		return 1
	}
	return n
}
