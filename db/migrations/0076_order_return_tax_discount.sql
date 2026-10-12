-- +goose Up
-- +goose StatementBegin

/*
 * Tax and discount on a return.
 *
 * Until now a return recorded only the gross value of the goods handed back
 * (unit price × quantity). What the customer is actually owed also depends on
 * the discount they were given and the tax they paid on those goods, so both
 * are now kept — on each return (return-based) and as a running total on the
 * order (order-based) so list views can show them without a join.
 *
 *   refund = gross - discount + tax
 *
 * Additive with defaults: existing inserts and reads are unaffected.
 */

-- Return-based: one row per return.
ALTER TABLE order_returns ADD COLUMN IF NOT EXISTS discount_pct   NUMERIC(5,2);
ALTER TABLE order_returns ADD COLUMN IF NOT EXISTS discount_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE order_returns ADD COLUMN IF NOT EXISTS tax_pct        NUMERIC(6,3);
ALTER TABLE order_returns ADD COLUMN IF NOT EXISTS tax_cents      INTEGER NOT NULL DEFAULT 0;
-- Net amount handed back: amount_cents - discount_cents + tax_cents.
ALTER TABLE order_returns ADD COLUMN IF NOT EXISTS refund_cents   INTEGER NOT NULL DEFAULT 0;

-- Order-based: running totals over every return on the order. The two
-- percentages are the rates applied on the most recent return.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS return_amount_cents   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS return_discount_pct   NUMERIC(5,2);
ALTER TABLE orders ADD COLUMN IF NOT EXISTS return_discount_cents INTEGER NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS return_tax_pct        NUMERIC(6,3);
ALTER TABLE orders ADD COLUMN IF NOT EXISTS return_tax_cents      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS return_total_cents    INTEGER NOT NULL DEFAULT 0;

-- Backfill: returns recorded before this had no tax or discount, so the
-- refund was the gross amount.
UPDATE order_returns SET refund_cents = amount_cents WHERE refund_cents = 0 AND amount_cents <> 0;

UPDATE orders o
   SET return_amount_cents = r.amount, return_total_cents = r.refund
  FROM (SELECT order_id, SUM(amount_cents) AS amount, SUM(refund_cents) AS refund
          FROM order_returns GROUP BY order_id) r
 WHERE r.order_id = o.id AND o.return_amount_cents = 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE orders DROP COLUMN IF EXISTS return_total_cents;
ALTER TABLE orders DROP COLUMN IF EXISTS return_tax_cents;
ALTER TABLE orders DROP COLUMN IF EXISTS return_tax_pct;
ALTER TABLE orders DROP COLUMN IF EXISTS return_discount_cents;
ALTER TABLE orders DROP COLUMN IF EXISTS return_discount_pct;
ALTER TABLE orders DROP COLUMN IF EXISTS return_amount_cents;
ALTER TABLE order_returns DROP COLUMN IF EXISTS refund_cents;
ALTER TABLE order_returns DROP COLUMN IF EXISTS tax_cents;
ALTER TABLE order_returns DROP COLUMN IF EXISTS tax_pct;
ALTER TABLE order_returns DROP COLUMN IF EXISTS discount_cents;
ALTER TABLE order_returns DROP COLUMN IF EXISTS discount_pct;
-- +goose StatementEnd
