-- +goose Up
-- +goose StatementBegin

-- The tax rate column holds two different units, and this settles it.
--
-- Everything that names the value calls it a percentage: the column is
-- tax_rate_pct, the API field is taxRatePct, and the console's input sits
-- behind a "%" with 6.625 for a placeholder. But the pricing code multiplied
-- it in raw, so an outlet set to 6.625 charged 662.5% and one set to 6% —
-- entered as 0.06 to make the arithmetic come out — charged the 6% intended.
--
-- Both spellings ended up in the table. Now that the arithmetic divides by a
-- hundred, the fraction-style rows would silently drop to a hundredth of the
-- rate they mean, so they are converted here in the same change.
--
-- A rate below 1 is taken to be a fraction. No sales-tax jurisdiction these
-- outlets trade in charges under one percent, and every affected row is 0.06
-- or 0.0663 — plainly 6% and 6.63%. A genuine sub-1% rate would have to be
-- re-entered afterwards, which is the right trade against leaving outlets
-- collecting a hundredth of what they owe.
UPDATE branches
   SET tax_rate_pct = tax_rate_pct * 100
 WHERE tax_rate_pct > 0
   AND tax_rate_pct < 1;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
UPDATE branches
   SET tax_rate_pct = tax_rate_pct / 100
 WHERE tax_rate_pct >= 1;
-- +goose StatementEnd
