-- +goose Up
-- +goose StatementBegin

-- Three separate tables, one per print job, each a singleton per outlet
-- (UNIQUE branch_id — not a list). Kept as three tables rather than one with
-- a discriminator column because kot and cancel are genuinely narrower
-- documents than the customer receipt — this way a kot/cancel row can never
-- carry a stray business_name column it has no use for.

CREATE TABLE IF NOT EXISTS print_receipt_templates (
  id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id                 UUID NOT NULL UNIQUE REFERENCES branches(id) ON DELETE CASCADE,
  franchise_id              UUID NOT NULL REFERENCES franchises(id),
  is_active                 BOOLEAN NOT NULL DEFAULT TRUE,

  show_customer_name        BOOLEAN NOT NULL DEFAULT TRUE,
  show_order_notes          BOOLEAN NOT NULL DEFAULT TRUE,
  show_product_comments     BOOLEAN NOT NULL DEFAULT TRUE,
  show_addon_price          BOOLEAN NOT NULL DEFAULT TRUE,
  include_customer_receipt  BOOLEAN NOT NULL DEFAULT TRUE,
  -- [{"column_name":"qty","order":"0"}, ...] — same JSONB-not-raw-string
  -- reasoning as delivery_zone: the DB can validate it's well-formed JSON.
  column_alignment          JSONB NOT NULL DEFAULT
    '[{"column_name":"qty","order":"0"},{"column_name":"item","order":"1"},{"column_name":"price","order":"2"},{"column_name":"total","order":"3"}]',
  date_time_format          TEXT NOT NULL DEFAULT '12h' CHECK (date_time_format IN ('12h', '24h')),
  business_name             TEXT NOT NULL DEFAULT '',
  mobile_no                 TEXT NOT NULL DEFAULT '',
  address                   TEXT NOT NULL DEFAULT '',
  gst_vat_number            TEXT NOT NULL DEFAULT '',
  business_logo             BOOLEAN NOT NULL DEFAULT FALSE,
  show_time                 BOOLEAN NOT NULL DEFAULT TRUE,
  text_weight               TEXT NOT NULL DEFAULT 'normal',

  created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS print_kot_templates (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id     UUID NOT NULL UNIQUE REFERENCES branches(id) ON DELETE CASCADE,
  franchise_id  UUID NOT NULL REFERENCES franchises(id),
  is_active     BOOLEAN NOT NULL DEFAULT TRUE,
  show_time     BOOLEAN NOT NULL DEFAULT TRUE,
  text_weight   TEXT NOT NULL DEFAULT 'normal',
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS print_cancel_templates (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id     UUID NOT NULL UNIQUE REFERENCES branches(id) ON DELETE CASCADE,
  franchise_id  UUID NOT NULL REFERENCES franchises(id),
  is_active     BOOLEAN NOT NULL DEFAULT TRUE,
  show_time     BOOLEAN NOT NULL DEFAULT TRUE,
  text_weight   TEXT NOT NULL DEFAULT 'normal',
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS print_receipt_templates;
DROP TABLE IF EXISTS print_kot_templates;
DROP TABLE IF EXISTS print_cancel_templates;
-- +goose StatementEnd