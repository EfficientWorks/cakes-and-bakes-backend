package api

import (
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/strx"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/domain"
	"bakehouse/services/catalog/internal/store"
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

// --- Admin request DTOs --------------------------------------------------------

type variantRequest struct {
	Name            string `json:"name"`
	Label           string `json:"label"`
	PriceDeltaCents int64  `json:"priceDeltaCents"`
	ServesMin       *int   `json:"servesMin"`
	ServesMax       *int   `json:"servesMax"`
	SortOrder       int    `json:"sortOrder"`
	IsActive        *bool  `json:"isActive"`
}

type modifierOptionRequest struct {
	Name            string   `json:"name"`
	PriceDeltaCents int64    `json:"priceDeltaCents"`
	ColourHex       string   `json:"colourHex"`
	ImageURL        string   `json:"imageUrl"`
	AllergenFlags   []string `json:"allergenFlags"`
	IsGlutenFree    bool     `json:"isGlutenFree"`
	SortOrder       int      `json:"sortOrder"`
	IsActive        *bool    `json:"isActive"`
}

type modifierGroupRequest struct {
	Name       string                  `json:"name"`
	Type       string                  `json:"type"`
	IsRequired bool                    `json:"isRequired"`
	MinSelect  int                     `json:"minSelect"`
	MaxSelect  int                     `json:"maxSelect"`
	SortOrder  int                     `json:"sortOrder"`
	Options    []modifierOptionRequest `json:"options"`
}

type imageRequest struct {
	URL       string `json:"url"`
	AltText   string `json:"altText"`
	Type      string `json:"type"`
	SortOrder int    `json:"sortOrder"`
	IsPrimary bool   `json:"isPrimary"`
}

type StorePricesRequest struct {
	BranchId   string `json:"storeId"`
	PriceCents int64  `json:"priceCents"`
}

// productRequest is the admin product create/update body. Scalar fields
// (name, basePriceCents, priceFloorCents, ...) are replaced wholesale on
// every PATCH — the caller is expected to resend the full set it wants to
// keep, same as any product-edit form. The three nested collections
// (images, variants, modifierGroups) are the exception: omitting the key
// entirely leaves the existing rows untouched; sending an explicit empty
// array clears them. See the nil-guards in Store.UpdateProduct.

type productRequest struct {
	CategoryID     string `json:"categoryId"`
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Description    string `json:"description"`
	BasePriceCents int64  `json:"basePriceCents"`
	PricingMode    string `json:"pricingMode"`
	LeadTime       int    `json:"leadTime"`
	LeadTimeUnit   string `json:"leadTimeUnit"`
	// KDS: average preparation window (Minimum / Maximum Time). Units: minutes or hours.
	MinPrepTime     int    `json:"minPrepTime"`
	MinPrepTimeUnit string `json:"minPrepTimeUnit"`
	MaxPrepTime     int    `json:"maxPrepTime"`
	MaxPrepTimeUnit string `json:"maxPrepTimeUnit"`
	// The product's one video: the deliveryUrl from POST /admin/videos/upload-url.
	VideoURL          string   `json:"videoUrl" example:"https://api.example.com/v1/media/videos/3f2a..."`
	ProductionStation string   `json:"productionStation"`
	AllergenFlags     []string `json:"allergenFlags"`
	DietaryFlags      []string `json:"dietaryFlags"`
	OccasionTags      []string `json:"occasionTags"`
	// The seasonal tags to map (ids from /admin/seasonal-tags). Wins over
	// occasionTags when both are sent.
	SeasonalTagIDs  []string `json:"seasonalTagIds"`
	FreshnessNote   string   `json:"freshnessNote"`
	IngredientsNote string   `json:"ingredientsNote"`
	ReturnsNote     string   `json:"returnsNote"`
	Disclaimer      string   `json:"disclaimer"`
	IsBestSeller    bool     `json:"isBestSeller"`
	// Pointer with a true default: the column defaults to true, and a create
	// body that simply does not mention availability meant a false zero value
	// reached the insert — so every product created through the API arrived
	// hidden from the storefront, with nothing to say why.
	IsActive          *bool  `json:"isActive"`
	ShowOnPos         *bool  `json:"showOnPos"`
	ShowOnWebsite     *bool  `json:"showOnWebsite"`
	SortOrder         int    `json:"sortOrder"`
	PriceFloorCents   *int64 `json:"priceFloorCents"`
	PriceCeilingCents *int64 `json:"priceCeilingCents"`
	// YYYY-MM-DD, datetime-local, ISO timestamp, or empty/null for "always on sale".
	AvailableFrom  *string                `json:"availableFrom"`
	AvailableUntil *string                `json:"availableUntil"`
	Images         []imageRequest         `json:"images"`
	Variants       []variantRequest       `json:"variants"`
	ModifierGroups []modifierGroupRequest `json:"modifierGroups"`

	StorePrices []StorePricesRequest `json:"storePrices"`
	BranchIds   []string             `json:"storeIds"`
}

func productFromRequest(req productRequest) catalogapi.Product {
	p := catalogapi.Product{
		CategoryID: req.CategoryID, Name: req.Name, Slug: req.Slug, Description: req.Description,
		BasePriceCents: req.BasePriceCents, LeadTime: req.LeadTime,
		PricingMode:  strx.OrDefault(strings.TrimSpace(req.PricingMode), catalogapi.PricingModeProduct),
		LeadTimeUnit: strx.OrDefault(req.LeadTimeUnit, "hours"),
		MinPrepTime:  req.MinPrepTime, MinPrepTimeUnit: strx.OrDefault(req.MinPrepTimeUnit, "minutes"),
		MaxPrepTime: req.MaxPrepTime, MaxPrepTimeUnit: strx.OrDefault(req.MaxPrepTimeUnit, "minutes"),
		VideoURL:          strx.NilIfEmpty(strings.TrimSpace(req.VideoURL)),
		ProductionStation: strx.NilIfEmpty(req.ProductionStation), AllergenFlags: req.AllergenFlags,
		DietaryFlags: req.DietaryFlags, OccasionTags: req.OccasionTags, SeasonalTagIDs: req.SeasonalTagIDs,
		FreshnessNote: strx.NilIfEmpty(req.FreshnessNote), IngredientsNote: strx.NilIfEmpty(req.IngredientsNote),
		ReturnsNote: strx.NilIfEmpty(req.ReturnsNote), Disclaimer: strx.NilIfEmpty(req.Disclaimer), IsBestSeller: req.IsBestSeller, SortOrder: req.SortOrder,
		PriceFloorCents: req.PriceFloorCents, PriceCeilingCents: req.PriceCeilingCents,
		IsActive:       boolOrDefault(req.IsActive, true),
		ShowOnPos:      boolOrDefault(req.ShowOnPos, true),
		ShowOnWebsite:  boolOrDefault(req.ShowOnWebsite, true),
		AvailableFrom:  parseAdminDatePtr(req.AvailableFrom),
		AvailableUntil: parseAdminDatePtr(req.AvailableUntil),
		BranchIds:      req.BranchIds,
	}
	for _, img := range req.Images {
		p.Images = append(p.Images, catalogapi.ProductImage{URL: img.URL, AltText: strx.NilIfEmpty(img.AltText), Type: strx.OrDefault(img.Type, catalogapi.ImageStudio), SortOrder: img.SortOrder, IsPrimary: img.IsPrimary})
	}
	for _, v := range req.Variants {
		p.Variants = append(p.Variants, catalogapi.ProductVariant{
			Name: v.Name, Label: strx.NilIfEmpty(v.Label), PriceDeltaCents: v.PriceDeltaCents,
			ServesMin: v.ServesMin, ServesMax: v.ServesMax, SortOrder: v.SortOrder, IsActive: boolOrDefault(v.IsActive, true),
		})
	}
	for _, g := range req.ModifierGroups {
		mg := catalogapi.ModifierGroup{Name: g.Name, Type: g.Type, IsRequired: g.IsRequired, MinSelect: g.MinSelect, MaxSelect: g.MaxSelect, SortOrder: g.SortOrder}
		for _, o := range g.Options {
			mg.Options = append(mg.Options, catalogapi.ModifierOption{
				Name: o.Name, PriceDeltaCents: o.PriceDeltaCents, ColourHex: strx.NilIfEmpty(o.ColourHex),
				ImageURL: strx.NilIfEmpty(o.ImageURL), AllergenFlags: o.AllergenFlags, IsGlutenFree: o.IsGlutenFree,
				SortOrder: o.SortOrder, IsActive: boolOrDefault(o.IsActive, true),
			})
		}
		p.ModifierGroups = append(p.ModifierGroups, mg)
	}

	for _, storePrice := range req.StorePrices {
		price := storePrice.PriceCents
		p.StorePrices = append(p.StorePrices, catalogapi.BranchProductRule{
			BranchID:           storePrice.BranchId,
			PriceOverrideCents: &price,
		})
	}
	return p
}

func boolOrDefault(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}

// --- Availability admin DTOs ------------------------------------------------------

type gridRowDTOType struct {
	ProductID      string   `json:"productId"`
	ProductName    string   `json:"productName"`
	BranchEnabled  bool     `json:"branchEnabled"`
	AvailableDays  []string `json:"availableDays"`
	TodayAvailable bool     `json:"todayAvailable"`
	SeasonalFrom   *string  `json:"seasonalFrom"`
	SeasonalUntil  *string  `json:"seasonalUntil"`
	PriceOverride  *int64   `json:"priceOverride"`
}

func gridRowDTO(r *domain.AvailabilityGridRow) gridRowDTOType {
	return gridRowDTOType{
		ProductID: r.ProductID, ProductName: r.ProductName, BranchEnabled: r.BranchEnabled,
		AvailableDays: r.AvailableDays, TodayAvailable: r.TodayAvailable,
		SeasonalFrom: r.SeasonalFrom, SeasonalUntil: r.SeasonalUntil, PriceOverride: r.PriceOverride,
	}
}

type ruleDTOType struct {
	ID                 string                    `json:"id"`
	BranchID           string                    `json:"branchId"`
	ProductID          string                    `json:"productId"`
	BranchEnabled      bool                      `json:"branchEnabled"`
	AvailableDays      []string                  `json:"availableDays"`
	DateOverrides      []catalogapi.DateOverride `json:"dateOverrides"`
	PriceOverrideCents *int64                    `json:"priceOverrideCents"`
	AvailableFrom      *string                   `json:"availableFrom"`
	AvailableUntil     *string                   `json:"availableUntil"`
	Reason             *string                   `json:"reason"`
	UpdatedAt          string                    `json:"updatedAt"`
}

func ruleDTO(r *catalogapi.BranchProductRule) ruleDTOType {
	out := ruleDTOType{
		ID: r.ID, BranchID: r.BranchID, ProductID: r.ProductID, BranchEnabled: r.BranchEnabled,
		AvailableDays: r.AvailableDays, DateOverrides: r.DateOverrides, PriceOverrideCents: r.PriceOverrideCents,
		Reason: r.Reason, UpdatedAt: r.UpdatedAt.Format(rfc3339),
	}
	if r.AvailableFrom != nil {
		v := r.AvailableFrom.Format(rfc3339)
		out.AvailableFrom = &v
	}
	if r.AvailableUntil != nil {
		v := r.AvailableUntil.Format(rfc3339)
		out.AvailableUntil = &v
	}
	return out
}

// --- Admin console DTOs -------------------------------------------------------

func categoryAdminDTO(row *store.CategoryRow) categoryAdminDTOType {
	return categoryAdminDTOType{
		ID: row.Category.ID, Name: row.Category.Name, Slug: row.Category.Slug,
		Description: row.Category.Description, ImageURL: row.Category.ImageURL,
		ImageAltText: row.Category.ImageAltText,
		ColorCode:    row.Category.ColorCode,
		SortOrder:    row.Category.SortOrder, IsActive: row.Category.IsActive,
		AvailableFrom:  formatDatePtr(row.Schedule.AvailableFrom),
		AvailableUntil: formatDatePtr(row.Schedule.AvailableUntil),
		BranchIds:      orEmpty(row.BranchIds),
	}
}

func modifierGroupDTO(g *catalogapi.ModifierGroup) adminModifierGroupDTO {
	options := make([]adminModifierOptionDTO, 0, len(g.Options))
	for _, o := range g.Options {
		var spPtr *[]adminStorePriceDTO
		var minPricePtr *int64
		var maxPricePtr *int64
		hasOverride := false
		minPrice := o.PriceDeltaCents
		maxPrice := o.PriceDeltaCents
		if o.StorePrices != nil {
			storePrices := make([]adminStorePriceDTO, 0, len(o.StorePrices))
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
				storePrices = append(storePrices, adminStorePriceDTO{
					BranchId:   sp.BranchID,
					Store:      sp.BranchName,
					PriceCents: sp.PriceOverrideCents,
				})
			}
			spPtr = &storePrices
		}
		if hasOverride {
			minPricePtr = &minPrice
			maxPricePtr = &maxPrice
		}
		options = append(options, adminModifierOptionDTO{
			ID:              o.ID,
			Name:            o.Name,
			PriceDeltaCents: o.PriceDeltaCents,
			MinPriceCents:   minPricePtr,
			MaxPriceCents:   maxPricePtr,
			IsPreSelected:   o.IsPreSelected,
			ImageURL:        o.ImageURL,
			IsGlutenFree:    o.IsGlutenFree,
			AllergenFlags:   o.AllergenFlags,
			SortOrder:       o.SortOrder,
			IsActive:        o.IsActive,
			StorePrices:     spPtr,
		})
	}
	// A nil max is "no maximum" — distinct from a max of zero, which would
	// mean the group can never be chosen from.
	var max *int
	if g.MaxSelect > 0 {
		v := g.MaxSelect
		max = &v
	}
	productIDs := g.ProductIDs
	if productIDs == nil {
		productIDs = []string{}
	}
	return adminModifierGroupDTO{
		ID: g.ID, Name: g.Name, DisplayName: g.DisplayName, Kind: g.Kind, Type: g.Type,
		IsRequired: g.IsRequired, AllowMultiple: g.AllowMultiple,
		MinSelect: g.MinSelect, MaxSelect: max, SortOrder: g.SortOrder,
		Options: options, ProductIDs: productIDs,
	}
}

func couponDTO(c *catalogapi.Coupon) couponDTOType {
	targets := c.TargetIDs
	if targets == nil {
		targets = []string{}
	}
	return couponDTOType{
		ID: c.ID, Code: c.Code, Name: c.Name, Type: c.Type, DiscountValue: c.DiscountValue,
		Status: c.Status, AppliesTo: c.AppliesTo, TargetIDs: targets,
		UsageLimit: c.UsageLimit, RedeemedCount: c.RedeemedCount,
		HasActiveDates: c.HasActiveDates,
		StartDate:      formatDatePtr(c.StartsAt), EndDate: formatDatePtr(c.EndsAt),
		HasMinimumOrder: c.HasMinimumOrder, MinimumOrderCents: c.MinimumOrderCents,
		BranchID: c.BranchID, ShowOnCheckout: c.ShowOnCheckout,
		CreatedAt: c.CreatedAt.Format(rfc3339),
	}
}

// adminDateFormats are the shapes a date arrives in.
//
// The console's date inputs are YYYY-MM-DD, but it passes them through
// `new Date(...).toISOString()` before sending, so what actually lands is a
// full RFC 3339 timestamp. Accepting only the first of those is what made
// product scheduling appear to do nothing: the value failed to parse, was
// silently dropped, and the save reported success over a window that had
// never been stored.
var adminDateFormats = []string{
	"2006-01-02",
	"2006-01-02T15:04",
	"2006-01-02T15:04:05",
	time.RFC3339,
	time.RFC3339Nano,
	"2006-01-02T15:04:05.000Z07:00",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
}

// parseAdminDateStrict reads a date, or explains why it could not.
//
// Empty means "not set", which is a real state — a product with no window is
// always on sale. Anything else that will not parse is an error rather than a
// silent nil: a date the caller meant and the server discarded is the worst of
// the three outcomes, because nothing anywhere says it happened.
func parseAdminDateStrict(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range adminDateFormats {
		if t, err := time.Parse(layout, value); err == nil {
			day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			return &day, nil
		}
	}
	location, err := time.LoadLocation("Asia/Kolkata")
	if err == nil {
		if t, err := time.ParseInLocation("2006-01-02T15:04", value, location); err == nil {
			day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			return &day, nil
		}
	}
	return nil, apperr.Validation(value + " is not a date/datetime. Use YYYY-MM-DD or an ISO 8601 timestamp.").
		WithFields(apperr.FieldError{Field: "date", Code: "INVALID", Message: "Use YYYY-MM-DD or ISO 8601."})
}

func formatDateTime(value string) (string, error) {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return "", err
	}

	return t.Format("2006-01-02T15:04"), nil
}

// parseAdminDate is the forgiving form, for callers that have no way to report
// an error. Prefer parseAdminDateStrict wherever the caller can return one.
func parseAdminDate(value string) *time.Time {
	t, _ := parseAdminDateStrict(value)
	return t
}

func parseAdminDatePtr(value *string) *time.Time {
	if value == nil {
		return nil
	}
	return parseAdminDate(*value)
}

// adminProductDTO is the edit form's view of a product.
//
// Distinct from productDetailDTO, which is what a *customer* is shown: that
// one carries a rendered price, similar products and an availability verdict,
// and leaves out everything only a manager may set. Serving it to the console
// was why the edit form could not show a preparation time, a production
// station or a price band — those fields never reached the browser, so
// editing them changed nothing and saving wrote the defaults back over them.
//
// Money stays in minor units and dates stay as calendar days, matching what
// the PATCH accepts, so the form can send back exactly what it read.
type adminProductDTOType struct {
	ID                string               `json:"id"`
	CategoryID        string               `json:"categoryId"`
	CategorySlug      string               `json:"categorySlug"`
	Name              string               `json:"name"`
	Slug              string               `json:"slug"`
	Description       string               `json:"description"`
	BasePriceCents    int64                `json:"basePriceCents"`
	PricingMode       string               `json:"pricingMode"`
	MinPriceCents     *int64               `json:"minPriceCents"`
	MaxPriceCents     *int64               `json:"maxPriceCents"`
	LeadTime          int                  `json:"leadTime"`
	LeadTimeUnit      string               `json:"leadTimeUnit"`
	MinPrepTime       int                  `json:"minPrepTime"`
	MinPrepTimeUnit   string               `json:"minPrepTimeUnit"`
	MaxPrepTime       int                  `json:"maxPrepTime"`
	MaxPrepTimeUnit   string               `json:"maxPrepTimeUnit"`
	VideoURL          *string              `json:"videoUrl"`
	ProductionStation *string              `json:"productionStation"`
	AllergenFlags     []string             `json:"allergenFlags"`
	DietaryFlags      []string             `json:"dietaryFlags"`
	OccasionTags      []string             `json:"occasionTags"`
	SeasonalTagIDs    []string             `json:"seasonalTagIds"`
	SeasonalTags      []seasonalTagDTOType `json:"seasonalTags"`
	FreshnessNote     *string              `json:"freshnessNote"`
	IngredientsNote   *string              `json:"ingredientsNote"`
	ReturnsNote       *string              `json:"returnsNote"`
	Disclaimer        *string              `json:"disclaimer"`
	IsBestSeller      bool                 `json:"isBestSeller"`
	IsActive          bool                 `json:"isActive"`
	ShowOnPos         bool                 `json:"showOnPos"`
	ShowOnWebsite     bool                 `json:"showOnWebsite"`
	SortOrder         int                  `json:"sortOrder"`
	PriceFloorCents   *int64               `json:"priceFloorCents"`
	PriceCeilingCents *int64               `json:"priceCeilingCents"`
	AvailableFrom     *string              `json:"availableFrom"`
	AvailableUntil    *string              `json:"availableUntil"`
	IsModifier        bool                 `json:"isModifier"`

	Images         []imageDTOType          `json:"images"`
	Variants       []variantDTOType        `json:"variants,omitempty"`
	ModifierGroups []adminModifierGroupDTO `json:"modifierGroups"`
	StorePrices    *[]adminStorePriceDTO   `json:"storePrices,omitempty"`
	BranchIds      *[]string               `json:"storeIds,omitempty"`
}

func adminProductDTO(p *catalogapi.Product) adminProductDTOType {
	images := make([]imageDTOType, 0, len(p.Images))
	for i := range p.Images {
		images = append(images, imageDTO(&p.Images[i]))
	}
	// variants := make([]variantDTOType, 0, len(p.Variants))
	// for i := range p.Variants {
	// 	variants = append(variants, variantDTO(p.BasePriceCents, &p.Variants[i]))
	// }
	groups := make([]adminModifierGroupDTO, 0, len(p.ModifierGroups))
	for i := range p.ModifierGroups {
		groups = append(groups, modifierGroupDTO(&p.ModifierGroups[i]))
	}

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

	var spPtr *[]adminStorePriceDTO
	if p.StorePrices != nil {
		storePrices := make([]adminStorePriceDTO, 0, len(p.StorePrices))
		for i := range p.StorePrices {
			storePrices = append(storePrices, adminStorePriceDTO{
				BranchId:   p.StorePrices[i].BranchID,
				Store:      p.StorePrices[i].BranchName,
				PriceCents: p.StorePrices[i].PriceOverrideCents,
			})
		}
		spPtr = &storePrices
	}

	var bIdsPtr *[]string
	if p.BranchIds != nil {
		ids := orEmpty(p.BranchIds)
		bIdsPtr = &ids
	}

	return adminProductDTOType{
		ID: p.ID, CategoryID: p.CategoryID, CategorySlug: p.CategorySlug,
		Name: p.Name, Slug: p.Slug, Description: p.Description,
		BasePriceCents: p.BasePriceCents,
		PricingMode:    p.EffectivePricingMode(),
		MinPriceCents:  minPricePtr,
		MaxPriceCents:  maxPricePtr,
		LeadTime:       p.LeadTime,
		LeadTimeUnit:   strx.OrDefault(p.LeadTimeUnit, "hours"),
		MinPrepTime:    p.MinPrepTime, MinPrepTimeUnit: strx.OrDefault(p.MinPrepTimeUnit, "minutes"),
		MaxPrepTime: p.MaxPrepTime, MaxPrepTimeUnit: strx.OrDefault(p.MaxPrepTimeUnit, "minutes"),
		VideoURL:          p.VideoURL,
		ProductionStation: p.ProductionStation,
		AllergenFlags:     orEmpty(p.AllergenFlags), DietaryFlags: orEmpty(p.DietaryFlags),
		OccasionTags:   orEmpty(p.OccasionTags),
		SeasonalTagIDs: seasonalTagIDs(p.SeasonalTags), SeasonalTags: seasonalTagDTOs(p.SeasonalTags),
		FreshnessNote: p.FreshnessNote, IngredientsNote: p.IngredientsNote,
		ReturnsNote:  p.ReturnsNote,
		Disclaimer:   p.Disclaimer,
		IsBestSeller: p.IsBestSeller, IsActive: p.IsActive, ShowOnPos: p.ShowOnPos, ShowOnWebsite: p.ShowOnWebsite, SortOrder: p.SortOrder,
		PriceFloorCents: p.PriceFloorCents, PriceCeilingCents: p.PriceCeilingCents,
		AvailableFrom: formatDatePtr(p.AvailableFrom), AvailableUntil: formatDatePtr(p.AvailableUntil),
		IsModifier: p.IsModifier || len(p.ModifierGroups) > 0,
		Images:     images, ModifierGroups: groups, StorePrices: spPtr, BranchIds: bIdsPtr,
	}
}

/*
 * A scheduled window carries a time, not just a day.
 *
 * The column holds a timestamp and the parser has always accepted one, but
 * this wrote back only the date — so an admin who scheduled a product to go
 * live at 9am saw "9am" accepted, saved, and then replaced by midnight the
 * next time the form loaded. The time was in the database the whole while;
 * only this could not say so.
 */
func formatDatePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	// Use RFC3339 base string and format to desired layout via formatDateTime
	if ft, err := formatDateTime(t.Format(time.RFC3339)); err == nil {
		return &ft
	}
	// Fallback to direct formatting without timezone
	ft := t.Format("2006-01-02T15:04")
	return &ft
}

// productPatchRequest is the PATCH body: every field optional, so a caller can
// change one thing without restating the product. `productRequest` stays the
// POST body, where the fields really are required.
type productPatchRequest struct {
	CategoryID     *string `json:"categoryId"`
	Name           *string `json:"name"`
	Slug           *string `json:"slug"`
	Description    *string `json:"description"`
	BasePriceCents *int64  `json:"basePriceCents"`
	PricingMode    *string `json:"pricingMode"`
	LeadTime       *int    `json:"leadTime"`
	LeadTimeUnit   *string `json:"leadTimeUnit"`
	// KDS: average preparation window. Units: minutes or hours.
	MinPrepTime     *int    `json:"minPrepTime"`
	MinPrepTimeUnit *string `json:"minPrepTimeUnit"`
	MaxPrepTime     *int    `json:"maxPrepTime"`
	MaxPrepTimeUnit *string `json:"maxPrepTimeUnit"`
	// The product's one video. Omit to keep it; "" removes it.
	VideoURL          *string   `json:"videoUrl"`
	ProductionStation *string   `json:"productionStation"`
	AllergenFlags     *[]string `json:"allergenFlags"`
	DietaryFlags      *[]string `json:"dietaryFlags"`
	OccasionTags      *[]string `json:"occasionTags"`
	// Replaces the product's seasonal tags (ids); omit to keep them.
	SeasonalTagIDs    *[]string `json:"seasonalTagIds"`
	FreshnessNote     *string   `json:"freshnessNote"`
	IngredientsNote   *string   `json:"ingredientsNote"`
	ReturnsNote       *string   `json:"returnsNote"`
	Disclaimer        *string   `json:"disclaimer"`
	IsBestSeller      *bool     `json:"isBestSeller"`
	IsActive          *bool     `json:"isActive"`
	ShowOnPos         *bool     `json:"showOnPos"`
	ShowOnWebsite     *bool     `json:"showOnWebsite"`
	SortOrder         *int      `json:"sortOrder"`
	PriceFloorCents   *int64    `json:"priceFloorCents"`
	PriceCeilingCents *int64    `json:"priceCeilingCents"`
	// Pointers, so that omitting the keys leaves an existing schedule alone
	// while sending them as "" clears it — which is what the console's
	// "Visibility & scheduling" toggle does when it is switched off.
	AvailableFrom  *string                 `json:"availableFrom"`
	AvailableUntil *string                 `json:"availableUntil"`
	Images         *[]imageRequest         `json:"images"`
	Variants       *[]variantRequest       `json:"variants"`
	ModifierGroups *[]modifierGroupRequest `json:"modifierGroups"`
	BranchIds      *[]string               `json:"storeIds"`
	StorePrices    *[]StorePricesRequest   `json:"storePrices"`
}

func productPatchFromRequest(req productPatchRequest) (catalogapi.ProductPatch, error) {
	patch := catalogapi.ProductPatch{
		CategoryID: req.CategoryID, Name: req.Name, Slug: req.Slug, Description: req.Description,
		BasePriceCents: req.BasePriceCents, PricingMode: req.PricingMode, LeadTime: req.LeadTime, LeadTimeUnit: req.LeadTimeUnit,
		MinPrepTime: req.MinPrepTime, MinPrepTimeUnit: req.MinPrepTimeUnit, MaxPrepTime: req.MaxPrepTime, MaxPrepTimeUnit: req.MaxPrepTimeUnit,
		VideoURL:          req.VideoURL,
		ProductionStation: req.ProductionStation, AllergenFlags: req.AllergenFlags,
		DietaryFlags: req.DietaryFlags, OccasionTags: req.OccasionTags, SeasonalTagIDs: req.SeasonalTagIDs,
		FreshnessNote: req.FreshnessNote, IngredientsNote: req.IngredientsNote,
		ReturnsNote: req.ReturnsNote, Disclaimer: req.Disclaimer, IsBestSeller: req.IsBestSeller, IsActive: req.IsActive,
		ShowOnPos: req.ShowOnPos, ShowOnWebsite: req.ShowOnWebsite,
		SortOrder: req.SortOrder, PriceFloorCents: req.PriceFloorCents,
		PriceCeilingCents: req.PriceCeilingCents,
		BranchIds:         req.BranchIds,
	}

	// Either end being present means the caller is talking about scheduling,
	// so both are read — a patch that sets only "until" is still an explicit
	// statement that "from" is open-ended.
	if req.AvailableFrom != nil || req.AvailableUntil != nil {
		window := catalogapi.DateWindow{}
		if req.AvailableFrom != nil {
			from, err := parseAdminDateStrict(*req.AvailableFrom)
			if err != nil {
				return catalogapi.ProductPatch{}, err
			}
			window.From = from
		}
		if req.AvailableUntil != nil {
			until, err := parseAdminDateStrict(*req.AvailableUntil)
			if err != nil {
				return catalogapi.ProductPatch{}, err
			}
			window.Until = until
		}
		patch.AvailabilityWindow = &window
	}

	/*
	 * Children go through the same converters the POST body uses, so a product
	 * edited by PATCH ends up shaped exactly like one created by POST.
	 *
	 * Each is forced to a non-nil slice. The store decides whether to clear a
	 * collection by asking whether it is nil, and the converters build their
	 * result by appending — so an explicitly empty array came out nil, which
	 * the store read as "not mentioned" and left alone.
	 *
	 * That is why deleting every photo from a product appeared to work and
	 * then showed the picture again on the next load: the request said "no
	 * images", the server agreed, and nothing was deleted.
	 */
	if req.Images != nil {
		full := productFromRequest(productRequest{Images: *req.Images})
		images := full.Images
		if images == nil {
			images = []catalogapi.ProductImage{}
		}
		patch.Images = &images
	}
	if req.Variants != nil {
		full := productFromRequest(productRequest{Variants: *req.Variants})
		variants := full.Variants
		if variants == nil {
			variants = []catalogapi.ProductVariant{}
		}
		patch.Variants = &variants
	}
	if req.ModifierGroups != nil {
		full := productFromRequest(productRequest{ModifierGroups: *req.ModifierGroups})
		groups := full.ModifierGroups
		if groups == nil {
			groups = []catalogapi.ModifierGroup{}
		}
		patch.ModifierGroups = &groups
	}
	if req.StorePrices != nil {
		full := productFromRequest(productRequest{StorePrices: *req.StorePrices})
		prices := full.StorePrices
		if prices == nil {
			prices = []catalogapi.BranchProductRule{}
		}
		patch.StorePrices = &prices
	}
	return patch, nil
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
