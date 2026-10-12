package catalogapi

import (
	"strings"
	"time"
)

const (
	ModifierFlavour      = "flavour"
	ModifierAddon        = "addon"
	ModifierPresentation = "presentation"
	ModifierWrapColour   = "wrap_colour"

	ImageStudio   = "studio"
	ImageCustomer = "customer"
)

const (
	PricingModeProduct   = "product"
	PricingModeVariation = "variation"
)

func ValidPricingMode(v string) bool {
	return v == PricingModeProduct || v == PricingModeVariation
}

type Category struct {
	ID          string
	Name        string
	Slug        string
	Description *string
	// ImageURL and ImageAltText back the photo-led category tiles on the home
	// page and the Shop Now tabs.
	ImageURL     *string
	ImageAltText *string
	ColorCode    *string
	SortOrder    int
	IsActive     bool
}

type ProductImage struct {
	ID        string
	ProductID string
	URL       string
	AltText   *string
	Type      string
	SortOrder int
	IsPrimary bool
}

type ProductVariant struct {
	ID              string
	ProductID       string
	Name            string
	Label           *string
	PriceDeltaCents int64
	ServesMin       *int
	ServesMax       *int
	SortOrder       int
	IsActive        bool
}

type ModifierOption struct {
	ID              string
	GroupID         string
	Name            string
	PriceDeltaCents int64
	// IsPreSelected marks the option a variation group loads with. At most
	// one per group — enforced in the service, since a partial unique index
	// cannot express it across an update that flips two rows at once.
	IsPreSelected bool
	ColourHex     *string
	ImageURL      *string
	AllergenFlags []string
	IsGlutenFree  bool
	IsActive      bool
	SortOrder     int
	StorePrices   []BranchModifierRule
}

type ModifierGroup struct {
	ID string
	// ProductID is empty for a library group — one authored once and applied
	// to many products through product_modifier_groups. Groups created before
	// the library existed still carry theirs.
	ProductID string
	Name      string
	// DisplayName is what the customer reads; Name is the internal label the
	// admin list and search use.
	DisplayName string
	// Kind is variation | addon — the two-way split the console works in.
	// Type is the finer storefront taxonomy (flavour, addon, presentation,
	// wrap_colour) and is what the product page still groups by.
	Kind          string
	Type          string
	IsRequired    bool
	AllowMultiple bool
	MinSelect     int
	// MaxSelect 0 means no maximum, matching the nullable column.
	MaxSelect int
	SortOrder int
	Options   []ModifierOption
	// ProductIDs is the canonical list of products this group is applied to.
	ProductIDs []string
}

type Product struct {
	ID             string
	CategoryID     string
	CategorySlug   string
	Name           string
	Slug           string
	Description    string
	BasePriceCents int64
	// PricingMode is one of the PricingMode* constants; see there. Empty is
	// read as PricingModeProduct everywhere, so rows and callers that predate
	// the column behave exactly as before.
	PricingMode  string
	LeadTime     int
	LeadTimeUnit string
	// KDS: the average preparation window, each end with its unit
	// ("minutes" | "hours"). Independent of LeadTime, which is still what
	// scheduling uses.
	MinPrepTime       int
	MinPrepTimeUnit   string
	MaxPrepTime       int
	MaxPrepTimeUnit   string
	VideoURL          *string
	ProductionStation *string
	AllergenFlags     []string
	DietaryFlags      []string
	// OccasionTags is the legacy text form of the product's tags (their
	// slugs), kept in step with SeasonalTags.
	OccasionTags []string
	// SeasonalTags are the tags mapped to the product, read with it.
	SeasonalTags []SeasonalTag
	// SeasonalTagIDs, on a write, replaces the product's tags; nil leaves
	// them as they are.
	SeasonalTagIDs    []string
	FreshnessNote     *string
	IngredientsNote   *string
	ReturnsNote       *string
	Disclaimer        *string
	IsBestSeller      bool
	PopularityScore   int
	IsActive          bool
	ShowOnPos         bool
	ShowOnWebsite     bool
	SortOrder         int
	PriceFloorCents   *int64
	PriceCeilingCents *int64
	// AvailableFrom/AvailableUntil are the master-catalogue on-sale window —
	// a product sold only for Mother's Day, or only through December. Null on
	// either end means unbounded in that direction, and null on both means
	// always on sale, which is what an ordinary product is.
	//
	// Distinct from the same-named fields on BranchProductRule: those are one
	// branch's seasonal opinion (layer 4 of the cascade), these apply
	// everywhere the product is sold.
	AvailableFrom  *time.Time
	AvailableUntil *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time

	Images         []ProductImage
	Variants       []ProductVariant
	ModifierGroups []ModifierGroup
	IsModifier     bool

	StorePrices []BranchProductRule `json:"storePrices"`
	BranchIds   []string            `json:"branchIds"`

	// MaxModifierDeltaCents is the sum of the highest price_delta_cents option
	// across all modifier groups. Populated by loadListChildren (list views) so
	// the storefront can show a price range without loading full modifier groups.
	MaxModifierDeltaCents int64
}

// DateOverride is one entry in branch_product_rules.date_overrides (layer 4
// of the availability cascade).
type DateOverride struct {
	Date      string `json:"date"` // YYYY-MM-DD
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

type BranchCategoryRule struct {
	ID             string
	BranchID       string
	CategoryID     string
	BranchEnabled  bool
	AvailableFrom  *time.Time
	AvailableUntil *time.Time
	Reason         *string
	UpdatedBy      *string
	UpdatedAt      time.Time
}

type BranchProductRule struct {
	ID                 string
	BranchID           string
	BranchName         string
	ProductID          string
	BranchEnabled      bool
	AvailableDays      []string
	DateOverrides      []DateOverride
	PriceOverrideCents *int64
	AvailableFrom      *time.Time
	AvailableUntil     *time.Time
	Reason             *string
	UpdatedBy          *string
	UpdatedAt          time.Time
}

type BranchModifierRule struct {
	ID                 string
	BranchID           string
	BranchName         string
	ModifierGroupID    string
	ModifierOptionID   string
	PriceOverrideCents *int64
	AvailableFrom      *time.Time
	AvailableUntil     *time.Time
	Reason             *string
	UpdatedBy          *string
	UpdatedAt          time.Time
}

// Availability is the resolved result of the 4-layer cascade for one
// product at one branch on one date.
type Availability struct {
	Available         bool
	Reason            string
	NextAvailableDate *string
}

// ReasonBranchNotSelected marks availability that was never resolved, because
// the caller browsed the catalogue without naming a branch. It is not a claim
// that the item is stocked anywhere — the binding check happens at checkout,
// against the branch actually fulfilling the order.
const ReasonBranchNotSelected = "branch_not_selected"

const ReasonProductNotMapped = "product_not_mapped_to_branch"

const ReasonCategoryNotMapped = "category_not_mapped_to_branch"

// Resolved reports whether this availability reflects a real branch check.
func (a Availability) Resolved() bool { return a.Reason != ReasonBranchNotSelected }

// DefaultAvailableDays is the fallback weekly availability applied when a
// branch has no explicit rule for a product (availability cascade layer 2).
var DefaultAvailableDays = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

type ContentItem struct {
	Key       string
	Value     string
	Type      string
	UpdatedAt time.Time
}

// Storefront sort orders for the Shop Now grid.
const (
	// SortMostGifted ranks by order volume, fed by the popularity rollup, and
	// falls back to best-seller then curated sort order for ties.
	SortMostGifted = "most_gifted"
	SortPriceAsc   = "price_asc"
	SortPriceDesc  = "price_desc"
	SortNewest     = "newest"
	SortNameAsc    = "name_asc"
)

// ProductFilter drives the storefront product grid: search, facets, price
// range, sort and pagination. A zero value lists everything a branch carries,
// most-gifted first.
type ProductFilter struct {
	// Search matches product name and description on partial words.
	Search string
	// CategoryID narrows to one category tab.
	CategoryID string
	// CategorySlug narrows the same way, by the readable name instead.
	//
	// A till tabs across categories using the list it was just given, and
	// making it carry a uuid around to ask for "Bouquets" is friction with
	// nothing behind it. Ignored when CategoryID is set — an id is more
	// specific, so it wins.
	CategorySlug string
	// Occasions match ANY of the given tags — picking Birthday and Anniversary
	// widens the result set, which is what a shopper expects from those chips.
	Occasions []string
	// TagIDs match ANY of the given seasonal tags (active ones only).
	TagIDs []string
	// Dietary flags must ALL be present. Picking gluten-free and vegan means
	// both, since these are restrictions rather than preferences.
	Dietary []string
	// MinPriceCents and MaxPriceCents are a price band matched against what
	// the shopper can actually pay, per the product's pricing mode:
	//   - product:   the base price (the branch override when PriceBranchID
	//                is set) — a single figure.
	//   - variation: from the sum of the cheapest option in each variation
	//                group up to the sum of the dearest (branch option
	//                overrides applied) — the range the grid shows.
	// A product matches when that price or range overlaps the band, so
	// maxPriceCents=1300 returns everything that can be bought for $13.
	MinPriceCents *int64
	MaxPriceCents *int64
	// PriceBranchID prices the band and the price sorts with that branch's
	// overrides. Set by the service once the branch is known to exist.
	PriceBranchID string
	// BestSellerOnly restricts to the handpicked row on the home page.
	BestSellerOnly bool
	ShowOnPos      *bool
	ShowOnWebsite  *bool
	Channel        string
	// Sort is one of the Sort* constants; empty means SortMostGifted.
	Sort string
	// Page is 1-based. Limit is capped at MaxProductPageSize.
	Page           int
	Limit          int
	SearchNameOnly bool
	// MappedBranchID restricts the result to products mapped to that branch:
	// an enabled branch_product_rules row, in a category the branch has
	// enabled too. The till uses it so a store only sees what it sells,
	// and the count and pager agree with the grid. The storefront leaves it
	// empty and reports unmapped products as unavailable instead.
	MappedBranchID string
}

// Product page-size bounds, matching the API conventions (default 50, max 200).
const (
	DefaultProductPageSize = 50
	MaxProductPageSize     = 200
)

// Normalise clamps paging and defaults the sort, so both the store and the
// handler work from the same rules.
func (f ProductFilter) Normalise() ProductFilter {
	if f.Sort == "" {
		f.Sort = SortMostGifted
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit <= 0 {
		f.Limit = DefaultProductPageSize
	}
	if f.Limit > MaxProductPageSize {
		f.Limit = MaxProductPageSize
	}
	return f
}

// Offset is the SQL offset for the filter's page.
func (f ProductFilter) Offset() int { return (f.Page - 1) * f.Limit }

// --- Admin console shapes ---------------------------------------------------
//
// Categories gained scheduling, modifier groups became a reusable library, and
// coupons arrived — all three because the admin console needs them and the
// schema did not carry them (migration 0022). See
// backed-bouquet-admin/docs/api-gaps.md for the reasoning.

// CategoryWrite is the admin-editable half of a category. Read-only fields
// (item counts) are derived, never accepted from a client.
type CategoryWrite struct {
	Name        string
	Slug        string
	Description *string
	// The photo-led category tile on the home page and the Shop tabs. Null
	// leaves the storefront on its own design art.
	ImageURL     *string
	ImageAltText *string
	ColorCode    *string
	SortOrder    int
	// Nil means "leave as it is" on an update, and defaults to on at create.
	// A plain bool made every partial update that omitted the field re-show a
	// category somebody had deliberately hidden.
	IsActive       *bool
	AvailableFrom  *time.Time
	AvailableUntil *time.Time

	BranchIds []string `json:"storeIds"`
}

// ModifierKind is the two-way split the console works in.
//
// Deliberately coarser than modifier_groups.type (flavour / addon /
// presentation / wrap_colour): a variation is "pick one from a set" and an
// add-on is "add any number of extras", and those are the two different forms,
// validations and product-page behaviours. The finer type is preserved for the
// storefront, which still groups by it.
const (
	ModifierKindVariation = "variation"
	ModifierKindAddon     = "addon"
)

// Coupon is a discount code, scoped either to the whole network (BranchID nil,
// SA-13) or to one branch (ADM-23).
type Coupon struct {
	ID   string
	Code string
	Name string
	Type string
	// DiscountValue is whole percent for `percentage`, integer cents for
	// `fixed`, and ignored for the free_* types.
	DiscountValue     int64
	Status            string
	AppliesTo         string
	TargetIDs         []string
	UsageLimit        *int
	RedeemedCount     int
	HasActiveDates    bool
	StartsAt          *time.Time
	EndsAt            *time.Time
	HasMinimumOrder   bool
	MinimumOrderCents int64
	BranchID          *string
	ShowOnCheckout    bool
	CreatedAt         time.Time
}

// DateWindow is an inclusive from/until pair, with nil meaning unbounded at
// that end.
type DateWindow struct {
	From  *time.Time
	Until *time.Time
}

// OnSaleOn reports whether the product's seasonal window covers a date.
//
// Compared date-to-date rather than instant-to-instant: the columns are DATE,
// and "on sale until the 14th" has to include every hour of the 14th, which a
// naive Before/After against midnight would not.
func (p Product) OnSaleOn(date time.Time) bool {
	day := date.Format("2006-01-02")
	if p.AvailableFrom != nil && day < p.AvailableFrom.Format("2006-01-02") {
		return false
	}
	if p.AvailableUntil != nil && day > p.AvailableUntil.Format("2006-01-02") {
		return false
	}
	return true
}

// IsVariationPriced reports whether the product is charged from its
// variations rather than its base price.
func (p Product) IsVariationPriced() bool {
	return p.PricingMode == PricingModeVariation
}

// EffectivePricingMode is PricingMode with the empty value spelled out.
func (p Product) EffectivePricingMode() string {
	if p.PricingMode == "" {
		return PricingModeProduct
	}
	return p.PricingMode
}

// VariationPriceRange is the cheapest and dearest way to pick one option
// from every variation group the product carries, summed across groups.
//
// With groups (10, 30) and (5, 10) the range is 15–40; with (10, 30) alone it
// is 10–30. Add-on groups are not counted — they are optional extras, not a
// choice the shopper must make. hasVariations is false when the product has
// no variation group with at least one option, in which case min and max are
// both zero and the caller should fall back to the base price.
//
// Option prices are taken as loaded, so a caller that has already applied a
// branch's modifier price overrides gets that branch's range.
func (p Product) VariationPriceRange() (minCents, maxCents int64, hasVariations bool) {
	for _, g := range p.ModifierGroups {
		if g.Kind != ModifierKindVariation || len(g.Options) == 0 {
			continue
		}
		groupMin, groupMax := g.Options[0].PriceDeltaCents, g.Options[0].PriceDeltaCents
		for _, o := range g.Options[1:] {
			if o.PriceDeltaCents < groupMin {
				groupMin = o.PriceDeltaCents
			}
			if o.PriceDeltaCents > groupMax {
				groupMax = o.PriceDeltaCents
			}
		}
		minCents += groupMin
		maxCents += groupMax
		hasVariations = true
	}
	return minCents, maxCents, hasVariations
}

// PriceRange is what a shopper can end up paying for the product, before
// add-ons: the variation range on its own for a variation-priced product,
// and the base price plus that range for a product-priced one. A product
// with no variations is a single price at both ends.
func (p Product) PriceRange() (minCents, maxCents int64) {
	vMin, vMax, has := p.VariationPriceRange()
	if p.IsVariationPriced() && has {
		return vMin, vMax
	}
	return p.BasePriceCents + vMin, p.BasePriceCents + vMax
}

// ProductPatch is a partial change to a master-catalogue product.
//
// Every field is a pointer so that "not sent" and "sent as empty/false" are
// distinguishable — without which the console's one-tap availability toggle
// could not send just `isActive` without blanking the product's name.
type ProductPatch struct {
	CategoryID        *string
	Name              *string
	Slug              *string
	Description       *string
	BasePriceCents    *int64
	PricingMode       *string
	LeadTime          *int
	LeadTimeUnit      *string
	MinPrepTime       *int
	MinPrepTimeUnit   *string
	MaxPrepTime       *int
	MaxPrepTimeUnit   *string
	VideoURL          *string
	ProductionStation *string
	AllergenFlags     *[]string
	DietaryFlags      *[]string
	OccasionTags      *[]string
	SeasonalTagIDs    *[]string
	FreshnessNote     *string
	IngredientsNote   *string
	ReturnsNote       *string
	Disclaimer        *string
	IsBestSeller      *bool
	IsActive          *bool
	ShowOnPos         *bool
	ShowOnWebsite     *bool
	SortOrder         *int
	PriceFloorCents   *int64
	PriceCeilingCents *int64
	// AvailabilityWindow is nil when the patch does not mention scheduling.
	// A non-nil window replaces both ends at once, including with nulls —
	// which is how the console clears a schedule. Two independent pointers
	// could not express "clear this one", since nil would already mean
	// "leave it alone".
	AvailabilityWindow *DateWindow
	// Children are replaced wholesale when present, and left alone when
	// absent — there is no meaningful way to partially patch a list whose
	// items have no stable client-side identity.
	Images         *[]ProductImage
	Variants       *[]ProductVariant
	ModifierGroups *[]ModifierGroup
	BranchIds      *[]string            `json:"storeIds"`
	StorePrices    *[]BranchProductRule `json:"storePrices"`
}

// Apply overlays the patch onto a stored product, returning what it should
// become. Absent scalar fields keep the value they already had.
//
// Children are the exception and are cleared to nil unless the patch supplies
// them. The store reads a nil collection as "leave this alone" and a non-nil
// one as "replace it wholesale" — so carrying the loaded variants through
// would make every partial edit delete and re-insert them, which fails
// outright the moment any cart still references one.
func (patch ProductPatch) Apply(p Product) Product {
	p.Images, p.Variants, p.ModifierGroups, p.StorePrices = nil, nil, nil, nil

	set := func(dst *string, src *string) {
		if src != nil {
			*dst = *src
		}
	}
	setPtr := func(dst **string, src *string) {
		if src != nil {
			if *src == "" {
				*dst = nil
			} else {
				v := *src
				*dst = &v
			}
		}
	}

	set(&p.CategoryID, patch.CategoryID)
	set(&p.Name, patch.Name)
	set(&p.Slug, patch.Slug)
	set(&p.Description, patch.Description)
	set(&p.LeadTimeUnit, patch.LeadTimeUnit)
	set(&p.MinPrepTimeUnit, patch.MinPrepTimeUnit)
	set(&p.MaxPrepTimeUnit, patch.MaxPrepTimeUnit)
	if patch.MinPrepTime != nil {
		p.MinPrepTime = *patch.MinPrepTime
	}
	if patch.MaxPrepTime != nil {
		p.MaxPrepTime = *patch.MaxPrepTime
	}
	set(&p.PricingMode, patch.PricingMode)
	setPtr(&p.ProductionStation, patch.ProductionStation)
	setPtr(&p.VideoURL, patch.VideoURL)
	setPtr(&p.FreshnessNote, patch.FreshnessNote)
	setPtr(&p.IngredientsNote, patch.IngredientsNote)
	setPtr(&p.ReturnsNote, patch.ReturnsNote)
	setPtr(&p.Disclaimer, patch.Disclaimer)

	if patch.BasePriceCents != nil {
		p.BasePriceCents = *patch.BasePriceCents
	}
	if patch.LeadTime != nil {
		p.LeadTime = *patch.LeadTime
	}
	if patch.IsBestSeller != nil {
		p.IsBestSeller = *patch.IsBestSeller
	}
	if patch.IsActive != nil {
		p.IsActive = *patch.IsActive
	}
	if patch.ShowOnPos != nil {
		p.ShowOnPos = *patch.ShowOnPos
	}
	if patch.ShowOnWebsite != nil {
		p.ShowOnWebsite = *patch.ShowOnWebsite
	}
	if patch.SortOrder != nil {
		p.SortOrder = *patch.SortOrder
	}
	if patch.PriceFloorCents != nil {
		p.PriceFloorCents = patch.PriceFloorCents
	}
	if patch.PriceCeilingCents != nil {
		p.PriceCeilingCents = patch.PriceCeilingCents
	}
	if patch.AvailabilityWindow != nil {
		p.AvailableFrom = patch.AvailabilityWindow.From
		p.AvailableUntil = patch.AvailabilityWindow.Until
	}
	if patch.AllergenFlags != nil {
		p.AllergenFlags = *patch.AllergenFlags
	}
	if patch.DietaryFlags != nil {
		p.DietaryFlags = *patch.DietaryFlags
	}
	if patch.OccasionTags != nil {
		p.OccasionTags = *patch.OccasionTags
	}
	p.SeasonalTagIDs = nil
	if patch.SeasonalTagIDs != nil {
		p.SeasonalTagIDs = *patch.SeasonalTagIDs
	}
	if patch.Images != nil {
		p.Images = *patch.Images
	}
	if patch.Variants != nil {
		p.Variants = *patch.Variants
	}
	if patch.ModifierGroups != nil {
		p.ModifierGroups = *patch.ModifierGroups
	}
	if patch.BranchIds != nil {
		p.BranchIds = *patch.BranchIds
	}
	if patch.StorePrices != nil {
		p.StorePrices = make([]BranchProductRule, 0, len(*patch.StorePrices))

		for _, sp := range *patch.StorePrices {
			p.StorePrices = append(p.StorePrices, BranchProductRule{
				BranchID:           sp.BranchID,
				PriceOverrideCents: sp.PriceOverrideCents,
			})
		}
	}
	return p
}

// PrepMinutes converts a preparation time and its unit to minutes.
func PrepMinutes(value int, unit string) int {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(unit)), "hour") {
		return value * 60
	}
	return value
}

// NormalizePrepUnit maps what a client may send onto "minutes" or "hours";
// ok is false for anything else. Empty means minutes.
func NormalizePrepUnit(unit string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "min", "mins", "minute", "minutes":
		return "minutes", true
	case "h", "hr", "hrs", "hour", "hours":
		return "hours", true
	}
	return "", false
}
