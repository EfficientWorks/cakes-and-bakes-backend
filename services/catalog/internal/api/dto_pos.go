package api

import (
	"strings"

	"bakehouse/pkg/strx"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/domain"
)

// --- POS Product and Modifier DTOs ---------------------------------------------
// POS responses omit minPriceCents and maxPriceCents from products and modifier options.

type posModifierOptionDTOType struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	PriceDelta    int64   `json:"priceDelta"`
	ColourHex     *string `json:"colourHex,omitempty"`
	ImageURL      *string `json:"imageUrl,omitempty"`
	IsGlutenFree  bool    `json:"isGlutenFree"`
	IsPreSelected bool    `json:"isPreSelected"`
}

type posModifierGroupDTOType struct {
	ID         string                     `json:"id"`
	Name       string                     `json:"name"`
	Type       string                     `json:"type"`
	Kind       string                     `json:"kind"`
	IsRequired bool                       `json:"isRequired"`
	MinSelect  int                        `json:"minSelect"`
	MaxSelect  int                        `json:"maxSelect"`
	Options    []posModifierOptionDTOType `json:"options"`
}

type posProductListDTOType struct {
	ID              string                    `json:"id"`
	Name            string                    `json:"name"`
	Slug            string                    `json:"slug"`
	Description     string                    `json:"description"`
	BasePrice       int64                     `json:"basePrice"`
	PricingMode     string                    `json:"pricingMode"`
	Images          []imageDTOType            `json:"images"`
	Availability    availabilityDTOType       `json:"availability"`
	IsBestSeller    bool                      `json:"isBestSeller"`
	OccasionTags    []string                  `json:"occasionTags"`
	SeasonalTags    []seasonalTagDTOType      `json:"seasonalTags"`
	CategorySlug    string                    `json:"categorySlug"`
	IsActive        bool                      `json:"isActive"`
	ShowOnPos       bool                      `json:"showOnPos"`
	ShowOnWebsite   bool                      `json:"showOnWebsite"`
	LeadTime        int                       `json:"leadTime"`
	LeadTimeUnit    string                    `json:"leadTimeUnit"`
	MinPrepTime     int                       `json:"minPrepTime"`
	MinPrepTimeUnit string                    `json:"minPrepTimeUnit"`
	MaxPrepTime     int                       `json:"maxPrepTime"`
	MaxPrepTimeUnit string                    `json:"maxPrepTimeUnit"`
	VideoURL        *string                   `json:"videoUrl"`
	AvailableFrom   *string                   `json:"availableFrom"`
	AvailableUntil  *string                   `json:"availableUntil"`
	IsModifier      bool                      `json:"isModifier"`
	MinPriceCents   *int64                    `json:"minPriceCents"`
	MaxPriceCents   *int64                    `json:"maxPriceCents"`
	StorePrices     *[]adminStorePriceDTO     `json:"storePrices,omitempty"`
	BranchIds       *[]string                 `json:"storeIds,omitempty"`
	ModifierGroups  []posModifierGroupDTOType `json:"modifierGroups,omitempty"`
}

func posProductListDTO(p *domain.ProductWithAvailability) posProductListDTOType {
	base := productListDTO(p)
	applyStorefrontPriceRange(&base, &p.Product)
	out := posProductListDTOFrom(base)
	if p.IsVariationPriced() && len(p.ModifierGroups) > 0 {
		out.ModifierGroups = posModifierGroupsDTO(p.ModifierGroups)
	}
	return out
}

// posModifierGroupsDTO is the till's rendering of a product's groups — the
// storefront shape without per-branch min/max on each option.
func posModifierGroupsDTO(groups []catalogapi.ModifierGroup) []posModifierGroupDTOType {
	out := make([]posModifierGroupDTOType, 0, len(groups))
	for _, g := range groups {
		heading := g.DisplayName
		if strings.TrimSpace(heading) == "" {
			heading = g.Name
		}
		gd := posModifierGroupDTOType{
			ID:         g.ID,
			Name:       heading,
			Type:       g.Type,
			Kind:       string(g.Kind),
			IsRequired: g.IsRequired,
			MinSelect:  g.MinSelect,
			MaxSelect:  g.MaxSelect,
		}
		for _, o := range g.Options {
			gd.Options = append(gd.Options, posModifierOptionDTOType{
				ID:            o.ID,
				Name:          o.Name,
				PriceDelta:    o.PriceDeltaCents,
				ColourHex:     o.ColourHex,
				ImageURL:      o.ImageURL,
				IsGlutenFree:  o.IsGlutenFree,
				IsPreSelected: o.IsPreSelected,
			})
		}
		gd.Options = orEmpty(gd.Options)
		out = append(out, gd)
	}
	return out
}

func posProductListDTOFrom(base productListDTOType) posProductListDTOType {
	return posProductListDTOType{
		ID:              base.ID,
		Name:            base.Name,
		Slug:            base.Slug,
		Description:     base.Description,
		BasePrice:       base.BasePrice,
		PricingMode:     base.PricingMode,
		Images:          base.Images,
		Availability:    base.Availability,
		IsBestSeller:    base.IsBestSeller,
		OccasionTags:    base.OccasionTags,
		SeasonalTags:    base.SeasonalTags,
		CategorySlug:    base.CategorySlug,
		IsActive:        base.IsActive,
		ShowOnPos:       base.ShowOnPos,
		ShowOnWebsite:   base.ShowOnWebsite,
		LeadTime:        base.LeadTime,
		LeadTimeUnit:    base.LeadTimeUnit,
		MinPrepTime:     base.MinPrepTime,
		MinPrepTimeUnit: base.MinPrepTimeUnit,
		MaxPrepTime:     base.MaxPrepTime,
		MaxPrepTimeUnit: base.MaxPrepTimeUnit,
		VideoURL:        base.VideoURL,
		AvailableFrom:   base.AvailableFrom,
		AvailableUntil:  base.AvailableUntil,
		IsModifier:      base.IsModifier,
		MinPriceCents:   base.MinPriceCents,
		MaxPriceCents:   base.MaxPriceCents,
		StorePrices:     base.StorePrices,
		BranchIds:       base.BranchIds,
	}
}

type posProductDetailDTOType struct {
	posProductListDTOType
	LeadTime        int                       `json:"leadTime"`
	LeadTimeUnit    string                    `json:"leadTimeUnit"`
	PriceNote       string                    `json:"priceNote"`
	ModifierGroups  []posModifierGroupDTOType `json:"modifierGroups"`
	SimilarProducts []posProductListDTOType   `json:"similarProducts"`
	Allergens       []string                  `json:"allergens"`
	Dietary         []string                  `json:"dietary"`
	Freshness       *string                   `json:"freshness"`
	Ingredients     *string                   `json:"ingredients"`
	Returns         *string                   `json:"returns"`
	Disclaimer      *string                   `json:"disclaimer"`
}

func posProductDetailDTO(p *domain.ProductWithAvailability, similar []catalogapi.Product) posProductDetailDTOType {
	baseList := productListDTO(p)
	applyStorefrontPriceRange(&baseList, &p.Product)
	posList := posProductListDTOFrom(baseList)
	posList.StorePrices = nil
	posList.BranchIds = nil

	out := posProductDetailDTOType{
		posProductListDTOType: posList,
		LeadTime:              p.LeadTime,
		LeadTimeUnit:          strx.OrDefault(p.LeadTimeUnit, "hours"),
		PriceNote:             priceNoteFor(p),
		Allergens:             orEmpty(p.AllergenFlags),
		Dietary:               orEmpty(p.DietaryFlags),
		Freshness:             p.FreshnessNote,
		Ingredients:           p.IngredientsNote,
		Disclaimer:            p.Disclaimer,
		Returns:               p.ReturnsNote,
	}
	out.ModifierGroups = posModifierGroupsDTO(p.ModifierGroups)
	out.posProductListDTOType.ModifierGroups = nil
	for _, sp := range similar {
		s := sp
		ps := productSummaryDTO(&s)
		applyStorefrontPriceRange(&ps, &s)
		out.SimilarProducts = append(out.SimilarProducts, posProductListDTOFrom(ps))
	}
	out.SimilarProducts = orEmpty(out.SimilarProducts)
	return out
}
