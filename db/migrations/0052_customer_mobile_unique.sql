-- +goose Up
-- +goose StatementBegin

CREATE OR REPLACE FUNCTION normalise_mobile(raw TEXT) RETURNS TEXT
    LANGUAGE sql IMMUTABLE STRICT AS $$
    SELECT NULLIF(regexp_replace(raw, '[^0-9]', '', 'g'), '')
$$;

DO $$
DECLARE dupes TEXT;
BEGIN
    SELECT string_agg(format('%s x%s [%s]', digits, n, ids), '; ') INTO dupes
      FROM (SELECT normalise_mobile(mobile_number) AS digits, count(*) AS n,
                   string_agg(id::text, ',') AS ids
              FROM customers
             WHERE deleted_at IS NULL AND normalise_mobile(mobile_number) IS NOT NULL
             GROUP BY 1 HAVING count(*) > 1) d;
    IF dupes IS NOT NULL THEN
        RAISE EXCEPTION 'db=% host=%:% - duplicate live mobiles: %',
            current_database(), inet_server_addr(), inet_server_port(), dupes;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_mobile
    ON customers (normalise_mobile(mobile_number))
    WHERE deleted_at IS NULL AND normalise_mobile(mobile_number) IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
DROP INDEX IF EXISTS idx_customers_mobile;
DROP FUNCTION IF EXISTS normalise_mobile(TEXT);