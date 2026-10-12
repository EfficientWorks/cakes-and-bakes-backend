package api

import (
	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/tenant"
)

// MountPublic attaches the three storefront forms and the newsletter footer
// directly onto v1. They are unauthenticated by design — a customer must be
// able to reach the business without an account — and are throttled per email
// address in the service layer instead.
func MountPublic(v1 chi.Router, h *Handler) {
	v1.Route("/enquiries", func(r chi.Router) {
		r.Post("/custom-order", h.SubmitCustomOrder)
		r.Post("/franchise", h.SubmitFranchiseEnquiry)
		r.Post("/contact", h.SubmitContact)
	})

	v1.Route("/newsletter", func(r chi.Router) {
		r.Post("/subscribe", h.Subscribe)
		r.Post("/unsubscribe", h.Unsubscribe)
	})
}

// MountAdmin attaches the enquiry queue onto the shared /admin router (already
// behind auth.RequireStaff). Viewing an enquiry exposes customer contact
// details, so it is gated on the same permission as customer records.
func MountAdmin(admin chi.Router, h *Handler) {
	admin.Route("/enquiries", func(r chi.Router) {
		r.Use(tenant.RequirePermission(tenant.PermCustomersView))
		r.Get("/", h.ListEnquiries)
		r.Get("/{enquiryId}", h.GetEnquiry)
		r.Patch("/{enquiryId}", h.UpdateEnquiry)
	})
}
