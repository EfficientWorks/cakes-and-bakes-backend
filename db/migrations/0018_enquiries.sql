-- +goose Up
-- Enquiry capture (SF-34) and newsletter consent (NOT-06).
--
-- One table with a type discriminator rather than three near-identical tables:
-- every enquiry shares the same lifecycle, the same routing rule and the same
-- admin queue, and only the captured fields differ. Those live in `details`.

CREATE TABLE enquiries (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type          TEXT NOT NULL CHECK (type IN ('custom_order', 'franchise', 'contact')),

    -- Common across every form, so they are columns rather than JSON: admin
    -- searches and sorts on these.
    name          TEXT NOT NULL,
    email         TEXT NOT NULL,
    phone         TEXT,
    message       TEXT,

    -- Routing. A custom-order enquiry names a store, so it belongs to that
    -- branch and its franchise; a franchise enquiry is brand-level and both
    -- stay null (SF-34: routed to the correct recipient).
    franchise_id  UUID REFERENCES franchises(id),
    branch_id     UUID REFERENCES branches(id),

    -- Type-specific fields: event date/time/type, guest count, interested
    -- location, and so on.
    details       JSONB NOT NULL DEFAULT '{}'::jsonb,

    status        TEXT NOT NULL DEFAULT 'new' CHECK (status IN ('new', 'in_progress', 'closed')),
    handled_by    UUID REFERENCES staff_users(id),
    handled_at    TIMESTAMPTZ,
    staff_note    TEXT,

    source_ip     TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_enquiries_queue  ON enquiries (status, created_at DESC);
CREATE INDEX idx_enquiries_branch ON enquiries (branch_id, created_at DESC);
CREATE INDEX idx_enquiries_type   ON enquiries (type, created_at DESC);
CREATE INDEX idx_enquiries_email  ON enquiries (lower(email));

-- Newsletter consent. Marketing requires explicit opt-in with a stored
-- timestamp (NOT-06, SF-24), and unsubscribing must be recorded rather than
-- deleting the row, so a later re-subscribe is still an auditable consent.
CREATE TABLE newsletter_subscribers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL,
    source          TEXT NOT NULL DEFAULT 'footer',
    consented_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    unsubscribed_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_newsletter_email ON newsletter_subscribers (lower(email));

-- +goose Down
DROP TABLE IF EXISTS newsletter_subscribers;
DROP TABLE IF EXISTS enquiries;
