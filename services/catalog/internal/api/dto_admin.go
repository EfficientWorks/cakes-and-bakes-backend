package api

import (
	"bakehouse/pkg/strx"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/catalog/internal/domain"
	"bakehouse/services/catalog/internal/store"
)

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
