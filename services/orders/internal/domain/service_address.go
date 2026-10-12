package domain

import (
	"context"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/services/orders/ordersapi"
)

const addressCacheTTL = 30 * 24 * time.Hour

func normaliseAddressQuery(q string) string {
	return strings.ToLower(strings.TrimSpace(q))
}

func (s *Service) SearchAddress(ctx context.Context, rawQuery string) ([]ordersapi.AddressResult, error) {
	q := strings.TrimSpace(rawQuery)
	if len(q) < 3 {
		return nil, apperr.Validation("Type at least 3 characters to search.").
			WithFields(apperr.FieldError{Field: "q", Code: "MIN_LENGTH", Message: "At least 3 characters."})
	}
	norm := normaliseAddressQuery(q)

	cached, err := s.store.GetAddressSearchCache(ctx, norm)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if cached != nil && time.Since(cached.FetchedAt) < addressCacheTTL {
		return cached.Results, nil
	}

	if !s.locationIQ.Configured() {
		if cached != nil {
			return cached.Results, nil
		}
		return []ordersapi.AddressResult{}, nil
	}

	results, err := s.locationIQ.Search(ctx, q)
	if err != nil {
		if cached != nil {
			return cached.Results, nil
		}
		return nil, apperr.Internal(err)
	}

	_ = s.store.UpsertAddressSearchCache(ctx, norm, results)

	return results, nil
}
