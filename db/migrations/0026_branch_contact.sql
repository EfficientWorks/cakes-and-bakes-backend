-- +goose Up

-- The console's branch form has always had Phone and Email fields, and the
-- API's branch response has always carried the keys — but there were no
-- columns behind them, so what a manager typed was accepted, discarded, and
-- came back empty the next time the form was opened.
--
-- These are the outlet's own public contact details, distinct from the
-- franchise's primary contact: the live site lists a different number and
-- address for each of the three shops, which is exactly the case this could
-- not represent.
ALTER TABLE branches
    ADD COLUMN phone TEXT,
    ADD COLUMN email TEXT;

-- +goose Down

ALTER TABLE branches
    DROP COLUMN IF EXISTS email,
    DROP COLUMN IF EXISTS phone;
