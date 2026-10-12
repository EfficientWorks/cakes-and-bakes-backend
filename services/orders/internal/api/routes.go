package api

import (
	"github.com/go-chi/chi/v5"
)

// MountStorefront attaches cart/scheduling/checkout onto the shared
// /storefront router. Cart identity resolves from an optional customer
// bearer token or the X-Cart-Token header (see Handler.cartIdentity) — the
// /storefront router carries auth.OptionalCustomer, not RequireCustomer, so
// guest checkout keeps working (SF-29).
func MountStorefront(sf chi.Router, h *Handler) {
	sf.Get("/slots", h.GetSlots)
	sf.Get("/address/search", h.SearchAddress)

	sf.Route("/cart", func(c chi.Router) {
		c.Get("/", h.GetCart)
		c.Post("/items", h.AddCartItem)
		c.Patch("/items/{cartItemId}", h.UpdateCartItem)
		c.Delete("/items/{cartItemId}", h.RemoveCartItem)
		c.Post("/fulfilment", h.SetFulfilment)
		c.Post("/schedule", h.SetSchedule)
		c.Post("/tip", h.SetTip)
		c.Post("/coupon", h.ApplyCoupon)
		c.Delete("/coupon", h.RemoveCoupon)
	})

	sf.Route("/checkout", func(co chi.Router) {
		co.Post("/validate", h.ValidateCart)
	})

	sf.Post("/orders", h.CreateOrder)

	// The recipient book. Guest-friendly: identified by ?purchaserEmail on
	// every verb, with the customer id from an optional bearer token widening
	// what a signed-in purchaser can see.
	sf.Route("/recipients", func(rc chi.Router) {
		rc.Get("/", h.ListStorefrontRecipients)
		rc.Post("/", h.CreateStorefrontRecipient)
		rc.Get("/{recipientId}", h.GetStorefrontRecipient)
		rc.Put("/{recipientId}", h.UpdateStorefrontRecipient)
		rc.Delete("/{recipientId}", h.DeleteStorefrontRecipient)
	})
}

// MountPublicTracking attaches the public order-tracking endpoint directly
// onto v1 (not under /storefront — matches the documented /orders/track/:reference path).
func MountPublicTracking(v1 chi.Router, h *Handler) {
	v1.Get("/orders/track/{reference}", h.TrackOrder)
}

// MountAccount attaches customer order-history/self-service routes onto the
// shared /account router (already behind auth.RequireCustomer).
func MountAccount(account chi.Router, h *Handler) {
	account.Route("/orders", func(o chi.Router) {
		o.Get("/", h.ListMyOrders)
		o.Get("/{orderId}", h.GetMyOrder)
		o.Post("/{orderId}/cancel", h.CancelMyOrder)
		o.Post("/{orderId}/reorder", h.ReorderMyOrder)
	})
	account.Post("/claim-order", h.ClaimOrder)
}

// MountAdminOrders attaches order management onto the shared /admin router
// (already behind auth.RequireStaff).
func MountAdminOrders(admin chi.Router, h *Handler) {
	admin.Route("/orders", func(o chi.Router) {
		o.Get("/", h.ListOrdersAdmin)
		o.Get("/views/today", h.ListOrdersToday)
		o.Get("/views/tomorrow", h.ListOrdersTomorrow)
		o.Get("/views/unfulfilled", h.ListOrdersUnfulfilled)
		o.Post("/", h.CreateManualOrder)
		o.Get("/{orderId}", h.GetOrderAdmin)
		o.Patch("/{orderId}", h.AmendOrderAdmin)
		o.Post("/{orderId}/cancel", h.CancelOrderAdmin)
		o.Post("/{orderId}/refund", h.RefundOrderAdmin)
		o.Post("/{orderId}/mark-fulfilled", h.MarkFulfilledAdmin)
		o.Post("/{orderId}/resend-confirmation", h.ResendConfirmationAdmin)
		o.Post("/{orderId}/collect-cash", h.RecordCashCollectedAdmin)
		o.Post("/{orderId}/status", h.SetOrderStatusAdmin)
	})
}

/*
 * MountPOS attaches the till's endpoints.
 *
 * Its own prefix rather than living under /admin: the console and the counter
 * are different jobs at different screens, and a POS client should not have to
 * pick its way through routes meant for a back office. Same staff token
 * though, and the same permission checks behind it — a till is not a way
 * around who may cancel an order.
 *
 * Reads reuse the admin endpoints, which are already branch-scoped: an order
 * is an order, and a second list that could drift from the first is a bug
 * waiting to happen.
 */
func MountPOS(pos chi.Router, h *Handler) {
	pos.Route("/orders", func(o chi.Router) {
		pos.Get("/address/search", h.SearchAddressPOS)
		pos.Post("/delivery/quote", h.POSDeliveryQuote)
		o.Get("/", h.ListPOSOrders)
		o.Get("/{orderId}", h.GetPOSOrder)
		o.Post("/", h.CreatePOSOrder)
		// Bulk first: chi would otherwise read "bulk" as an {orderId}.
		o.Post("/bulk", h.BulkPOSOrderAction)

		o.Put("/{orderId}", h.UpdatePOSOrder)
		o.Delete("/{orderId}", h.DeletePOSOrder)
		o.Post("/{orderId}/return", h.ReturnPOSOrder)

		// Moving an order along — mostly for orders the till did not take.
		// A website order arrives paid and still has to be handed over.
		o.Post("/{orderId}/status", h.SetPOSOrderStatus)
		o.Post("/{orderId}/mark-fulfilled", h.MarkPOSOrderFulfilled)
		o.Post("/{orderId}/cancel", h.CancelPOSOrder)
		o.Post("/{orderId}/refund", h.RefundPOSOrder)

		o.Post("/{orderId}/split", h.SplitOrder)
		o.Delete("/{orderId}/split", h.ClearSplit)
		o.Get("/{orderId}/bills", h.ListBills)
		o.Post("/{orderId}/payments", h.TakePayment)

		// Card payment links (Stripe), one open per order.
		o.Post("/{orderId}/payment-link", h.SendPaymentLink)
		o.Get("/{orderId}/payment-link", h.GetPaymentLink)
		o.Delete("/{orderId}/payment-link", h.CancelPaymentLink)
	})
	pos.Get("/payments/options", h.POSPaymentOptions)

	// The cash drawer: the float, what goes in and out of it, and closing it.
	pos.Route("/cash", func(c chi.Router) {
		c.Get("/day", h.GetCashDay)
		c.Put("/starting-cash", h.SetStartingCash)
		c.Get("/movements", h.ListCashMovements)
		c.Post("/movements", h.AddCashMovement)
		c.Delete("/movements/{movementId}", h.DeleteCashMovement)
	})

	// Reading the reconciliation and committing to it are the same resource,
	// which is why they share a path rather than being /day-close/summary and
	// /day-close/submit.
	pos.Get("/day-close", h.GetDayClose)
	pos.Post("/day-close", h.CloseDay)

	pos.Get("/reports/daily", h.DailyReport)
}

// MountKDS attaches the Kitchen Display's order endpoints under /kds.
func MountKDS(kds chi.Router, h *Handler) {
	kds.Route("/orders", func(o chi.Router) {
		o.Get("/", h.KDSListOrders)
		// Fixed paths before {orderId}.
		o.Get("/tomorrow/slots", h.KDSSlotSummary)
		o.Get("/tomorrow/products", h.KDSProductSummary)
		o.Get("/week", h.KDSWeekSummary)
		o.Get("/calendar", h.KDSCalendar)
		o.Get("/summary", h.KDSFooterSummary)

		o.Get("/{orderId}", h.KDSGetOrder)
		o.Patch("/{orderId}/kot-status", h.KDSUpdateOrderKOT)
		o.Patch("/{orderId}/items/{itemId}/kot-status", h.KDSUpdateItemKOT)
		o.Post("/{orderId}/acknowledge-amendment", h.KDSAcknowledgeAmendment)
	})
}

// MountAdminBranches attaches schedule/slot configuration onto the shared
// /admin/branches router — the same one franchise.MountAdminBranches and
// catalog.MountAdminBranchAvailability attach to (see
// franchise.MountAdminBranches for why this router is shared rather than each
// module creating its own "/branches" mount).
func MountAdminBranches(branches chi.Router, h *Handler) {
	branches.Get("/{branchId}/schedule", h.GetScheduleConfig)
	branches.Patch("/{branchId}/schedule", h.UpdateScheduleConfig)
	branches.Get("/{branchId}/slots", h.ListSlotsAdmin)
	branches.Patch("/{branchId}/slots/{slotId}", h.UpliftSlotCapacity)
}
