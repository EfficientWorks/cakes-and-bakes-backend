-- +goose Up
-- +goose StatementBegin

ALTER TABLE branch_product_rules
ALTER COLUMN available_days
DROP NOT NULL;

ALTER TABLE cart_items
ADD COLUMN available BOOLEAN NOT NULL DEFAULT true;

CREATE TABLE branch_category_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
    branch_id UUID NOT NULL REFERENCES branches (id) ON DELETE CASCADE,
    category_id UUID NOT NULL REFERENCES product_categories (id) ON DELETE CASCADE,
    branch_enabled BOOLEAN NOT NULL DEFAULT true,
    available_from TIMESTAMPTZ,
    available_until TIMESTAMPTZ,
    reason TEXT,
    updated_by UUID,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (branch_id, category_id)
);

CREATE INDEX idx_branch_category_rules_branch ON branch_category_rules (branch_id);

CREATE INDEX idx_branch_category_rules_product ON branch_category_rules (category_id);

CREATE TABLE branch_modifier_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
    branch_id UUID NOT NULL REFERENCES branches (id) ON DELETE CASCADE,
    modifier_group_id UUID NOT NULL REFERENCES modifier_groups (id) ON DELETE CASCADE,
    modifier_option_id UUID NOT NULL REFERENCES modifier_options (id) ON DELETE CASCADE,
    price_override_cents BIGINT,
    available_from TIMESTAMPTZ,
    available_until TIMESTAMPTZ,
    reason TEXT,
    updated_by UUID,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (
        branch_id,
        modifier_group_id,
        modifier_option_id
    )
);

CREATE INDEX idx_branch_modifier_rules_branch ON branch_modifier_rules (branch_id);

CREATE INDEX idx_branch_modifier_rules_modifier_group ON branch_modifier_rules (modifier_group_id);

CREATE INDEX idx_branch_modifier_rules_modifier_option ON branch_modifier_rules (modifier_option_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE branch_product_rules
ALTER COLUMN available_days
SET NOT NULL;

ALTER TABLE cart_items DROP COLUMN available;

DROP TABLE IF EXISTS branch_category_rules;

DROP TABLE IF EXISTS branch_modifier_rules;

-- +goose StatementEnd