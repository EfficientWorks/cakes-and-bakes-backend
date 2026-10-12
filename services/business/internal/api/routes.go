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
	})
}
