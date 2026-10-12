package franchiseapi

import (
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func zipStore(p DeliveryPricing) *Branch {
	p.Region = RegionZip
	return &Branch{Name: "Edison", DeliveryZone: DeliveryZone{Zips: []string{"08837", "08820"}}, DeliveryPricing: p}
}

func radiusStore(p DeliveryPricing) *Branch {
	p.Region, p.RadiusValue = RegionRadius, 10
	if p.RadiusUnit == "" {
		p.RadiusUnit = UnitKm
	}
	return &Branch{Name: "Edison", DeliveryPricing: p}
}

func TestZipRegionChecksTheZipFirst(t *testing.T) {
	b := zipStore(DeliveryPricing{ChargeType: ChargeFree})
	if q := b.QuoteDelivery(DeliveryQuoteInput{Postcode: "08837-4401"}); !q.Deliverable || q.FeeCents != 0 {
		t.Fatalf("mapped zip: %+v", q)
	}
	q := b.QuoteDelivery(DeliveryQuoteInput{Postcode: "10001"})
	if q.Deliverable || !strings.Contains(q.Reason, "10001") {
		t.Fatalf("unmapped zip: %+v", q)
	}
}

func TestZipCharges(t *testing.T) {
	fixed := zipStore(DeliveryPricing{ChargeType: ChargeFixed, FixedFeeCents: 3000})
	if q := fixed.QuoteDelivery(DeliveryQuoteInput{Postcode: "08820"}); q.FeeCents != 3000 {
		t.Fatalf("fixed: %+v", q)
	}

	byZip := zipStore(DeliveryPricing{ChargeType: ChargeByArea, ZipFees: []ZipFee{
		{Zip: "08837", FeeCents: 500},
		{Zip: "08820", FeeCents: 900, FreeOverCents: ptr(int64(5000))},
	}})
	cases := []struct {
		zip      string
		subtotal int64
		want     int64
	}{{"08837", 9000, 500}, {"08820", 4999, 900}, {"08820", 5000, 0}}
	for _, c := range cases {
		if q := byZip.QuoteDelivery(DeliveryQuoteInput{Postcode: c.zip, SubtotalCents: c.subtotal}); q.FeeCents != c.want {
			t.Errorf("by zip %s/%d: got %d want %d", c.zip, c.subtotal, q.FeeCents, c.want)
		}
	}

	amount := zipStore(DeliveryPricing{ChargeType: ChargeOrderAmount, FreeOverCents: 5000, OtherOrdersFeeCents: 1000})
	if q := amount.QuoteDelivery(DeliveryQuoteInput{Postcode: "08837", SubtotalCents: 6000}); q.FeeCents != 0 {
		t.Fatalf("big order should be free: %+v", q)
	}
	if q := amount.QuoteDelivery(DeliveryQuoteInput{Postcode: "08837", SubtotalCents: 4000}); q.FeeCents != 1000 {
		t.Fatalf("small order pays: %+v", q)
	}
}

func TestTimeSlotReplacesTheFee(t *testing.T) {
	b := zipStore(DeliveryPricing{ChargeType: ChargeOrderAmount, FreeOverCents: 5000, OtherOrdersFeeCents: 1000,
		TimeBased: true, TimeSlots: []TimeFee{{From: "09:00", To: "10:00", FeeCents: 2500}}})
	// Inside the slot: the slot fee.
	if q := b.QuoteDelivery(DeliveryQuoteInput{Postcode: "08837", SubtotalCents: 4000, SlotStart: "09:00:00"}); q.FeeCents != 2500 || q.Rule != "time_slot" {
		t.Fatalf("in slot: %+v", q)
	}
	// Inside the slot but big enough to be free: stays free.
	if q := b.QuoteDelivery(DeliveryQuoteInput{Postcode: "08837", SubtotalCents: 9000, SlotStart: "09:00:00"}); q.FeeCents != 0 || q.Rule != ChargeOrderAmount {
		t.Fatalf("free over in slot: %+v", q)
	}
	// 10:00 is the end, which still counts.
	if q := b.QuoteDelivery(DeliveryQuoteInput{Postcode: "08837", SubtotalCents: 4000, SlotStart: "10:00:00"}); q.FeeCents != 2500 || q.Rule != "time_slot" {
		t.Fatalf("at slot end: %+v", q)
	}
	// Past the end: the regular charge.
	if q := b.QuoteDelivery(DeliveryQuoteInput{Postcode: "08837", SubtotalCents: 9000, SlotStart: "10:01"}); q.FeeCents != 0 {
		t.Fatalf("after slot: %+v", q)
	}
	// Where slots meet, the one starting then wins.
	two := zipStore(DeliveryPricing{ChargeType: ChargeFixed, FixedFeeCents: 1000, TimeBased: true,
		TimeSlots: []TimeFee{{From: "10:00", To: "11:00", FeeCents: 2000}, {From: "11:00", To: "12:00", FeeCents: 3000}}})
	if q := two.QuoteDelivery(DeliveryQuoteInput{Postcode: "08837", SlotStart: "11:00:00"}); q.FeeCents != 3000 {
		t.Fatalf("meeting slots: %+v", q)
	}
	if q := two.QuoteDelivery(DeliveryQuoteInput{Postcode: "08837", SlotStart: "12:00"}); q.FeeCents != 3000 {
		t.Fatalf("last slot end: %+v", q)
	}
	// Still outside the area: no delivery, time slot or not.
	if q := b.QuoteDelivery(DeliveryQuoteInput{Postcode: "99999", SlotStart: "09:30"}); q.Deliverable {
		t.Fatalf("out of area: %+v", q)
	}
	// Free over by ZIP and by distance band also beat the time slot; below it the slot applies.
	slots := []TimeFee{{From: "09:00", To: "10:00", FeeCents: 2500}}
	byZip := zipStore(DeliveryPricing{ChargeType: ChargeByArea, TimeBased: true, TimeSlots: slots,
		ZipFees: []ZipFee{{Zip: "08837", FeeCents: 900, FreeOverCents: ptr(int64(5000))}}})
	if q := byZip.QuoteDelivery(DeliveryQuoteInput{Postcode: "08837", SubtotalCents: 5000, SlotStart: "09:30"}); q.FeeCents != 0 {
		t.Fatalf("zip free over in slot: %+v", q)
	}
	if q := byZip.QuoteDelivery(DeliveryQuoteInput{Postcode: "08837", SubtotalCents: 4999, SlotStart: "09:30"}); q.FeeCents != 2500 {
		t.Fatalf("zip below free over in slot: %+v", q)
	}
	byBand := radiusStore(DeliveryPricing{ChargeType: ChargeByArea, TimeBased: true, TimeSlots: slots,
		DistanceBands: []DistanceBand{{Min: 0, Max: 10, FeeCents: 700, FreeOverCents: ptr(int64(5000))}}})
	if q := byBand.QuoteDelivery(DeliveryQuoteInput{DistanceKm: ptr(4.0), SubtotalCents: 6000, SlotStart: "09:30"}); q.FeeCents != 0 {
		t.Fatalf("band free over in slot: %+v", q)
	}
	if q := byBand.QuoteDelivery(DeliveryQuoteInput{DistanceKm: ptr(4.0), SubtotalCents: 1000, SlotStart: "09:30"}); q.FeeCents != 2500 {
		t.Fatalf("band below free over in slot: %+v", q)
	}
	// Free delivery ignores time slots.
	free := zipStore(DeliveryPricing{ChargeType: ChargeFree, TimeBased: true, TimeSlots: []TimeFee{{From: "09:00", To: "10:00", FeeCents: 2500}}})
	if q := free.QuoteDelivery(DeliveryQuoteInput{Postcode: "08837", SlotStart: "09:30"}); q.FeeCents != 0 {
		t.Fatalf("free with slot: %+v", q)
	}
}

// Slots with gaps, as admin enters them (1–2 km, 3–5 km): a slot's fee applies
// inside its min–max; inside the radius but outside every slot is free.
func TestDistanceBandsWithGaps(t *testing.T) {
	b := &Branch{Name: "Coffee Corner", DeliveryPricing: DeliveryPricing{
		Region: RegionRadius, RadiusValue: 5, RadiusUnit: UnitKm, ChargeType: ChargeByArea,
		DistanceBands: []DistanceBand{{Min: 3, Max: 5, FeeCents: 2000}, {Min: 1, Max: 2, FeeCents: 1000}},
	}}
	for _, c := range []struct {
		km   float64
		fee  int64
		okay bool
	}{{0.4, 0, true}, {1.5, 1000, true}, {2, 1000, true}, {2.5, 0, true}, {4, 2000, true}, {5, 2000, true}, {5.2, 0, false}} {
		q := b.QuoteDelivery(DeliveryQuoteInput{DistanceKm: ptr(c.km)})
		if q.Deliverable != c.okay || q.FeeCents != c.fee {
			t.Fatalf("%.1f km: got deliverable=%v fee=%d, want %v %d (%s)", c.km, q.Deliverable, q.FeeCents, c.okay, c.fee, q.Reason)
		}
	}
	// Miles store: the address distance is converted before the slots are read.
	b.DeliveryPricing.RadiusUnit = UnitMiles
	if q := b.QuoteDelivery(DeliveryQuoteInput{DistanceKm: ptr(6.4)}); q.FeeCents != 2000 { // 6.4 km = 3.98 mi
		t.Fatalf("miles: %+v", q)
	}
}

func TestRadiusRegionAndBands(t *testing.T) {
	b := radiusStore(DeliveryPricing{ChargeType: ChargeByArea, DistanceBands: []DistanceBand{
		{Min: 0, Max: 5, FeeCents: 500},
		{Min: 5, Max: 8, FeeCents: 900, FreeOverCents: ptr(int64(5000))},
	}})
	if q := b.QuoteDelivery(DeliveryQuoteInput{DistanceKm: ptr(3.2)}); q.FeeCents != 500 {
		t.Fatalf("band 1: %+v", q)
	}
	if q := b.QuoteDelivery(DeliveryQuoteInput{DistanceKm: ptr(6.0), SubtotalCents: 5000}); q.FeeCents != 0 || !q.Deliverable {
		t.Fatalf("band 2 free over: %+v", q)
	}
	// Inside the 10 km radius but beyond every band: delivered, free.
	if q := b.QuoteDelivery(DeliveryQuoteInput{DistanceKm: ptr(9.0)}); !q.Deliverable || q.FeeCents != 0 {
		t.Fatalf("no band: %+v", q)
	}
	// Beyond the radius.
	if q := b.QuoteDelivery(DeliveryQuoteInput{DistanceKm: ptr(10.5)}); q.Deliverable || !strings.Contains(q.Reason, "10 km") {
		t.Fatalf("beyond radius: %+v", q)
	}
	// Address not located.
	if q := b.QuoteDelivery(DeliveryQuoteInput{}); q.Deliverable {
		t.Fatalf("no distance: %+v", q)
	}
}

func TestRadiusInMiles(t *testing.T) {
	b := radiusStore(DeliveryPricing{RadiusUnit: UnitMiles, ChargeType: ChargeFixed, FixedFeeCents: 700})
	// 15 km is 9.3 miles: inside 10 miles.
	if q := b.QuoteDelivery(DeliveryQuoteInput{DistanceKm: ptr(15.0)}); !q.Deliverable || q.FeeCents != 700 {
		t.Fatalf("15 km in a 10 mile radius: %+v", q)
	}
	if q := b.QuoteDelivery(DeliveryQuoteInput{DistanceKm: ptr(17.0)}); q.Deliverable {
		t.Fatalf("17 km is 10.6 miles: %+v", q)
	}
}

func TestLegacyStoreKeepsFlatFee(t *testing.T) {
	b := &Branch{DeliveryZone: DeliveryZone{Zips: []string{"08837"}}, DeliveryFeeCents: 599}
	if q := b.QuoteDelivery(DeliveryQuoteInput{Postcode: "08837"}); !q.Deliverable || q.FeeCents != 599 {
		t.Fatalf("legacy: %+v", q)
	}
}

func TestNormalizeZip(t *testing.T) {
	cases := map[string]string{
		"08837":       "08837",
		" 08837-1234": "08837",
		"120, Chatham County, Georgia, 31408, USA": "31408",
		"": "",
	}
	for in, want := range cases {
		if got := NormalizeZip(in); got != want {
			t.Errorf("NormalizeZip(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidate(t *testing.T) {
	bad := []DeliveryPricing{
		{Region: "planet", ChargeType: ChargeFree},
		{Region: RegionRadius, RadiusUnit: UnitKm, ChargeType: ChargeFree},
		{Region: RegionRadius, RadiusValue: 10, RadiusUnit: UnitKm, ChargeType: ChargeByArea},
		{Region: RegionRadius, RadiusValue: 10, RadiusUnit: UnitKm, ChargeType: ChargeByArea, DistanceBands: []DistanceBand{{Min: 0, Max: 12}}},
		{Region: RegionZip, ChargeType: ChargeFixed, FixedFeeCents: -1},
		{Region: RegionZip, ChargeType: ChargeFixed, TimeBased: true, TimeSlots: []TimeFee{{From: "10:00", To: "09:00"}}},
	}
	for i, p := range bad {
		if p.Validate(nil) == nil {
			t.Errorf("case %d should fail: %+v", i, p)
		}
	}
	good := DeliveryPricing{Region: RegionRadius, RadiusValue: 10, RadiusUnit: UnitMiles, ChargeType: ChargeByArea,
		DistanceBands: []DistanceBand{{Min: 0, Max: 10, FeeCents: 500}}, TimeBased: true,
		TimeSlots: []TimeFee{{From: "09:00", To: "10:00", FeeCents: 100}}}
	if err := good.Validate(nil); err != nil {
		t.Fatal(err)
	}
}
