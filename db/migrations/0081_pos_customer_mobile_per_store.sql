-- +goose Up
-- +goose StatementBegin

/*
 * A POS customer's mobile number is unique within its store, not across the
 * brand.
 *
 * POS customers belong to one store (customers.branch_id) and each till lists
 * only its own. With one brand-wide index, a number already used by a website
 * customer, or by a customer at another store, refused the new customer with
 * "already exists" while staff could find no such person in their list.
 *
 * Website accounts keep their own brand-wide uniqueness: a mobile identifies
 * one online account.
 *
 * Both new indexes are subsets of the old one, so existing data satisfies
 * them.
 */
DROP INDEX IF EXISTS idx_customers_mobile;

CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_pos_mobile_per_branch
    ON customers (branch_id, normalise_mobile(mobile_number))
    WHERE deleted_at IS NULL AND customer_type = 'pos' AND normalise_mobile(mobile_number) IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_online_mobile
    ON customers (normalise_mobile(mobile_number))
    WHERE deleted_at IS NULL AND customer_type <> 'pos' AND normalise_mobile(mobile_number) IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
DROP INDEX IF EXISTS idx_customers_online_mobile;
DROP INDEX IF EXISTS idx_customers_pos_mobile_per_branch;
CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_mobile
    ON customers (normalise_mobile(mobile_number))
    WHERE deleted_at IS NULL AND normalise_mobile(mobile_number) IS NOT NULL;
