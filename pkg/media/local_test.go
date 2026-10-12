package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pngBytes is a real 1x1 PNG. Content sniffing looks at the magic number, so
// a fake would be rejected — which is the point of the check.
var pngBytes = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
	0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

func newLocal(t *testing.T) *LocalStorer {
	t.Helper()
	store, err := NewLocalStorer(t.TempDir(), "https://api.example.com/")
	if err != nil {
		t.Fatalf("NewLocalStorer: %v", err)
	}
	return store
}

func TestLocalRoundTrip(t *testing.T) {
	store := newLocal(t)
	ctx := context.Background()

	ticket, err := store.CreateUploadTicket(ctx)
	if err != nil {
		t.Fatalf("CreateUploadTicket: %v", err)
	}

	// The trailing slash on the configured base URL must not double up.
	if want := "https://api.example.com/v1/media/" + ticket.ID; ticket.DeliveryURL != want {
		t.Errorf("DeliveryURL = %q, want %q", ticket.DeliveryURL, want)
	}
	if want := "https://api.example.com/v1/admin/images/" + ticket.ID + "/content"; ticket.UploadURL != want {
		t.Errorf("UploadURL = %q, want %q", ticket.UploadURL, want)
	}

	if err := store.Receive(ctx, ticket.ID, "image/png", bytes.NewReader(pngBytes)); err != nil {
		t.Fatalf("Receive: %v", err)
	}

	file, contentType, err := store.Open(ticket.ID)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if contentType != "image/png" {
		t.Errorf("content type = %q, want image/png", contentType)
	}
	got, _ := io.ReadAll(file)
	decoded, format, err := image.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatalf("stored bytes do not decode as an image: %v", err)
	}
	if format != "png" {
		t.Errorf("stored format = %q, want png (the source has an alpha channel)", format)
	}
	if b := decoded.Bounds(); b.Dx() != 1 || b.Dy() != 1 {
		t.Errorf("stored image is %dx%d, want 1x1 — small images must not be resized", b.Dx(), b.Dy())
	}
	// Released before the delete below: Windows refuses to remove an open file.
	if err := file.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := store.Delete(ctx, ticket.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, _, err := store.Open(ticket.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Open after Delete = %v, want ErrNotFound", err)
	}
}

// The type is decided by the bytes, not by what the caller claims — otherwise
// a script could be stored and later served from our own origin.
func TestReceiveRejectsNonImagesWhateverTheHeaderSays(t *testing.T) {
	store := newLocal(t)
	ticket, _ := store.CreateUploadTicket(context.Background())

	script := []byte("<?php system($_GET['c']); ?>")
	err := store.Receive(context.Background(), ticket.ID, "image/png", bytes.NewReader(script))
	if !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("Receive error = %v, want ErrUnsupportedType", err)
	}

	if _, _, err := store.Open(ticket.ID); !errors.Is(err, ErrNotFound) {
		t.Error("a rejected upload was left on disk")
	}
}

func TestReceiveRejectsOversizeAndLeavesNothingBehind(t *testing.T) {
	store := newLocal(t)
	ticket, _ := store.CreateUploadTicket(context.Background())

	// A valid PNG header followed by more than the cap allows.
	oversize := append(append([]byte{}, pngBytes...), bytes.Repeat([]byte{0}, MaxUploadBytes+1)...)
	err := store.Receive(context.Background(), ticket.ID, "image/png", bytes.NewReader(oversize))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Receive error = %v, want ErrTooLarge", err)
	}
	if _, _, err := store.Open(ticket.ID); !errors.Is(err, ErrNotFound) {
		t.Error("an oversize upload was left on disk")
	}
}

// Ids come out of a URL path and become a filename, so anything that is not
// one of ours has to be refused before it is joined to a directory.
func TestIDsThatCouldEscapeTheDirectoryAreRefused(t *testing.T) {
	store := newLocal(t)
	ctx := context.Background()

	hostile := []string{
		"../../../../etc/passwd",
		"..",
		"a/b",
		"",
		strings.Repeat("f", 31),            // right alphabet, wrong length
		strings.Repeat("f", 33),            //
		strings.Repeat("g", 32),            // right length, wrong alphabet
		"ABCDEF0123456789ABCDEF0123456789", // uppercase is not what we issue
	}

	for _, id := range hostile {
		t.Run(id, func(t *testing.T) {
			if err := store.Receive(ctx, id, "image/png", bytes.NewReader(pngBytes)); !errors.Is(err, ErrInvalidID) {
				t.Errorf("Receive(%q) = %v, want ErrInvalidID", id, err)
			}
			if _, _, err := store.Open(id); !errors.Is(err, ErrInvalidID) {
				t.Errorf("Open(%q) = %v, want ErrInvalidID", id, err)
			}
			if err := store.Delete(ctx, id); !errors.Is(err, ErrInvalidID) {
				t.Errorf("Delete(%q) = %v, want ErrInvalidID", id, err)
			}
		})
	}
}

func TestEveryTicketGetsItsOwnID(t *testing.T) {
	store := newLocal(t)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		ticket, err := store.CreateUploadTicket(context.Background())
		if err != nil {
			t.Fatalf("CreateUploadTicket: %v", err)
		}
		if seen[ticket.ID] {
			t.Fatalf("duplicate image id %q", ticket.ID)
		}
		seen[ticket.ID] = true
	}
}

func TestNewLocalStorerCreatesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "images")
	if _, err := NewLocalStorer(dir, "http://localhost:8080"); err != nil {
		t.Fatalf("NewLocalStorer: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("directory not created: %v", err)
	}
}

// The variant argument is accepted so call sites need not know which storer
// they have, but local disk holds one rendition.
func TestVariantsAllResolveToTheSameFile(t *testing.T) {
	store := newLocal(t)
	a := store.DeliveryURL("0123456789abcdef0123456789abcdef", VariantThumbnail)
	b := store.DeliveryURL("0123456789abcdef0123456789abcdef", VariantPublic)
	if a != b {
		t.Errorf("variants gave different URLs: %q vs %q", a, b)
	}
}
