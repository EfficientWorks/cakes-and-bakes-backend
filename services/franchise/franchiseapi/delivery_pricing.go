package franchiseapi

import (
	"bakehouse/pkg/country"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

/*
 * Delivery pricing, per store.
 *
 * Two questions, always in this order:
 *
 *  1. Does this store deliver here at all? (the region)
 *       zip:    the customer's ZIP is one of the store's mapped ZIPs
 *               (Branch.DeliveryZone.Zips).
 *       radius: the straight-line distance from the store to the customer
 *               is within RadiusValue (km or miles).
 *
 *  2. What does it cost? (the charge type)
 *       free:         nothing.
 *       fixed:        FixedFeeCents on every order.
 *       by_area:      zip region: the fee set against the customer's ZIP;
 *                     radius region: the fee of the distance band the
 *                     customer falls in (no band: no delivery). Either may
 *                     be free above an order amount.
 *       order_amount: free at or above FreeOverCents, otherwise
 *                     OtherOrdersFeeCents.
 *
 *     Charge by delivery time (any type but free): when the order's slot
 *     starts inside one of the time slots, that slot's fee is charged instead
 *     of everything above — except for an order at or above its free-over
 *     amount, which stays free.
 *
 * Money is cents throughout; the order amount compared against thresholds is
 * the item subtotal before coupons, tip, tax and delivery.
 */

const (
	RegionZip    = "zip"
	RegionRadius = "radius"

	UnitKm    = "km"
	UnitMiles = "mi"

	ChargeFree        = "free"
	ChargeFixed       = "fixed"
	ChargeByArea      = "by_area"
	ChargeOrderAmount = "order_amount"
)

const kmPerMile = 1.609344

// DeliveryPricing is a store's delivery region and charges, stored as JSON on
// the branch. The ZIP list itself stays in Branch.DeliveryZone.
type DeliveryPricing struct {
	Region      string  `json:"region"`      // zip | radius
	RadiusValue float64 `json:"radiusValue"` // radius region: the furthest the store delivers
	RadiusUnit  string  `json:"radiusUnit"`  // km | mi; also the unit of DistanceBands

	ChargeType    string `json:"chargeType"` // free | fixed | by_area | order_amount
	FixedFeeCents int64  `json:"fixedFeeCents"`

	ZipFees       []ZipFee       `json:"zipFees"`       // by_area, zip region
	DistanceBands []DistanceBand `json:"distanceBands"` // by_area, radius region

	FreeOverCents       int64 `json:"freeOverCents"`       // order_amount: free at or above this
	OtherOrdersFeeCents int64 `json:"otherOrdersFeeCents"` // order_amount: below it

	TimeBased bool      `json:"timeBased"`
	TimeSlots []TimeFee `json:"timeSlots"`
}

// ZipFee is the charge for one mapped ZIP. FreeOverCents nil: never free.
type ZipFee struct {
	Zip           string `json:"zip"`
	FeeCents      int64  `json:"feeCents"`
	FreeOverCents *int64 `json:"freeOverCents"`
}

// DistanceBand charges FeeCents from Min up to and including Max, in the
// pricing's RadiusUnit.
type DistanceBand struct {
	Min           float64 `json:"min"`
	Max           float64 `json:"max"`
	FeeCents      int64   `json:"feeCents"`
	FreeOverCents *int64  `json:"freeOverCents"`
}

// TimeFee charges FeeCents for slots starting at or after From and before To
// ("HH:MM", store-local).
type TimeFee struct {
	From     string `json:"from"`
	To       string `json:"to"`
	FeeCents int64  `json:"feeCents"`
}

// DeliveryQuoteInput is what is known about a delivery when it is priced.
type DeliveryQuoteInput struct {
	Postcode string
	// DistanceKm from the store to the address; nil when either end could not
	// be located. Required for a radius region.
	DistanceKm    *float64
	SubtotalCents int64
	// SlotStart "HH:MM" or "HH:MM:SS", store-local; empty before a slot is chosen.
	SlotStart string
}

// DeliveryQuote is the answer. When Deliverable is false, Reason says why in
// words a customer can read and FeeCents is 0.
type DeliveryQuote struct {
	Deliverable bool     `json:"deliverable"`
	FeeCents    int64    `json:"feeCents"`
	Reason      string   `json:"reason,omitempty"`
	Rule        string   `json:"rule"` // which charge applied: free | fixed | by_area | order_amount | time_slot
	DistanceKm  *float64 `json:"distanceKm,omitempty"`
}

// Pricing returns the store's delivery pricing, or the equivalent of the old
// flat setup (its ZIP list plus DeliveryFeeCents on every order) for a store
// saved before pricing existed.
func (b *Branch) Pricing() DeliveryPricing {
	if b.DeliveryPricing.Region != "" {
		return b.DeliveryPricing
	}
	return DeliveryPricing{Region: RegionZip, ChargeType: ChargeFixed, FixedFeeCents: b.DeliveryFeeCents}
}

// NeedsDistance reports whether quoting this store needs the customer's
// location rather than only their ZIP.
func (b *Branch) NeedsDistance() bool { return b.Pricing().Region == RegionRadius }

// QuoteDelivery prices a delivery from this store.
func (b *Branch) QuoteDelivery(in DeliveryQuoteInput) DeliveryQuote {
	p := b.Pricing()
	q := DeliveryQuote{DistanceKm: in.DistanceKm}

	// 1. The region.
	var distInUnit float64
	switch p.Region {
	case RegionRadius:
		if in.DistanceKm == nil {
			q.Reason = "We couldn't find that address on the map. Check the street and " + b.postcodeLabel() + "."
			return q
		}
		distInUnit = FromKm(*in.DistanceKm, p.RadiusUnit)
		if p.RadiusValue <= 0 || distInUnit > p.RadiusValue+1e-9 {
			q.Reason = fmt.Sprintf("This address is %s away. %s delivers up to %s.",
				formatDistance(distInUnit, p.RadiusUnit), b.Name, formatDistance(p.RadiusValue, p.RadiusUnit))
			return q
		}
	default:
		postcode := NormalizePostcode(in.Postcode)
		if postcode == "" {
			q.Reason = "Enter a " + b.postcodeLabel() + " to check delivery."
			return q
		}
		if !postcodeInList(b.DeliveryZone.Zips, postcode) {
			q.Reason = fmt.Sprintf("%s doesn't deliver to %s.", b.Name, postcode)
			return q
		}
	}

	// 2. The charge. freeOver records an order big enough for its free-over
	// amount: that stays free whatever the delivery time.
	q.Deliverable, q.Rule = true, p.ChargeType
	freeOver := false
	switch p.ChargeType {
	case ChargeFree:
		return q // time slots don't apply to free delivery
	case ChargeFixed:
		q.FeeCents = p.FixedFeeCents
	case ChargeOrderAmount:
		if p.FreeOverCents > 0 && in.SubtotalCents >= p.FreeOverCents {
			q.FeeCents, freeOver = 0, true
		} else {
			q.FeeCents = p.OtherOrdersFeeCents
		}
	case ChargeByArea:
		if p.Region == RegionRadius {
			// A slot's fee applies inside its min–max. An address within the
			// radius but outside every slot (nearer than the first, between
			// two, past the last) is still delivered to, free.
			if band, ok := bandFor(p.DistanceBands, distInUnit); ok {
				q.FeeCents, freeOver = applyFreeOver(band.FeeCents, band.FreeOverCents, in.SubtotalCents)
			}
		} else {
			if zf, ok := feeForPostcode(p.ZipFees, in.Postcode); ok {
				q.FeeCents, freeOver = applyFreeOver(zf.FeeCents, zf.FreeOverCents, in.SubtotalCents)
			}
		}
	default:
		q.Rule = ChargeFixed
		q.FeeCents = p.FixedFeeCents
	}

	// Charge by delivery time replaces the fee when the slot matches, unless
	// the order already earned free delivery.
	if p.TimeBased && in.SlotStart != "" && !freeOver {
		if fee, ok := timeSlotFee(p.TimeSlots, in.SlotStart); ok {
			q.FeeCents, q.Rule = fee, "time_slot"
		}
	}
	return q
}

// Validate checks a pricing an admin is saving and returns the first problem.
func (p DeliveryPricing) Validate(zips []string) error {
	switch p.Region {
	case RegionZip:
	case RegionRadius:
		if p.RadiusValue <= 0 {
			return fmt.Errorf("Enter the delivery radius.")
		}
		if p.RadiusUnit != UnitKm && p.RadiusUnit != UnitMiles {
			return fmt.Errorf("Delivery radius unit must be km or mi.")
		}
	default:
		return fmt.Errorf("Delivery region must be zip or radius.")
	}
	switch p.ChargeType {
	case ChargeFree, ChargeFixed, ChargeOrderAmount:
	case ChargeByArea:
		if p.Region == RegionRadius {
			if len(p.DistanceBands) == 0 {
				return fmt.Errorf("Add at least one distance slot.")
			}
			for i, b := range p.DistanceBands {
				if b.Min < 0 || b.Max <= b.Min {
					return fmt.Errorf("Distance slot %d: the maximum must be more than the minimum.", i+1)
				}
				if b.Max > p.RadiusValue+1e-9 {
					return fmt.Errorf("Distance slot %d goes beyond the delivery radius.", i+1)
				}
			}
		}
	default:
		return fmt.Errorf("Choose how delivery is charged.")
	}
	neg := p.FixedFeeCents < 0 || p.FreeOverCents < 0 || p.OtherOrdersFeeCents < 0
	for _, z := range p.ZipFees {
		neg = neg || z.FeeCents < 0 || (z.FreeOverCents != nil && *z.FreeOverCents < 0)
	}
	for _, b := range p.DistanceBands {
		neg = neg || b.FeeCents < 0 || (b.FreeOverCents != nil && *b.FreeOverCents < 0)
	}
	for _, t := range p.TimeSlots {
		neg = neg || t.FeeCents < 0
	}
	if neg {
		return fmt.Errorf("Delivery charges can't be negative.")
	}
	if p.TimeBased {
		for i, t := range p.TimeSlots {
			from, err1 := parseClock(t.From)
			to, err2 := parseClock(t.To)
			if err1 != nil || err2 != nil || to <= from {
				return fmt.Errorf("Time slot %d: enter a start and an end time after it.", i+1)
			}
		}
	}
	return nil
}

// FromKm converts kilometres to the given unit.
func FromKm(km float64, unit string) float64 {
	if unit == UnitMiles {
		return km / kmPerMile
	}
	return km
}

// MilesToKm converts miles to kilometres.
func MilesToKm(mi float64) float64 { return mi * kmPerMile }

// postcodeLabel is what this store's country calls a postcode.
func (b *Branch) postcodeLabel() string {
	if p, ok := country.Lookup(b.Country); ok {
		return strings.ToLower(p.PostcodeLabel)
	}
	return "postcode"
}

// bandFor picks the distance slot whose min–max holds the distance (the
// nearer one when two share a boundary). None: the address is outside every
// slot.
func bandFor(bands []DistanceBand, d float64) (DistanceBand, bool) {
	sorted := append([]DistanceBand(nil), bands...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Min < sorted[j].Min })
	for _, b := range sorted {
		if d >= b.Min-1e-9 && d <= b.Max+1e-9 {
			return b, true
		}
	}
	return DistanceBand{}, false
}

// applyFreeOver returns the fee, and whether the free-over amount waived it.
func applyFreeOver(fee int64, freeOver *int64, subtotal int64) (int64, bool) {
	if freeOver != nil && *freeOver > 0 && subtotal >= *freeOver {
		return 0, true
	}
	return fee, false
}

// timeSlotFee finds the time slot a delivery time falls in. Both ends count:
// 11:00 is inside 10:00–11:00. Where slots meet (10:00–11:00, 11:00–12:00)
// the one starting at that time wins over the one ending at it.
func timeSlotFee(slots []TimeFee, slotStart string) (int64, bool) {
	start, err := parseClock(slotStart)
	if err != nil {
		return 0, false
	}
	endMatch, endFound := int64(0), false
	for _, t := range slots {
		from, err1 := parseClock(t.From)
		to, err2 := parseClock(t.To)
		if err1 != nil || err2 != nil {
			continue
		}
		if start >= from && start < to {
			return t.FeeCents, true
		}
		if start == to && !endFound {
			endMatch, endFound = t.FeeCents, true
		}
	}
	return endMatch, endFound
}

// parseClock returns minutes since midnight for "HH:MM" or "HH:MM:SS".
func parseClock(s string) (int, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"15:04:05", "15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Hour()*60 + t.Minute(), nil
		}
	}
	return 0, fmt.Errorf("bad time %q", s)
}

func formatDistance(v float64, unit string) string {
	label := "km"
	if unit == UnitMiles {
		label = "miles"
	}
	return fmt.Sprintf("%s %s", trimFloat(math.Round(v*10)/10), label)
}

func trimFloat(v float64) string {
	s := fmt.Sprintf("%.1f", v)
	return strings.TrimSuffix(s, ".0")
}
