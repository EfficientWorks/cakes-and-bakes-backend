// Package identity is the composition root for the identity service:
// customer auth, staff auth, RBAC role assignments and refresh sessions.
//
// The two auth realms are fully isolated — a customer account can never
// become a staff account. Everything under internal/ is private to this
// service; the types it publishes live in identityapi.
package identity

import (
	"context"
	"net/http"

	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/pkg/auth"
	"bakehouse/pkg/email"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/ratelimit"
	"bakehouse/pkg/sealbox"
	"bakehouse/pkg/tenant"
	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/api"
	"bakehouse/services/identity/internal/domain"
	"bakehouse/services/identity/internal/store"
)

// TokenTTLs groups the four access/refresh lifetimes the two realms use.
type TokenTTLs struct {
	CustomerAccess  time.Duration
	CustomerRefresh time.Duration
	StaffAccess     time.Duration
	StaffRefresh    time.Duration
}

// Module is the assembled identity service.
type Module struct {
	store   *store.Store
	svc     *domain.Service
	handler *api.Handler
}

// New assembles the service. limiter may be nil — rate limiting then
// degrades to allow-all rather than failing startup.
func New(
	pool *pgxpool.Pool,
	customerTokens, staffTokens *auth.Issuer,
	emailer email.Emailer,
	limiter *ratelimit.Limiter,
	ttls TokenTTLs,
) *Module {
	st := store.NewStore(pool)
	svc := domain.NewService(
		st, customerTokens, staffTokens, emailer, limiter,
		ttls.CustomerAccess, ttls.CustomerRefresh, ttls.StaffAccess, ttls.StaffRefresh,
	)
	return &Module{store: st, svc: svc, handler: api.NewHandler(svc)}
}

// UseBranchResolver lets the POS login turn a Business ID into an outlet.
//
// Wired after both modules exist, because franchise is built second and only
// this one endpoint needs the link.
func (m *Module) UseBranchResolver(r domain.BranchResolver) { m.svc.UseBranchResolver(r) }

// UseLocationResolver lets POS and KDS sign-in tell a store's Location ID
// from a central kitchen's.
func (m *Module) UseLocationResolver(r domain.LocationResolver) { m.svc.UseLocationResolver(r) }

// UseScopeStatus lets sign-in refuse staff whose business is suspended.
func (m *Module) UseScopeStatus(r domain.ScopeStatusReader) { m.svc.UseScopeStatus(r) }

// UsePasscodeBox sets the key till passcodes are sealed with, so admin can
// reveal them.
func (m *Module) UsePasscodeBox(b *sealbox.Box) { m.svc.UsePasscodeBox(b) }

// MountCustomerAuth attaches customer sign-up and sign-in (behind the
// business resolver).
func (m *Module) MountCustomerAuth(r chi.Router) { api.MountCustomerAuth(r, m.handler) }

// MountStaffAuth attaches staff, POS and KDS sign-in.
func (m *Module) MountStaffAuth(r chi.Router) { api.MountStaffAuth(r, m.handler) }

// MountAccount attaches profile, address and recipient management onto the
// shared /account router.
func (m *Module) MountAccount(r chi.Router) { api.MountAccount(r, m.handler) }

// MountAdmin attaches staff management onto the shared /admin router.
func (m *Module) MountAdmin(r chi.Router) { api.MountAdmin(r, m.handler) }

// MountPOS attaches customer management onto the shared /pos router.
func (m *Module) MountPOS(r chi.Router) { api.MountPOS(r, m.handler) }

// BusinessOwners creates a registered business's first login (platform API).
func (m *Module) BusinessOwners() identityapi.BusinessOwners { return m.svc }

// SeedSuperAdmin creates a brand-scoped super admin if one does not already
// exist, returning the existing user's id when it does. It is the only
// write path this module exposes outside its HTTP handlers, and exists so
// cmd/seed can bootstrap a fresh database without reaching into internal/.
//
// Never call this against production data.
func (m *Module) SeedSuperAdmin(ctx context.Context, email, password, firstName, lastName string) (id string, created bool, err error) {
	if existing, err := m.store.GetStaffByEmail(ctx, email); err == nil {
		// The account exists, but existing is not the same as usable: a run
		// that created the user and then failed before its role assignment
		// leaves an account that authenticates and is then refused with
		// NO_ROLE_ASSIGNED. Returning on the user alone made that permanent —
		// re-running the seeder would report "already exists" and change
		// nothing, so the environment could never be bootstrapped.
		//
		// The role is therefore reconciled here rather than assumed.
		assignments, listErr := m.store.ListStaffRoleAssignments(ctx, existing.ID)
		if listErr != nil {
			return "", false, listErr
		}
		for _, ra := range assignments {
			if ra.Role == tenant.RoleSuperAdmin && ra.ScopeType == tenant.ScopeBrand {
				return existing.ID, false, nil
			}
		}
		if _, err := m.store.CreateStaffRoleAssignment(ctx, identityapi.StaffRoleAssignment{
			StaffUserID: existing.ID,
			Role:        tenant.RoleSuperAdmin,
			ScopeType:   tenant.ScopeBrand,
		}); err != nil {
			return "", false, err
		}
		return existing.ID, false, nil
	}
	// The bootstrap super admin: no request, and so no caller to authorise it.
	staff, err := m.svc.CreateStaffUserAsSystem(ctx, email, password, firstName, lastName, identityapi.StaffRoleAssignment{
		Role:      tenant.RoleSuperAdmin,
		ScopeType: tenant.ScopeBrand,
	})
	if err != nil {
		return "", false, err
	}
	return staff.ID, true, nil
}

// StaffSessionGuard is middleware for the POS and KDS routers, after the
// token is verified: it ends a session whose staff account has been
// deactivated, or whose store or central kitchen is no longer trading, with
// a 401 so the till returns to its login screen. Answers are cached for a
// few seconds per account and per location.
func (m *Module) StaffSessionGuard() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, _ := auth.StaffFromContext(r.Context())
			if err := m.svc.CheckStaffSession(r.Context(), claims); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
