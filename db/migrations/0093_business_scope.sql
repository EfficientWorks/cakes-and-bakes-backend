-- +goose Up
-- +goose StatementBegin

/*
 * Every business-owned row says which business owns it.
 *
 * Catalogue, customers, orders, coupons, tags, site content, enquiries,
 * newsletter sign-ups and invoices belong to one business. Rows under a store
 * (cash days, printers, slots, …) already belong to it through the store.
 *
 * Rows created before businesses existed keep business_id NULL, and stay
 * visible only to accounts that have no business either. Names that had to be
 * unique across the whole system (a product's web address, a coupon code, a
 * customer's email) now only have to be unique within a business:
 * NULLS NOT DISTINCT keeps them unique among the older, business-less rows.
 */
ALTER TABLE products               ADD COLUMN business_id UUID REFERENCES businesses (id);
ALTER TABLE product_categories     ADD COLUMN business_id UUID REFERENCES businesses (id);
ALTER TABLE modifier_groups        ADD COLUMN business_id UUID REFERENCES businesses (id);
ALTER TABLE coupons                ADD COLUMN business_id UUID REFERENCES businesses (id);
ALTER TABLE seasonal_tags          ADD COLUMN business_id UUID REFERENCES businesses (id);
ALTER TABLE site_content           ADD COLUMN business_id UUID REFERENCES businesses (id);
ALTER TABLE customers              ADD COLUMN business_id UUID REFERENCES businesses (id);
ALTER TABLE orders                 ADD COLUMN business_id UUID REFERENCES businesses (id);
ALTER TABLE enquiries              ADD COLUMN business_id UUID REFERENCES businesses (id);
ALTER TABLE newsletter_subscribers ADD COLUMN business_id UUID REFERENCES businesses (id);
ALTER TABLE invoices               ADD COLUMN business_id UUID REFERENCES businesses (id);

-- Orders already know their store; copy its business so reports can filter
-- on the order row alone.
UPDATE orders o SET business_id = b.business_id FROM branches b WHERE b.id = o.branch_id AND b.business_id IS NOT NULL;

-- New orders take their store's business. A trigger rather than application
-- code, so every insert path (website, POS, phone, KDS manual) is covered and
-- an order can never be filed under the wrong business.
CREATE FUNCTION orders_set_business() RETURNS trigger AS $$
BEGIN
    IF NEW.business_id IS NULL THEN
        SELECT business_id INTO NEW.business_id FROM branches WHERE id = NEW.branch_id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER orders_set_business BEFORE INSERT ON orders
    FOR EACH ROW EXECUTE FUNCTION orders_set_business();

CREATE INDEX idx_products_business           ON products (business_id);
CREATE INDEX idx_product_categories_business ON product_categories (business_id);
CREATE INDEX idx_modifier_groups_business    ON modifier_groups (business_id);
CREATE INDEX idx_coupons_business            ON coupons (business_id);
CREATE INDEX idx_seasonal_tags_business      ON seasonal_tags (business_id);
CREATE INDEX idx_customers_business          ON customers (business_id);
CREATE INDEX idx_orders_business_created     ON orders (business_id, created_at DESC);
CREATE INDEX idx_enquiries_business          ON enquiries (business_id);
CREATE INDEX idx_invoices_business           ON invoices (business_id);

-- Unique within a business instead of across the platform.
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_slug_key;
CREATE UNIQUE INDEX products_business_slug_key ON products (business_id, slug) NULLS NOT DISTINCT;

ALTER TABLE product_categories DROP CONSTRAINT IF EXISTS product_categories_slug_key;
CREATE UNIQUE INDEX product_categories_business_slug_key ON product_categories (business_id, slug) NULLS NOT DISTINCT;

DROP INDEX IF EXISTS seasonal_tags_slug_idx;
CREATE UNIQUE INDEX seasonal_tags_business_slug_idx ON seasonal_tags (business_id, slug) NULLS NOT DISTINCT
    WHERE deleted_at IS NULL;

DROP INDEX IF EXISTS idx_coupons_code_network;
CREATE UNIQUE INDEX idx_coupons_code_network ON coupons (business_id, upper(code)) NULLS NOT DISTINCT
    WHERE branch_id IS NULL AND deleted_at IS NULL;
DROP INDEX IF EXISTS idx_coupons_one_active_new_customer;
CREATE UNIQUE INDEX idx_coupons_one_active_new_customer ON coupons (business_id) NULLS NOT DISTINCT
    WHERE applies_to = 'new_customer' AND status = 'active' AND deleted_at IS NULL;

ALTER TABLE site_content DROP CONSTRAINT IF EXISTS site_content_pkey;
CREATE UNIQUE INDEX site_content_business_key ON site_content (business_id, key) NULLS NOT DISTINCT;

DROP INDEX IF EXISTS idx_customers_email;
CREATE UNIQUE INDEX idx_customers_email ON customers (business_id, lower(email)) NULLS NOT DISTINCT
    WHERE deleted_at IS NULL AND email IS NOT NULL AND email <> '' AND customer_type <> 'pos';
DROP INDEX IF EXISTS idx_customers_online_mobile;
CREATE UNIQUE INDEX idx_customers_online_mobile ON customers (business_id, normalise_mobile(mobile_number)) NULLS NOT DISTINCT
    WHERE deleted_at IS NULL AND customer_type <> 'pos' AND normalise_mobile(mobile_number) <> '';

DROP INDEX IF EXISTS idx_newsletter_email;
CREATE UNIQUE INDEX idx_newsletter_email ON newsletter_subscribers (business_id, lower(email)) NULLS NOT DISTINCT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER IF EXISTS orders_set_business ON orders;
DROP FUNCTION IF EXISTS orders_set_business();

DROP INDEX IF EXISTS idx_newsletter_email;
CREATE UNIQUE INDEX idx_newsletter_email ON newsletter_subscribers (lower(email));
DROP INDEX IF EXISTS idx_customers_online_mobile;
CREATE UNIQUE INDEX idx_customers_online_mobile ON customers (normalise_mobile(mobile_number))
    WHERE deleted_at IS NULL AND customer_type <> 'pos' AND normalise_mobile(mobile_number) <> '';
DROP INDEX IF EXISTS idx_customers_email;
CREATE UNIQUE INDEX idx_customers_email ON customers (lower(email))
    WHERE deleted_at IS NULL AND email IS NOT NULL AND email <> '' AND customer_type <> 'pos';
DROP INDEX IF EXISTS site_content_business_key;
ALTER TABLE site_content ADD PRIMARY KEY (key);
DROP INDEX IF EXISTS idx_coupons_one_active_new_customer;
CREATE UNIQUE INDEX idx_coupons_one_active_new_customer ON coupons ((true))
    WHERE applies_to = 'new_customer' AND status = 'active' AND deleted_at IS NULL;
DROP INDEX IF EXISTS idx_coupons_code_network;
CREATE UNIQUE INDEX idx_coupons_code_network ON coupons (upper(code)) WHERE branch_id IS NULL AND deleted_at IS NULL;
DROP INDEX IF EXISTS seasonal_tags_business_slug_idx;
CREATE UNIQUE INDEX seasonal_tags_slug_idx ON seasonal_tags (slug) WHERE deleted_at IS NULL;
DROP INDEX IF EXISTS product_categories_business_slug_key;
ALTER TABLE product_categories ADD CONSTRAINT product_categories_slug_key UNIQUE (slug);
DROP INDEX IF EXISTS products_business_slug_key;
ALTER TABLE products ADD CONSTRAINT products_slug_key UNIQUE (slug);

ALTER TABLE invoices               DROP COLUMN IF EXISTS business_id;
ALTER TABLE newsletter_subscribers DROP COLUMN IF EXISTS business_id;
ALTER TABLE enquiries              DROP COLUMN IF EXISTS business_id;
ALTER TABLE orders                 DROP COLUMN IF EXISTS business_id;
ALTER TABLE customers              DROP COLUMN IF EXISTS business_id;
ALTER TABLE site_content           DROP COLUMN IF EXISTS business_id;
ALTER TABLE seasonal_tags          DROP COLUMN IF EXISTS business_id;
ALTER TABLE coupons                DROP COLUMN IF EXISTS business_id;
ALTER TABLE modifier_groups        DROP COLUMN IF EXISTS business_id;
ALTER TABLE product_categories     DROP COLUMN IF EXISTS business_id;
ALTER TABLE products               DROP COLUMN IF EXISTS business_id;

-- +goose StatementEnd
