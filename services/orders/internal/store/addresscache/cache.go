package addresscache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/orders/ordersapi"
)

type AddressSearchCacheEntry struct {
	Results   []ordersapi.AddressResult
	FetchedAt time.Time
}

func (s *Repo) GetAddressSearchCache(ctx context.Context, queryNorm string) (*AddressSearchCacheEntry, error) {
	var raw []byte
	var fetchedAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT results, fetched_at
		  FROM address_search_cache
		 WHERE query_norm = $1`, queryNorm).Scan(&raw, &fetchedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var results []ordersapi.AddressResult
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, err
	}
	return &AddressSearchCacheEntry{Results: results, FetchedAt: fetchedAt}, nil
}

func (s *Repo) UpsertAddressSearchCache(ctx context.Context, queryNorm string, results []ordersapi.AddressResult) error {
	raw, err := json.Marshal(results)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO address_search_cache (query_norm, results, fetched_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (query_norm) DO UPDATE
		    SET results = EXCLUDED.results, fetched_at = NOW()`,
		queryNorm, raw)
	return err
}
