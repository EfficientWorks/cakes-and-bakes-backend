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

// BusinessQueryParam names it for clients that cannot set a header.
const BusinessQueryParam = "business"

// businessLookup finds a business by one key (a slug, or a website origin):
// (nil, nil) when there is none.
type businessLookup func(ctx context.Context, key string) (*businessapi.Business, error)

/*
 * ResolveBusiness puts the business a public request is for on its context.
 *
 * How a shop is tied to its business, first match wins:
 *
 *  1. the X-Business header (a slug, or ?business= for an EventSource) —
 *     what the website sends on every call,
 *     from its NEXT_PUBLIC_BUSINESS_SLUG; server-side renders have no Origin,
 *     so this is the one that always works;
 *  2. the request's Origin, when it is a business's registered website — a
 *     browser call from https://cakesandbakes.co.uk is Cakes & Bakes;
 *  3. defaultSlug (STOREFRONT_BUSINESS), for a deployment that serves one
 *     business.
 *
 * With none, the request sees only data from before businesses existed. A
 * header naming an unknown business is refused rather than ignored, so a
 * misconfigured shop fails loudly instead of showing someone else's menu.
 *
 * Staff routes replace this with the business on the staff token
 * (tenant.BusinessFromStaff), so a header can never widen what staff see.
 *
 * Every storefront request passes through here, so answers — misses too —
 * are cached for a minute, and the cache is bounded: Origin is set by the
 * caller, and a stream of made-up origins must not grow memory or reach the
 * database on every request.
 */
func ResolveBusiness(bySlug, byWebsite businessLookup, defaultSlug string) func(http.Handler) http.Handler {
	slugs := newBusinessCache(bySlug)
	sites := newBusinessCache(byWebsite)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var b *businessapi.Business
			slug := strings.TrimSpace(r.Header.Get(BusinessHeader))
			if slug == "" {
				// An EventSource cannot send headers; it names the business in
				// the query instead.
				slug = strings.TrimSpace(r.URL.Query().Get(BusinessQueryParam))
			}
			if slug != "" {
				b = slugs.get(r.Context(), slug)
				if b == nil {
					httpx.WriteError(w, r, apperr.NotFound("No business called \""+slug+"\"."))
					return
				}
			} else if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" && origin != "null" {
				b = sites.get(r.Context(), origin)
			}
			if b == nil && defaultSlug != "" {
				b = slugs.get(r.Context(), defaultSlug)
			}
			if b == nil {
				next.ServeHTTP(w, r)
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

type cacheEntry struct {
	business *businessapi.Business // nil: no such business
	at       time.Time
}

type businessCache struct {
	lookup  businessLookup
	mu      sync.Mutex
	entries map[string]cacheEntry
}

const (
	businessCacheTTL = time.Minute
	// businessCacheMax bounds the entries kept; past it the cache starts over.
	businessCacheMax = 2048
	// maxKeyLength refuses absurd keys before they reach the cache or the
	// database.
	maxKeyLength = 255
)

func newBusinessCache(lookup businessLookup) *businessCache {
	return &businessCache{lookup: lookup, entries: map[string]cacheEntry{}}
}

// get returns the business for key, or nil when there is none.
func (c *businessCache) get(ctx context.Context, key string) *businessapi.Business {
	if c.lookup == nil || len(key) > maxKeyLength {
		return nil
	}
	k := strings.ToLower(key)
	c.mu.Lock()
	e, ok := c.entries[k]
	c.mu.Unlock()
	if ok && time.Since(e.at) < businessCacheTTL {
		return e.business
	}
	// A lookup answers (nil, nil) for "no such business"; an error is the
	// database failing, which is not remembered.
	b, err := c.lookup(ctx, key)
	if err != nil {
		return nil
	}
	c.mu.Lock()
	if len(c.entries) >= businessCacheMax {
		c.entries = map[string]cacheEntry{}
	}
	c.entries[k] = cacheEntry{business: b, at: time.Now()}
	c.mu.Unlock()
	return b
}
