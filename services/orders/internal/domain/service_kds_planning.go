package domain

import (
	"context"
	"math"
	"sort"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/timex"
)

// ================================================================== planning

func roundPct(part, whole int) float64 {
	if whole <= 0 {
		return 0
	}
	return math.Round(float64(part)*1000/float64(whole)) / 10
}

func planningRange(scope *KDSScope, now time.Time, start, end *time.Time, defaultDays int) (time.Time, time.Time, error) {
	loc := scope.location()
	n := now.In(loc)
	first := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1) // tomorrow
	from, to := first, first.AddDate(0, 0, defaultDays-1)
	if start != nil {
		from = *start
		if end == nil {
			to = from.AddDate(0, 0, defaultDays-1)
		}
	}
	if end != nil {
		to = *end
	}
	if to.Before(from) {
		return from, to, apperr.Validation("endDate cannot be before startDate.").
			WithFields(apperr.FieldError{Field: "endDate", Code: "INVALID", Message: "Before startDate."})
	}
	if to.Sub(from) > 92*24*time.Hour {
		return from, to, apperr.Validation("A range is at most 93 days.").
			WithFields(apperr.FieldError{Field: "endDate", Code: "RANGE", Message: "At most 93 days."})
	}
	return from, to, nil
}

// KDSSlotSummaryRow is one time slot's share of the orders.
type KDSSlotSummaryRow struct {
	SlotStart  string  `json:"slotStart"`
	SlotEnd    string  `json:"slotEnd"`
	OrderCount int     `json:"orderCount"`
	Percentage float64 `json:"percentage"`
	IsPeak     bool    `json:"isPeak"`
}

// KDSSlotSummary is GET /kds/orders/tomorrow/slots.
type KDSSlotSummary struct {
	StartDate   string              `json:"startDate"`
	EndDate     string              `json:"endDate"`
	TotalOrders int                 `json:"totalOrders"`
	Slots       []KDSSlotSummaryRow `json:"slots"`
}

// PlanningQuery is shared by the tomorrow / week / calendar reads.
type PlanningQuery struct {
	Scope           KDSScopeQuery
	CurrentTime     time.Time
	StartDate       *time.Time
	EndDate         *time.Time
	SlotStart       string
	SlotEnd         string
	FulfilmentTypes []string
}

// KDSSlotSummary groups the range's orders by time slot. Percentage is the
// slot's share of all the orders in the range; the busiest slot is the peak.
func (s *Service) KDSSlotSummary(ctx context.Context, claims *auth.StaffClaims, q PlanningQuery) (*KDSSlotSummary, error) {
	scope, err := s.ResolveKDSScope(ctx, claims, q.Scope)
	if err != nil {
		return nil, err
	}
	from, to, err := planningRange(scope, q.CurrentTime, q.StartDate, q.EndDate, 1)
	if err != nil {
		return nil, err
	}
	counts, err := s.store.KDSSlotCounts(ctx, scope.BranchIDs, from, to, q.FulfilmentTypes)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	type key struct{ start, end string }
	sum := map[key]int{}
	var keys []key
	total, peak := 0, 0
	for _, c := range counts {
		k := key{hhmm(c.SlotStart), hhmm(c.SlotEnd)}
		if _, ok := sum[k]; !ok {
			keys = append(keys, k)
		}
		sum[k] += c.OrderCount
		total += c.OrderCount
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].start < keys[j].start })
	for _, k := range keys {
		if sum[k] > peak {
			peak = sum[k]
		}
	}
	out := &KDSSlotSummary{StartDate: timex.FormatDate(from), EndDate: timex.FormatDate(to), TotalOrders: total, Slots: []KDSSlotSummaryRow{}}
	for _, k := range keys {
		out.Slots = append(out.Slots, KDSSlotSummaryRow{
			SlotStart: k.start, SlotEnd: k.end, OrderCount: sum[k],
			Percentage: roundPct(sum[k], total), IsPeak: peak > 0 && sum[k] == peak,
		})
	}
	return out, nil
}

func hhmm(t string) string {
	if len(t) >= 5 {
		return t[:5]
	}
	return t
}

// KDSProductRow is one product (and variation) to make.
type KDSProductRow struct {
	ProductID     string             `json:"productId"`
	ProductName   string             `json:"productName"`
	VariantName   string             `json:"variantName"`
	TotalQuantity int                `json:"totalQuantity"`
	OrderCount    int                `json:"orderCount"`
	SlotCount     int                `json:"slotCount"`
	Percentage    float64            `json:"percentage"`
	Modifiers     []KDSModifierCount `json:"modifiers"`
	// This row's configuration: the selected variations (flavours such as
	// "15 Cupcakes") and add-ons. The same product with a different set is
	// its own row.
	Variations []string `json:"variations"`
	Addons     []string `json:"addons"`
}

// KDSModifierCount is a flavour or add-on with the units that carry it.
type KDSModifierCount struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
}

// KDSCategoryRow groups products under their category.
type KDSCategoryRow struct {
	CategoryID    string          `json:"categoryId"`
	CategoryName  string          `json:"categoryName"`
	TotalQuantity int             `json:"totalQuantity"`
	OrderCount    int             `json:"orderCount"`
	Products      []KDSProductRow `json:"products"`
}

// KDSProductSummary is GET /kds/orders/tomorrow/products.
type KDSProductSummary struct {
	StartDate     string           `json:"startDate"`
	EndDate       string           `json:"endDate"`
	SlotStart     string           `json:"slotStart,omitempty"`
	SlotEnd       string           `json:"slotEnd,omitempty"`
	TotalQuantity int              `json:"totalQuantity"`
	Categories    []KDSCategoryRow `json:"categories"`
}

// KDSProductSummary groups the range's order lines by category, product and
// variation (optionally for one slot). Percentage is the product's share of
// all units in the range.
func (s *Service) KDSProductSummary(ctx context.Context, claims *auth.StaffClaims, q PlanningQuery) (*KDSProductSummary, error) {
	scope, err := s.ResolveKDSScope(ctx, claims, q.Scope)
	if err != nil {
		return nil, err
	}
	from, to, err := planningRange(scope, q.CurrentTime, q.StartDate, q.EndDate, 1)
	if err != nil {
		return nil, err
	}
	aggs, err := s.store.KDSItemAggregates(ctx, scope.BranchIDs, from, to, q.SlotStart, q.SlotEnd, q.FulfilmentTypes)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	total := 0
	for _, a := range aggs {
		total += a.TotalQuantity
	}
	byCat := map[string]*KDSCategoryRow{}
	var catOrder []string
	catOrders := map[string]int{}
	for _, a := range aggs {
		c, ok := byCat[a.CategoryID]
		if !ok {
			c = &KDSCategoryRow{CategoryID: a.CategoryID, CategoryName: a.CategoryName, Products: []KDSProductRow{}}
			byCat[a.CategoryID] = c
			catOrder = append(catOrder, a.CategoryID)
		}
		mods := make([]KDSModifierCount, 0, len(a.Modifiers))
		for name, qty := range a.Modifiers {
			mods = append(mods, KDSModifierCount{Name: name, Quantity: qty})
		}
		sort.Slice(mods, func(i, j int) bool {
			if mods[i].Quantity != mods[j].Quantity {
				return mods[i].Quantity > mods[j].Quantity
			}
			return mods[i].Name < mods[j].Name
		})
		c.Products = append(c.Products, KDSProductRow{
			ProductID: a.ProductID, ProductName: a.ProductName, VariantName: a.VariantName,
			TotalQuantity: a.TotalQuantity, OrderCount: a.OrderCount, SlotCount: a.SlotCount,
			Percentage: roundPct(a.TotalQuantity, total), Modifiers: mods,
			Variations: nonNilStrings(a.Variations), Addons: nonNilStrings(a.Addons),
		})
		c.TotalQuantity += a.TotalQuantity
		catOrders[a.CategoryID] += a.OrderCount
	}
	out := &KDSProductSummary{StartDate: timex.FormatDate(from), EndDate: timex.FormatDate(to),
		SlotStart: q.SlotStart, SlotEnd: q.SlotEnd, TotalQuantity: total, Categories: []KDSCategoryRow{}}
	sort.SliceStable(catOrder, func(i, j int) bool { return byCat[catOrder[i]].CategoryName < byCat[catOrder[j]].CategoryName })
	for _, id := range catOrder {
		c := byCat[id]
		sort.SliceStable(c.Products, func(i, j int) bool { return c.Products[i].TotalQuantity > c.Products[j].TotalQuantity })
		c.OrderCount = catOrders[id]
		out.Categories = append(out.Categories, *c)
	}
	return out, nil
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
