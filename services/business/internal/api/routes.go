package api

import "github.com/go-chi/chi/v5"

// MountPlatform attaches the platform API under /platform, behind the
// platform key. It carries no staff or customer auth: a business's own staff
// never reach it.
func MountPlatform(v1 chi.Router, h *Handler, platformKey string) {
	v1.Route("/platform", func(r chi.Router) {
		r.Use(RequirePlatformKey(platformKey))
		r.Get("/countries", h.ListCountries)
		r.Get("/businesses", h.ListBusinesses)
		r.Post("/businesses", h.RegisterBusiness)
		r.Get("/businesses/{businessId}", h.GetBusiness)
		r.Patch("/businesses/{businessId}", h.UpdateBusiness)
		r.Post("/businesses/{businessId}/owner", h.CreateOwner)
		r.Post("/businesses/{businessId}/logo", h.UploadLogo)
	})
}

// MountAdmin attaches the console's read of its own business onto the
// staff-authenticated /admin router.
func MountAdmin(admin chi.Router, h *Handler) {
	admin.Get("/business", h.MyBusiness)
}

// MountStorefront attaches GET /business onto the /storefront router (after
// ResolveBusiness has chosen the business).
func MountStorefront(sf chi.Router, h *Handler) {
	sf.Get("/business", h.ShopBusiness)
}
