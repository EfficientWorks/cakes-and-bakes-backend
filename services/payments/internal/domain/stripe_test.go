package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stripe/stripe-go/v83/webhook"

	"bakehouse/pkg/apperr"
)

func TestIsStripeSessionID(t *testing.T) {
	if !isStripeSessionID("cs_test_a1B2") || isStripeSessionID("3f1b2c4d-5678-4abc-9def-0123456789ab") {
		t.Fatal("session id detection is wrong")
	}
}

func TestStripeWebhookSignature(t *testing.T) {
	s := &Service{}
	if err := s.HandleStripeWebhook(context.Background(), []byte(`{}`), "x"); !hasCode(err, "WEBHOOK_NOT_CONFIGURED") {
		t.Fatalf("no secret: %v", err)
	}

	s.stripeWebhookSecret = "whsec_test"
	payload := []byte(`{"id":"evt_1","object":"event","type":"customer.created","data":{"object":{}}}`)
	if err := s.HandleStripeWebhook(context.Background(), payload, "t=1,v1=bad"); !hasCode(err, "INVALID_SIGNATURE") {
		t.Fatalf("bad signature: %v", err)
	}

	signed := webhook.GenerateTestSignedPayload(&webhook.UnsignedPayload{
		Payload: payload, Secret: "whsec_test", Timestamp: time.Now(),
	})
	// An event type the handler ignores, so the nil store is never touched.
	if err := s.HandleStripeWebhook(context.Background(), signed.Payload, signed.Header); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
}

func hasCode(err error, code string) bool {
	var ae *apperr.Error
	return errors.As(err, &ae) && ae.Code == code
}
