-- +goose Up
-- +goose StatementBegin

/*
 * An order is taken in its business's currency.
 *
 * The trigger that files a new order under its store's business also sets
 * the currency from that business, so no insert path (website, POS, phone,
 * KDS) has to know it — and none can record a UK order in dollars. Orders of
 * stores from before businesses existed keep whatever the insert said.
 */
CREATE OR REPLACE FUNCTION orders_set_business() RETURNS trigger AS $$
DECLARE
    biz_currency TEXT;
BEGIN
    IF NEW.business_id IS NULL THEN
        SELECT business_id INTO NEW.business_id FROM branches WHERE id = NEW.branch_id;
    END IF;
    IF NEW.business_id IS NOT NULL THEN
        SELECT currency INTO biz_currency FROM businesses WHERE id = NEW.business_id;
        IF biz_currency IS NOT NULL THEN
            NEW.currency := biz_currency;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

CREATE OR REPLACE FUNCTION orders_set_business() RETURNS trigger AS $$
BEGIN
    IF NEW.business_id IS NULL THEN
        SELECT business_id INTO NEW.business_id FROM branches WHERE id = NEW.branch_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- +goose StatementEnd
