package auth

import "testing"

func TestHashSecretRoundTrip(t *testing.T) {
	hash, err := HashSecret("Passw0rd!23")
	if err != nil {
		t.Fatalf("HashSecret: %v", err)
	}
	ok, err := VerifySecret(hash, "Passw0rd!23")
	if err != nil {
		t.Fatalf("VerifySecret: %v", err)
	}
	if !ok {
		t.Fatal("expected correct password to verify")
	}
}

func TestVerifySecretRejectsWrongPassword(t *testing.T) {
	hash, err := HashSecret("Passw0rd!23")
	if err != nil {
		t.Fatalf("HashSecret: %v", err)
	}
	ok, err := VerifySecret(hash, "wrong-password")
	if err != nil {
		t.Fatalf("VerifySecret: %v", err)
	}
	if ok {
		t.Fatal("expected wrong password to fail verification")
	}
}

func TestHashOpaqueTokenIsDeterministic(t *testing.T) {
	a := HashOpaqueToken("same-token")
	b := HashOpaqueToken("same-token")
	if a != b {
		t.Fatal("HashOpaqueToken must be deterministic for lookup by hash")
	}
	if HashOpaqueToken("different-token") == a {
		t.Fatal("different tokens must hash differently")
	}
}
