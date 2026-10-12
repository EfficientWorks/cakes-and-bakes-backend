-- +goose Up
-- +goose StatementBegin

/*
 * Delivery pricing per store: ZIP or radius region; free, fixed, per-ZIP /
 * per-distance-band, or free above an order amount; optional time-slot fees.
 * The shape is franchiseapi.DeliveryPricing. The ZIP list stays in
 * delivery_zone.
 *
 * Existing stores are written as what they already were, ZIP region with
 * their flat delivery fee on every order, so nothing changes for them until
 * an admin edits the pricing.
 */
ALTER TABLE branches ADD COLUMN IF NOT EXISTS delivery_pricing JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE branches
   SET delivery_pricing = jsonb_build_object(
         'region', 'zip',
         'radiusValue', 0,
         'radiusUnit', 'mi',
         'chargeType', 'fixed',
         'fixedFeeCents', COALESCE(delivery_fee_cents, 0),
         'zipFees', '[]'::jsonb,
         'distanceBands', '[]'::jsonb,
         'freeOverCents', 0,
         'otherOrdersFeeCents', 0,
         'timeBased', false,
         'timeSlots', '[]'::jsonb)
 WHERE delivery_pricing = '{}'::jsonb;

/*
 * Where the cart's delivery address is, located once when the address is set
 * so pricing a radius store doesn't geocode on every cart read.
 */
ALTER TABLE carts ADD COLUMN IF NOT EXISTS delivery_latitude  DOUBLE PRECISION;
ALTER TABLE carts ADD COLUMN IF NOT EXISTS delivery_longitude DOUBLE PRECISION;

-- +goose StatementEnd

-- +goose Down
ALTER TABLE carts DROP COLUMN IF EXISTS delivery_longitude;
ALTER TABLE carts DROP COLUMN IF EXISTS delivery_latitude;
ALTER TABLE branches DROP COLUMN IF EXISTS delivery_pricing;
