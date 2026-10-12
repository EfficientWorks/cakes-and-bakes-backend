package api

import (
	"net/url"
	"testing"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/testutil"
	"bakehouse/services/catalog/catalogapi"
)

func TestParseProductFilterMultiValue(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
		field string
	}{
		{"repeated parameters", "occasion=birthday&occasion=anniversary", []string{"birthday", "anniversary"}, "occasion"},
		{"comma separated", "occasion=birthday,anniversary", []string{"birthday", "anniversary"}, "occasion"},
		{"mixed, with padding", "occasion=birthday, anniversary&occasion=sympathy", []string{"birthday", "anniversary", "sympathy"}, "occasion"},
		{"blank entries dropped", "occasion=,,birthday,", []string{"birthday"}, "occasion"},
		{"dietary uses the same rules", "dietary=vegan,gluten_free", []string{"vegan", "gluten_free"}, "dietary"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := url.ParseQuery(tt.query)
			testutil.RequireNoError(t, err)

			f, err := parseProductFilter(q)
			testutil.RequireNoError(t, err)

			got := f.Occasions
			if tt.field == "dietary" {
				got = f.Dietary
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("index %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseProductFilterRejectsBadInput(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"unknown sort", "sort=cheapest_first"},
		{"non-numeric price", "minPriceCents=abc"},
		{"negative price", "minPriceCents=-100"},
		{"inverted price range", "minPriceCents=9000&maxPriceCents=1000"},
		{"page zero", "page=0"},
		{"non-numeric limit", "limit=lots"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := url.ParseQuery(tt.query)
			testutil.RequireNoError(t, err)

			if _, err := parseProductFilter(q); err == nil {
				t.Fatal("expected a validation error, got nil")
			} else {
				testutil.RequireCategory(t, err, apperr.CategoryValidation)
			}
		})
	}
}

func TestParseProductFilterAcceptsEmptyQuery(t *testing.T) {
	f, err := parseProductFilter(url.Values{})
	testutil.RequireNoError(t, err)

	// An empty query must not become a filter that matches nothing.
	testutil.RequireEqual(t, len(f.Occasions), 0)
	testutil.RequireEqual(t, len(f.Dietary), 0)
	testutil.RequireEqual(t, f.BestSellerOnly, false)
	if f.MinPriceCents != nil || f.MaxPriceCents != nil {
		t.Error("price bounds should be nil when unset")
	}

	n := f.Normalise()
	testutil.RequireEqual(t, n.Sort, catalogapi.SortMostGifted)
}
func TestParsePaging(t *testing.T) {
	cases := []struct {
		name        string
		query       string
		page, limit int
		wantErr     bool
	}{
		{"defaults", "", 0, 0, false},
		{"page and limit", "page=3&limit=20", 3, 20, false},
		{"per_page alias", "page=2&per_page=25", 2, 25, false},
		{"offset to page", "offset=40&limit=20", 3, 20, false},
		{"offset with default size", "offset=100", 3, 0, false},
		{"page beats offset", "page=1&offset=40&limit=20", 1, 20, false},
		{"offset not a multiple", "offset=15&limit=20", 0, 0, true},
		{"negative offset", "offset=-1", 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := url.ParseQuery(tc.query)
			page, limit, err := parsePaging(q)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && (page != tc.page || limit != tc.limit) {
				t.Fatalf("got page=%d limit=%d, want %d/%d", page, limit, tc.page, tc.limit)
			}
		})
	}
}
