package catalogapi

import "testing"

func TestProductFilterNormalise(t *testing.T) {
	tests := []struct {
		name      string
		in        ProductFilter
		wantSort  string
		wantPage  int
		wantLimit int
	}{
		{"zero value defaults to most gifted, page 1", ProductFilter{}, SortMostGifted, 1, DefaultProductPageSize},
		{"page below 1 is clamped up", ProductFilter{Page: 0}, SortMostGifted, 1, DefaultProductPageSize},
		{"negative page is clamped up", ProductFilter{Page: -3}, SortMostGifted, 1, DefaultProductPageSize},
		{"limit above the cap is clamped down", ProductFilter{Limit: 5000}, SortMostGifted, 1, MaxProductPageSize},
		{"explicit sort is kept", ProductFilter{Sort: SortPriceAsc}, SortPriceAsc, 1, DefaultProductPageSize},
		{"explicit paging is kept", ProductFilter{Page: 3, Limit: 12}, SortMostGifted, 3, 12},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.Normalise()
			if got.Sort != tt.wantSort {
				t.Errorf("Sort = %q, want %q", got.Sort, tt.wantSort)
			}
			if got.Page != tt.wantPage {
				t.Errorf("Page = %d, want %d", got.Page, tt.wantPage)
			}
			if got.Limit != tt.wantLimit {
				t.Errorf("Limit = %d, want %d", got.Limit, tt.wantLimit)
			}
		})
	}
}

// Offset drives SQL paging, so an off-by-one here silently skips or repeats a
// row between pages of the grid.
func TestProductFilterOffset(t *testing.T) {
	tests := []struct {
		page, limit, want int
	}{
		{1, 50, 0},
		{2, 50, 50},
		{3, 12, 24},
	}
	for _, tt := range tests {
		f := ProductFilter{Page: tt.page, Limit: tt.limit}.Normalise()
		if got := f.Offset(); got != tt.want {
			t.Errorf("page %d limit %d: Offset() = %d, want %d", tt.page, tt.limit, got, tt.want)
		}
	}
}
