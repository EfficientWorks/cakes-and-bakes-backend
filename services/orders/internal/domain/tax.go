package domain

import (
	"math"

	"bakehouse/services/franchise/franchiseapi"
)

/*
 * Sales tax on a taxable amount.
 *
 * The rate is a percentage, and every name it is given says so — the column is
 * tax_rate_pct, the field is TaxRatePct, and the console's input sits behind a
 * "%" with 6.625 for a placeholder. It was being multiplied in raw, so an
 * outlet configured at 6.625 charged six and a half times the order.
 *
 * Whoever set up the first outlets worked around that by entering 0.0663 where
 * they meant 6.63%, which came out right and left the column holding two
 * different units at once. Migration 0033 puts those back into percent; this
 * is the arithmetic that makes them mean what they say.
 *
 * Rounded to the nearest cent rather than truncated: over a day's orders,
 * always rounding down is a systematic shortfall in the tax actually
 * collected, and it is the shop that owes the difference.
 */
func taxOn(taxableCents int64, ratePct *float64) int64 {
	if ratePct == nil || *ratePct <= 0 || taxableCents <= 0 {
		return 0
	}
	return int64(math.Round(float64(taxableCents) * (*ratePct) / 100))
}

// branchTaxName is what this outlet calls its tax, for the basket and the
// receipt. Branches carry it now that the jurisdiction columns are gone.
func branchTaxName(branch *franchiseapi.Branch) string {
	if branch == nil || branch.TaxName == nil {
		return ""
	}
	return *branch.TaxName
}
