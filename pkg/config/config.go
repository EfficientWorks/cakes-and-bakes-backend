// Package config loads process configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Dev-only fallbacks. They keep `make run` working with no .env at all, and
// Validate refuses to start with any of them outside a local environment.
const (
	devCustomerJWTSecret = "dev-customer-secret-change-me"
	devStaffJWTSecret    = "dev-staff-secret-change-me"

	// EnvLocal is the only environment where the dev fallbacks are allowed.
	EnvLocal = "local"

	// EnvProduction is the environment where the API docs default to off.
	EnvProduction = "production"
)

// Config is the full set of environment-driven settings for the API process.
type Config struct {
	Env  string // local | staging | production
	Port string

	DatabaseURL string
	RedisURL    string

	JWTIssuer         string
	CustomerJWTSecret string
	StaffJWTSecret    string
	// Key for sealing till passcodes admin can reveal; falls back to
	// STAFF_JWT_SECRET when unset.
	PasscodeEncryptionKey string
	AccessTokenTTL        time.Duration
	RefreshTokenTTL       time.Duration
	StaffAccessTokenTTL   time.Duration
	StaffRefreshTokenTTL  time.Duration

	LocationIQAccessToken string

	// PlatformAPIKey unlocks /v1/platform, where the emzo team registers
	// businesses. Empty keeps that API closed.
	PlatformAPIKey string

	// StorefrontBusiness is the slug of the business public requests (the
	// website, customer accounts) act for when they send no X-Business
	// header. Empty: only data from before businesses existed.
	StorefrontBusiness string

	// Stripe takes every card payment: the storefront's hosted checkout,
	// POS payment links and refunds.
	StripeSecretKey      string
	StripePublishableKey string
	StripeWebhookSecret  string

	PostmarkServerToken string
	PostmarkFromEmail   string

	SendGridAPIKey string
	// MailFrom is the sender, as either "name@example.com" or
	// "Name <name@example.com>". It must be a verified sender on the SendGrid
	// account or every send is refused.
	MailFrom string

	SMTPHost      string
	SMTPPort      string
	SMTPUsername  string
	SMTPPassword  string
	SMTPFromEmail string

	R2AccountID  string
	R2AccessKey  string
	R2SecretKey  string
	R2Bucket     string
	R2PublicHost string

	SentryDSN string

	GoogleMapsAPIKey string

	// Cloudflare Images. All three are needed together — with any one missing
	// the storer is disabled, because two out of three produces URLs that
	// 404 rather than an error anyone would notice.
	//
	// CloudflareAccountID and CloudflareImagesHash are different strings that
	// look alike: the id addresses the API, the hash appears in delivery URLs.
	// Swapping them is the classic mistake and the only symptom is broken
	// images, so they are named apart here.
	CloudflareAccountID   string
	CloudflareImagesToken string
	CloudflareImagesHash  string

	// Local image storage, used when Cloudflare is not configured.
	//
	// PublicBaseURL is the origin a *browser* reaches this API on, which is
	// not always the address it listens on — behind a tunnel or a proxy the
	// two differ, and delivery URLs built from the wrong one point nowhere.
	MediaLocalDir string
	PublicBaseURL string

	// StorefrontBaseURL is the origin the *customer storefront* is served on.
	//
	// Stripe's hosted Checkout sends the customer back to a URL once they
	// have paid, and that URL is built from this rather than
	// accepted from the request. A redirect_url the client gets to choose is
	// an open redirect wearing a payment flow as a disguise — anyone could
	// point a genuinely paid session at a page they control. So the browser
	// names nothing, and this is the only place the return origin comes from.
	StorefrontBaseURL string
	// EmailLogoURL is where the white wordmark in email headers is loaded
	// from. Empty means <PublicBaseURL>/email/logo-white.png, served by the API
	// from the logo embedded in pkg/email/assets.
	EmailLogoURL string

	// AdminBaseURL is the origin the *staff console* is served on.
	//
	// Every link an email sends a member of staff is built from this. It was
	// hard-coded to a domain that does not resolve, so a staff invitation
	// pointed at nothing.
	AdminBaseURL string

	// BrandInboxEmail is where enquiries with no branch attached land.
	//
	// Was a hard-coded constant, so a deployment could not point it at a real
	// mailbox — every franchise and general enquiry went to an address that
	// may not exist.
	BrandInboxEmail string

	CORSAllowedOrigins []string

	// DocsEnabled serves the Swagger UI. It defaults off in production, where
	// the spec would enumerate every admin route and its shape, and on
	// everywhere else. Set DOCS_ENABLED to override in either direction.
	DocsEnabled bool
}

// Load reads Config from the environment, applying sane local-dev defaults.
//
// A `.env` file at the repo root is folded into the environment first, so the
// credentials kept there are actually used. Real environment variables always
// win over it — see loadDotEnvFromRoot.
func Load() Config {
	loadDotEnvFromRoot()

	return Config{
		Env:  getEnv("APP_ENV", EnvLocal),
		Port: getEnv("PORT", "8080"),

		DatabaseURL: getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5500/bakehouse?sslmode=disable"),
		RedisURL:    getEnv("REDIS_URL", "redis://localhost:6390/0"),

		JWTIssuer:             getEnv("JWT_ISSUER", "emzo-bakehouse"),
		CustomerJWTSecret:     getEnv("CUSTOMER_JWT_SECRET", devCustomerJWTSecret),
		StaffJWTSecret:        getEnv("STAFF_JWT_SECRET", devStaffJWTSecret),
		PasscodeEncryptionKey: getEnv("PASSCODE_ENCRYPTION_KEY", ""),
		AccessTokenTTL:        getDuration("CUSTOMER_ACCESS_TTL", time.Hour),
		RefreshTokenTTL:       getDuration("CUSTOMER_REFRESH_TTL", 30*24*time.Hour),
		StaffAccessTokenTTL:   getDuration("STAFF_ACCESS_TTL", 8*time.Hour),
		StaffRefreshTokenTTL:  getDuration("STAFF_REFRESH_TTL", 7*24*time.Hour),

		LocationIQAccessToken: getEnv("LOCATIONIQ_ACCESS_TOKEN", ""),
		PlatformAPIKey:        getEnv("PLATFORM_API_KEY", ""),
		StorefrontBusiness:    getEnv("STOREFRONT_BUSINESS", ""),

		StripeSecretKey:      getEnv("STRIPE_SECRET_KEY", ""),
		StripePublishableKey: getEnv("STRIPE_PUBLISHABLE_KEY", ""),
		StripeWebhookSecret:  getEnv("STRIPE_WEBHOOK_SECRET", ""),

		PostmarkServerToken: getEnv("POSTMARK_SERVER_TOKEN", ""),
		PostmarkFromEmail:   getEnv("POSTMARK_FROM_EMAIL", ""),

		SendGridAPIKey: getEnv("SENDGRID_API_KEY", ""),
		MailFrom:       getEnv("MAIL_FROM", ""),

		SMTPHost:      getEnv("SMTP_HOST", ""),
		SMTPPort:      getEnv("SMTP_PORT", "587"),
		SMTPUsername:  getEnv("SMTP_USERNAME", ""),
		SMTPPassword:  getEnv("SMTP_PASSWORD", ""),
		SMTPFromEmail: getEnv("SMTP_EMAIL_FROM", ""),

		R2AccountID:  getEnv("R2_ACCOUNT_ID", ""),
		R2AccessKey:  getEnv("R2_ACCESS_KEY", ""),
		R2SecretKey:  getEnv("R2_SECRET_KEY", ""),
		R2Bucket:     getEnv("R2_BUCKET", "emzo-bakehouse-media"),
		R2PublicHost: getEnv("R2_PUBLIC_HOST", ""),

		SentryDSN: getEnv("SENTRY_DSN", ""),

		GoogleMapsAPIKey: getEnv("GOOGLE_MAPS_API_KEY", ""),

		CloudflareAccountID:   getEnv("CLOUDFLARE_ACCOUNT_ID", ""),
		CloudflareImagesToken: getEnv("CLOUDFLARE_IMAGES_TOKEN", ""),
		CloudflareImagesHash:  getEnv("CLOUDFLARE_IMAGES_HASH", ""),

		MediaLocalDir: getEnv("MEDIA_LOCAL_DIR", "storage/images"),
		PublicBaseURL: getEnv("PUBLIC_BASE_URL", "http://localhost:8080"),

		// Trailing slash trimmed here so every URL built from it can join
		// with a leading "/" and never produce "https://host//checkout".
		StorefrontBaseURL: strings.TrimRight(getEnv("STOREFRONT_BASE_URL", "http://localhost:3000"), "/"),
		EmailLogoURL:      strings.TrimSpace(getEnv("EMAIL_LOGO_URL", "")),
		AdminBaseURL:      strings.TrimRight(getEnv("ADMIN_BASE_URL", "http://localhost:3001"), "/"),
		// Falls back to the send-from address, which is at least a mailbox that
		// exists, rather than to a domain nobody reads.
		BrandInboxEmail: getEnv("BRAND_INBOX_EMAIL", getEnv("MAIL_FROM", "")),

		CORSAllowedOrigins: getList("CORS_ALLOWED_ORIGINS", "*"),
		// CORSAllowedOrigins: getList("CORS_ALLOWED_ORIGINS", getEnv("CORS_ALLOWED_ORIGIN", "http://localhost:3000")),

		DocsEnabled: getBool("DOCS_ENABLED", getEnv("APP_ENV", EnvLocal) != EnvProduction),
	}
}

// Validate rejects a configuration that would be unsafe to run. It is
// called before anything connects, so a misconfigured deploy fails loudly
// at startup rather than quietly serving traffic signed with a secret that
// is published in this repository.
//
// Local development is exempt: the whole point of the dev fallbacks is that
// `make run` works on a fresh clone.
func (c Config) Validate() error {
	if c.Env == EnvLocal {
		return nil
	}

	var problems []string
	if c.CustomerJWTSecret == "" || c.CustomerJWTSecret == devCustomerJWTSecret {
		problems = append(problems, "CUSTOMER_JWT_SECRET is unset or still the development default")
	}
	if c.StaffJWTSecret == "" || c.StaffJWTSecret == devStaffJWTSecret {
		problems = append(problems, "STAFF_JWT_SECRET is unset or still the development default")
	}
	if c.CustomerJWTSecret != "" && c.CustomerJWTSecret == c.StaffJWTSecret {
		problems = append(problems, "CUSTOMER_JWT_SECRET and STAFF_JWT_SECRET must differ — the two auth realms are isolated")
	}
	if c.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL is required")
	}
	// CORS_ALLOWED_ORIGINS = "*" is permitted, including in production. It is
	// an open policy, and deliberately so: this API authenticates with a
	// Bearer token in a header rather than with cookies, so an arbitrary
	// origin cannot ride a session it does not already hold a token for.
	// Narrow it to the real storefront domains once they are fixed.

	if len(problems) > 0 {
		return fmt.Errorf("configuration invalid for APP_ENV=%s: %s", c.Env, strings.Join(problems, "; "))
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

// getList reads a comma-separated setting, trimming blanks. Used for CORS
// origins, where a marketing site and an admin panel are commonly both
// allowed.
func getList(key, fallback string) []string {
	raw := os.Getenv(key)
	if raw == "" {
		raw = fallback
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// getBool reads a boolean setting, falling back when unset. "1", "true",
// "yes" and "on" are true, case-insensitively; anything else is false.
func getBool(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "":
		return fallback
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
