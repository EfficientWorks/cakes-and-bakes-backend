package cart

import (
	"context"

	"bakehouse/services/orders/ordersapi"
)

func (s *Repo) GetCartByID(ctx context.Context, id string) (*ordersapi.Cart, error) {
	return s.scanCartWithItems(ctx, cartSelect+" WHERE id = $1", id)
}

func (s *Repo) GetCartByCustomer(ctx context.Context, customerID string) (*ordersapi.Cart, error) {
	return s.scanCartWithItems(ctx, cartSelect+" WHERE customer_id = $1 ORDER BY updated_at DESC LIMIT 1", customerID)
}

func (s *Repo) GetCartByGuestTokenHash(ctx context.Context, hash string) (*ordersapi.Cart, error) {
	return s.scanCartWithItems(ctx, cartSelect+" WHERE guest_token_hash = $1 AND expires_at > NOW()", hash)
}
