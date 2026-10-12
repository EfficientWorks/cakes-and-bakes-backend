-- +goose Up

-- Per-branch scheduling configuration (SCH-01, SCH-02, SCH-05).
CREATE TABLE branch_schedule_config (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id             UUID UNIQUE NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    slot_duration_mins    INTEGER NOT NULL DEFAULT 60,
    pickup_cutoff_time    TIME NOT NULL DEFAULT '16:00',
    delivery_cutoff_time  TIME NOT NULL DEFAULT '14:00',
    max_orders_per_slot   INTEGER NOT NULL DEFAULT 10,
    default_lead_hours    INTEGER NOT NULL DEFAULT 2,
    advance_days_max      INTEGER NOT NULL DEFAULT 30,
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Slot capacity, reserved atomically at order creation (SCH-03, SCH-04,
-- NFR-05). One row per (branch, date, slot, fulfilment type); created
-- lazily the first time a slot is offered or booked.
CREATE TABLE slot_capacity (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    branch_id       UUID NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
    date            DATE NOT NULL,
    slot_start      TIME NOT NULL,
    slot_end        TIME NOT NULL,
    fulfilment_type TEXT NOT NULL CHECK (fulfilment_type IN ('pickup','delivery')),
    max_capacity    INTEGER NOT NULL,
    reserved_count  INTEGER NOT NULL DEFAULT 0,

    UNIQUE (branch_id, date, slot_start, fulfilment_type)
);

CREATE INDEX idx_slot_capacity_branch_date ON slot_capacity (branch_id, date, fulfilment_type);

-- +goose Down
DROP TABLE IF EXISTS slot_capacity;
DROP TABLE IF EXISTS branch_schedule_config;
