package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"
)

// fakeObjects is an in-memory bucket.
type fakeObjects struct {
	objects map[string][]byte
	types   map[string]string
}

func newFakeObjects() *fakeObjects {
	return &fakeObjects{objects: map[string][]byte{}, types: map[string]string{}}
}

func (f *fakeObjects) put(_ context.Context, key, contentType string, body []byte) error {
	f.objects[key], f.types[key] = body, contentType
	return nil
}

func (f *fakeObjects) get(_ context.Context, key string) (io.ReadCloser, string, error) {
	b, ok := f.objects[key]
	if !ok {
		return nil, "", ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), f.types[key], nil
}

func (f *fakeObjects) remove(_ context.Context, key string) error {
	delete(f.objects, key)
	return nil
}

func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 120, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestR2ImageStore(t *testing.T) {
	bucket := newFakeObjects()
	store := &R2ImageStore{objects: bucket, publicHost: "https://media.example.com", apiBaseURL: "https://api.example.com"}

	ticket, err := store.CreateUploadTicket(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ticket.UploadURL != "https://api.example.com/v1/admin/images/"+ticket.ID+"/content" {
		t.Fatalf("upload goes through the API for checking, got %s", ticket.UploadURL)
	}
	if ticket.DeliveryURL != "https://media.example.com/images/"+ticket.ID {
		t.Fatalf("delivery is the public R2 URL, got %s", ticket.DeliveryURL)
	}

	// A large opaque photo is resized and stored as a JPEG.
	if err := store.Receive(context.Background(), ticket.ID, "image/png", bytes.NewReader(testPNG(t, 2400, 1200))); err != nil {
		t.Fatal(err)
	}
	stored := bucket.objects["images/"+ticket.ID]
	if bucket.types["images/"+ticket.ID] != "image/jpeg" {
		t.Fatalf("content type = %s", bucket.types["images/"+ticket.ID])
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(stored))
	if err != nil || cfg.Width > MaxEdgePixels {
		t.Fatalf("stored image not resized: %+v %v", cfg, err)
	}

	// Not an image: refused, nothing stored.
	err = store.Receive(context.Background(), strings.Repeat("a", 32), "image/png", strings.NewReader("<script>alert(1)</script>"))
	if !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("html accepted: %v", err)
	}

	// An id this package did not issue cannot name an object key.
	if err := store.Receive(context.Background(), "../../secrets", "", bytes.NewReader(testPNG(t, 10, 10))); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("path id accepted: %v", err)
	}

	if err := store.Delete(context.Background(), ticket.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := bucket.objects["images/"+ticket.ID]; ok {
		t.Fatal("delete left the object")
	}
}

func TestR2VideoStore(t *testing.T) {
	bucket := newFakeObjects()
	store := &R2VideoStore{objects: bucket, publicHost: "https://media.example.com", apiBaseURL: "https://api.example.com"}
	ticket, err := store.CreateUploadTicket(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mp4 := append([]byte{0, 0, 0, 24}, []byte("ftypisom0000")...)
	if err := store.Receive(context.Background(), ticket.ID, bytes.NewReader(mp4)); err != nil {
		t.Fatal(err)
	}
	if bucket.types["videos/"+ticket.ID] != "video/mp4" {
		t.Fatalf("content type = %s", bucket.types["videos/"+ticket.ID])
	}
	if store.PublicURL(ticket.ID) != "https://media.example.com/videos/"+ticket.ID {
		t.Fatal("wrong public URL")
	}
	if err := store.Receive(context.Background(), ticket.ID, strings.NewReader("not a video")); !errors.Is(err, ErrUnsupportedVideo) {
		t.Fatalf("non-video accepted: %v", err)
	}
}

func TestR2ConfigComplete(t *testing.T) {
	full := R2Config{AccountID: "a", AccessKey: "k", SecretKey: "s", Bucket: "b", PublicHost: "https://m", APIBaseURL: "https://api"}
	if !full.Complete() {
		t.Fatal("full config not complete")
	}
	partial := full
	partial.SecretKey = ""
	if partial.Complete() {
		t.Fatal("partial config counted as complete")
	}
}
