-- +goose Up
ALTER TABLE products RENAME COLUMN lead_time_hours TO lead_time;
ALTER TABLE products ADD COLUMN IF NOT EXISTS lead_time_unit VARCHAR(20) NOT NULL DEFAULT 'hours';

-- +goose Down
ALTER TABLE products DROP COLUMN IF EXISTS lead_time_unit;
ALTER TABLE products RENAME COLUMN lead_time TO lead_time_hours;
