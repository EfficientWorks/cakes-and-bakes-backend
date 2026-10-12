package api

import (
	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/tenant"
)

// MountAdminBranches attaches branch dashboard/report routes onto the
// shared /admin/branches router (already created in main.go). Every route
// is added flat, directly on the shared router — see
// catalog.MountAdminBranchAvailability for why: two modules independently
// calling .Route() at the identical "/{branchId}" prefix panics chi, but
// each adding its own flat .Get/.Post calls under that same wildcard
// pattern does not.
func MountAdminBranches(branches chi.Router, h *Handler) {
	requireOwnBranch := tenant.RequirePermission(tenant.PermReportsOwnBranch)
	// Static before the wildcard: chi matches "/today" ahead of "/{branchId}",
	// so this does not shadow the per-branch routes below it.
	branches.With(requireOwnBranch).Get("/today", h.OutletsToday)
	branches.With(requireOwnBranch).Get("/{branchId}/dashboard", h.BranchDashboard)
	branches.With(requireOwnBranch).Get("/{branchId}/reports/sales", h.BranchSales)
	branches.With(requireOwnBranch).Get("/{branchId}/reports/products", h.BranchProducts)
	branches.With(requireOwnBranch).Get("/{branchId}/reports/fulfilment", h.BranchFulfilment)
	branches.With(requireOwnBranch).Get("/{branchId}/reports/refunds", h.BranchRefunds)
	branches.With(requireOwnBranch).Get("/{branchId}/amendments", h.BranchAmendments)
}

// MountAdminNetwork owns /admin/network outright — no other module mounts
// anything under this prefix, so it can Route() its own subrouter safely.
func MountAdminNetwork(admin chi.Router, h *Handler) {
	admin.Route("/network", func(r chi.Router) {
		r.Get("/dashboard", h.NetworkDashboard)
		r.Get("/revenue-trend", h.NetworkRevenueTrend)
		r.Get("/products", h.NetworkProducts)
		r.Get("/channels", h.NetworkChannels)
		r.Get("/fulfilment", h.NetworkFulfilment)
		r.Get("/branch-performance", h.NetworkBranchPerformance)
		r.Get("/peak-periods", h.NetworkPeakPeriods)
		r.Get("/refunds", h.NetworkRefunds)
		r.Get("/comparable-sales", h.NetworkComparableSales)
		r.Get("/audit-log", h.NetworkAuditLog)
	})
}

// MountAdminCustomers owns /admin/customers outright, same reasoning.
func MountAdminCustomers(admin chi.Router, h *Handler) {
	admin.Route("/customers", func(r chi.Router) {
		r.Get("/", h.ListCustomers)
		r.Get("/{customerId}", h.GetCustomer)
		r.Get("/{customerId}/orders", h.GetCustomerOrders)
		r.Get("/{customerId}/notes", h.ListCustomerNotes)
		r.Post("/{customerId}/notes", h.AddCustomerNote)
		r.Post("/{customerId}/tags", h.AddCustomerTag)
		r.Delete("/{customerId}/tags/{tag}", h.RemoveCustomerTag)
	})
}
