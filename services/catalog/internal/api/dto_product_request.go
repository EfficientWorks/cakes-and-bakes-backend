package api

import (
	"strings"

	"bakehouse/pkg/strx"
	"bakehouse/services/catalog/catalogapi"
)

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
