package api

import (
	"github.com/go-chi/chi/v5"
)

// MountStorefront attaches the public delivery-eligibility endpoints onto
// the shared /storefront router.
func MountStorefront(sf chi.Router, h *Handler) {
	sf.Post("/check-delivery", h.CheckDelivery)
	sf.Get("/branches", h.ListPickupBranches)
	sf.Post("/zone-coverage-notify", h.ZoneCoverageNotify)
}

// MountAdmin attaches the governance-rules endpoint onto the shared /admin
// router (already behind auth.RequireStaff — see cmd/api/main.go).
func MountAdmin(admin chi.Router, h *Handler) {
	admin.Get("/governance", h.ListGovernance)

	// Corporate Offices — central kitchens a KDS can sign in to.
	admin.Route("/central-kitchens", func(ck chi.Router) {
		ck.Get("/", h.ListCentralKitchens)
		ck.Post("/", h.CreateCentralKitchen)
		ck.Get("/{centralKitchenId}", h.GetCentralKitchen)
		ck.Patch("/{centralKitchenId}", h.UpdateCentralKitchen)
		ck.Delete("/{centralKitchenId}", h.DeleteCentralKitchen)
	})
}

// MountKDS attaches the Kitchen Display's location configuration: settings
// and printers, for a store or a central kitchen.
func MountKDS(kds chi.Router, h *Handler) {
	kds.Get("/settings", h.GetKDSSettings)
	kds.Patch("/settings/{settingsId}", h.UpdateKDSSettings)

	kds.Route("/printers", func(p chi.Router) {
		p.Get("/", h.ListKDSPrinters)
		p.Post("/", h.CreateKDSPrinter)
		p.Get("/{printerId}", h.GetKDSPrinter)
		p.Put("/{printerId}", h.UpdateKDSPrinter)
		p.Delete("/{printerId}", h.DeleteKDSPrinter)
		p.Post("/{printerId}/connect", h.ConnectKDSPrinter)
		p.Post("/{printerId}/disconnect", h.DisconnectKDSPrinter)
	})
}

// MountAdminBranches attaches branch CRUD onto a shared /admin/branches
// router — shared because the catalog module also attaches branch-scoped
// availability routes to the SAME router (see
// catalog.MountAdminBranchAvailability and cmd/api/main.go). chi panics if
// two modules each try to Route() their own subrouter at the identical
// "/branches" prefix, so the router itself is created once in main.go and
// passed to both.
func MountAdminBranches(branches chi.Router, h *Handler) {
	branches.Get("/", h.ListBranches)
	branches.Post("/", h.CreateBranch)
	branches.Get("/{branchId}", h.GetBranch)
	branches.Patch("/{branchId}", h.UpdateBranch)
	branches.Post("/{branchId}/suspend", h.SuspendBranch)
	branches.Post("/{branchId}/reactivate", h.ReactivateBranch)
	branches.Post("/{branchId}/emergency-close", h.EmergencyClose)
	branches.Get("/{branchId}/exceptions", h.ListExceptions)
	branches.Post("/{branchId}/exceptions", h.CreateException)
	branches.Delete("/{branchId}/exceptions/{exceptionId}", h.DeleteException)
}

// MountAdminBranchRequests attaches branch-request CRUD and lifecycle actions
// onto a dedicated /admin/branch-requests router.
func MountAdminBranchRequests(r chi.Router, h *Handler) {
	r.Get("/", h.ListBranchRequests)
	r.Post("/", h.CreateBranchRequest)
	r.Get("/{requestId}", h.GetBranchRequest)
	r.Post("/{requestId}/status", h.UpdateBranchRequestStatus)
}

/*
 * MountPOS attaches printer configuration to the till's prefix.
 *
 * Printers live with the franchise service because they are a property of an
 * outlet rather than of an order, but they are only ever configured from the
 * POS settings screen — so they are mounted here rather than under /admin,
 * where nothing would ever call them.
 */
func MountPOS(pos chi.Router, h *Handler) {
	pos.Route("/printers", func(p chi.Router) {
		p.Get("/", h.ListPrinters)
		p.Post("/", h.CreatePrinter)
		p.Get("/{printerId}", h.GetPrinter)
		p.Put("/{printerId}", h.UpdatePrinter)
		p.Delete("/{printerId}", h.DeletePrinter)

		// Selecting a printer on the settings screen, as opposed to
		// configuring one: see Service.SetPrinterConnected.
		p.Post("/{printerId}/connect", h.ConnectPrinter)
		p.Post("/{printerId}/disconnect", h.DisconnectPrinter)
	})
	pos.Route("/print-templates", func(p chi.Router) {
		p.Get("/{receiptType}", h.GetPrintTemplate)
		p.Put("/{receiptType}", h.UpdatePrintTemplate)
	})
}
