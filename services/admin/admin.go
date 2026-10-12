// Package admin is the composition root for the backoffice read model:
// branch and franchise dashboards, network reports, comparable sales,
// customer records and audit-log search.
//
// admin owns no domain data. It reads across the operational tables that
// other services write, which is why it has a store of its own but no
// published contract for anyone else to consume.
package admin

import (
	"bakehouse/pkg/email"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"bakehouse/services/admin/internal/api"
	"bakehouse/services/admin/internal/domain"
	"bakehouse/services/admin/internal/store"
)

// Module is the assembled admin service.
type Module struct {
	store   *store.Store
	svc     *domain.Service
	handler *api.Handler
}

// New assembles the service.
func New(pool *pgxpool.Pool) *Module {
	st := store.NewStore(pool)
	svc := domain.NewService(st)
	return &Module{store: st, svc: svc, handler: api.NewHandler(svc)}
}

// UseEmailer lets raised invoices reach the people they bill.
func (m *Module) UseEmailer(e email.Emailer) { m.svc.UseEmailer(e) }

// MountAdminInvoices attaches the invoice module onto the shared /admin router.
func (m *Module) MountAdminInvoices(r chi.Router) { api.MountAdminInvoices(r, m.handler) }

// MountAdminBranches attaches branch dashboards and reports onto the shared
// /admin/branches router.
func (m *Module) MountAdminBranches(r chi.Router) { api.MountAdminBranches(r, m.handler) }

// MountAdminNetwork attaches the brand-wide network reports.
func (m *Module) MountAdminNetwork(r chi.Router) { api.MountAdminNetwork(r, m.handler) }

// MountAdminCustomers attaches customer records, notes and tags.
func (m *Module) MountAdminCustomers(r chi.Router) { api.MountAdminCustomers(r, m.handler) }
