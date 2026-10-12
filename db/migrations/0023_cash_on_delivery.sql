-- +goose Up

-- Cash on delivery.
--
-- This is the first payment method where the order exists before the money
-- does. Everything else in checkout captures first and only then creates the
-- order, deliberately, so a failed payment never leaves a phantom order
-- behind (see 0021_payments_precede_orders). COD inverts that: the order is
-- real and unpaid until someone hands over cash at the door or the counter.
--
-- It is modelled honestly rather than as a fake capture. `payments.method`
-- already allows 'cash' and `payments.status` already allows 'pending', so a
-- COD order carries a genuine pending payment row that is settled when the
-- order is fulfilled. Nothing anywhere has to pretend the money arrived
-- early, and the unsettled balance is queryable — which is the whole point,
-- because uncollected cash is the risk COD introduces.

-- ---------------------------------------------------------------------------
-- Per branch, not per brand
-- ---------------------------------------------------------------------------

-- Whether this outlet takes cash. Off by default: accepting cash is an
-- operational decision about float, reconciliation and driver safety, and a
-- new branch should have to opt in rather than discover it is already
-- exposed. Governance level is branch (it sits with the other fulfilment
-- settings a branch manager owns, ADM-16).
ALTER TABLE branches
    ADD COLUMN accepts_cash_on_delivery BOOLEAN NOT NULL DEFAULT false;

-- A ceiling on what a branch will let go out unpaid. NULL means no ceiling.
-- Expressed in integer cents like every other money column.
ALTER TABLE branches
    ADD COLUMN cod_max_order_cents INTEGER;

-- ---------------------------------------------------------------------------
-- The order remembers how it was paid for
-- ---------------------------------------------------------------------------

-- Until now the method lived only on the payment row, so answering "is this
-- one of the COD orders going out today" meant joining payments on every
-- order list. The kitchen ticket, the driver's run sheet and the admin order
-- list all need it, so it is denormalised onto the order.
--
-- 'card' for everything that exists today, which is what every current row is.
ALTER TABLE orders
    ADD COLUMN payment_method TEXT NOT NULL DEFAULT 'card'
        CHECK (payment_method IN ('card','apple_pay','google_pay','cash_on_delivery','terminal','manual_counter'));

-- When the cash actually arrived, and who took it. Null on an unpaid COD
-- order and on every card order (those settle through Stripe, and
-- payments.captured_at is the record).
ALTER TABLE orders
    ADD COLUMN cash_collected_at      TIMESTAMPTZ,
    ADD COLUMN cash_collected_by      UUID;

-- Finding the unpaid ones has to be cheap: it is the query the branch runs at
-- close of day, and the one the network dashboard runs to surface money still
-- out on the road.
CREATE INDEX idx_orders_unpaid_cod ON orders (branch_id, scheduled_date)
    WHERE payment_method = 'cash_on_delivery' AND payment_status = 'pending';

-- +goose Down

DROP INDEX IF EXISTS idx_orders_unpaid_cod;
ALTER TABLE orders
    DROP COLUMN IF EXISTS cash_collected_by,
    DROP COLUMN IF EXISTS cash_collected_at,
    DROP COLUMN IF EXISTS payment_method;
ALTER TABLE branches
    DROP COLUMN IF EXISTS cod_max_order_cents,
    DROP COLUMN IF EXISTS accepts_cash_on_delivery;
