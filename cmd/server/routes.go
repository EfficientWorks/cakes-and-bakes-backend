package main

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/auth"
	"bakehouse/pkg/email"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/realtime"
	"bakehouse/pkg/tenant"
	"bakehouse/services/admin"
	"bakehouse/services/business"
	"bakehouse/services/catalog"
	"bakehouse/services/enquiries"
	"bakehouse/services/franchise"
	"bakehouse/services/identity"
	"bakehouse/services/orders"
	"bakehouse/services/payments"
)

// services is every assembled module, ready to mount.
type services struct {
	identity  *identity.Module
	franchise *franchise.Module
	catalog   *catalog.Module
	orders    *orders.Module
	payments  *payments.Module
	// What main knows about each provider, for /admin/integrations.
	integrations integrationFlags
	admin        *admin.Module
	enquiries    *enquiries.Module
	business     *business.Module
	// storefrontBusiness is the slug public requests act for when they do not
	// send X-Business (STOREFRONT_BUSINESS).
	storefrontBusiness string
}

// buildRouter assembles the full route tree.
//
// It is separate from main so it can be exercised without a database: chi
// panics at registration time on a duplicate route or a doubly-mounted
// prefix, and four services share /admin/branches, so that failure mode is
// worth catching in a test rather than at deploy. See routes_test.go.
func buildRouter(
	log *slog.Logger,
	docsEnabled bool,
	corsOrigins []string,
	customerTokens, staffTokens *auth.Issuer,
	svc services,
) *chi.Mux {
	router := httpx.NewRouter(log, corsOrigins)
	httpx.MountSwagger(router, docsEnabled)

	// The wordmark in every email's header. Public and unversioned: it is
	// fetched by mail clients and their image proxies, with no auth, and the
	// URL is baked into emails already sitting in inboxes.
	router.Get(email.LogoPath, email.LogoHandler)

	router.Route("/v1", func(v1 chi.Router) {
		svc.identity.MountStaffAuth(v1)
		svc.orders.MountPublicTracking(v1)
		svc.payments.MountWebhook(v1)
		// The emzo team's platform API: register businesses and their owner
		// logins. Guarded by PLATFORM_API_KEY, not by staff auth.
		svc.business.MountPlatform(v1)

		// Image serving sits on v1 rather than under /storefront: the URLs go
		// straight into <img> tags on a public page, and no auth applies.
		svc.catalog.MountPublicMedia(v1)

		// Customer-facing routes act for one business: the X-Business header,
		// else STOREFRONT_BUSINESS. Webhooks, the platform API, media, tracking
		// and staff sign-in stay outside: staff routes take the business from
		// the staff token instead (tenant.BusinessFromStaff).
		v1.Group(func(pub chi.Router) {
			pub.Use(svc.business.ResolveBusiness(svc.storefrontBusiness))
			svc.identity.MountCustomerAuth(pub)
			svc.enquiries.MountPublic(pub)

			// /storefront is shared across services — each Mount call adds its
			// own flat routes onto the same router (chi panics if two callers
			// each Route() a fresh subrouter at an identical prefix).
			// OptionalCustomer, not RequireCustomer: guest checkout must keep
			// working, but a signed-in customer resolves their cart by customer
			// id rather than by the X-Cart-Token header.
			pub.Route("/storefront", func(sf chi.Router) {
				sf.Use(auth.OptionalCustomer(customerTokens))
				// Live catalogue (and, signed in, own-order) updates for the website.
				sf.Get("/events", storefrontEventsHandler(realtime.Default, customerTokens))
				svc.franchise.MountStorefront(sf)
				svc.catalog.MountStorefront(sf)
				svc.orders.MountStorefront(sf)
				svc.payments.MountStorefront(sf)
			})

			// /account is shared between identity (profile, addresses,
			// recipients) and orders (history, self-service).
			pub.Route("/account", func(account chi.Router) {
				account.Use(auth.RequireCustomer(customerTokens))
				svc.identity.MountAccount(account)
				svc.orders.MountAccount(account)
			})
		})

		/*
		 * The till.
		 *
		 * Its own prefix, the same staff token. A POS client signs in with a
		 * Business ID and a passcode rather than an email and a password, but
		 * what it gets back is an ordinary staff token — so every permission
		 * check behind these routes is the one the admin console gets, and a
		 * counter is not a way around who may cancel or refund.
		 */
		// The till's live stream, beside /pos for the same reason /admin/events
		// sits beside /admin: it also accepts ?accessToken= for EventSource.
		// The session guard ends a till's session the moment its staff account
		// is deactivated or its store stops trading (see identity.StaffSessionGuard).
		sessionGuard := svc.identity.StaffSessionGuard()
		v1.With(auth.RequireStaffOrQueryToken(staffTokens), sessionGuard, tenant.BusinessFromStaff).Get("/pos/events", posEventsHandler(realtime.Default))

		v1.Route("/pos", func(posRouter chi.Router) {
			posRouter.Use(auth.RequireStaff(staffTokens), sessionGuard, tenant.BusinessFromStaff)
			svc.identity.MountPOS(posRouter)
			svc.catalog.MountPOS(posRouter)
			svc.orders.MountPOS(posRouter)
			svc.franchise.MountPOS(posRouter)
		})

		/*
		 * The Kitchen Display.
		 *
		 * Same staff token as the POS (signed in via /v1/kds/auth, mounted by
		 * identity). The live stream alone also accepts ?accessToken=, because
		 * a browser EventSource cannot send an Authorization header.
		 */
		v1.Route("/kds", func(kdsRouter chi.Router) {
			kdsRouter.With(auth.RequireStaffOrQueryToken(staffTokens), sessionGuard, tenant.BusinessFromStaff).Get("/events", svc.orders.KDSEventsHandler())
			kdsRouter.Group(func(g chi.Router) {
				g.Use(auth.RequireStaff(staffTokens), sessionGuard, tenant.BusinessFromStaff)
				svc.franchise.MountKDS(g)
				svc.orders.MountKDS(g)
			})
		})

		// The admin console's live stream. Registered beside /admin rather than
		// inside it: the /admin router requires the Authorization header, and a
		// browser EventSource can only pass the token as ?accessToken=.
		v1.With(auth.RequireStaffOrQueryToken(staffTokens), tenant.BusinessFromStaff).Get("/admin/events", adminEventsHandler(realtime.Default))

		v1.Route("/admin", func(adminRouter chi.Router) {
			adminRouter.Use(auth.RequireStaff(staffTokens), tenant.BusinessFromStaff)

			svc.identity.MountAdmin(adminRouter)
			svc.franchise.MountAdmin(adminRouter)
			svc.catalog.MountAdmin(adminRouter)
			svc.orders.MountAdminOrders(adminRouter)
			svc.admin.MountAdminNetwork(adminRouter)
			svc.admin.MountAdminCustomers(adminRouter)
			svc.admin.MountAdminInvoices(adminRouter)
			svc.enquiries.MountAdmin(adminRouter)
			svc.business.MountAdmin(adminRouter)
			adminRouter.Get("/integrations", integrationsHandler(svc.integrations))

			// /admin/branches is shared between franchise (branch CRUD),
			// catalog (branch-scoped availability), orders (schedule and
			// slots) and admin (dashboards, reports), for the same reason
			// as /storefront above: the router is created once here and
			// passed to each service rather than each mounting its own.
			branches := chi.NewRouter()
			svc.franchise.MountAdminBranches(branches)
			svc.catalog.MountAdminBranchAvailability(branches)
			svc.orders.MountAdminBranches(branches)
			svc.admin.MountAdminBranches(branches)
			adminRouter.Mount("/branches", branches)

			// /admin/branch-requests is owned solely by the franchise
			// service; a fresh router is created here so the pattern stays
			// consistent and another module can mount alongside without a panic.
			branchRequests := chi.NewRouter()
			svc.franchise.MountAdminBranchRequests(branchRequests)
			adminRouter.Mount("/branch-requests", branchRequests)
		})
	})

	return router
}
