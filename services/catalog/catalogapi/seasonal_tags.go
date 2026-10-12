package catalogapi

import (
	"context"
	"regexp"
	"strings"
	"time"
)

/*
 * Seasonal tags.
 *
 * A tag ("Mother's Day", "Best Seller") is created once and mapped to
 * products. The storefront filter and gift finder match on tag ids.
 *
 * A tag may carry restricted dates: from a date and time to a date and time,
 * optionally every year. A product mapped to an active restricted tag cannot
 * be ordered on the website for a pickup or delivery slot inside that window
 * (it can still be viewed). The times are wall-clock times read in each
 * store's own time zone, and compared with the slot time the customer picks.
 */

const (
	TagActive   = "active"
	TagInactive = "inactive"
)

// SeasonalTag is a tag and its restricted dates.
type SeasonalTag struct {
	ID          string
	Name        string
	Slug        string
	Description *string
	Status      string
	// Restricted dates. From/To are wall-clock times (their location is
	// meaningless; read them in the store's time zone).
	Restricted     bool
	RestrictedFrom *time.Time
	RestrictedTo   *time.Time
	RepeatYearly   bool
	ProductCount   int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Active reports whether the tag is switched on.
func (t SeasonalTag) Active() bool { return t.Status == TagActive }

// Restriction is the tag's restricted window, when it has one and is active.
func (t SeasonalTag) Restriction() (TagRestriction, bool) {
	if !t.Active() || !t.Restricted || t.RestrictedFrom == nil || t.RestrictedTo == nil {
		return TagRestriction{}, false
	}
	return TagRestriction{TagID: t.ID, TagName: t.Name, From: *t.RestrictedFrom, To: *t.RestrictedTo, RepeatYearly: t.RepeatYearly}, true
}

// TagRestriction is one restricted window from one tag.
type TagRestriction struct {
	TagID        string
	TagName      string
	From, To     time.Time // wall clock
	RepeatYearly bool
}

// wall drops the location: restricted times and slot times are compared as
// wall-clock values in the store's own time zone.
func wall(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
}

// windows are the concrete windows to test against a time in year: the
// window itself, or for a yearly one, its occurrences around that year (a
// window can run over New Year).
func (r TagRestriction) windows(year int) [][2]time.Time {
	from, to := wall(r.From), wall(r.To)
	if !r.RepeatYearly {
		return [][2]time.Time{{from, to}}
	}
	length := to.Sub(from)
	out := make([][2]time.Time, 0, 3)
	for y := year - 1; y <= year+1; y++ {
		f := time.Date(y, from.Month(), from.Day(), from.Hour(), from.Minute(), 0, 0, time.UTC)
		out = append(out, [2]time.Time{f, f.Add(length)})
	}
	return out
}

// Overlaps reports whether a slot [start, end) (store-local wall clock)
// touches the restricted window.
func (r TagRestriction) Overlaps(start, end time.Time) bool {
	start, end = wall(start), wall(end)
	if !end.After(start) {
		end = start.Add(time.Minute)
	}
	for _, w := range r.windows(start.Year()) {
		if start.Before(w[1]) && end.After(w[0]) {
			return true
		}
	}
	return false
}

// CoversDay reports whether any part of a calendar day is restricted.
func (r TagRestriction) CoversDay(day time.Time) bool {
	d := wall(day)
	start := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	return r.Overlaps(start, start.AddDate(0, 0, 1))
}

// WindowIn is the occurrence of the window that touches the given year
// (the window itself for a one-off). For display: "Nov 24 – Nov 27".
func (r TagRestriction) WindowIn(year int) (time.Time, time.Time) {
	ws := r.windows(year)
	if len(ws) == 1 {
		return ws[0][0], ws[0][1]
	}
	return ws[1][0], ws[1][1]
}

// FreeAfter is the first moment at or after t not inside any of the windows
// (t itself when it is free).
func FreeAfter(t time.Time, rs []TagRestriction) time.Time {
	t = wall(t)
	for i := 0; i < 50; i++ { // windows chain at most a few times
		moved := false
		for _, r := range rs {
			for _, w := range r.windows(t.Year()) {
				if !t.Before(w[0]) && t.Before(w[1]) {
					t, moved = w[1], true
				}
			}
		}
		if !moved {
			return t
		}
	}
	return t
}

// AnyOverlaps reports the first restriction a slot falls in, if any.
func AnyOverlaps(rs []TagRestriction, start, end time.Time) (TagRestriction, bool) {
	for _, r := range rs {
		if r.Overlaps(start, end) {
			return r, true
		}
	}
	return TagRestriction{}, false
}

// TagRestrictions is how orders learns which products are restricted when.
type TagRestrictions interface {
	// RestrictionsForProducts returns, per product id, the restricted windows
	// of its active tags. Products without any are absent.
	RestrictionsForProducts(ctx context.Context, productIDs []string) (map[string][]TagRestriction, error)
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// TagSlug is the stable key for a tag name ("Mother's Day" → "mother-s-day";
// "thank_you" and "Thank You" → "thank-you").
func TagSlug(name string) string {
	return strings.Trim(slugUnsafe.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-"), "-")
}
