package main

import (
	"net/http"

	"bakehouse/pkg/httpx"
)

// integrationFlags is what main knows about each third-party provider at
// boot. Each entry reports configuration, not a live check.
type integrationFlags struct {
	StripeConfigured bool
	// EmailProvider is "sendgrid", "smtp" or "log" (not sending).
	EmailProvider string
}

type emailIntegrationStatus struct {
	Configured bool   `json:"configured"`
	Provider   string `json:"provider" example:"sendgrid"`
}

type paymentIntegrationStatus struct {
	Configured bool `json:"configured"`
	// Active: this provider takes new card payments.
	Active bool `json:"active"`
}

type integrationsResponse struct {
	Stripe paymentIntegrationStatus `json:"stripe"`
	Email  emailIntegrationStatus   `json:"email"`
}

// integrationsHandler godoc
//
//	@Summary		Third-party integration status
//	@Description	Which providers this server is configured for.
//	@Tags			admin-settings
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	httpx.Envelope{data=integrationsResponse}
//	@Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Router			/admin/integrations [get]
func integrationsHandler(flags integrationFlags) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		out := integrationsResponse{
			Stripe: paymentIntegrationStatus{Configured: flags.StripeConfigured, Active: flags.StripeConfigured},
			Email:  emailIntegrationStatus{Configured: flags.EmailProvider != "log", Provider: flags.EmailProvider},
		}
		httpx.OK(w, out)
	}
}
