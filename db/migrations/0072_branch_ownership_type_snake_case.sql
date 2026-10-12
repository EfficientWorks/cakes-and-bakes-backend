-- +goose Up

-- branches.ownership_type moves from 'corporate store' / 'franchise store'
-- (with a space) to 'corporate_store' / 'franchise_store', which is what the
-- API now sends and returns as ownershipType.
ALTER TABLE branches DROP CONSTRAINT IF EXISTS branches_ownership_type_check;

UPDATE branches
   SET ownership_type = CASE
       WHEN lower(replace(trim(ownership_type), ' ', '_')) IN ('corporate_store', 'corporate', 'own') THEN 'corporate_store'
       ELSE 'franchise_store'
   END
 WHERE ownership_type IS DISTINCT FROM 'corporate_store'
   AND ownership_type IS DISTINCT FROM 'franchise_store';

ALTER TABLE branches
    ALTER COLUMN ownership_type SET DEFAULT 'franchise_store',
    ALTER COLUMN ownership_type SET NOT NULL;

ALTER TABLE branches ADD CONSTRAINT branches_ownership_type_check
    CHECK (ownership_type IN ('corporate_store', 'franchise_store'));

-- +goose Down
ALTER TABLE branches DROP CONSTRAINT IF EXISTS branches_ownership_type_check;

UPDATE branches
   SET ownership_type = CASE ownership_type
       WHEN 'corporate_store' THEN 'corporate store'
       ELSE 'franchise store'
   END;

ALTER TABLE branches ALTER COLUMN ownership_type SET DEFAULT 'franchise store';

ALTER TABLE branches ADD CONSTRAINT branches_ownership_type_check
    CHECK (ownership_type IN ('corporate store', 'franchise store'));
