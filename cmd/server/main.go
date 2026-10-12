// Command server is the emzo Bakehouse backend. It is a modular monolith:
// every service under services/ is compiled into this one binary and wired
// together here, but each keeps its own api/domain/store layering and its
// own private internal/ tree. Services never import each other's internals
// — only the published <svc>api contract packages — so the boundaries that
// the network would enforce in a distributed deployment are enforced by the
// compiler here instead.
//
//	@title						emzo Bakehouse API
//	@version					1.0
//	@description				Multi-store ordering platform for bakeries: storefront, POS, kitchen display and back office.
//	@BasePath					/v1
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				Bearer access token. Customer and staff are separate realms.
//	@securityDefinitions.apikey	PlatformKey
//	@in							header
//	@name						X-Platform-Key
//	@description				Platform API key (PLATFORM_API_KEY) for /platform: registering businesses. emzo team only.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "bakehouse/api" // generated Swagger spec, served at /docs
	"bakehouse/pkg/audit"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/config"
	"bakehouse/pkg/db"
	"bakehouse/pkg/email"
	"bakehouse/pkg/events"
	"bakehouse/pkg/flags"
	"bakehouse/pkg/geo"
	"bakehouse/pkg/media"
	"bakehouse/pkg/obs"
	"bakehouse/pkg/ratelimit"
	"bakehouse/pkg/sealbox"
	"bakehouse/pkg/tenant"
	"bakehouse/services/admin"
	"bakehouse/services/business"
	"bakehouse/services/catalog"
	"bakehouse/services/enquiries"
	"bakehouse/services/franchise"
	"bakehouse/services/identity"
	"bakehouse/services/orders"
	"bakehouse/services/payments"

	"github.com/jackc/pgx/v5"
)

func main() {
	log := obs.NewLogger()
	// Installed as the package default too, so code that cannot reach this
	// logger — httpx.WriteError, for one — still writes in the same JSON
	// format rather than the stdlib's plain text.
	slog.SetDefault(log)

	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cfg.Validate(); err != nil {
		log.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	if err := db.Migrate(cfg.DatabaseURL, "db/migrations"); err != nil {
		log.Error("migration failed", "error", err)
		os.Exit(1)
	}
	log.Info("migrations applied")

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Brand-wide roles reach only their own business's stores.
	tenant.SetBranchBusinessLookup(func(branchID string) (string, bool, error) {
		var businessID string
		err := pool.QueryRow(context.Background(),
			`SELECT COALESCE(business_id::text, '') FROM branches WHERE id::text = $1`, branchID).Scan(&businessID)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return businessID, err == nil, err
	})

	// --- shared infrastructure ------------------------------------------

	featureFlags := flags.FromEnv()
	log.Info("feature flags loaded", "enabled", featureFlags.Enabled())

	limiter := ratelimit.New(cfg.RedisURL)
	if limiter == nil {
		log.Warn("redis unavailable — rate limiting disabled for this run")
	}

	// Mail: SendGrid first, then SMTP, then the log.
	//
	// The order is by preference, not by history — SendGrid's HTTP API works
	// on hosts that block outbound SMTP and reports its refusals in words. The
	// logging fallback is deliberately last and deliberately loud: an OTP a
	// customer never receives is a dead end, not a slow path, and it should be
	// obvious at boot which of the three is in force.
	// Before anything is sent: the links inside an email are built from these.
	email.SetBaseURLs(cfg.StorefrontBaseURL, cfg.AdminBaseURL)
	email.SetLogoURL(cfg.EmailLogoURL)
	email.SetAssetBaseURL(cfg.PublicBaseURL)
	if cfg.EmailLogoURL == "" && !email.HasEmbeddedLogo() {
		log.Warn("no email logo: add pkg/email/assets/logo-white.png or set EMAIL_LOGO_URL — emails show the name as text until then")
	}

	var emailer email.Emailer
	emailProvider := "log"
	switch {
	case cfg.SendGridAPIKey != "":
		sender, err := email.NewSendGridEmailer(cfg.SendGridAPIKey, cfg.MailFrom)
		if err != nil {
			log.Error("SendGrid is configured but unusable — emails will be logged, not sent", "error", err)
			emailer = email.NewLoggingEmailer(log)
		} else {
			log.Info("email via SendGrid", "from", sender.FromAddress())
			emailer = sender
			emailProvider = "sendgrid"
		}
	case cfg.SMTPHost != "" && cfg.SMTPUsername != "" && cfg.SMTPPassword != "":
		log.Info("email via SMTP", "host", cfg.SMTPHost, "port", cfg.SMTPPort, "from", cfg.SMTPFromEmail)
		emailer = email.NewSMTPEmailer(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPFromEmail)
		emailProvider = "smtp"
	default:
		log.Warn("no mail provider configured — emails will be logged, not sent. " +
			"Set SENDGRID_API_KEY and MAIL_FROM to send them.")
		emailer = email.NewLoggingEmailer(log)
	}

	var geocoder geo.Geocoder
	if cfg.GoogleMapsAPIKey == "" {
		log.Warn("GOOGLE_MAPS_API_KEY not set — nearest-branch distance ranking disabled for this run")
		geocoder = geo.NoopGeocoder{}
	} else {
		geocoder = geo.NewGoogleMapsGeocoder(cfg.GoogleMapsAPIKey)
	}

	// Image and video storage, in order of preference:
	//   R2         all six R2 values set: files go to the bucket and are served
	//              from its public host (CDN, no egress cost)
	//   Cloudflare Images (images only), when its three values are set
	//   Postgres   otherwise: no account needed, survives deploys, fine for a
	//              small catalogue but not for thousands of products
	r2 := media.R2Config{
		AccountID: cfg.R2AccountID, AccessKey: cfg.R2AccessKey, SecretKey: cfg.R2SecretKey,
		Bucket: cfg.R2Bucket, PublicHost: cfg.R2PublicHost, APIBaseURL: cfg.PublicBaseURL,
	}
	var imageStore media.Storer = media.DisabledStorer{}
	switch {
	case r2.Complete():
		imageStore = media.NewR2ImageStore(r2)
		log.Info("images stored in Cloudflare R2", "bucket", r2.Bucket, "servedFrom", r2.PublicHost)
	case cfg.CloudflareAccountID != "" && cfg.CloudflareImagesToken != "" && cfg.CloudflareImagesHash != "":
		imageStore = media.NewCloudflareImages(cfg.CloudflareAccountID, cfg.CloudflareImagesToken, cfg.CloudflareImagesHash)
		log.Info("images stored in Cloudflare Images")
	default:
		if cfg.R2AccessKey != "" || cfg.R2PublicHost != "" {
			log.Warn("R2 is partly configured and will not be used — set R2_ACCOUNT_ID, R2_ACCESS_KEY, R2_SECRET_KEY, R2_BUCKET, R2_PUBLIC_HOST and PUBLIC_BASE_URL")
		}
		// PUBLIC_BASE_URL is the one thing this cannot guess: the storefront
		// runs on a different origin from the API, so a relative delivery URL
		// would resolve against the wrong host.
		store, err := media.NewPostgresStorer(pool, cfg.PublicBaseURL)
		if err != nil {
			log.Warn("image uploads disabled", "error", err)
		} else {
			imageStore = store
			log.Info("images stored in the database", "servedFrom", cfg.PublicBaseURL+"/v1/media/",
				"note", "set R2_* to move them to Cloudflare R2")
		}
	}

	auditWriter := audit.NewWriter(pool)

	// Domain events go to a transactional outbox, relayed in-process. Until
	// a consumer exists the relay dispatches to a no-op publisher, which is
	// deliberate: outbox rows are still written in the same transaction as
	// the state change, so nothing is lost when a real consumer lands.
	outbox := events.NewOutbox(pool)
	relay := events.NewRelay(outbox, events.NoopPublisher{}, log)
	relay.Start(ctx)
	defer relay.Stop()

	customerTokens := auth.NewIssuer(cfg.CustomerJWTSecret, cfg.JWTIssuer)
	staffTokens := auth.NewIssuer(cfg.StaffJWTSecret, cfg.JWTIssuer)

	// --- services --------------------------------------------------------
	//
	// Wiring order follows the dependency direction: franchise owns the
	// hierarchy, catalog reads branches, payments owns the money seam, and
	// orders composes all three.

	identitySvc := identity.New(pool, customerTokens, staffTokens, emailer, limiter, identity.TokenTTLs{
		CustomerAccess:  cfg.AccessTokenTTL,
		CustomerRefresh: cfg.RefreshTokenTTL,
		StaffAccess:     cfg.StaffAccessTokenTTL,
		StaffRefresh:    cfg.StaffRefreshTokenTTL,
	})

	passcodeKey := cfg.PasscodeEncryptionKey
	if passcodeKey == "" {
		passcodeKey = cfg.StaffJWTSecret
	}
	identitySvc.UsePasscodeBox(sealbox.New(passcodeKey))

	franchiseSvc := franchise.New(pool, auditWriter, geocoder)
	// The till types an outlet code; identity has to be able to resolve it.
	identitySvc.UseBranchResolver(franchiseSvc.BusinessIDResolver())
	// And suspending a franchise has to stop its staff signing in, not just its
	// tills taking orders.
	identitySvc.UseScopeStatus(franchiseSvc.ScopeStatus())
	// KDS: a Location ID can name a store or a central kitchen.
	identitySvc.UseLocationResolver(franchiseSvc.BusinessIDResolver())
	catalogSvc := catalog.New(pool, auditWriter, franchiseSvc.Branches(), imageStore)
	// Product videos: R2 when it is configured, otherwise the database.
	if r2.Complete() {
		catalogSvc.UseVideos(media.NewR2VideoStore(r2))
	} else if videoStore, err := media.NewVideoStore(pool, cfg.PublicBaseURL); err != nil {
		log.Warn("product video uploads disabled", "error", err)
	} else {
		catalogSvc.UseVideos(videoStore)
	}

	paymentsSvc := payments.New(pool, payments.Config{
		StripeSecretKey:     cfg.StripeSecretKey,
		StripeWebhookSecret: cfg.StripeWebhookSecret,
		StorefrontBaseURL:   cfg.StorefrontBaseURL,
	})
	if !paymentsSvc.Configured() {
		log.Warn("STRIPE_SECRET_KEY not set — orders are captured via a no-op processor (no real money moves)")
	} else if cfg.StripeWebhookSecret == "" {
		log.Warn("STRIPE_WEBHOOK_SECRET not set — /v1/webhooks/stripe will reject every event")
	}

	ordersSvc := orders.New(pool, orders.Deps{
		Branches:              franchiseSvc.Branches(),
		Pricing:               catalogSvc.Pricing(),
		Coupons:               catalogSvc.Coupons(),
		Sales:                 catalogSvc.Sales(),
		Payments:              paymentsSvc.Processor(),
		Emailer:               emailer,
		Audit:                 auditWriter,
		LocationIQAccessToken: cfg.LocationIQAccessToken,
	})
	// KDS: central-kitchen coverage and per-location KDS settings.
	ordersSvc.UseKDS(franchiseSvc.KDS())
	// Card payments: payment links and refunds through Stripe, and
	// their webhooks reported back into orders (bill payments, SSE).
	ordersSvc.UseCardPayments(paymentsSvc.Cards())
	// Seasonal tags: restricted dates keep products off those slots (website).
	ordersSvc.UseTagRestrictions(catalogSvc.TagRestrictions())
	paymentsSvc.OnEvent(ordersSvc.HandlePaymentEvent)
	// Street-address geocoding for radius delivery pricing: stores are located
	// from their address, customers from theirs. LocationIQ, the same account
	// the address search uses.
	var addrGeo geo.AddressGeocoder = geo.NoopAddressGeocoder{}
	if cfg.LocationIQAccessToken != "" {
		addrGeo = geo.NewLocationIQGeocoder(cfg.LocationIQAccessToken)
	}
	franchiseSvc.UseAddressGeocoder(addrGeo)
	ordersSvc.UseAddressGeocoder(addrGeo)
	ordersSvc.StartScheduledPromoter(ctx)
	if cfg.LocationIQAccessToken == "" {
		log.Warn("LOCATIONIQ_ACCESS_TOKEN not set — address search serves cached results only, no live lookups")
	}

	adminSvc := admin.New(pool)
	// Raised invoices have to reach the people they bill.
	adminSvc.UseEmailer(emailer)

	enquiriesSvc := enquiries.New(pool, franchiseSvc.Branches(), emailer, limiter)
	businessSvc := business.New(pool, identitySvc.BusinessOwners(), cfg.PlatformAPIKey)
	businessSvc.UseMedia(imageStore)

	// Emails go out as the business they are about, and hosted payment pages
	// return customers to that business's own website.
	email.SetBrandLookup(func(ctx context.Context, id string) (email.Brand, bool) {
		b, err := businessSvc.Business(ctx, id)
		if err != nil {
			return email.Brand{}, false
		}
		return email.Brand{Name: b.Name, LogoURL: deref(b.LogoURL), WebsiteURL: deref(b.WebsiteURL), Currency: b.Currency}, true
	})
	// Addresses are searched in the business's own country.
	geo.SetCountryLookup(func(ctx context.Context) string {
		if id := tenant.BusinessID(ctx); id != "" {
			if b, err := businessSvc.Business(ctx, id); err == nil {
				return b.CountryCode
			}
		}
		return ""
	})
	paymentsSvc.UseShopURL(func(ctx context.Context) string {
		if id := tenant.BusinessID(ctx); id != "" {
			if b, err := businessSvc.Business(ctx, id); err == nil {
				return deref(b.WebsiteURL)
			}
		}
		return ""
	})
	if cfg.PlatformAPIKey == "" {
		log.Warn("PLATFORM_API_KEY not set — /v1/platform (business registration) answers 503")
	}
	// Brand-level enquiries need a mailbox somebody reads, not a constant.
	enquiriesSvc.UseBrandInbox(cfg.BrandInboxEmail)
	enquiriesSvc.UseBusinessInbox(func(ctx context.Context) string {
		if id := tenant.BusinessID(ctx); id != "" {
			if b, err := businessSvc.Business(ctx, id); err == nil {
				return deref(b.ContactEmail)
			}
		}
		return ""
	})

	// --- routing ---------------------------------------------------------

	router := buildRouter(log, cfg.DocsEnabled, cfg.CORSAllowedOrigins, customerTokens, staffTokens, services{
		identity:  identitySvc,
		franchise: franchiseSvc,
		catalog:   catalogSvc,
		orders:    ordersSvc,
		payments:  paymentsSvc,
		integrations: integrationFlags{
			StripeConfigured: paymentsSvc.Configured(),
			EmailProvider:    emailProvider,
		},
		admin:     adminSvc,
		enquiries: enquiriesSvc,
		business:  businessSvc,

		storefrontBusiness: cfg.StorefrontBusiness,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("listening", "port", cfg.Port, "env", cfg.Env, "docs", "/docs/index.html")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
