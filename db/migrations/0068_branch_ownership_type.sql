-- +goose Up

ALTER TABLE branches ADD COLUMN IF NOT EXISTS ownership_type TEXT;

UPDATE branches b
SET ownership_type = CASE WHEN COALESCE(f.is_company_owned, false)
                          THEN 'corporate store' ELSE 'franchise store' END
FROM franchises f
WHERE f.id = b.franchise_id AND b.ownership_type IS NULL;

UPDATE branches SET ownership_type = 'franchise store' WHERE ownership_type IS NULL;

ALTER TABLE branches
    ALTER COLUMN ownership_type SET DEFAULT 'franchise store',
    ALTER COLUMN ownership_type SET NOT NULL;

ALTER TABLE branches DROP CONSTRAINT IF EXISTS branches_ownership_type_check;
ALTER TABLE branches ADD CONSTRAINT branches_ownership_type_check
    CHECK (ownership_type IN ('corporate store', 'franchise store'));

-- Existing franchise references stay intact. New branch-only writes use this
-- compatibility parent until the legacy franchise graph is retired.
INSERT INTO franchises (id, name, legal_entity, is_company_owned, status)
VALUES ('00000000-0000-0000-0000-000000000001', 'Legacy branch compatibility', 'Baked Bouquet', true, 'active')
ON CONFLICT (id) DO NOTHING;

ALTER TABLE branches
    ALTER COLUMN franchise_id SET DEFAULT '00000000-0000-0000-0000-000000000001';

-- +goose Down
ALTER TABLE branches
    ALTER COLUMN franchise_id DROP DEFAULT,
    DROP CONSTRAINT IF EXISTS branches_ownership_type_check,
    DROP COLUMN IF EXISTS ownership_type;