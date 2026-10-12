package domain

import (
	"context"

	"bakehouse/services/catalog/catalogapi"
)

// foldSales reduces an order's lines to one increment per product.
//
// An order can carry the same bouquet twice with different flavours — two
// lines, one product — and the ranking should count that once at the combined
// quantity rather than twice at three apiece.
//
// A non-positive quantity is dropped rather than added: it is not a real sale,
// and a negative one would let a malformed line quietly demote a product.
//
// Insertion order is preserved so the statement's parameters are stable, which
// keeps the two returned slices readable in a query log and the function
// testable without sorting.
func foldSales(sales []catalogapi.ProductSale) (ids []string, quantities []int32) {
	totals := make(map[string]int32, len(sales))
	for _, sale := range sales {
		if sale.ProductID == "" || sale.Quantity <= 0 {
			continue
		}
		if _, seen := totals[sale.ProductID]; !seen {
			ids = append(ids, sale.ProductID)
		}
		totals[sale.ProductID] += int32(sale.Quantity)
	}

	quantities = make([]int32, 0, len(ids))
	for _, id := range ids {
		quantities = append(quantities, totals[id])
	}
	return ids, quantities
}

// RecordProductSales implements catalogapi.SalesRecorder.
//
// Called by the orders service when an order is marked fulfilled, and the only
// thing in this system that ever writes `products.popularity_score`. See the
// interface doc for why fulfilment — not placement — is the trigger.
func (s *Service) RecordProductSales(ctx context.Context, sales []catalogapi.ProductSale) error {
	ids, quantities := foldSales(sales)
	if len(ids) == 0 {
		return nil
	}
	return s.store.AddPopularity(ctx, ids, quantities)
}
