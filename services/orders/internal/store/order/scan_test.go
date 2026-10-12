package order

import (
	"strings"
	"testing"

	"bakehouse/services/orders/ordersapi"
)

// countSelectedColumns counts the comma-separated expressions between SELECT
// and FROM, ignoring commas inside parentheses so a correlated subquery counts
// as the single column it is.
func countSelectedColumns(t *testing.T, query string) int {
	t.Helper()

	upper := strings.ToUpper(query)
	start := strings.Index(upper, "SELECT")
	end := strings.LastIndex(upper, "FROM")
	if start < 0 || end < 0 || end < start {
		t.Fatalf("could not find SELECT ... FROM in:\n%s", query)
	}
	list := query[start+len("SELECT") : end]

	depth, columns := 0, 1
	for _, r := range list {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				columns++
			}
		}
	}
	return columns
}

// The list query is spliced together from the detail one, so the column list
// cannot drift — but the scans are hand-written, and adding a column to
// SelectSQL without adding a target silently broke every admin order list
// with an INTERNAL_ERROR that named nothing. These two tests turn that into a
// failure here instead.
func TestOrderSelectMatchesItsScanTargets(t *testing.T) {
	var o ordersapi.Order
	want := countSelectedColumns(t, SelectSQL)
	got := len(ScanTargets(&o))

	if got != want {
		t.Errorf("SelectSQL returns %d columns but ScanTargets has %d destinations — "+
			"a column was added to one and not the other", want, got)
	}
}

func TestOrderListSelectIsTheDetailSelectPlusOne(t *testing.T) {
	detail := countSelectedColumns(t, SelectSQL)
	list := countSelectedColumns(t, orderListSelectWithCount)

	if list != detail+1 {
		t.Errorf("orderListSelectWithCount has %d columns, want %d (SelectSQL plus item_count)",
			list, detail+1)
	}
}

// The scan is only correct if the discount column sits where the query puts
// it, which a column count alone cannot prove. This pins the pair that broke.
func TestOrderSelectCarriesTheDiscountCode(t *testing.T) {
	if !strings.Contains(SelectSQL, "discount_code") {
		t.Fatal("SelectSQL does not select discount_code")
	}
	if !strings.Contains(orderListSelectWithCount, "discount_code") {
		t.Error("the list query lost discount_code when it was derived")
	}
}
