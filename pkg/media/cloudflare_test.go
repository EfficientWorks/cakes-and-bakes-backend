package media

import (
	"context"
	"errors"
	"testing"
)

func TestDeliveryURLUsesTheAccountHashNotTheAccountID(t *testing.T) {
	// The two are different strings that look alike. Getting them the wrong
	// way round yields URLs that 404 with no other symptom, so this pins
	// which one reaches the delivery URL.
	const accountID = "0123456789abcdef0123456789abcdef"
	const accountHash = "AbCdEfGhIjKlMnOpQrStUv"

	store := NewCloudflareImages(accountID, "token", accountHash)

	got := store.DeliveryURL("img-1", VariantThumbnail)
	want := "https://imagedelivery.net/" + accountHash + "/img-1/thumbnail"
	if got != want {
		t.Errorf("DeliveryURL = %q, want %q", got, want)
	}
	if got == "https://imagedelivery.net/"+accountID+"/img-1/thumbnail" {
		t.Error("delivery URL was built from the account id instead of the account hash")
	}
}

func TestDeliveryURLDefaultsToPublicVariant(t *testing.T) {
	store := NewCloudflareImages("acct", "token", "hash")
	if got, want := store.DeliveryURL("img-1", ""), "https://imagedelivery.net/hash/img-1/public"; got != want {
		t.Errorf("DeliveryURL = %q, want %q", got, want)
	}
}

// A half-configured provider must be treated as absent: two of three values
// produce URLs that resolve to nothing, which is harder to notice than an
// upload that refuses outright.
func TestPartialConfigurationIsDisabled(t *testing.T) {
	cases := []struct {
		name            string
		id, token, hash string
	}{
		{"no account id", "", "token", "hash"},
		{"no token", "acct", "", "hash"},
		{"no hash", "acct", "token", ""},
		{"nothing", "", "", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := NewCloudflareImages(c.id, c.token, c.hash)
			if store.Enabled() {
				t.Error("Enabled() = true for a partial configuration")
			}
			if _, err := store.CreateUploadTicket(context.Background()); !errors.Is(err, ErrNotConfigured) {
				t.Errorf("CreateUploadTicket error = %v, want ErrNotConfigured", err)
			}
			if got := store.DeliveryURL("img-1", VariantPublic); got != "" {
				t.Errorf("DeliveryURL = %q, want empty for an unconfigured provider", got)
			}
		})
	}
}

func TestDisabledStorerRefusesEverything(t *testing.T) {
	var s Storer = DisabledStorer{}
	if s.Enabled() {
		t.Error("DisabledStorer reports itself enabled")
	}
	if _, err := s.CreateUploadTicket(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("CreateUploadTicket error = %v, want ErrNotConfigured", err)
	}
	if err := s.Delete(context.Background(), "img-1"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("Delete error = %v, want ErrNotConfigured", err)
	}
}
