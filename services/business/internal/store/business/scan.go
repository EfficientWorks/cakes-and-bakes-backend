package business

import (
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/business/businessapi"
	"bakehouse/services/business/internal/store/shared"
)

func scanBusiness(row pgx.Row) (*businessapi.Business, error) {
	var b businessapi.Business
	err := row.Scan(&b.ID, &b.Name, &b.Slug, &b.CountryCode, &b.Currency, &b.Timezone, &b.Locale,
		&b.TaxMode, &b.TaxName, &b.DefaultTaxRatePct, &b.ContactEmail, &b.ContactPhone, &b.LogoURL, &b.WebsiteURL, &b.Status,
		&b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}
