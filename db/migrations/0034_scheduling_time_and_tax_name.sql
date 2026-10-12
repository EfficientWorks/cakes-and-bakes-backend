-- +goose Up
-- +goose StatementBegin

-- Scheduling gains a time of day.
--
-- A date alone cannot say "this goes on sale at 9am on Friday" — it could only
-- mean the whole of Friday, so a product meant for a morning launch was live
-- from midnight, and one being withdrawn stayed up until the end of its last
-- day. Every existing value becomes midnight in the branch's own reckoning,
-- which is what a plain date already meant.
ALTER TABLE products
  ALTER COLUMN available_from  TYPE TIMESTAMPTZ USING available_from::timestamptz,
  ALTER COLUMN available_until TYPE TIMESTAMPTZ USING available_until::timestamptz;

ALTER TABLE product_categories
  ALTER COLUMN available_from  TYPE TIMESTAMPTZ USING available_from::timestamptz,
  ALTER COLUMN available_until TYPE TIMESTAMPTZ USING available_until::timestamptz;

ALTER TABLE branch_product_rules
  ALTER COLUMN available_from  TYPE TIMESTAMPTZ USING available_from::timestamptz,
  ALTER COLUMN available_until TYPE TIMESTAMPTZ USING available_until::timestamptz;


-- Tax is the outlet's own affair, not a jurisdiction lookup.
--
-- The state and county columns implied a rate derived from where the shop is,
-- which nothing here does — the rate is typed in by hand per outlet. What was
-- missing was the one thing a receipt actually needs: what to call the tax.
-- "Tax" is not what a customer in New Jersey expects to read, and it is not
-- what the shop's accountant expects either.
ALTER TABLE branches ADD COLUMN IF NOT EXISTS tax_name TEXT NOT NULL DEFAULT 'Sales Tax';

-- Seeded from whatever the jurisdiction columns held, so an outlet that had
-- said "NJ" keeps saying something recognisable rather than reverting to the
-- generic default.
UPDATE branches
   SET tax_name = trim(coalesce(tax_state, '') || ' Sales Tax')
 WHERE coalesce(trim(tax_state), '') <> '';

ALTER TABLE branches DROP COLUMN IF EXISTS tax_state;
ALTER TABLE branches DROP COLUMN IF EXISTS tax_county;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE branches ADD COLUMN IF NOT EXISTS tax_state TEXT;
ALTER TABLE branches ADD COLUMN IF NOT EXISTS tax_county TEXT;
ALTER TABLE branches DROP COLUMN IF EXISTS tax_name;

ALTER TABLE branch_product_rules
  ALTER COLUMN available_from  TYPE DATE USING available_from::date,
  ALTER COLUMN available_until TYPE DATE USING available_until::date;
ALTER TABLE product_categories
  ALTER COLUMN available_from  TYPE DATE USING available_from::date,
  ALTER COLUMN available_until TYPE DATE USING available_until::date;
ALTER TABLE products
  ALTER COLUMN available_from  TYPE DATE USING available_from::date,
  ALTER COLUMN available_until TYPE DATE USING available_until::date;
-- +goose StatementEnd
