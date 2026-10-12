package domain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/orders/internal/store"
	"bakehouse/services/orders/ordersapi"
)

// GetAvailableSlots implements SF-20/SCH-01..SCH-06: only slots the branch
// can genuinely fulfil are ever returned — respecting opening hours,
// exception-date overrides, lead time, the same-day cut-off, and remaining
// capacity. A full slot is never returned (the customer never sees a slot
// it then can't book).
// maxSlotListingDays bounds one slot listing; it is not a booking limit
// (a later date is listed by asking from that date).
const maxSlotListingDays = 400

func (s *Service) GetAvailableSlots(ctx context.Context, branchID, fulfilmentType string, startDate time.Time, days int) ([]ordersapi.DaySlots, string, error) {
	branch, err := s.branches.GetBranch(ctx, branchID)
	if err != nil {
		return nil, "", apperr.NotFound("Branch not found.")
	}
	cfg, err := s.store.GetScheduleConfig(ctx, branchID)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}
	exceptions, err := s.branches.ListBranchExceptions(ctx, branchID)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}
	exceptionByDate := map[string]franchiseapi.BranchException{}
	for _, e := range exceptions {
		exceptionByDate[e.Date.Format("2006-01-02")] = e
	}

	loc, err := time.LoadLocation(branch.Timezone)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	leadHours := cfg.DefaultLeadHours
	if leadHours <= 0 {
		leadHours = 2
	}
	earliestBookable := now.Add(time.Duration(leadHours) * time.Hour)

	cutoff := cfg.DeliveryCutoffTime
	if fulfilmentType == ordersapi.FulfilmentPickup {
		cutoff = cfg.PickupCutoffTime
	}

	if days <= 0 {
		days = 14
	}
	// No advance-booking limit: any future date can be ordered for. Only a
	// request for an unreasonable span is trimmed.
	if days > maxSlotListingDays {
		days = maxSlotListingDays
	}

	// Pass 1: work out every slot the window could offer, without touching
	// the database. Pass 2 fetches (or creates) all of them in one query.
	type dayPlan struct {
		dateStr string
		label   string
		keys    []store.SlotKey
	}
	var plans []dayPlan
	var allKeys []store.SlotKey
	for d := 0; d < days; d++ {
		date := startDate.AddDate(0, 0, d)
		dateStr := date.Format("2006-01-02")
		isToday := date.Format("2006-01-02") == now.Format("2006-01-02")

		ranges := fulfilmentRanges(branch, exceptionByDate[dateStr], date, fulfilmentType)
		if len(ranges) == 0 {
			continue
		}

		// Same-day cut-off: once past cutoff, today offers nothing more,
		// regardless of remaining lead time (SCH-05).
		if isToday {
			cutoffTime, err := time.ParseInLocation("15:04:05", normaliseTime(cutoff), loc)
			if err == nil {
				cutoffToday := time.Date(now.Year(), now.Month(), now.Day(), cutoffTime.Hour(), cutoffTime.Minute(), 0, 0, loc)
				if now.After(cutoffToday) {
					continue
				}
			}
		}

		// Every range of the day, cut into fulfilmentSlotMinutes slots.
		var keys []store.SlotKey
		for _, r := range ranges {
			keys = append(keys, candidateSlots(date, r.Open, r.Close, fulfilmentSlotMinutes, earliestBookable, loc)...)
		}
		if len(keys) == 0 {
			continue
		}

		label := dateStr
		switch d {
		case 0:
			label = "Today"
		case 1:
			label = "Tomorrow"
		}
		plans = append(plans, dayPlan{dateStr: dateStr, label: label, keys: keys})
		allKeys = append(allKeys, keys...)
	}

	byKey, err := s.store.GetOrCreateSlots(ctx, branch.ID, fulfilmentType, cfg.MaxOrdersPerSlot, allKeys)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}

	var out []ordersapi.DaySlots
	for _, p := range plans {
		var slots []ordersapi.SlotCapacity
		for _, k := range p.keys {
			sc, ok := byKey[store.SlotMapKey(k.Date, k.SlotStart)]
			// A full slot is never offered (SCH-04).
			if ok && sc.Remaining() > 0 {
				slots = append(slots, sc)
			}
		}
		if len(slots) == 0 {
			continue
		}
		out = append(out, ordersapi.DaySlots{Date: p.dateStr, Label: p.label, Slots: slots})
	}

	cutoffNote := ""
	if isTodayInList(out) {
		remaining := time.Until(cutoffTimeToday(cutoff, loc))
		if remaining > 0 {
			h := int(remaining.Hours())
			m := int(remaining.Minutes()) % 60
			cutoffNote = fmt.Sprintf("Order within %dh %dm for %s today", h, m, fulfilmentType)
		}
	}

	return out, cutoffNote, nil
}

func isTodayInList(days []ordersapi.DaySlots) bool {
	today := time.Now().Format("2006-01-02")
	for _, d := range days {
		if d.Date == today {
			return true
		}
	}
	return false
}

func cutoffTimeToday(cutoff string, loc *time.Location) time.Time {
	now := time.Now().In(loc)
	t, err := time.ParseInLocation("15:04:05", normaliseTime(cutoff), loc)
	if err != nil {
		return now
	}
	return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, loc)
}

func normaliseTime(t string) string {
	if len(t) == 5 { // "14:00" -> "14:00:00"
		return t + ":00"
	}
	return t
}

// fulfilmentSlotMinutes is the length of a pickup or delivery slot: each of
// the day's ranges is cut into slots this long.
const fulfilmentSlotMinutes = 30

// fulfilmentRanges is when a fulfilment runs on a date: the exception for the
// date when there is one (closed, or its own hours), else the store's pickup
// or delivery hours for that weekday. nil means closed.
func fulfilmentRanges(branch *franchiseapi.Branch, exception franchiseapi.BranchException, date time.Time, fulfilmentType string) []franchiseapi.TimeRange {
	if exception.ID != "" {
		if exception.IsClosed {
			return nil
		}
		if exception.OpenTime != nil && exception.CloseTime != nil {
			return []franchiseapi.TimeRange{{Open: hhmm(*exception.OpenTime), Close: hhmm(*exception.CloseTime)}}
		}
	}
	fulfilment := "pickup"
	if fulfilmentType == ordersapi.FulfilmentDelivery {
		fulfilment = "delivery"
	}
	return branch.RangesOn(fulfilment, date)
}

// resolveDayHours applies the exception-date override (highest priority)
// then the weekly schedule (SCH-06, SCH-07).
func resolveDayHours(branch *franchiseapi.Branch, exception franchiseapi.BranchException, date time.Time) (open, close string, closed bool) {
	if exception.ID != "" {
		if exception.IsClosed {
			return "", "", true
		}
		if exception.OpenTime != nil && exception.CloseTime != nil {
			return *exception.OpenTime, *exception.CloseTime, false
		}
	}

	dayName := strings.ToLower(date.Weekday().String())
	for _, h := range branch.OpeningHours {
		if h.Day == dayName {
			if h.Open == nil || h.Close == nil {
				return "", "", true
			}
			return *h.Open, *h.Close, false
		}
	}
	return "", "", true // no configured hours for this day = closed
}

// candidateSlots lists the slots between open and close on date that start
// no earlier than earliestBookable. It only does the arithmetic; capacity is
// looked up for the whole window at once by the caller.
func candidateSlots(date time.Time, open, close string, durationMins int, earliestBookable time.Time, loc *time.Location) []store.SlotKey {
	openT, err := time.Parse("15:04", open)
	if err != nil {
		return nil
	}
	closeT, err := time.Parse("15:04", close)
	if err != nil {
		return nil
	}
	if durationMins <= 0 {
		// A zero duration would never advance the cursor.
		return nil
	}

	var out []store.SlotKey
	cursor := openT
	for cursor.Before(closeT) {
		slotEnd := cursor.Add(time.Duration(durationMins) * time.Minute)
		if slotEnd.After(closeT) {
			break
		}
		slotStartAbsolute := time.Date(date.Year(), date.Month(), date.Day(), cursor.Hour(), cursor.Minute(), 0, 0, loc)
		if !slotStartAbsolute.Before(earliestBookable) {
			out = append(out, store.SlotKey{Date: date, SlotStart: cursor.Format("15:04"), SlotEnd: slotEnd.Format("15:04")})
		}
		cursor = slotEnd
	}
	return out
}

// --- Admin schedule configuration (SCH-01, SCH-02, SCH-05, SCH-09) ---------

func (s *Service) GetScheduleConfig(ctx context.Context, branchID string) (*ordersapi.ScheduleConfig, error) {
	return s.store.GetScheduleConfig(ctx, branchID)
}

func (s *Service) UpdateScheduleConfig(ctx context.Context, cfg ordersapi.ScheduleConfig) (*ordersapi.ScheduleConfig, error) {
	if cfg.SlotDurationMins <= 0 {
		cfg.SlotDurationMins = 60
	}
	if cfg.MaxOrdersPerSlot <= 0 {
		cfg.MaxOrdersPerSlot = 10
	}
	return s.store.UpsertScheduleConfig(ctx, cfg)
}

func (s *Service) ListSlots(ctx context.Context, branchID string, date time.Time, fulfilmentType string) ([]ordersapi.SlotCapacity, error) {
	return s.store.ListSlots(ctx, branchID, date, fulfilmentType)
}

// UpliftSlotCapacity is a temporary capacity increase for a known busy date
// (SCH-09) — it edits the one slot row directly without touching the
// standing schedule configuration.
func (s *Service) UpliftSlotCapacity(ctx context.Context, slotID string, maxCapacity int) error {
	if maxCapacity < 1 {
		return apperr.Validation("Capacity must be at least 1.")
	}
	if err := s.store.UpdateSlotCapacity(ctx, slotID, maxCapacity); err != nil {
		return notFoundOrInternal(err, "Slot not found.")
	}
	return nil
}
