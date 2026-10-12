package geo

import (
	"context"
	"testing"
)

// recordingGeocoder answers every query and remembers what it was asked.
type recordingGeocoder struct{ asked []string }

func (r *recordingGeocoder) GeocodeAddress(_ context.Context, q string) (float64, float64, bool, error) {
	r.asked = append(r.asked, q)
	return 1, 2, true, nil
}

func TestLocateAddressUsesZipForAreaNames(t *testing.T) {
	cases := []struct {
		street, postcode, want string
	}{
		// The address search's ZIP area: located by the ZIP, not by "Manhattan".
		{"10002, Manhattan, New York County, New York, New York, USA", "10002", "10002, USA"},
		{"", "08817", "08817, USA"},
		// A real street address is still located as a whole.
		{"350 5th Ave, New York, NY 10118", "10118", "350 5th Ave, New York, NY 10118, 10118, USA"},
		{"1/23 Main St", "19426", "1/23 Main St, 19426, USA"},
	}
	for _, c := range cases {
		g := &recordingGeocoder{}
		if _, _, ok := LocateAddress(context.Background(), g, c.street, "", "", c.postcode); !ok {
			t.Fatalf("%q: not located", c.street)
		}
		if len(g.asked) == 0 || g.asked[0] != c.want {
			t.Fatalf("%q: first query %q, want %q", c.street, g.asked, c.want)
		}
	}
}
