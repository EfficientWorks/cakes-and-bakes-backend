package product

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/store/shared"
)

func scanProduct(row pgx.Row) (*catalogapi.Product, error) {
	var p catalogapi.Product
	err := row.Scan(&p.ID, &p.CategoryID, &p.CategorySlug, &p.Name, &p.Slug, &p.Description, &p.BasePriceCents, &p.LeadTime, &p.LeadTimeUnit,
		&p.ProductionStation, &p.AllergenFlags, &p.DietaryFlags, &p.OccasionTags, &p.FreshnessNote,
		&p.IngredientsNote, &p.ReturnsNote, &p.Disclaimer, &p.IsBestSeller, &p.IsActive, &p.SortOrder,
		&p.PopularityScore,
		&p.PriceFloorCents, &p.PriceCeilingCents,
		&p.AvailableFrom, &p.AvailableUntil, &p.CreatedAt, &p.UpdatedAt,
		&p.IsModifier,
		&p.ShowOnPos, &p.ShowOnWebsite, &p.PricingMode,
		&p.MinPrepTime, &p.MinPrepTimeUnit, &p.MaxPrepTime, &p.MaxPrepTimeUnit,
		&p.VideoURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan product: %w", err)
	}
	return &p, nil
}
