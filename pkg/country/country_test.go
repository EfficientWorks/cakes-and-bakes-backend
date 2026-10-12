package country

import "testing"

func TestLookup(t *testing.T) {
	gb, ok := Lookup(" gb ")
	if !ok || gb.Currency != "GBP" || gb.TaxMode != TaxInclusive || gb.Timezone != "Europe/London" {
		t.Fatalf("GB profile = %+v, ok=%v", gb, ok)
	}
	if _, ok := Lookup("FR"); ok {
		t.Fatal("FR is not a supported country yet")
	}
}

func TestValidPostcode(t *testing.T) {
	gb, _ := Lookup("GB")
	for _, v := range []string{"SW1A 1AA", "sw1a1aa", "NW1 6XE", "M1 1AE", "B33 8TH", "CR2 6XH", "DN55 1PT"} {
		if !gb.ValidPostcode(v) {
			t.Errorf("GB should accept %q", v)
		}
	}
	for _, v := range []string{"08817", "SW1A", "12345 6"} {
		if gb.ValidPostcode(v) {
			t.Errorf("GB should reject %q", v)
		}
	}
	us, _ := Lookup("US")
	if !us.ValidPostcode("08817") || !us.ValidPostcode("08817-1234") || us.ValidPostcode("NW1 6XE") {
		t.Error("US ZIP rules are wrong")
	}
	in, _ := Lookup("IN")
	if !in.ValidPostcode("560001") || in.ValidPostcode("060001") {
		t.Error("IN PIN rules are wrong")
	}
}

func TestAllIsSorted(t *testing.T) {
	all := All()
	if len(all) != 3 || all[0].Code != "GB" || all[1].Code != "IN" || all[2].Code != "US" {
		t.Fatalf("All() = %v", all)
	}
}
