package cart

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/orders/ordersapi"
)

func scanCart(row pgx.Row) (*ordersapi.Cart, error) {
	var c ordersapi.Cart
	err := row.Scan(&c.ID, &c.BranchID, &c.CustomerID, &c.FulfilmentType, &c.DeliveryStreet, &c.DeliveryCity,
		&c.DeliveryState, &c.DeliveryPostcode, &c.ScheduledDate, &c.SlotStart, &c.SlotEnd, &c.DiscountCode,
		&c.TipCents, &c.CreatedAt, &c.UpdatedAt, &c.DeliveryLatitude, &c.DeliveryLongitude)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ordersapi.ErrNotFound
	}
	return &c, err
}

func (s *Repo) scanCartWithItems(ctx context.Context, query string, arg interface{}) (*ordersapi.Cart, error) {
	row := s.pool.QueryRow(ctx, query, arg)
	c, err := scanCart(row)
	if err != nil {
		return nil, err
	}
	items, err := s.listCartItems(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	c.Items = items
	return c, nil
}

// listCartItems reads a cart's lines.
//
// The line's name and price are denormalised onto cart_items at add time, so
// a price change cannot silently rewrite a cart the customer is looking at.
// The slug and the image are deliberately NOT: they are presentation, they
// have no bearing on what was agreed, and a product renamed or rephotographed
// should show its current form. So both are joined live from the catalogue —
// the slug because the storefront's cart links each line back to its product
// page, the image because there is nothing else to render the thumbnail from.
func (s *Repo) listCartItems(ctx context.Context, cartID string) ([]ordersapi.CartItem, error) {
	query := `
		SELECT ci.id, ci.cart_id, ci.product_id, ci.product_name, p.slug, 
		       (SELECT pi.url FROM product_images pi 
		         WHERE pi.product_id = ci.product_id 
		         ORDER BY pi.is_primary DESC, pi.sort_order 
		         LIMIT 1) AS product_image_url, 
		       ci.variant_id, ci.variant_name, ci.unit_price_cents, ci.quantity, 
		       ci.selected_flavours, ci.selected_addons, ci.selected_presentation, ci.selected_wrap_colour, 
		       ci.is_gift, ci.recipient_first_name, ci.recipient_last_name, ci.gift_message, ci.created_at, 
		       ci.available 
		FROM cart_items ci 
		LEFT JOIN products p ON p.id = ci.product_id 
		WHERE ci.cart_id = $1 
		ORDER BY ci.created_at
	`

	rows, err := s.pool.Query(ctx, query, cartID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ordersapi.CartItem
	for rows.Next() {
		var i ordersapi.CartItem
		var flavoursRaw, addonsRaw, presRaw, wrapRaw []byte
		if err := rows.Scan(&i.ID, &i.CartID, &i.ProductID, &i.ProductName, &i.ProductSlug, &i.ProductImageURL,
			&i.VariantID, &i.VariantName,
			&i.UnitPriceCents, &i.Quantity, &flavoursRaw, &addonsRaw, &presRaw, &wrapRaw,
			&i.IsGift, &i.RecipientFirstName, &i.RecipientLastName, &i.GiftMessage, &i.CreatedAt,
			&i.Available); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(flavoursRaw, &i.SelectedFlavours)
		_ = json.Unmarshal(addonsRaw, &i.SelectedAddons)
		if presRaw != nil {
			_ = json.Unmarshal(presRaw, &i.SelectedPresentation)
		}
		if wrapRaw != nil {
			_ = json.Unmarshal(wrapRaw, &i.SelectedWrapColour)
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
