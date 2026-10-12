-- +goose Up

-- A branch may have no configured tax rate/name yet. Tax is configured later
-- and reads already fall back to a display label when tax_name is NULL.
ALTER TABLE branches
    ALTER COLUMN tax_name DROP NOT NULL;

-- +goose Down
-- Existing NULL values must be populated before restoring the constraint.
UPDATE branches
SET tax_name = 'Sales Tax'
WHERE tax_name IS NULL;

ALTER TABLE branches
    ALTER COLUMN tax_name SET NOT NULL;