package api

import (
	"bakehouse/pkg/strx"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/domain"
)

const rfc3339 = "2006-01-02T15:04:05Z07:00"

// --- Storefront-facing DTOs -----------------------------------------------------

type categoryDTOType struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description"`
	// ImageURL and ImageAltText drive the category tiles. Both are nullable:
	// a category with no artwork yet renders as a text tile rather than a
	// broken image.
	ImageURL     *string `json:"imageUrl"`
	ImageAltText *string `json:"imageAltText"`
	ColorCode    *string `json:"colorCode"`
}

func categoryDTO(c *catalogapi.Category) categoryDTOType {
	return categoryDTOType{
		ID: c.ID, Name: c.Name, Slug: c.Slug, Description: c.Description,
		ImageURL: c.ImageURL, ImageAltText: c.ImageAltText,
		ColorCode: c.ColorCode,
	}
}

type imageDTOType struct {
	URL       string  `json:"url"`
	AltText   *string `json:"altText"`
	Type      string  `json:"type"`
	IsPrimary bool    `json:"isPrimary"`
}

func imageDTO(i *catalogapi.ProductImage) imageDTOType {
	return imageDTOType{URL: i.URL, AltText: i.AltText, Type: i.Type, IsPrimary: i.IsPrimary}
}

type variantDTOType struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Label      *string `json:"label"`
	PriceDelta int64   `json:"priceDelta"`
	TotalPrice int64   `json:"totalPrice"`
	ServesMin  *int    `json:"servesMin"`
	ServesMax  *int    `json:"servesMax"`
}

func variantDTO(basePrice int64, v *catalogapi.ProductVariant) variantDTOType {
	return variantDTOType{
		ID: v.ID, Name: v.Name, Label: v.Label, PriceDelta: v.PriceDeltaCents,
		TotalPrice: basePrice + v.PriceDeltaCents, ServesMin: v.ServesMin, ServesMax: v.ServesMax,
	}
}

type availabilityDTOType struct {
	Available         bool    `json:"available"`
	Reason            string  `json:"reason,omitempty"`
	NextAvailableDate *string `json:"nextAvailableDate,omitempty"`
}

func availabilityDTO(a catalogapi.Availability) availabilityDTOType {
	return availabilityDTOType{Available: a.Available, Reason: a.Reason, NextAvailableDate: a.NextAvailableDate}
}

// productListDTOType is the lightweight shape for storefront listing/admin
// list — no modifier groups (CAT-07: unavailable items still appear, with a
// reason, never silently omitted).
type productListDTOType struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Slug         string              `json:"slug"`
	Description  string              `json:"description"`
	BasePrice    int64               `json:"basePrice"`
	PricingMode  string              `json:"pricingMode"`
	Images       []imageDTOType      `json:"images"`
	Availability availabilityDTOType `json:"availability"`
	IsBestSeller bool                `json:"isBestSeller"`
	OccasionTags []string            `json:"occasionTags"`
	// Seasonal tags (active and inactive); restricted dates on an active one
	// make the product unorderable for those slots.
	SeasonalTags []seasonalTagDTOType `json:"seasonalTags"`
	// Variants     []variantDTOType    `json:"variants"`
	CategorySlug  string `json:"categorySlug"`
	IsActive      bool   `json:"isActive"`
	ShowOnPos     bool   `json:"showOnPos"`
	ShowOnWebsite bool   `json:"showOnWebsite"`
	// How long the kitchen needs before this can be collected.
	LeadTime     int    `json:"leadTime"`
	LeadTimeUnit string `json:"leadTimeUnit"`
	// KDS: average preparation window (Minimum / Maximum Time).
	MinPrepTime     int    `json:"minPrepTime"`
	MinPrepTimeUnit string `json:"minPrepTimeUnit"`
	MaxPrepTime     int    `json:"maxPrepTime"`
	MaxPrepTimeUnit string `json:"maxPrepTimeUnit"`
	// The product's video (one per product), shown with the images. Null when none.
	VideoURL *string `json:"videoUrl"`
	// YYYY-MM-DD, or null for unbounded at that end.
	AvailableFrom  *string `json:"availableFrom"`
	AvailableUntil *string `json:"availableUntil"`
	IsModifier     bool    `json:"isModifier"`

	// MinPriceCents is the lowest possible starting price across store overrides.
	// MaxPriceCents is the highest possible price across store overrides and modifiers.
	// null when there are no branch price overrides for this product.
	MinPriceCents *int64 `json:"minPriceCents"`
	MaxPriceCents *int64 `json:"maxPriceCents"`

	StorePrices *[]adminStorePriceDTO `json:"storePrices,omitempty"`
	BranchIds   *[]string             `json:"storeIds,omitempty"`

	ModifierGroups []modifierGroupDTOType `json:"modifierGroups,omitempty"`
}

func productListDTO(p *domain.ProductWithAvailability) productListDTOType {
	out := productListDTOType{
		ID: p.ID, Name: p.Name, Slug: p.Slug, Description: p.Description, BasePrice: p.BasePriceCents,
		PricingMode:  p.EffectivePricingMode(),
		Availability: availabilityDTO(p.Availability), IsBestSeller: p.IsBestSeller,
		OccasionTags: orEmpty(p.OccasionTags), CategorySlug: p.CategorySlug, IsActive: p.IsActive,
		SeasonalTags: seasonalTagDTOs(p.SeasonalTags),
		ShowOnPos:    p.ShowOnPos, ShowOnWebsite: p.ShowOnWebsite,
		LeadTime: p.LeadTime, LeadTimeUnit: strx.OrDefault(p.LeadTimeUnit, "hours"),
		MinPrepTime: p.MinPrepTime, MinPrepTimeUnit: strx.OrDefault(p.MinPrepTimeUnit, "minutes"),
		MaxPrepTime: p.MaxPrepTime, MaxPrepTimeUnit: strx.OrDefault(p.MaxPrepTimeUnit, "minutes"),
		VideoURL:      p.VideoURL,
		AvailableFrom: formatDatePtr(p.AvailableFrom), AvailableUntil: formatDatePtr(p.AvailableUntil),
		IsModifier: p.IsModifier || len(p.ModifierGroups) > 0,
	}
	for _, img := range p.Images {
		out.Images = append(out.Images, imageDTO(&img))
	}
	out.Images = orEmpty(out.Images)

	// Compute the base price range across all store overrides first.
	// Only set min/max price range if there are branch product rules with price overrides.
	var minPricePtr *int64
	var maxPricePtr *int64

	hasOverride := false
	minBase := p.BasePriceCents
	maxBase := p.BasePriceCents
	for i := range p.StorePrices {
		if p.StorePrices[i].PriceOverrideCents != nil && *p.StorePrices[i].PriceOverrideCents != 0 {
			hasOverride = true
			v := *p.StorePrices[i].PriceOverrideCents
			if v < minBase {
				minBase = v
			}
			if v > maxBase {
				maxBase = v
			}
		}
	}

	if hasOverride {
		// min/max are purely across base price and branch price overrides —
		// modifiers are intentionally excluded from this range.
		calcMin := minBase
		calcMax := maxBase
		minPricePtr = &calcMin
		maxPricePtr = &calcMax
	}

	out.MinPriceCents = minPricePtr
	out.MaxPriceCents = maxPricePtr

	if p.IsVariationPriced() && len(p.ModifierGroups) > 0 {
		out.ModifierGroups = modifierGroupsStorefrontDTO(p.ModifierGroups)
		if vMin, vMax, has := p.VariationPriceRange(); has {
			out.MinPriceCents, out.MaxPriceCents = &vMin, &vMax
		}
	}

	if p.StorePrices != nil {
		storePrices := make([]adminStorePriceDTO, 0, len(p.StorePrices))
		for i := range p.StorePrices {
			price := p.StorePrices[i].PriceOverrideCents
			if price == nil {
				base := p.BasePriceCents
				price = &base
			}
			storePrices = append(storePrices, adminStorePriceDTO{
				BranchId:   p.StorePrices[i].BranchID,
				Store:      p.StorePrices[i].BranchName,
				PriceCents: price,
			})
		}
		out.StorePrices = &storePrices
	}

	if p.BranchIds != nil {
		ids := orEmpty(p.BranchIds)
		out.BranchIds = &ids
	}

	return out
}

func productSummaryDTO(p *catalogapi.Product) productListDTOType {
	pa := domain.ProductWithAvailability{Product: *p, Availability: catalogapi.Availability{Available: p.IsActive}}
	return productListDTO(&pa)
}

// orEmpty guarantees a JSON array rather than `null`.
//
// A nil Go slice marshals to `null`, and a client that reasonably does
// `response.similarProducts.length` then crashes on a product that simply has
// no similar ones. Every slice field on the way out goes through here, so
// "none" is always `[]` and consumers never have to tell the two apart.
func orEmpty[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}
