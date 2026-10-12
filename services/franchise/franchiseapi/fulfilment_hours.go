package franchiseapi

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

/*
 * Pickup and delivery hours.
 *
 * Each fulfilment has its own week, and a day can have several ranges
 * (9:00–12:00 and 14:00–18:00). Slots are cut from every range of the day.
 * A store whose hours were never set falls back to its opening hours, so it
 * keeps the slots it always had.
 */

// TimeRange is one open–close stretch of a day, "HH:MM" store-local.
type TimeRange struct {
	Open  string `json:"open"`
	Close string `json:"close"`
}

// DayHours is one weekday's hours for a fulfilment.
type DayHours struct {
	Day    string      `json:"day"` // monday..sunday
	Closed bool        `json:"closed"`
	Ranges []TimeRange `json:"ranges"`
}

// Weekdays in display order.
var Weekdays = []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"}

// HoursFor is the week a fulfilment ("pickup" or "delivery") runs on: its own
// hours when set (delivery uses pickup's when the store keeps them the same),
// otherwise the opening hours.
func (b *Branch) HoursFor(fulfilment string) []DayHours {
	if fulfilment == "delivery" && !b.SameFulfilmentHours && len(b.DeliveryHours) > 0 {
		return b.DeliveryHours
	}
	if len(b.PickupHours) > 0 {
		return b.PickupHours
	}
	return HoursFromOpening(b.OpeningHours)
}

// RangesOn is the day's ranges for a fulfilment on date (nil = closed).
func (b *Branch) RangesOn(fulfilment string, date time.Time) []TimeRange {
	day := strings.ToLower(date.Weekday().String())
	for _, d := range b.HoursFor(fulfilment) {
		if d.Day == day {
			if d.Closed {
				return nil
			}
			return d.Ranges
		}
	}
	return nil
}

// HoursFromOpening turns the one-range opening hours into a week.
func HoursFromOpening(opening []OpeningHour) []DayHours {
	out := make([]DayHours, 0, len(opening))
	for _, h := range opening {
		d := DayHours{Day: strings.ToLower(h.Day)}
		if h.Open == nil || h.Close == nil || *h.Open == "" || *h.Close == "" {
			d.Closed = true
		} else {
			d.Ranges = []TimeRange{{Open: hhmm(*h.Open), Close: hhmm(*h.Close)}}
		}
		out = append(out, d)
	}
	return out
}

// NormalizeHours checks a week admin is saving and returns it tidied: days
// lower-cased, ranges sorted, a closed day with no ranges.
func NormalizeHours(in []DayHours, label string) ([]DayHours, error) {
	if in == nil {
		return nil, nil
	}
	seen := map[string]bool{}
	out := make([]DayHours, 0, len(in))
	for _, d := range in {
		d.Day = strings.ToLower(strings.TrimSpace(d.Day))
		if !isWeekday(d.Day) {
			return nil, fmt.Errorf("%s hours: %q is not a day of the week", label, d.Day)
		}
		if seen[d.Day] {
			return nil, fmt.Errorf("%s hours: %s is listed twice", label, d.Day)
		}
		seen[d.Day] = true
		if d.Closed {
			d.Ranges = nil
			out = append(out, d)
			continue
		}
		if len(d.Ranges) == 0 {
			return nil, fmt.Errorf("%s hours: add hours for %s, or mark it closed", label, d.Day)
		}
		for i := range d.Ranges {
			r := &d.Ranges[i]
			r.Open, r.Close = hhmm(r.Open), hhmm(r.Close)
			open, err1 := time.Parse("15:04", r.Open)
			close, err2 := time.Parse("15:04", r.Close)
			if err1 != nil || err2 != nil || !close.After(open) {
				return nil, fmt.Errorf("%s hours: on %s, each range needs a start and a later end", label, d.Day)
			}
		}
		sort.Slice(d.Ranges, func(i, j int) bool { return d.Ranges[i].Open < d.Ranges[j].Open })
		for i := 1; i < len(d.Ranges); i++ {
			if d.Ranges[i].Open < d.Ranges[i-1].Close {
				return nil, fmt.Errorf("%s hours: on %s, the hours overlap", label, d.Day)
			}
		}
		out = append(out, d)
	}
	return out, nil
}

func isWeekday(d string) bool {
	for _, w := range Weekdays {
		if w == d {
			return true
		}
	}
	return false
}

// hhmm trims "09:00:00" to "09:00".
func hhmm(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 5 {
		return s[:5]
	}
	return s
}
