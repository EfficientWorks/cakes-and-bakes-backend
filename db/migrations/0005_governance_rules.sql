-- +goose Up

-- The operational definition of "super admin can only change the details"
-- and "each franchise manages their products" holding true simultaneously
-- (BRD Section 5.2/5.3). Enforced server-side wherever a governed field is
-- written, not just hidden in the UI (GOV-02).
CREATE TABLE governance_rules (
    field_key   TEXT PRIMARY KEY,
    level       TEXT NOT NULL CHECK (level IN ('brand', 'franchise', 'branch')),
    description TEXT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO governance_rules (field_key, level, description) VALUES
    ('product.core_fields',        'brand',     'Core product name, description, imagery'),
    ('product.base_price',         'brand',     'Core product base price'),
    ('brand.assets',               'brand',     'Brand assets, logo, colours, tone'),
    ('content.policy_pages',       'brand',     'Policy, FAQ and privacy page content'),
    ('promotion.network_wide',     'brand',     'Network-wide promotions'),
    ('document.templates',         'brand',     'Document and contract templates'),
    ('governance.matrix',          'brand',     'The governance matrix itself'),
    ('product.local_from_library', 'franchise', 'Local product from approved library'),
    ('product.price_override',     'franchise', 'Price adjustment within brand band'),
    ('promotion.local',            'franchise', 'Local promotions and discount codes'),
    ('branch.delivery_zone',       'franchise', 'Delivery zone and delivery fee'),
    ('staff.accounts',             'franchise', 'Staff accounts and role assignment'),
    ('branch.opening_hours',       'branch',    'Opening hours and exception dates'),
    ('branch.slot_capacity',       'branch',    'Slot capacity and cut-off times'),
    ('product.daily_availability', 'branch',    'Daily item availability / sold out'),
    ('branch.emergency_close',     'branch',    'Emergency close'),
    ('order.lifecycle',            'branch',    'Order status, amendments, refunds'),
    ('device.configuration',       'branch',    'Printer and KDS device configuration');

-- +goose Down
DROP TABLE IF EXISTS governance_rules;
