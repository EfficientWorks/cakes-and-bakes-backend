-- +goose Up
-- +goose StatementBegin

-- Printer configuration, per outlet.
--
-- Two kinds behave differently and the difference is the whole point of the
-- table: an outlet can have several Wi-Fi printers going at once — kitchen,
-- hook, receipts — but only one Bluetooth printer, because the till pairs
-- with one device at a time. Selecting a second Bluetooth printer drops the
-- first rather than refusing, which is what the radio button on the settings
-- screen implies.
CREATE TABLE IF NOT EXISTS pos_printers (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id      UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
  franchise_id   UUID NOT NULL REFERENCES franchises(id),
  name           TEXT NOT NULL CHECK (length(btrim(name)) > 0),
  kind           TEXT NOT NULL CHECK (kind IN ('wifi', 'bluetooth')),
  -- Wi-Fi printers are reached by address; Bluetooth ones are paired by
  -- device id. One column each rather than a shared "address" so a Wi-Fi
  -- printer with no IP cannot be saved and then silently never print.
  ip_address     TEXT,
  device_address TEXT,
  is_connected   BOOLEAN NOT NULL DEFAULT FALSE,
  created_by     UUID REFERENCES staff_users(id),
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  CONSTRAINT pos_printers_wifi_needs_ip
    CHECK (kind <> 'wifi' OR (ip_address IS NOT NULL AND length(btrim(ip_address)) > 0))
);

CREATE INDEX IF NOT EXISTS pos_printers_branch_idx ON pos_printers (branch_id);

-- A name is how staff tell two printers apart on the settings screen, so two
-- "Kitchen Printer"s at one outlet is a support call waiting to happen.
CREATE UNIQUE INDEX IF NOT EXISTS pos_printers_branch_name_idx
  ON pos_printers (branch_id, lower(btrim(name)));

-- One IP, one printer. Two rows pointing at the same device print everything
-- twice and neither is obviously wrong when you look at them.
CREATE UNIQUE INDEX IF NOT EXISTS pos_printers_branch_ip_idx
  ON pos_printers (branch_id, lower(btrim(ip_address)))
  WHERE kind = 'wifi' AND ip_address IS NOT NULL;

-- The one-Bluetooth-at-a-time rule, in the database rather than only in the
-- service. Application code disconnects the previous one first; this is what
-- makes a bug in that code fail loudly instead of leaving an outlet with two
-- "connected" Bluetooth printers and no way to tell which is really paired.
CREATE UNIQUE INDEX IF NOT EXISTS pos_printers_one_connected_bluetooth_idx
  ON pos_printers (branch_id)
  WHERE kind = 'bluetooth' AND is_connected;


-- The cash drawer, one row per outlet per trading day.
--
-- starting_cash is the float the drawer opens with. carry_forward is the
-- "apply the same amount daily" tick: when it is set, a day that has no row
-- of its own inherits this amount rather than opening at zero, which is what
-- a shop that always floats the same £200 actually wants.
--
-- The close fields are null until the day is closed, and stay updatable
-- afterwards — a miscount found ten minutes later should be correctable
-- without inventing a second record of the same day.
CREATE TABLE IF NOT EXISTS pos_cash_days (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id           UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
  franchise_id        UUID NOT NULL REFERENCES franchises(id),
  business_date       DATE NOT NULL,

  starting_cash_cents BIGINT NOT NULL DEFAULT 0 CHECK (starting_cash_cents >= 0),
  carry_forward       BOOLEAN NOT NULL DEFAULT FALSE,

  closed_at           TIMESTAMPTZ,
  closed_by           UUID REFERENCES staff_users(id),
  counted_cash_cents  BIGINT,
  -- Both stored rather than recomputed on read: what the drawer was expected
  -- to hold is a statement about the day as it was counted. A refund entered
  -- the next morning must not quietly rewrite last night's shortage.
  expected_cash_cents BIGINT,
  shortage_cents      BIGINT,
  note                TEXT NOT NULL DEFAULT '',

  created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  UNIQUE (branch_id, business_date)
);

CREATE INDEX IF NOT EXISTS pos_cash_days_branch_date_idx
  ON pos_cash_days (branch_id, business_date DESC);


-- Money into and out of the drawer that is not a sale: the owner adding
-- change, a supplier paid in cash, petty cash for milk.
CREATE TABLE IF NOT EXISTS pos_cash_movements (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id     UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
  franchise_id  UUID NOT NULL REFERENCES franchises(id),
  business_date DATE NOT NULL,
  kind          TEXT NOT NULL CHECK (kind IN ('payin', 'payout')),
  amount_cents  BIGINT NOT NULL CHECK (amount_cents > 0),
  reason        TEXT NOT NULL DEFAULT '',
  created_by    UUID REFERENCES staff_users(id),
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS pos_cash_movements_branch_date_idx
  ON pos_cash_movements (branch_id, business_date);



-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS pos_cash_movements;
DROP TABLE IF EXISTS pos_cash_days;
DROP TABLE IF EXISTS pos_printers;
-- +goose StatementEnd
