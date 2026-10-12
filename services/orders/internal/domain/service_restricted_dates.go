package domain

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/orders/ordersapi"
)

/*
 * Restricted dates from seasonal tags (website orders only).
 *
 * A product mapped to an active tag with restricted dates cannot be ordered
 * for a pickup or delivery slot inside that window. The windows are store-
 * local wall clock, and so are slot dates and times, so they are compared
 * directly — the customer's chosen slot against the tag's from/to.
 *
 *   - slots: GET /storefront/slots?productId=… (product page) or ?cart=true
 *     (the cart's "choose a new date") leaves out the restricted slots and
 *     lists the dates that lose every slot to a restriction;
 *   - the cart: each line restricted at the chosen slot says so, with the
 *     date it is next available (PriceCart, so every cart response has it);
 *   - choosing a slot and checking out refuse a restricted one.
 *
 * Counter and admin manual orders are not restricted: staff decide there.
 */

// UseTagRestrictions plugs in the catalogue's restricted windows.
func (s *Service) UseTagRestrictions(r catalogapi.TagRestrictions) { s.tagRestrictions = r }

// RestrictionInfo is what a restricted slot listing adds.
type RestrictionInfo struct {
	// UnavailableDates are days in the window that had slots but lost every
	// one to a restriction ("Not available for this product").
	UnavailableDates []string
	// Restrictions are the windows that applied, per product.
	Restrictions []ProductRestriction
}

// ProductRestriction is one product's restricted window from one tag.
type ProductRestriction struct {
	ProductID    string
	TagID        string
	TagName      string
	From, To     time.Time // this year's occurrence, store-local
	RepeatYearly bool
}

// productRestrictions reads the windows for products; empty when the
// catalogue is not wired or nothing is restricted.
func (s *Service) productRestrictions(ctx context.Context, productIDs []string) map[string][]catalogapi.TagRestriction {
	if s.tagRestrictions == nil || len(productIDs) == 0 {
		return nil
	}
	rs, err := s.tagRestrictions.RestrictionsForProducts(ctx, productIDs)
	if err != nil {
		slog.Warn("restricted dates: could not read tag restrictions", "error", err)
		return nil
	}
	return rs
}

func allRestrictions(byProduct map[string][]catalogapi.TagRestriction) []catalogapi.TagRestriction {
	var out []catalogapi.TagRestriction
	for _, rs := range byProduct {
		out = append(out, rs...)
	}
	return out
}

// restrictedHorizonDays is how far past the slot window blocked days are listed.
const restrictedHorizonDays = 366

// mergeDates is the sorted union of two lists of YYYY-MM-DD dates.
func mergeDates(a, b []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, d := range append(append([]string{}, a...), b...) {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

// restrictedDaysAfter lists the days after 'after', up to a year on, that
// the windows cover entirely (nothing can be ordered for them). A day free
// for its last minute only (a window ending 23:59) counts as covered.
func restrictedDaysAfter(rs []catalogapi.TagRestriction, after time.Time) []string {
	out := []string{}
	day := time.Date(after.Year(), after.Month(), after.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	for i := 0; i < restrictedHorizonDays; i++ {
		if !catalogapi.FreeAfter(day, rs).Before(day.Add(24*time.Hour - time.Minute)) {
			out = append(out, day.Format("2006-01-02"))
		}
		day = day.AddDate(0, 0, 1)
	}
	return out
}

// slotWall is a slot's start or end as store-local wall clock.
func slotWall(date time.Time, hhmm string) time.Time {
	t, err := time.Parse("15:04", hhmm5(hhmm))
	if err != nil {
		return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)
	}
	return time.Date(date.Year(), date.Month(), date.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
}

// hhmm5 is "HH:MM" from "HH:MM" or "HH:MM:SS".
func hhmm5(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 5 {
		return s[:5]
	}
	return s
}

// GetAvailableSlotsFor is GetAvailableSlots without the slots that are
// restricted for any of the products.
func (s *Service) GetAvailableSlotsFor(ctx context.Context, branchID, fulfilmentType string, startDate time.Time, days int, productIDs []string) ([]ordersapi.DaySlots, string, *RestrictionInfo, error) {
	slots, cutoffNote, err := s.GetAvailableSlots(ctx, branchID, fulfilmentType, startDate, days)
	if err != nil {
		return nil, "", nil, err
	}
	info := &RestrictionInfo{UnavailableDates: []string{}, Restrictions: []ProductRestriction{}}
	byProduct := s.productRestrictions(ctx, productIDs)
	if len(byProduct) == 0 {
		return slots, cutoffNote, info, nil
	}
	rs := allRestrictions(byProduct)

	out := make([]ordersapi.DaySlots, 0, len(slots))
	for _, day := range slots {
		date, err := time.Parse("2006-01-02", day.Date)
		if err != nil {
			out = append(out, day)
			continue
		}
		kept := day.Slots[:0:0]
		for _, sc := range day.Slots {
			if _, blocked := catalogapi.AnyOverlaps(rs, slotWall(date, sc.SlotStart), slotWall(date, sc.SlotEnd)); !blocked {
				kept = append(kept, sc)
			}
		}
		if len(kept) == 0 {
			info.UnavailableDates = append(info.UnavailableDates, day.Date)
			continue
		}
		day.Slots = kept
		out = append(out, day)
	}
	// Every day a restriction covers entirely is blocked too, whether or not
	// the listing had slots for it: a day the store is closed, a day past the
	// booking horizon, or past the slot window (12 Oct – 17 Dec against a
	// month of slots). The calendar strikes the whole range.
	info.UnavailableDates = mergeDates(info.UnavailableDates,
		restrictedDaysAfter(rs, time.Date(startDate.Year(), startDate.Month(), startDate.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)))

	year := startDate.Year()
	for pid, list := range byProduct {
		for _, r := range list {
			from, to := r.WindowIn(year)
			info.Restrictions = append(info.Restrictions, ProductRestriction{
				ProductID: pid, TagID: r.TagID, TagName: r.TagName, From: from, To: to, RepeatYearly: r.RepeatYearly,
			})
		}
	}
	sort.Slice(info.Restrictions, func(i, j int) bool { return info.Restrictions[i].From.Before(info.Restrictions[j].From) })
	return out, cutoffNote, info, nil
}

// CartProductIDs is the products in the caller's cart (for ?cart=true).
func (s *Service) CartProductIDs(ctx context.Context, id CartIdentity) []string {
	cart, err := s.resolveCart(ctx, id)
	if err != nil || cart == nil {
		return nil
	}
	ids := make([]string, 0, len(cart.Items))
	for _, it := range cart.Items {
		ids = append(ids, it.ProductID)
	}
	return ids
}

// markRestrictedLines flags each cart line that cannot be ordered for the
// cart's chosen slot. Returns how many are flagged.
func (s *Service) markRestrictedLines(ctx context.Context, cart *ordersapi.Cart) int {
	for i := range cart.Items {
		cart.Items[i].Restriction = nil
	}
	if cart.ScheduledDate == nil || cart.SlotStart == nil || len(cart.Items) == 0 {
		return 0
	}
	ids := make([]string, 0, len(cart.Items))
	for _, it := range cart.Items {
		ids = append(ids, it.ProductID)
	}
	byProduct := s.productRestrictions(ctx, ids)
	if len(byProduct) == 0 {
		return 0
	}
	start := slotWall(*cart.ScheduledDate, *cart.SlotStart)
	end := start.Add(fulfilmentSlotMinutes * time.Minute)
	if cart.SlotEnd != nil {
		end = slotWall(*cart.ScheduledDate, *cart.SlotEnd)
	}
	flagged := 0
	for i := range cart.Items {
		it := &cart.Items[i]
		rs := byProduct[it.ProductID]
		r, blocked := catalogapi.AnyOverlaps(rs, start, end)
		if !blocked {
			continue
		}
		from, to := r.WindowIn(start.Year())
		next := catalogapi.FreeAfter(start, rs)
		it.Restriction = &ordersapi.ItemRestriction{
			TagID: r.TagID, TagName: r.TagName,
			WindowFrom: from.Format("2006-01-02T15:04:05"), WindowTo: to.Format("2006-01-02T15:04:05"),
			AvailableFrom: next.Format("2006-01-02"),
			Message: fmt.Sprintf("%s can't be ordered for %s. Available from %s.",
				it.ProductName, start.Format("Jan 2"), next.Format("Jan 2")),
		}
		flagged++
	}
	return flagged
}

// errRestrictedItems is the refusal for a slot some cart lines can't have.
func errRestrictedItems(cart *ordersapi.Cart) error {
	var names []string
	var lines []map[string]any
	for _, it := range cart.Items {
		if it.Restriction == nil {
			continue
		}
		names = append(names, it.ProductName)
		lines = append(lines, map[string]any{
			"itemId": it.ID, "productId": it.ProductID, "productName": it.ProductName,
			"tagName": it.Restriction.TagName, "availableFrom": it.Restriction.AvailableFrom,
		})
	}
	msg := fmt.Sprintf("%s isn't available for the chosen date. Change the date or remove it.", strings.Join(names, ", "))
	if len(names) > 1 {
		msg = fmt.Sprintf("%s aren't available for the chosen date. Change the date or remove them.", strings.Join(names, ", "))
	}
	return apperr.New("PRODUCT_RESTRICTED_DATE", apperr.CategoryBusinessRule, msg).
		WithMeta(map[string]any{"items": lines})
}

// isManualCart: an admin manual order's cart, which restrictions skip.
func (id CartIdentity) isManualCart() bool {
	return id.CustomerID == nil && strings.HasPrefix(id.GuestToken, "manual-")
}
