-- +goose Up
-- +goose StatementBegin

/*
 * A POS customer's email is unique within its store, as its mobile is (0081).
 *
 * With one brand-wide index, an email used by a website account or by a
 * customer at another store refused the new POS customer with "already
 * exists" while the till's own list had nobody with it.
 *
 * Website accounts keep brand-wide uniqueness under the old name: an email
 * signs in to one online account. Both indexes are subsets of the old one,
 * so existing data satisfies them.
 */
DROP INDEX IF EXISTS idx_customers_email;

CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_email
    ON customers (lower(email))
    WHERE deleted_at IS NULL AND email IS NOT NULL AND email <> '' AND customer_type <> 'pos';

CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_pos_email_per_branch
    ON customers (branch_id, lower(email))
    WHERE deleted_at IS NULL AND email IS NOT NULL AND email <> '' AND customer_type = 'pos';

-- +goose StatementEnd

-- +goose Down
DROP INDEX IF EXISTS idx_customers_pos_email_per_branch;
DROP INDEX IF EXISTS idx_customers_email;
CREATE UNIQUE INDEX IF NOT EXISTS idx_customers_email ON customers (lower(email))
    WHERE deleted_at IS NULL AND email IS NOT NULL AND email <> '';
