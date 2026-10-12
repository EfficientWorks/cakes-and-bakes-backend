package api

import (
	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/realtime"
)

// MountStorefront attaches the public catalogue endpoints onto the shared
// /storefront router.
func MountStorefront(sf chi.Router, h *Handler) {
	sf.Get("/product-filters", h.StorefrontFacets)
	sf.Get("/categories", h.ListCategories)
	sf.Get("/categories/{categoryId}", h.GetCategoryStorefront)
	sf.Get("/products", h.ListProducts)
	sf.Get("/products/{slug}", h.GetProduct)
	sf.Get("/products/{slug}/{branchId}", h.GetProductByBranchId)
	sf.Get("/content", h.GetContent)
	sf.Get("/coupons", h.ListStorefrontCoupons)
	sf.Get("/seasonal-tags", h.StorefrontSeasonalTags)
}

/*
 * MountPOS gives the till the same catalogue reads the storefront gets.
 *
 * The same handlers, not a second implementation: a counter sells the same
 * products, at the same branch, under the same availability and scheduling
 * rules, and two lists that could disagree about what is on sale is a bug
 * waiting to be found by a customer.
 *
 * Mounted here rather than left at /storefront so a POS client has one prefix
 * to know about, and so these reads sit behind the staff token like everything
 * else the till calls.
 */
func MountPOS(pos chi.Router, h *Handler) {
	pos.Get("/categories", h.ListCategoriesPOS)
	pos.Get("/categories/{categoryId}", h.GetCategoryPOS)
	pos.Get("/products", h.ListProductsPOS)
	pos.Get("/products/{slug}", h.GetProductPOS)
}

// MountPublicMedia attaches locally-stored image serving directly onto v1,
// outside /storefront: these URLs go into <img> tags and are public by nature,
// and nesting them under a storefront path would imply a scoping they do not
// have. A no-op in effect when a CDN provider serves the images instead.
func MountPublicMedia(v1 chi.Router, h *Handler) {
	v1.Get("/media/{imageId}", h.ServeImage)
	v1.Get("/media/videos/{videoId}", h.ServeVideo)
}

// MountAdmin attaches master-catalogue CRUD and site content onto the
// shared /admin router (already behind auth.RequireStaff).
func MountAdmin(admin chi.Router, h *Handler) {
	admin.Route("/products", func(pr chi.Router) {
		pr.Use(liveWrites(realtime.EventCatalogProduct, "id"))
		pr.Get("/", h.ListProductsAdmin)
		pr.Post("/", h.CreateProduct)
		pr.Get("/{id}", h.GetProductAdmin)
		pr.Patch("/{id}", h.UpdateProduct)
		pr.Delete("/{id}", h.DeleteProduct)
		pr.Put("/{id}/modifiers", h.SetProductModifiers)
	})

	// Image upload is mounted here rather than in its own service: every
	// image in the system belongs to something the catalogue owns.
	admin.Route("/images", func(ir chi.Router) {
		ir.Post("/upload-url", h.CreateImageUploadTicket)
		// Only reached when this server stores images itself; with a CDN
		// provider the browser uploads to the provider instead.
		ir.Post("/{imageId}/content", h.UploadImageContent)
		ir.Delete("/{imageId}", h.DeleteImage)
	})

	// Product videos: one per product, stored by this server.
	admin.Route("/videos", func(vr chi.Router) {
		vr.Post("/upload-url", h.CreateVideoUploadTicket)
		vr.Post("/{videoId}/content", h.UploadVideoContent)
		vr.Delete("/{videoId}", h.DeleteVideo)
	})

	admin.Route("/seasonal-tags", func(tr chi.Router) {
		tr.Use(liveWrites(realtime.EventCatalogProduct, "tagId"))
		tr.Get("/", h.ListSeasonalTags)
		tr.Post("/", h.CreateSeasonalTag)
		tr.Get("/{tagId}", h.GetSeasonalTag)
		tr.Put("/{tagId}", h.UpdateSeasonalTag)
		tr.Patch("/{tagId}", h.UpdateSeasonalTag)
		tr.Delete("/{tagId}", h.DeleteSeasonalTag)
	})

	admin.Route("/categories", func(cr chi.Router) {
		cr.Use(liveWrites(realtime.EventCatalogCategory, "categoryId"))
		cr.Get("/", h.ListCategoriesAdmin)
		cr.Post("/", h.CreateCategory)
		cr.Get("/{categoryId}", h.GetCategoryAdmin)
		cr.Patch("/{categoryId}", h.UpdateCategory)
		cr.Put("/{categoryId}", h.UpdateCategory)
		cr.Patch("/{categoryId}/availability", h.SetCategoryActive)
		cr.Delete("/{categoryId}", h.DeleteCategory)
	})

	// The modifier library — groups authored once and applied to many
	// products, which is what "Apply To Items" needs and what nesting them
	// inside a product could never express.
	admin.Route("/modifiers", func(mr chi.Router) {
		mr.Use(liveWrites(realtime.EventCatalogModifier, "modifierId"))
		mr.Get("/", h.ListModifiers)
		mr.Post("/", h.CreateModifier)
		mr.Patch("/{modifierId}", h.UpdateModifier)
		mr.Delete("/{modifierId}", h.DeleteModifier)
		mr.Put("/{modifierId}/products", h.ApplyModifierToProducts)
	})

	admin.Route("/coupons", func(cr chi.Router) {
		cr.Use(liveWrites(realtime.EventCatalogCoupon, "couponId"))
		cr.Get("/", h.ListCoupons)
		cr.Post("/", h.CreateCoupon)
		cr.Patch("/{couponId}", h.UpdateCoupon)
		cr.Post("/{couponId}/status", h.SetCouponStatus)
		cr.Delete("/{couponId}", h.DeleteCoupon)
	})

	admin.With(liveWrites(realtime.EventCatalogContent, "key")).Patch("/content/{key}", h.SetContent)
	admin.Get("/content/{key}", h.GetContentByKey)
}

// MountAdminBranchAvailability attaches branch-scoped catalogue-availability
// routes onto the shared /admin/branches router — see
// franchise.MountAdminBranches for why this router is shared rather than
// each module creating its own "/branches" mount.
func MountAdminBranchAvailability(branches chi.Router, h *Handler) {
	branches.Get("/{branchId}/availability", h.GetAvailabilityGrid)
	live := branches.With(liveWrites(realtime.EventCatalogAvailability, "productId"))
	live.Post("/{branchId}/availability/bulk-disable", h.BulkDisable)
	live.Patch("/{branchId}/products/{productId}/availability", h.UpdateAvailability)
	live.Post("/{branchId}/products/{productId}/date-override", h.CreateDateOverride)
	live.Post("/{branchId}/products/{productId}/sold-out", h.SetSoldOut)
	live.Post("/{branchId}/products/{productId}/restore", h.RestoreToday)
}
