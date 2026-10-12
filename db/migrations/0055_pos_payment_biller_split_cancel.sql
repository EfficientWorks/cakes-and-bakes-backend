-- +goose Up
-- +goose StatementBegin

-- ------------------------------------------------------------ payment method
--
-- A counter order does not know how it will be paid until the money is taken,
-- so the column can no longer default to 'card' at insert time. It becomes
-- nullable and is written by the payment path when the order is settled.
-- 'cash' and 'mixed' (more than one tender against the same order) join the
-- allowed values, because a till takes both.
ALTER TABLE orders ALTER COLUMN payment_method DROP NOT NULL;
ALTER TABLE orders ALTER COLUMN payment_method DROP DEFAULT;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_payment_method_check;
ALTER TABLE orders ADD CONSTRAINT orders_payment_method_check CHECK (
  payment_method IS NULL OR payment_method IN (
    'card','cash','mixed','apple_pay','google_pay','cash_on_delivery','terminal','manual_counter'
  )
);

-- ------------------------------------------------------------------- biller
--
-- Who rang the order up (staff id from the token at create) and who last
-- touched it from the till (staff id from the token on every POS update).
ALTER TABLE orders ADD COLUMN IF NOT EXISTS biller_id         TEXT;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS updated_biller_id TEXT;

-- --------------------------------------------------------------- split mode
--
-- How the bill was split ('amount' | 'item' | 'custom'), denormalised onto
-- the order so the order GET can say so without reading order_bills.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS split_mode TEXT
  CHECK (split_mode IS NULL OR split_mode IN ('amount','item','custom'));

-- -------------------------------------------------------------- cancel time
--
-- When the till cancelled a line, as the till reports it.
ALTER TABLE order_items ADD COLUMN IF NOT EXISTS cancelled_at TIMESTAMPTZ;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE order_items DROP COLUMN IF EXISTS cancelled_at;
ALTER TABLE orders DROP COLUMN IF EXISTS split_mode;
ALTER TABLE orders DROP COLUMN IF EXISTS updated_biller_id;
ALTER TABLE orders DROP COLUMN IF EXISTS biller_id;
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_payment_method_check;
UPDATE orders SET payment_method = 'card' WHERE payment_method IS NULL OR payment_method IN ('cash','mixed');
ALTER TABLE orders ALTER COLUMN payment_method SET DEFAULT 'card';
ALTER TABLE orders ALTER COLUMN payment_method SET NOT NULL;
ALTER TABLE orders ADD CONSTRAINT orders_payment_method_check
  CHECK (payment_method IN ('card','apple_pay','google_pay','cash_on_delivery','terminal','manual_counter'));
-- +goose StatementEnd
