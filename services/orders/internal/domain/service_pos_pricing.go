package domain

import (
	"context"
	"math"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/orders/ordersapi"
)

// pricePOSItemsFromCatalog computes base price + modifier price deltas from the
// catalog for new lines (lines without an ID), setting UnitPriceCents and
// LineTotalCents (= UnitPriceCents * Quantity).
func (s *Service) pricePOSItemsFromCatalog(ctx context.Context, branchID string, items []ordersapi.POSItem) ([]ordersapi.POSItem, error) {
	out := make([]ordersapi.POSItem, len(items))
	copy(out, items)

	for i := range out {
		it := &out[i]
		// An edit names the line by its ID and keeps the recorded price;
		// a new line without ID is priced fresh against the catalog.
		if it.ID != "" {
			continue
		}

		// A manual or custom item (like a custom candle or misc counter item) has no catalog product ID.
		// Its pricing and naming are taken directly from the request rather than queried from the catalog.
		if strings.TrimSpace(it.ProductID) == "" {
			if it.LineTotalCents == 0 && it.UnitPriceCents > 0 {
				it.LineTotalCents = it.UnitPriceCents * int64(it.Quantity)
			} else if it.LineTotalCents > 0 && it.UnitPriceCents == 0 && it.Quantity > 0 {
				it.UnitPriceCents = it.LineTotalCents / int64(it.Quantity)
			}
			if it.DiscountPct != nil && *it.DiscountPct > 0 {
				it.DiscountCents = int64(math.Round(float64(it.LineTotalCents) * (*it.DiscountPct) / 100))
			}
			if it.DiscountCents > it.LineTotalCents {
				it.DiscountCents = it.LineTotalCents
			}
			continue
		}

		if s.catalog == nil {
			continue
		}

		var variantID *string
		if strings.TrimSpace(it.VariantID) != "" {
			v := strings.TrimSpace(it.VariantID)
			variantID = &v
		}

		flavourIDs := make([]string, 0, len(it.Flavours))
		for _, f := range it.Flavours {
			if strings.TrimSpace(f.ID) != "" {
				flavourIDs = append(flavourIDs, strings.TrimSpace(f.ID))
			}
		}

		addonIDs := make([]string, 0, len(it.Addons))
		for _, a := range it.Addons {
			if strings.TrimSpace(a.ID) != "" {
				addonIDs = append(addonIDs, strings.TrimSpace(a.ID))
			}
		}

		if it.UnitPriceCents > 0 {
			lineTotal := it.UnitPriceCents * int64(it.Quantity)
			it.LineTotalCents = lineTotal

			// Calculate item-level discount in cents if percentage is given
			if it.DiscountPct != nil && *it.DiscountPct > 0 {
				it.DiscountCents = int64(math.Round(float64(lineTotal) * (*it.DiscountPct) / 100))
			}
			if it.DiscountCents > lineTotal {
				it.DiscountCents = lineTotal
			}

			// Sync resolved product name and modifiers from the catalog if not provided
			if s.catalog != nil && it.ProductID != "" {
				if priced, err := s.catalog.GetPricedItem(ctx, branchID, it.ProductID, variantID, flavourIDs, addonIDs, nil, nil, time.Now()); err == nil {
					if it.ProductName == "" {
						it.ProductName = priced.ProductName
					}
					if priced.VariantName != nil && it.VariantName == "" {
						it.VariantName = *priced.VariantName
					}
					it.Flavours = syncModifierNames(it.Flavours, priced.SelectedFlavours)
					it.Addons = syncModifierNames(it.Addons, priced.SelectedAddons)
				}
			}
			continue
		}

		priced, err := s.catalog.GetPricedItem(ctx, branchID, it.ProductID, variantID, flavourIDs, addonIDs, nil, nil, time.Now())
		if err != nil {
			return nil, err
		}
		if !priced.Available {
			return nil, apperr.New("PRODUCT_UNAVAILABLE", apperr.CategoryBusinessRule, "This product is currently unavailable: "+priced.AvailableReason)
		}

		it.ProductName = priced.ProductName
		if priced.VariantName != nil {
			it.VariantName = *priced.VariantName
		}
		it.UnitPriceCents = priced.UnitPriceCents
		lineTotal := priced.UnitPriceCents * int64(it.Quantity)
		it.LineTotalCents = lineTotal

		// Calculate item-level discount in cents if percentage is given
		if it.DiscountPct != nil && *it.DiscountPct > 0 {
			it.DiscountCents = int64(math.Round(float64(lineTotal) * (*it.DiscountPct) / 100))
		}
		if it.DiscountCents > lineTotal {
			it.DiscountCents = lineTotal
		}

		// Sync resolved modifier names and prices from the catalog onto the line
		it.Flavours = syncModifierNames(it.Flavours, priced.SelectedFlavours)
		it.Addons = syncModifierNames(it.Addons, priced.SelectedAddons)
	}
	return out, nil
}

// syncModifierNames merges catalog-resolved names and price deltas into the
// client-supplied modifier refs. If the client sent only an id (no name),
// this fills the name and priceDelta from the catalog. If the client sent
// nothing at all, it uses the full catalog list.
func syncModifierNames(refs []ordersapi.SelectedModifierRef, catalogMods []catalogapi.SelectedModifier) []ordersapi.SelectedModifierRef {
	if len(refs) == 0 {
		if len(catalogMods) == 0 {
			return refs
		}
		out := make([]ordersapi.SelectedModifierRef, len(catalogMods))
		for i, m := range catalogMods {
			out[i] = ordersapi.SelectedModifierRef{ID: m.ID, Name: m.Name, PriceDelta: m.PriceDelta}
		}
		return out
	}
	// Build a lookup by id from catalog results.
	byID := make(map[string]catalogapi.SelectedModifier, len(catalogMods))
	for _, m := range catalogMods {
		byID[m.ID] = m
	}
	out := make([]ordersapi.SelectedModifierRef, len(refs))
	for i, r := range refs {
		if r.Name == "" || r.PriceDelta == 0 {
			if cm, ok := byID[r.ID]; ok {
				r.Name = cm.Name
				r.PriceDelta = cm.PriceDelta
			}
		}
		out[i] = r
	}
	return out
}
