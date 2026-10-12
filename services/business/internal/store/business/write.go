package business

import (
	"context"

	"bakehouse/services/business/businessapi"
	"bakehouse/services/business/internal/store/shared"
)

// CreateBusiness inserts a fully resolved business (country defaults already
// applied by the domain). A slug already in use returns shared.ErrDuplicate.
func (r *Repo) CreateBusiness(ctx context.Context, b businessapi.Business) (*businessapi.Business, error) {
	const q = `
		INSERT INTO businesses (name, slug, country_code, currency, timezone, locale, tax_mode, tax_name,
			default_tax_rate_pct, contact_email, contact_phone, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		RETURNING ` + businessColumns
	out, err := scanBusiness(r.pool.QueryRow(ctx, q, b.Name, b.Slug, b.CountryCode, b.Currency, b.Timezone,
		b.Locale, b.TaxMode, b.TaxName, b.DefaultTaxRatePct, b.ContactEmail, b.ContactPhone, b.Status))
	return out, shared.Classify(err)
}

// UpdateBusiness applies a patch; nil fields keep their value.
func (r *Repo) UpdateBusiness(ctx context.Context, id string, p businessapi.BusinessPatch) (*businessapi.Business, error) {
	const q = `
		UPDATE businesses SET
			name                 = COALESCE($2, name),
			contact_email        = COALESCE($3, contact_email),
			contact_phone        = COALESCE($4, contact_phone),
			currency             = COALESCE($5, currency),
			timezone             = COALESCE($6, timezone),
			locale               = COALESCE($7, locale),
			tax_mode             = COALESCE($8, tax_mode),
			tax_name             = COALESCE($9, tax_name),
			default_tax_rate_pct = COALESCE($10, default_tax_rate_pct),
			status               = COALESCE($11, status),
			updated_at           = NOW()
		WHERE id = $1
		RETURNING ` + businessColumns
	return scanBusiness(r.pool.QueryRow(ctx, q, id, p.Name, p.ContactEmail, p.ContactPhone, p.Currency,
		p.Timezone, p.Locale, p.TaxMode, p.TaxName, p.DefaultTaxRatePct, p.Status))
}
