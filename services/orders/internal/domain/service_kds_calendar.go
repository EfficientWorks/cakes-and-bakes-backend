package domain

import (
	"context"
	"math"
	"sort"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/timex"
	"bakehouse/services/franchise/franchiseapi"
)

// KDSDayRow is one day of the week view.
type KDSDayRow struct {
	Date       string `json:"date"`
	DayName    string `json:"dayName"`
	OrderCount int    `json:"orderCount"`
	// Share of all orders in the range.
	Percentage float64 `json:"percentage"`
	// Relative to the busiest day (100 = the busiest) — for the load bar.
	LoadPercentage float64 `json:"loadPercentage"`
	IsPeak         bool    `json:"isPeak"`
}

// KDSWeekSummary is GET /kds/orders/week.
type KDSWeekSummary struct {
	StartDate     string      `json:"startDate"`
	EndDate       string      `json:"endDate"`
	TotalOrders   int         `json:"totalOrders"`
	AveragePerDay float64     `json:"averagePerDay"`
	PeakDate      *string     `json:"peakDate"`
	Days          []KDSDayRow `json:"days"`
}

// KDSWeekSummary counts orders per day over the range (default: the next 7
// days from tomorrow). Every day in the range is listed, zeros included.
func (s *Service) KDSWeekSummary(ctx context.Context, claims *auth.StaffClaims, q PlanningQuery) (*KDSWeekSummary, error) {
	scope, err := s.ResolveKDSScope(ctx, claims, q.Scope)
	if err != nil {
		return nil, err
	}
	from, to, err := planningRange(scope, q.CurrentTime, q.StartDate, q.EndDate, 7)
	if err != nil {
		return nil, err
	}
	counts, err := s.store.KDSDayCounts(ctx, scope.BranchIDs, from, to, q.FulfilmentTypes)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	byDay := map[string]int{}
	total, peak := 0, 0
	for _, c := range counts {
		d := timex.FormatDate(c.Date)
		byDay[d] += c.OrderCount
		total += c.OrderCount
	}
	for _, n := range byDay {
		if n > peak {
			peak = n
		}
	}
	out := &KDSWeekSummary{StartDate: timex.FormatDate(from), EndDate: timex.FormatDate(to), TotalOrders: total, Days: []KDSDayRow{}}
	days := 0
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		days++
		key := timex.FormatDate(d)
		n := byDay[key]
		row := KDSDayRow{Date: key, DayName: d.Weekday().String(), OrderCount: n,
			Percentage: roundPct(n, total), LoadPercentage: roundPct(n, peak), IsPeak: peak > 0 && n == peak}
		if row.IsPeak && out.PeakDate == nil {
			k := key
			out.PeakDate = &k
		}
		out.Days = append(out.Days, row)
	}
	if days > 0 {
		out.AveragePerDay = math.Round(float64(total)*10/float64(days)) / 10
	}
	return out, nil
}

// KDSCalendarCell is one day × slot.
type KDSCalendarCell struct {
	Date       string `json:"date"`
	SlotStart  string `json:"slotStart"`
	SlotEnd    string `json:"slotEnd"`
	OrderCount int    `json:"orderCount"`
	// none | low | medium | high, from the location's calendar settings.
	Level string `json:"level"`
}

// KDSCalendarLevel is one legend entry.
type KDSCalendarLevel struct {
	Level string `json:"level"`
	From  int    `json:"from"`
	To    *int   `json:"to"`
}

// KDSCalendar is GET /kds/orders/calendar.
type KDSCalendar struct {
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Days      []struct {
		Date    string `json:"date"`
		DayName string `json:"dayName"`
		IsToday bool   `json:"isToday"`
	} `json:"days"`
	Slots []struct {
		SlotStart string `json:"slotStart"`
		SlotEnd   string `json:"slotEnd"`
	} `json:"slots"`
	Cells   []KDSCalendarCell  `json:"cells"`
	Levels  []KDSCalendarLevel `json:"levels"`
	Summary struct {
		PreOrders          int     `json:"preOrders"`
		BusiestDay         *string `json:"busiestDay"`
		BusiestDate        *string `json:"busiestDate"`
		AvgPreOrdersPerDay float64 `json:"avgPreOrdersPerDay"`
		BookedSlots        int     `json:"bookedSlots"`
	} `json:"summary"`
}

func calendarLevel(n int, st *franchiseapi.KDSSettings) string {
	switch {
	case n <= 0:
		return "none"
	case n >= st.CalendarHighFrom:
		return "high"
	case n >= st.CalendarMediumFrom:
		return "medium"
	case n >= st.CalendarLowFrom:
		return "low"
	}
	return "none"
}

// KDSCalendar is a day × time-slot grid of order counts with Low / Medium /
// High labels from the location's calendar settings (default: this week,
// Sunday to Saturday).
func (s *Service) KDSCalendar(ctx context.Context, claims *auth.StaffClaims, q PlanningQuery) (*KDSCalendar, error) {
	scope, err := s.ResolveKDSScope(ctx, claims, q.Scope)
	if err != nil {
		return nil, err
	}
	loc := scope.location()
	n := q.CurrentTime.In(loc)
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
	if q.StartDate == nil && q.EndDate == nil {
		start := today.AddDate(0, 0, -int(today.Weekday()))
		end := start.AddDate(0, 0, 6)
		q.StartDate, q.EndDate = &start, &end
	}
	from, to, err := planningRange(scope, q.CurrentTime, q.StartDate, q.EndDate, 7)
	if err != nil {
		return nil, err
	}
	counts, err := s.store.KDSSlotCounts(ctx, scope.BranchIDs, from, to, q.FulfilmentTypes)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	st := scope.Settings
	out := &KDSCalendar{StartDate: timex.FormatDate(from), EndDate: timex.FormatDate(to), Cells: []KDSCalendarCell{}}
	hi := st.CalendarHighFrom
	lowTo, medTo := st.CalendarLowTo, st.CalendarMediumTo
	out.Levels = []KDSCalendarLevel{
		{Level: "none", From: 0, To: new(int)},
		{Level: "low", From: st.CalendarLowFrom, To: &lowTo},
		{Level: "medium", From: st.CalendarMediumFrom, To: &medTo},
		{Level: "high", From: hi, To: nil},
	}

	type slotKey struct{ start, end string }
	slotSeen := map[slotKey]bool{}
	var slots []slotKey
	cell := map[string]int{}
	perDay := map[string]int{}
	for _, c := range counts {
		k := slotKey{hhmm(c.SlotStart), hhmm(c.SlotEnd)}
		if !slotSeen[k] {
			slotSeen[k] = true
			slots = append(slots, k)
		}
		d := timex.FormatDate(c.Date)
		cell[d+"|"+k.start+"|"+k.end] += c.OrderCount
		perDay[d] += c.OrderCount
		out.Summary.PreOrders += c.OrderCount
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].start < slots[j].start })
	for _, k := range slots {
		out.Slots = append(out.Slots, struct {
			SlotStart string `json:"slotStart"`
			SlotEnd   string `json:"slotEnd"`
		}{k.start, k.end})
	}
	days, busiest := 0, 0
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		days++
		key := timex.FormatDate(d)
		out.Days = append(out.Days, struct {
			Date    string `json:"date"`
			DayName string `json:"dayName"`
			IsToday bool   `json:"isToday"`
		}{key, d.Weekday().String(), d.Equal(today)})
		if perDay[key] > busiest {
			busiest = perDay[key]
			name, date := d.Weekday().String(), key
			out.Summary.BusiestDay, out.Summary.BusiestDate = &name, &date
		}
		for _, k := range slots {
			nOrders := cell[key+"|"+k.start+"|"+k.end]
			if nOrders > 0 {
				out.Summary.BookedSlots++
			}
			out.Cells = append(out.Cells, KDSCalendarCell{Date: key, SlotStart: k.start, SlotEnd: k.end,
				OrderCount: nOrders, Level: calendarLevel(nOrders, st)})
		}
	}
	if out.Slots == nil {
		out.Slots = []struct {
			SlotStart string `json:"slotStart"`
			SlotEnd   string `json:"slotEnd"`
		}{}
	}
	if days > 0 {
		out.Summary.AvgPreOrdersPerDay = math.Round(float64(out.Summary.PreOrders)*10/float64(days)) / 10
	}
	return out, nil
}

// KDSFooter is the board's status line.
type KDSFooter struct {
	PreordersTomorrow int    `json:"preordersTomorrow"`
	SlotsNearlyFull   int    `json:"slotsNearlyFull"`
	TomorrowDate      string `json:"tomorrowDate"`
}

// KDSFooterSummary: tomorrow's pre-orders and how many of tomorrow's bookable
// slots are at 75%+ of capacity.
func (s *Service) KDSFooterSummary(ctx context.Context, claims *auth.StaffClaims, q PlanningQuery) (*KDSFooter, error) {
	scope, err := s.ResolveKDSScope(ctx, claims, q.Scope)
	if err != nil {
		return nil, err
	}
	from, _, err := planningRange(scope, q.CurrentTime, nil, nil, 1)
	if err != nil {
		return nil, err
	}
	days, err := s.store.KDSDayCounts(ctx, scope.BranchIDs, from, from, nil)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	out := &KDSFooter{TomorrowDate: timex.FormatDate(from)}
	for _, d := range days {
		out.PreordersTomorrow += d.OrderCount
	}
	if n, err := s.store.KDSSlotsNearlyFull(ctx, scope.BranchIDs, from); err == nil {
		out.SlotsNearlyFull = n
	}
	return out, nil
}
