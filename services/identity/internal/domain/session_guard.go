package domain

import (
	"context"
	"sync"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
)

/*
 * POS / KDS session guard.
 *
 * A staff access token is a signed JWT valid for hours, and nothing used to
 * look at it again once issued: deactivating someone, or closing their store,
 * left the till logged in and taking orders until the token ran out.
 *
 * CheckStaffSession runs on every /pos and /kds request (see
 * Module.StaffSessionGuard) and on refresh. It asks two questions, each
 * answered from a few-second cache so a busy till isn't a database load:
 *
 *   - is this staff account still active?
 *   - is the store or central kitchen the session signed in to still trading?
 *
 * Either "no" is a 401, which makes the till's refresh fail too, so it lands
 * on the login screen with the reason.
 */

const sessionCacheTTL = 5 * time.Second

type sessionCache struct {
	mu   sync.Mutex
	vals map[string]sessionCacheEntry
}

type sessionCacheEntry struct {
	ok bool
	at time.Time
}

func (c *sessionCache) get(key string) (bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, hit := c.vals[key]
	if !hit || time.Since(e.at) > sessionCacheTTL {
		return false, false
	}
	return e.ok, true
}

func (c *sessionCache) put(key string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.vals == nil || len(c.vals) > 10000 {
		c.vals = map[string]sessionCacheEntry{}
	}
	c.vals[key] = sessionCacheEntry{ok: ok, at: time.Now()}
}

func errSessionDeactivated() error {
	return apperr.New("ACCOUNT_DEACTIVATED", apperr.CategoryAuthentication,
		"This staff account has been deactivated. Contact your administrator.")
}

func errLocationClosed() error {
	return apperr.New("OUTLET_INACTIVE", apperr.CategoryAuthentication,
		"This store location is no longer active. Contact your administrator.")
}

// cached answers a yes/no question through the session cache. Only a yes is
// remembered: a "no" is asked again every time, so a store reopened or a
// person reactivated works on the very next request, and a till that has been
// logged out makes few requests anyway. A failed lookup counts as yes: a
// database hiccup must not log every till out.
func (s *Service) cached(key string, ask func() (bool, error)) bool {
	if ok, hit := s.sessions.get(key); hit {
		return ok
	}
	ok, err := ask()
	if err != nil {
		return true
	}
	if ok {
		s.sessions.put(key, true)
	}
	return ok
}

func (s *Service) branchTrading(ctx context.Context, branchID string) bool {
	if s.scopes == nil || branchID == "" {
		return true
	}
	return s.cached("b:"+branchID, func() (bool, error) { return s.scopes.ScopeActive(ctx, branchID) })
}

func (s *Service) kitchenTrading(ctx context.Context, kitchenID string) bool {
	if s.locations == nil || kitchenID == "" {
		return true
	}
	return s.cached("k:"+kitchenID, func() (bool, error) { return s.locations.CentralKitchenActive(ctx, kitchenID) })
}

// assertLocationTrading refuses a session whose store or kitchen has been
// suspended, closed, or otherwise taken out of trading.
func (s *Service) assertLocationTrading(ctx context.Context, loc auth.StaffLocation) error {
	if !s.branchTrading(ctx, loc.BranchID) || !s.kitchenTrading(ctx, loc.KitchenID) {
		return errLocationClosed()
	}
	return nil
}

// CheckStaffSession is the per-request POS / KDS check.
func (s *Service) CheckStaffSession(ctx context.Context, claims *auth.StaffClaims) error {
	if claims == nil {
		return apperr.Unauthenticated("Session expired or invalid. Please log in again.")
	}
	active := s.cached("s:"+claims.StaffID, func() (bool, error) {
		u, err := s.store.GetStaffByID(ctx, claims.StaffID)
		if err != nil {
			return false, err
		}
		return u.IsActive, nil
	})
	if !active {
		return errSessionDeactivated()
	}

	if claims.LocationBranchID != "" || claims.LocationKitchenID != "" {
		return s.assertLocationTrading(ctx, auth.StaffLocation{
			BranchID: claims.LocationBranchID, KitchenID: claims.LocationKitchenID})
	}

	// A token from before sessions carried their location: judge by the
	// stores the person is assigned to. Brand-wide staff have none and pass.
	assigned, trading := 0, 0
	for _, r := range claims.RoleAssignments {
		switch {
		case r.ScopeType == "branch" && r.BranchID != nil:
			assigned++
			if s.branchTrading(ctx, *r.BranchID) {
				trading++
			}
		case r.ScopeType == "central_kitchen" && r.CentralKitchenID != nil:
			assigned++
			if s.kitchenTrading(ctx, *r.CentralKitchenID) {
				trading++
			}
		}
	}
	if assigned > 0 && trading == 0 {
		return errLocationClosed()
	}
	return nil
}
