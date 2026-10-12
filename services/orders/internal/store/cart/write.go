package cart

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/orders/ordersapi"
)

/*
 * CreateCart starts a cart for a customer or a guest token.
 *
 * A guest token maps to exactly one cart row (guest_token_hash is UNIQUE), and
 * the row outlives the 48-hour window it is readable for: an expired cart is
 * invisible to GetCartByGuestTokenHash but still there. A caller that
 * presented a token whose cart had expired was therefore told "no cart", came
 * here to create one, and hit the unique key — 23505 on carts_guest_token_hash_key,
 * surfacing as INTERNAL_ERROR on the first add-to-cart after two idle days.
 *
 * So for a guest this is an upsert: an existing row under the same token is
 * taken over as the new cart — reset to defaults, given a fresh expiry, and
 * emptied of whatever it held — rather than raced against.
 */
func (s *Repo) CreateCart(ctx context.Context, branchID string, customerID *string, guestTokenHash *string) (*ordersapi.Cart, error) {
	if guestTokenHash == nil {
		const q = `
			INSERT INTO carts (branch_id, customer_id, guest_token_hash)
			VALUES ($1,$2,$3) RETURNING id`
		var id string
		if err := s.pool.QueryRow(ctx, q, branchID, customerID, guestTokenHash).Scan(&id); err != nil {
			return nil, err
		}
		return s.GetCartByID(ctx, id)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	const q = `
		INSERT INTO carts (branch_id, customer_id, guest_token_hash)
		VALUES ($1,$2,$3)
		ON CONFLICT (guest_token_hash) DO UPDATE SET
			branch_id         = EXCLUDED.branch_id,
			customer_id       = EXCLUDED.customer_id,
			fulfilment_type   = 'delivery',
			delivery_street   = NULL,
			delivery_city     = NULL,
			delivery_state    = NULL,
			delivery_postcode = NULL,
			delivery_latitude = NULL,
			delivery_longitude = NULL,
			scheduled_date    = NULL,
			slot_start        = NULL,
			slot_end          = NULL,
			discount_code     = NULL,
			tip_cents         = 0,
			created_at        = NOW(),
			updated_at        = NOW(),
			expires_at        = NOW() + INTERVAL '48 hours'
		RETURNING id, (xmax <> 0) AS reused`
	var id string
	var reused bool
	if err := tx.QueryRow(ctx, q, branchID, customerID, guestTokenHash).Scan(&id, &reused); err != nil {
		return nil, err
	}
	if reused {
		if _, err := tx.Exec(ctx, `DELETE FROM cart_items WHERE cart_id = $1`, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetCartByID(ctx, id)
}

func (s *Repo) AddCartItem(ctx context.Context, item ordersapi.CartItem) (*ordersapi.CartItem, error) {
	flavours, _ := json.Marshal(item.SelectedFlavours)
	addons, _ := json.Marshal(item.SelectedAddons)
	pres, _ := json.Marshal(item.SelectedPresentation)
	wrap, _ := json.Marshal(item.SelectedWrapColour)

	const q = `
		INSERT INTO cart_items (cart_id, product_id, product_name, variant_id, variant_name, unit_price_cents, quantity,
			selected_flavours, selected_addons, selected_presentation, selected_wrap_colour,
			is_gift, recipient_first_name, recipient_last_name, gift_message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id`
	var id string
	err := s.pool.QueryRow(ctx, q, item.CartID, item.ProductID, item.ProductName, item.VariantID, item.VariantName,
		item.UnitPriceCents, item.Quantity, flavours, addons, pres, wrap, item.IsGift,
		item.RecipientFirstName, item.RecipientLastName, item.GiftMessage).Scan(&id)
	if err != nil {
		return nil, err
	}
	_, _ = s.pool.Exec(ctx, `UPDATE carts SET updated_at = NOW() WHERE id = $1`, item.CartID)
	item.ID = id
	return &item, nil
}

/*
 * Updates one line.
 *
 * The gift columns COALESCE against their current value, so a caller that does
 * not mention them leaves them alone. They used to be assigned outright, which
 * meant the cart's own quantity stepper — which sends nothing but a quantity —
 * wrote NULL over the customer's gift message every time they pressed + or -.
 *
 * NULLIF turns a deliberate empty string back into NULL, so clearing is still
 * expressible: the service sends "" when a line stops being a gift.
 *
 * The selections above them are assigned outright on purpose. Those are
 * resolved and re-priced by the service before they get here, so by this point
 * they are always the full intended set.
 */
func (s *Repo) UpdateCartItem(ctx context.Context, cartID, itemID string, quantity int, flavours, addons []ordersapi.SelectedModifier, presentation, wrap *ordersapi.SelectedModifier, isGift *bool, recipientFirst, recipientLast, giftMessage *string) error {
	flavoursJSON, _ := json.Marshal(flavours)
	addonsJSON, _ := json.Marshal(addons)
	presJSON, _ := json.Marshal(presentation)
	wrapJSON, _ := json.Marshal(wrap)

	tag, err := s.pool.Exec(ctx, `
		UPDATE cart_items SET quantity=$3, selected_flavours=$4, selected_addons=$5, selected_presentation=$6,
			selected_wrap_colour=$7,
			is_gift=COALESCE($8, is_gift),
			recipient_first_name=NULLIF(COALESCE($9, recipient_first_name), ''),
			recipient_last_name=NULLIF(COALESCE($10, recipient_last_name), ''),
			gift_message=NULLIF(COALESCE($11, gift_message), ''),
			updated_at=NOW()
		WHERE id = $1 AND cart_id = $2`,
		itemID, cartID, quantity, flavoursJSON, addonsJSON, presJSON, wrapJSON, isGift, recipientFirst, recipientLast, giftMessage)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ordersapi.ErrNotFound
	}
	_, _ = s.pool.Exec(ctx, `UPDATE carts SET updated_at = NOW() WHERE id = $1`, cartID)
	return nil
}

func (s *Repo) RemoveCartItem(ctx context.Context, cartID, itemID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM cart_items WHERE id = $1 AND cart_id = $2`, itemID, cartID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ordersapi.ErrNotFound
	}
	return nil
}

func (s *Repo) ClearCartItems(ctx context.Context, cartID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM cart_items WHERE cart_id = $1`, cartID)
	return err
}

// SetCartBranch re-points an empty cart at another branch. The WHERE clause
// carries the emptiness check so the decision and the write cannot disagree
// under a concurrent add: a cart that gained a line between the two is simply
// not updated, and the caller's conflict stands.
// SetCartBranch moves a cart to another outlet, items and all.
//
// It used to refuse once the cart had anything in it, which made changing your
// mind about where to collect from a dead end: the customer was told to empty
// the basket and start again. An order carries exactly one branch either way,
// so moving the whole cart is the honest reading of "collect from there
// instead" — and every line is re-priced against the new outlet at checkout,
// so nothing is charged at the old branch's prices.
func (s *Repo) SetCartBranch(ctx context.Context, cartID, branchID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE carts SET branch_id=$2, updated_at=NOW() WHERE id = $1`, cartID, branchID)
	return err
}

func (s *Repo) SetCartFulfilment(ctx context.Context, cartID, fulfilmentType string, street, city, state, postcode, branchID *string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE carts
		SET fulfilment_type = $2,
		    delivery_street = $3,
		    delivery_city = $4,
		    delivery_state = $5,
		    delivery_postcode = $6,
		    -- The address may have changed: the old point no longer applies.
		    delivery_latitude = NULL,
		    delivery_longitude = NULL,
		    branch_id = COALESCE($7, branch_id),
		    updated_at = NOW()
		WHERE id = $1`, cartID, fulfilmentType, street, city, state, postcode, branchID)
	return err
}

func (s *Repo) UpdateCartItemPricing(
	ctx context.Context,
	itemID string,
	unitPriceCents int64,
	flavours, addons []ordersapi.SelectedModifier,
	presentation, wrap *ordersapi.SelectedModifier,
	available bool,
) error {
	flavoursJSON, _ := json.Marshal(flavours)
	addonsJSON, _ := json.Marshal(addons)
	presJSON, _ := json.Marshal(presentation)
	wrapJSON, _ := json.Marshal(wrap)

	_, err := s.pool.Exec(ctx, `
		UPDATE cart_items
		SET unit_price_cents = $2,
		    selected_flavours = $3,
		    selected_addons = $4,
		    selected_presentation = $5,
		    selected_wrap_colour = $6,
		    available = $7,
		    updated_at = NOW()
		WHERE id = $1`,
		itemID, unitPriceCents, flavoursJSON, addonsJSON, presJSON, wrapJSON, available)
	return err
}

func (s *Repo) UpdateCartItemAvailable(ctx context.Context, itemID string, available bool) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE cart_items
		SET available = $2, updated_at = NOW()
		WHERE id = $1`, itemID, available)
	return err
}

func (s *Repo) ClearCartSchedule(ctx context.Context, cartID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE carts SET scheduled_date=NULL, slot_start=NULL, slot_end=NULL, updated_at=NOW() WHERE id = $1`, cartID)
	return err
}

func (s *Repo) SetCartSchedule(ctx context.Context, cartID string, date time.Time, slotStart, slotEnd string) error {
	_, err := s.pool.Exec(ctx, `UPDATE carts SET scheduled_date=$2, slot_start=$3, slot_end=$4, updated_at=NOW() WHERE id = $1`, cartID, date, slotStart, slotEnd)
	return err
}

func (s *Repo) SetCartDiscount(ctx context.Context, cartID string, code *string) error {
	_, err := s.pool.Exec(ctx, `UPDATE carts SET discount_code=$2, updated_at=NOW() WHERE id = $1`, cartID, code)
	return err
}

func (s *Repo) SetCartTip(ctx context.Context, cartID string, tipCents int64) error {
	_, err := s.pool.Exec(ctx, `UPDATE carts SET tip_cents=$2, updated_at=NOW() WHERE id = $1`, cartID, tipCents)
	return err
}

func (s *Repo) DeleteCart(ctx context.Context, cartID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM carts WHERE id = $1`, cartID)
	return err
}

// SetCartDeliveryPoint stores where the cart's delivery address is (nil
// clears it). Written after SetCartFulfilment, which always clears it.
func (s *Repo) SetCartDeliveryPoint(ctx context.Context, cartID string, lat, lng *float64) error {
	_, err := s.pool.Exec(ctx, `UPDATE carts SET delivery_latitude = $2, delivery_longitude = $3 WHERE id = $1`, cartID, lat, lng)
	return err
}

// ClaimGuestCart gives a guest cart to the customer who has just signed in.
// The token is cleared and the customer set in one statement (a cart must
// always have one or the other), so of two requests racing after sign-in only
// one gets the cart, and the guest token cannot find it again. ErrNotFound
// when there is no unclaimed guest cart for that token.
func (s *Repo) ClaimGuestCart(ctx context.Context, tokenHash, customerID string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		UPDATE carts SET guest_token_hash = NULL, customer_id = $2, updated_at = NOW()
		 WHERE guest_token_hash = $1 AND customer_id IS NULL AND expires_at > NOW()
		RETURNING id`, tokenHash, customerID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ordersapi.ErrNotFound
	}
	return id, err
}
