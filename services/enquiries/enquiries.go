// Package enquiries is the composition root for enquiry capture: the
// custom-order, franchise and contact forms on the storefront, the newsletter
// footer, and the admin queue behind them (SF-34, SA-14, NOT-06).
//
// It owns the enquiry lifecycle but no commerce data. A custom-order enquiry
// names a store, which is what routes it to a branch — that is the only reason
// this service reads the franchise contract.
package enquiries

import (
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/pkg/email"
	"bakehouse/pkg/ratelimit"
	"bakehouse/services/enquiries/internal/api"
	"bakehouse/services/enquiries/internal/domain"
	"bakehouse/services/enquiries/internal/store"
	"bakehouse/services/franchise/franchiseapi"
)

// Module is the assembled enquiries service.
type Module struct {
	store   *store.Store
	svc     *domain.Service
	handler *api.Handler
}

// New assembles the service. emailer and limiter may be nil: without them the
// forms still record, they simply lose branch notification and throttling.
func New(pool *pgxpool.Pool, branches franchiseapi.BranchReader, emailer email.Emailer, limiter *ratelimit.Limiter) *Module {
	st := store.NewStore(pool)
	svc := domain.NewService(st, branches, emailer, limiter)
	return &Module{store: st, svc: svc, handler: api.NewHandler(svc)}
}

// MountPublic attaches the storefront forms and the newsletter endpoints.
// UseBrandInbox points brand-level enquiries at a configured mailbox.
func (m *Module) UseBrandInbox(addr string) { m.svc.UseBrandInbox(addr) }

func (m *Module) MountPublic(r chi.Router) { api.MountPublic(r, m.handler) }

// MountAdmin attaches the enquiry queue onto the shared /admin router.
func (m *Module) MountAdmin(r chi.Router) { api.MountAdmin(r, m.handler) }
