package api

import (
	"strings"

	"bakehouse/pkg/strx"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/domain"
)

// --- Product detail DTO -----------------------------------------------------------

type modifierOptionDTOType struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	PriceDelta    int64   `json:"priceDelta"`
	MinPriceCents *int64  `json:"minPriceCents"`
	MaxPriceCents *int64  `json:"maxPriceCents"`
	ColourHex     *string `json:"colourHex,omitempty"`
	ImageURL      *string `json:"imageUrl,omitempty"`
	IsGlutenFree  bool    `json:"isGlutenFree"`
	IsPreSelected bool    `json:"isPreSelected"`
}

func modifierOptionStorefrontDTO(o *catalogapi.ModifierOption) modifierOptionDTOType {
	var minPricePtr *int64
	var maxPricePtr *int64
	hasOverride := false
	minPrice := o.PriceDeltaCents
	maxPrice := o.PriceDeltaCents
	if o.StorePrices != nil {
		for _, sp := range o.StorePrices {
			if sp.PriceOverrideCents != nil && *sp.PriceOverrideCents != 0 {
				hasOverride = true
				v := *sp.PriceOverrideCents
				if v < minPrice {
					minPrice = v
				}
				if v > maxPrice {
					maxPrice = v
				}
			}
		}
	}
	if hasOverride {
		minPricePtr = &minPrice
		maxPricePtr = &maxPrice
	}
	return modifierOptionDTOType{
		ID:            o.ID,
		Name:          o.Name,
		PriceDelta:    o.PriceDeltaCents,
		MinPriceCents: minPricePtr,
		MaxPriceCents: maxPricePtr,
		ColourHex:     o.ColourHex,
		ImageURL:      o.ImageURL,
		IsGlutenFree:  o.IsGlutenFree,
		IsPreSelected: o.IsPreSelected,
	}
}

type modifierGroupDTOType struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	// Which of the two things this is: "variation" (one priced choice, drawn as
	// options) or "addon" (several optional extras, drawn with pictures). The
	// storefront needs it to decide how to render, and `type` cannot answer —
	// it names the flavour of the group, not its kind.
	Kind       string                  `json:"kind"`
	IsRequired bool                    `json:"isRequired"`
	MinSelect  int                     `json:"minSelect"`
	MaxSelect  int                     `json:"maxSelect"`
	Options    []modifierOptionDTOType `json:"options"`
}

func modifierGroupsStorefrontDTO(groups []catalogapi.ModifierGroup) []modifierGroupDTOType {
	out := make([]modifierGroupDTOType, 0, len(groups))
	for _, g := range groups {
		heading := g.DisplayName
		if strings.TrimSpace(heading) == "" {
			heading = g.Name
		}
		gd := modifierGroupDTOType{ID: g.ID, Name: heading, Type: g.Type, Kind: string(g.Kind), IsRequired: g.IsRequired, MinSelect: g.MinSelect, MaxSelect: g.MaxSelect}
		for _, o := range g.Options {
			gd.Options = append(gd.Options, modifierOptionStorefrontDTO(&o))
		}
		gd.Options = orEmpty(gd.Options)
		out = append(out, gd)
	}
	return out
}

func applyStorefrontPriceRange(out *productListDTOType, p *catalogapi.Product) {
	minCents, maxCents := p.PriceRange()
	out.MinPriceCents, out.MaxPriceCents = &minCents, &maxCents
}

type productDetailDTOType struct {
	productListDTOType
	// How long the kitchen needs before this can be collected. The product
	// page turns it into a date — "available tomorrow" — which it cannot do
	// from an availability flag alone.
	LeadTime        int                    `json:"leadTime"`
	LeadTimeUnit    string                 `json:"leadTimeUnit"`
	PriceNote       string                 `json:"priceNote"`
	ModifierGroups  []modifierGroupDTOType `json:"modifierGroups"`
	SimilarProducts []productListDTOType   `json:"similarProducts"`
	Allergens       []string               `json:"allergens"`
	Dietary         []string               `json:"dietary"`
	Freshness       *string                `json:"freshness"`
	Ingredients     *string                `json:"ingredients"`
	Returns         *string                `json:"returns"`
	Disclaimer      *string                `json:"disclaimer"`
}

func productDetailDTO(p *domain.ProductWithAvailability, similar []catalogapi.Product) productDetailDTOType {
	out := productDetailDTOType{
		productListDTOType: productListDTO(p),
		LeadTime:           p.LeadTime,
		LeadTimeUnit:       strx.OrDefault(p.LeadTimeUnit, "hours"),
		// Only meaningful when there is something to vary the price. A product
		// sold at one price should not carry a note saying it might not be.
		PriceNote:   priceNoteFor(p),
		Allergens:   orEmpty(p.AllergenFlags),
		Dietary:     orEmpty(p.DietaryFlags),
		Freshness:   p.FreshnessNote,
		Ingredients: p.IngredientsNote,
		Disclaimer:  p.Disclaimer,
		Returns:     p.ReturnsNote,
	}
	out.ModifierGroups = modifierGroupsStorefrontDTO(p.ModifierGroups)
	out.productListDTOType.ModifierGroups = nil
	applyStorefrontPriceRange(&out.productListDTOType, &p.Product)
	for _, sp := range similar {
		s := sp
		out.SimilarProducts = append(out.SimilarProducts, productSummaryDTO(&s))
	}
	out.SimilarProducts = orEmpty(out.SimilarProducts)
	out.StorePrices = nil
	out.BranchIds = nil
	return out
}

type storeProductDetailDTOType struct {
	productListDTOType
	// How long the kitchen needs before this can be collected. The product
	// page turns it into a date — "available tomorrow" — which it cannot do
	// from an availability flag alone.
	LeadTime        int                    `json:"leadTime"`
	LeadTimeUnit    string                 `json:"leadTimeUnit"`
	PriceNote       string                 `json:"priceNote"`
	ModifierGroups  []modifierGroupDTOType `json:"modifierGroups"`
	SimilarProducts []productListDTOType   `json:"similarProducts"`
	Allergens       []string               `json:"allergens"`
	Dietary         []string               `json:"dietary"`
	Freshness       *string                `json:"freshness"`
	Ingredients     *string                `json:"ingredients"`
	Returns         *string                `json:"returns"`
	Disclaimer      *string                `json:"disclaimer"`
	BasePrice       string                 `json:"basePrice,omitempty"`
}

func storeProductDetailDTO(p *domain.ProductWithAvailability, similar []catalogapi.Product) storeProductDetailDTOType {
	out := storeProductDetailDTOType{
		productListDTOType: productListDTO(p),
		LeadTime:           p.LeadTime,
		LeadTimeUnit:       strx.OrDefault(p.LeadTimeUnit, "hours"),
		// Only meaningful when there is something to vary the price. A product
		// sold at one price should not carry a note saying it might not be.
		PriceNote:   priceNoteFor(p),
		Allergens:   orEmpty(p.AllergenFlags),
		Dietary:     orEmpty(p.DietaryFlags),
		Freshness:   p.FreshnessNote,
		Ingredients: p.IngredientsNote,
		Disclaimer:  p.Disclaimer,
		Returns:     p.ReturnsNote,
	}
	out.StorePrices = nil
	out.BranchIds = nil
	out.ModifierGroups = modifierGroupsStorefrontDTO(p.ModifierGroups)
	out.productListDTOType.ModifierGroups = nil
	applyStorefrontPriceRange(&out.productListDTOType, &p.Product)
	for _, sp := range similar {
		s := sp
		out.SimilarProducts = append(out.SimilarProducts, productSummaryDTO(&s))
	}
	out.SimilarProducts = orEmpty(out.SimilarProducts)
	return out
}

// priceNoteFor is the "price varies" line, or nothing.
//
// It was unconditional, so a product with no options at all still told the
// shopper its price depended on choices it did not offer.
func priceNoteFor(p *domain.ProductWithAvailability) string {
	for _, g := range p.ModifierGroups {
		if len(g.Options) > 0 {
			return "Price varies based on size & customization"
		}
	}
	return ""
}
