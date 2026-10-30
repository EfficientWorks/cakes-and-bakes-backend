-- +goose Up
-- +goose StatementBegin

/*
 * Wholesale applications are taken as enquiries.
 *
 * The storefront's "Wholesale Register" form records a trade application in
 * the enquiry queue (trade number and sort code in `details`) instead of
 * creating an account, so the type check gains 'wholesale'.
 */
ALTER TABLE enquiries DROP CONSTRAINT IF EXISTS enquiries_type_check;
ALTER TABLE enquiries ADD CONSTRAINT enquiries_type_check
    CHECK (type IN ('custom_order', 'franchise', 'contact', 'wholesale'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Fails while wholesale enquiries exist; delete or retype them first.
ALTER TABLE enquiries DROP CONSTRAINT IF EXISTS enquiries_type_check;
ALTER TABLE enquiries ADD CONSTRAINT enquiries_type_check
    CHECK (type IN ('custom_order', 'franchise', 'contact'));

-- +goose StatementEnd
