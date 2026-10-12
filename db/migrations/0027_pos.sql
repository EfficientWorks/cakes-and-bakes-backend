-- +goose Up
-- +goose StatementBegin

-- Point of sale.
--
-- The till sells the same catalogue to the same customers at the same outlets,
-- so it writes to `orders` like every other channel rather than getting a
-- parallel world of its own — `channel = 'pos'` already existed for exactly
-- this. What it adds is everything a counter needs and a website does not: a
-- way to sign in without a keyboard, kitchen batches, returns, and a bill that
-- can be split three ways.

-- --------------------------------------------------------------- signing in

-- The code typed on the POS login screen to say which till this is.
--
-- Per outlet, not per company: the screen asks for a Business ID and then a
-- passcode, and the first of those has to be enough to know which shop is
-- open. Generated when the outlet is created so nobody has to invent one.
ALTER TABLE branches ADD COLUMN IF NOT EXISTS business_id TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS branches_business_id_key
  ON branches (business_id) WHERE business_id IS NOT NULL;

-- A short numeric code, hashed like any other credential.
--
-- Unique within an outlet rather than across the brand: four digits over one
-- shop's staff is comfortable, over every shop is not, and the Business ID has
-- already narrowed it to one outlet by the time this is checked.
ALTER TABLE staff_users ADD COLUMN IF NOT EXISTS passcode_hash TEXT;

-- ------------------------------------------------------------------ orders

-- Deleting an order hides it everywhere without losing it.
--
-- Distinct from cancelling, which is a state the customer and the admin are
-- both meant to see. A deleted order is a mistake being taken back — a wrong
-- till entry — and should read as though it never happened.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
CREATE INDEX IF NOT EXISTS orders_not_deleted_idx ON orders (branch_id, created_at DESC)
  WHERE deleted_at IS NULL;

-- Charged at the counter, alongside tax and delivery.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS service_charge_cents INTEGER NOT NULL DEFAULT 0;

-- An order-level discount, as a percentage and as the money it came to.
--
-- Both are kept: the percentage is what the operator chose and what a receipt
-- should say, the amount is what was actually taken off and must not drift if
-- the order is edited afterwards.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS discount_pct NUMERIC(5,2);

-- A returned order is a state of its own, not a cancellation: it was made,
-- paid for, and handed back.
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_status_check CHECK (status IN (
  'placed', 'confirmed', 'scheduled', 'in_production', 'ready',
  'out_for_delivery', 'fulfilled', 'cancelled', 'returned'
));

-- ------------------------------------------------------------- order items

-- Which trip to the kitchen this line was on.
--
-- A table orders, the ticket goes to the kitchen, then they order more against
-- the same order. The second lot is batch 2 — the kitchen needs to know what
-- is new and what it has already made, and the printed ticket has to say so.
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS batch_no INTEGER NOT NULL DEFAULT 1;
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS discount_pct NUMERIC(5,2);
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS discount_cents INTEGER NOT NULL DEFAULT 0;
-- How many of this line have been handed back.
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS returned_quantity INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS order_items_batch_idx ON order_items (order_id, batch_no);

-- ----------------------------------------------------------------- returns

CREATE TABLE IF NOT EXISTS order_returns (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id        UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  -- Null for a whole-order return, which is recorded as every line at once.
  reason          TEXT,
  amount_cents    INTEGER NOT NULL DEFAULT 0,
  is_full_return  BOOLEAN NOT NULL DEFAULT false,
  returned_by     UUID REFERENCES staff_users(id),
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS order_returns_order_idx ON order_returns (order_id);

CREATE TABLE IF NOT EXISTS order_return_items (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  return_id     UUID NOT NULL REFERENCES order_returns(id) ON DELETE CASCADE,
  order_item_id UUID NOT NULL REFERENCES order_items(id),
  quantity      INTEGER NOT NULL CHECK (quantity > 0),
  amount_cents  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS order_return_items_return_idx ON order_return_items (return_id);

-- ------------------------------------------------------------------- bills

-- One share of an order, to be settled on its own.
--
-- Three ways to arrive at the same thing: split the total into equal or
-- chosen amounts, split by which items belong to whom, or split by how each
-- share is being paid. However it was reached, a bill is an amount somebody
-- owes; the mode is kept only so a receipt can explain itself.
CREATE TABLE IF NOT EXISTS order_bills (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id     UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  -- 'amount' | 'item' | 'custom'
  split_mode   TEXT NOT NULL CHECK (split_mode IN ('amount', 'item', 'custom')),
  -- "Bill 1 of 3", for the till and the receipt.
  label        TEXT NOT NULL,
  sequence_no  INTEGER NOT NULL,
  amount_cents INTEGER NOT NULL CHECK (amount_cents >= 0),
  paid_cents   INTEGER NOT NULL DEFAULT 0,
  status       TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'paid', 'void')),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS order_bills_order_idx ON order_bills (order_id);

-- Which lines went onto an item-split bill. Empty for the other two modes,
-- where a bill is an amount rather than a set of things.
CREATE TABLE IF NOT EXISTS order_bill_items (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  bill_id       UUID NOT NULL REFERENCES order_bills(id) ON DELETE CASCADE,
  order_item_id UUID NOT NULL REFERENCES order_items(id),
  quantity      INTEGER NOT NULL CHECK (quantity > 0)
);
CREATE INDEX IF NOT EXISTS order_bill_items_bill_idx ON order_bill_items (bill_id);

-- Money taken at the counter.
--
-- Separate from `payments`, which is the Stripe record for the storefront: a
-- till takes cash, gives change, and may take several tenders against one
-- bill. When a card integration arrives it writes its reference here rather
-- than needing another table.
CREATE TABLE IF NOT EXISTS bill_payments (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id       UUID NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  -- Null for a payment against the whole order rather than one split share.
  bill_id        UUID REFERENCES order_bills(id) ON DELETE CASCADE,
  method         TEXT NOT NULL CHECK (method IN ('cash', 'card')),
  amount_cents   INTEGER NOT NULL CHECK (amount_cents > 0),
  tip_cents      INTEGER NOT NULL DEFAULT 0,
  -- What the customer handed over, so the till can show the change due.
  tendered_cents INTEGER,
  reference      TEXT,
  taken_by       UUID REFERENCES staff_users(id),
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS bill_payments_order_idx ON bill_payments (order_id);
CREATE INDEX IF NOT EXISTS bill_payments_bill_idx ON bill_payments (bill_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS bill_payments;
DROP TABLE IF EXISTS order_bill_items;
DROP TABLE IF EXISTS order_bills;
DROP TABLE IF EXISTS order_return_items;
DROP TABLE IF EXISTS order_returns;
ALTER TABLE order_items DROP COLUMN IF EXISTS returned_quantity;
ALTER TABLE order_items DROP COLUMN IF EXISTS discount_cents;
ALTER TABLE order_items DROP COLUMN IF EXISTS discount_pct;
ALTER TABLE order_items DROP COLUMN IF EXISTS batch_no;
ALTER TABLE orders DROP COLUMN IF EXISTS discount_pct;
ALTER TABLE orders DROP COLUMN IF EXISTS service_charge_cents;
ALTER TABLE orders DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE staff_users DROP COLUMN IF EXISTS passcode_hash;
ALTER TABLE branches DROP COLUMN IF EXISTS business_id;
-- +goose StatementEnd
