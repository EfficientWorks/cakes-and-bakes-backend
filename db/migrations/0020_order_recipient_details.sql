-- +goose Up

-- Fields the storefront checkout collects that the order had nowhere to keep.
--
-- All four come from the "Recipient" and "Fulfilment" steps of the web
-- checkout. Until now the two delivery preferences could only be smuggled into
-- delivery_instructions as prose, which meant nothing downstream could filter
-- or act on them, and the recipient email and gift reason were simply dropped.

-- The receiver's own email, distinct from purchaser_email.
--
-- purchaser_email is who pays and where the receipt goes; this is where a
-- delivery notification to the *recipient* would go. Nullable because most
-- gift orders only ever have the purchaser's address, and self-purchases have
-- no separate recipient at all.
ALTER TABLE orders ADD COLUMN recipient_email TEXT;

-- Why the gift is being sent — "Birthday", "Sympathy", and so on.
--
-- Free text rather than a CHECK constraint or a lookup table: the list is
-- presentational, the storefront owns it, and a new occasion should not need a
-- migration. It drives card wording and gives reporting something to group by.
ALTER TABLE orders ADD COLUMN gift_reason TEXT;

-- Two delivery preferences, as booleans rather than prose.
--
-- Defaulted false so every existing order reads as "no preference expressed",
-- which is what it was.
ALTER TABLE orders ADD COLUMN photo_on_delivery BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE orders ADD COLUMN surprise_delivery BOOLEAN NOT NULL DEFAULT false;

-- +goose Down

ALTER TABLE orders DROP COLUMN surprise_delivery;
ALTER TABLE orders DROP COLUMN photo_on_delivery;
ALTER TABLE orders DROP COLUMN gift_reason;
ALTER TABLE orders DROP COLUMN recipient_email;
