package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/tenant"
	"bakehouse/services/business/businessapi"
)

// BusinessHeader names the business a storefront request is for, by slug.
const BusinessHeader = "X-Business"

// slugLookup finds a business by slug (the store).
type slugLookup func(ctx context.Context, slug string) (*businessapi.Business, error)

/*
 * ResolveBusiness puts the business a public request is for on its context:
 * the X-Business header (a slug) when the caller sends one, otherwise
 * defaultSlug (STOREFRONT_BUSINESS) — so a website deployed for one business
 * needs no code change. With neither, the request has no business and sees
 * only data from before businesses existed.
 *
 * Staff routes replace this with the business on the staff token
 * (tenant.BusinessFromStaff), so a header can never widen what staff see.
 *
 * Slugs are cached for a minute: every storefront request passes through
 * here, and a business's slug and status change rarely.
 */
func ResolveBusiness(lookup slugLookup, defaultSlug string) func(http.Handler) http.Handler {
	cache := &slugCache{lookup: lookup, entries: map[string]slugEntry{}}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			slug := strings.TrimSpace(r.Header.Get(BusinessHeader))
			if slug == "" {
				slug = defaultSlug
			}
			if slug == "" {
				next.ServeHTTP(w, r)
				return
			}
			b, err := cache.get(r.Context(), slug)
			if err != nil {
				httpx.WriteError(w, r, apperr.NotFound("No business called \""+slug+"\"."))
				return
			}
			if b.Status != businessapi.StatusActive {
				httpx.WriteError(w, r, apperr.New("BUSINESS_SUSPENDED", apperr.CategoryAuthorization,
					"This shop is not available right now."))
				return
			}
			next.ServeHTTP(w, r.WithContext(tenant.WithBusiness(r.Context(), b.ID)))
		})
	}
}

type slugEntry struct {
	business *businessapi.Business
	at       time.Time
}

type slugCache struct {
	lookup  slugLookup
	mu      sync.Mutex
	entries map[string]slugEntry
}

const slugCacheTTL = time.Minute

func (c *slugCache) get(ctx context.Context, slug string) (*businessapi.Business, error) {
	key := strings.ToLower(slug)
	c.mu.Lock()
	e, ok := c.entries[key]
	c.mu.Unlock()
	if ok && time.Since(e.at) < slugCacheTTL {
		return e.business, nil
	}
	b, err := c.lookup(ctx, slug)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.entries[key] = slugEntry{business: b, at: time.Now()}
	c.mu.Unlock()
	return b, nil
}
