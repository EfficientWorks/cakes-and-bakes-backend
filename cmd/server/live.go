package main

import (
	"fmt"
	"net/http"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/realtime"
	"bakehouse/pkg/tenant"
)

/*
 * Live updates for the admin console and the website (Server-Sent Events).
 *
 * Same hub the KDS listens on. Events only say what changed and which record;
 * screens re-read through their normal APIs, so nothing here has to decide
 * what a viewer may see beyond which events reach them.
 */

// adminEventsHandler streams every catalogue change, and order changes at the
// branches the signed-in staff member can access (all of them for
// brand-wide roles). Mounted behind RequireStaffOrQueryToken, because a
// browser EventSource cannot send an Authorization header.
//
//	@Summary		Admin live updates (Server-Sent Events)
//	@Description	text/event-stream. Named events: catalog.product.changed, catalog.category.changed, catalog.modifier.changed, catalog.coupon.changed, catalog.content.changed, catalog.availability.changed (data: {action: created|updated|deleted, id, branchId?}) and order.created, order.updated, order.status_changed, order.amended, order.cancelled, order.kot_status_changed (data: {orderId, branchId, reference, status, kotStatus}) for the staff member's branches. A "ready" event opens the stream; a ": ping" comment is sent every 25 s. Pass the staff access token as ?accessToken= (EventSource cannot set headers).
//	@Tags			admin
//	@Produce		text/event-stream
//	@Param			accessToken	query	string	false	"Staff access token"
//	@Success		200	{string}	string	"event stream"
//	@Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Router			/admin/events [get]
func adminEventsHandler(hub *realtime.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := auth.StaffFromContext(r.Context())
		if !ok {
			httpx.WriteError(w, r, apperr.Unauthenticated("Missing bearer token."))
			return
		}
		events, unsubscribe := hub.SubscribeMatch(func(e realtime.Event) bool {
			if strings.HasPrefix(e.Type, "catalog.") {
				return true
			}
			return e.BranchID != "" && tenant.CanAccessBranch(claims, e.BranchID)
		})
		defer unsubscribe()
		realtime.Stream(w, r, events, "")
	}
}

// storefrontEventsHandler streams catalogue changes to every visitor, and —
// when a valid customer access token is given (?accessToken= or the
// Authorization header) — changes to that customer's own orders. Another
// customer's orders never reach the stream.
//
//	@Summary		Website live updates (Server-Sent Events)
//	@Description	text/event-stream. Public: catalog.product.changed, catalog.category.changed, catalog.modifier.changed, catalog.coupon.changed, catalog.content.changed, catalog.availability.changed (data: {action, id}). With a customer access token (?accessToken=), also order.created, order.updated, order.status_changed, order.amended, order.cancelled for that customer's orders (data: {orderId, reference, status}). A "ready" event opens the stream ({"signedIn": true|false}); a ": ping" comment every 25 s.
//	@Tags			storefront
//	@Produce		text/event-stream
//	@Param			accessToken	query	string	false	"Customer access token (optional)"
//	@Success		200	{string}	string	"event stream"
//	@Router			/storefront/events [get]
func storefrontEventsHandler(hub *realtime.Hub, customers *auth.Issuer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		customerID := ""
		if c, ok := auth.CustomerFromContext(r.Context()); ok {
			customerID = c.CustomerID
		} else if raw := strings.TrimSpace(r.URL.Query().Get("accessToken")); raw != "" {
			// An expired or foreign token is not an error here: the visitor
			// still gets catalogue updates, just not order updates.
			if c, err := customers.ParseCustomerAccessToken(raw); err == nil {
				customerID = c.CustomerID
			}
		}
		events, unsubscribe := hub.SubscribeMatch(func(e realtime.Event) bool {
			if strings.HasPrefix(e.Type, "catalog.") {
				return true
			}
			return customerID != "" && e.CustomerID == customerID
		})
		defer unsubscribe()
		ready := `{"signedIn":false}`
		if customerID != "" {
			ready = `{"signedIn":true}`
		}
		realtime.Stream(w, r, events, ready)
	}
}

// posEventsHandler is the till's live stream. Same hub and same staff token as
// the admin stream, narrowed to what a counter needs: catalogue changes (price,
// availability, menu) and order events for the outlet(s) the signed-in staff
// member can access. Orders placed on the website, in the admin console, on
// another till or moved by the kitchen (KDS) all arrive here, because every
// one of them publishes to the same hub.
//
// A till normally pins the stream to its own outlet with ?branchId=. Without
// it, a branch-scoped login gets its own branch and a brand-wide login gets
// every branch. A branchId the staff member cannot access is a 403.
//
// Mounted behind RequireStaffOrQueryToken so a browser-based POS (EventSource
// cannot send an Authorization header) can pass ?accessToken=; a native client
// can use the normal Authorization header.
//
//	@Summary		POS live updates (Server-Sent Events)
//	@Description	text/event-stream. catalog.* events (data: {action, id, branchId?}) and order.created, order.updated, order.status_changed, order.amended, order.cancelled, order.kot_status_changed, order.item.kot_status_changed (data: {orderId, orderItemId?, branchId, reference, status, kotStatus, at}) for the outlet(s) the staff member can access. Events only say what changed; re-read the order through GET /pos/orders/{orderId}. A "ready" event opens the stream ({"branchId": "..."}); a ": ping" comment is sent every 25 s. Pass ?accessToken= if the client cannot set headers.
//	@Tags			pos
//	@Produce		text/event-stream
//	@Param			accessToken	query		string	false	"Staff access token"
//	@Param			branchId	query		string	false	"Only this outlet's order events"
//	@Success		200			{string}	string	"event stream"
//	@Failure		401			{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
//	@Router			/pos/events [get]
func posEventsHandler(hub *realtime.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := auth.StaffFromContext(r.Context())
		if !ok {
			httpx.WriteError(w, r, apperr.Unauthenticated("Missing bearer token."))
			return
		}
		only := strings.TrimSpace(r.URL.Query().Get("branchId"))
		if only != "" && !tenant.CanAccessBranch(claims, only) {
			httpx.WriteError(w, r, apperr.PermissionDenied("You do not have access to that branch."))
			return
		}
		events, unsubscribe := hub.SubscribeMatch(func(e realtime.Event) bool {
			if strings.HasPrefix(e.Type, "catalog.") {
				return true
			}
			if e.BranchID == "" {
				return false
			}
			if only != "" {
				return e.BranchID == only
			}
			return tenant.CanAccessBranch(claims, e.BranchID)
		})
		defer unsubscribe()
		realtime.Stream(w, r, events, fmt.Sprintf(`{"branchId":%q}`, only))
	}
}
