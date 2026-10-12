-- +goose Up
-- +goose StatementBegin

/*
 * Invoices raised from the console.
 *
 * Two kinds, and the difference is who is being billed:
 *
 *   customer — a person, for an order or a bespoke request. The store on it is
 *              the outlet supplying the goods, and its tax rate is the one
 *              that applies.
 *   store    — one outlet billing another, which is how a central kitchen
 *              charges a franchise for what it supplied. There is no customer.
 *
 * Kept apart from orders on purpose. An invoice may reference an order, but it
 * is a document with its own life: it is issued, sent, and settled on its own
 * timetable, and editing an order must not silently rewrite a bill already in
 * somebody's inbox. Which is also why the money is stored rather than derived —
 * the totals are what was actually billed, whatever the catalogue says later.
 */
CREATE TABLE IF NOT EXISTS invoices (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  reference      TEXT NOT NULL UNIQUE,
  kind           TEXT NOT NULL CHECK (kind IN ('customer', 'store')),

  -- The outlet supplying, and whose tax rate applies.
  branch_id      UUID NOT NULL REFERENCES branches(id),
  franchise_id   UUID NOT NULL REFERENCES franchises(id),

  -- Exactly one of these two carries who is billed, per the kind.
  customer_id       UUID REFERENCES customers(id),
  billed_branch_id  UUID REFERENCES branches(id),

  -- Optional link to the order this bills for.
  order_id       UUID REFERENCES orders(id),

  issued_date    DATE NOT NULL,
  message        TEXT NOT NULL DEFAULT '',

  bank_name      TEXT NOT NULL DEFAULT '',
  account_number TEXT NOT NULL DEFAULT '',
  sort_code      TEXT NOT NULL DEFAULT '',

  subtotal_cents BIGINT NOT NULL DEFAULT 0,
  discount_cents BIGINT NOT NULL DEFAULT 0,
  -- Both kept: the percentage is what was chosen and what the document should
  -- print, the amount is what came off and must not drift.
  discount_pct   NUMERIC(5,2),
  tax_cents      BIGINT NOT NULL DEFAULT 0,
  tax_name       TEXT NOT NULL DEFAULT 'Tax',
  total_cents    BIGINT NOT NULL DEFAULT 0,

  status         TEXT NOT NULL DEFAULT 'unpaid' CHECK (status IN ('unpaid', 'paid', 'void')),
  paid_at        TIMESTAMPTZ,
  sent_at        TIMESTAMPTZ,

  created_by     UUID REFERENCES staff_users(id),
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT invoices_billed_party CHECK (
    (kind = 'customer' AND customer_id IS NOT NULL AND billed_branch_id IS NULL) OR
    (kind = 'store'    AND billed_branch_id IS NOT NULL AND customer_id IS NULL)
  )
);

CREATE INDEX IF NOT EXISTS invoices_branch_idx    ON invoices (branch_id);
CREATE INDEX IF NOT EXISTS invoices_franchise_idx ON invoices (franchise_id);
CREATE INDEX IF NOT EXISTS invoices_issued_idx    ON invoices (issued_date DESC);
CREATE INDEX IF NOT EXISTS invoices_status_idx    ON invoices (status);

/*
 * The lines, denormalised.
 *
 * Names and prices are copied rather than joined so a renamed product or a
 * price rise does not rewrite an invoice that has already been sent. The
 * chosen options travel as JSON for the same reason — the document has to keep
 * saying "6 Cupcakes · Vanilla, Strawberry" whatever happens to the modifier
 * library afterwards.
 */
CREATE TABLE IF NOT EXISTS invoice_items (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_id       UUID NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  product_id       UUID REFERENCES products(id),
  product_name     TEXT NOT NULL,
  variant_name     TEXT NOT NULL DEFAULT '',
  modifiers        JSONB NOT NULL DEFAULT '[]'::jsonb,
  quantity         INTEGER NOT NULL CHECK (quantity > 0),
  unit_price_cents BIGINT NOT NULL CHECK (unit_price_cents >= 0),
  line_total_cents BIGINT NOT NULL CHECK (line_total_cents >= 0),
  sort_order       INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS invoice_items_invoice_idx ON invoice_items (invoice_id);

-- Reference numbers. A sequence rather than a count of rows, so a deleted
-- invoice does not hand its number to the next one.
CREATE SEQUENCE IF NOT EXISTS invoice_reference_seq START 1000;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS invoice_items;
DROP TABLE IF EXISTS invoices;
DROP SEQUENCE IF EXISTS invoice_reference_seq;
-- +goose StatementEnd
