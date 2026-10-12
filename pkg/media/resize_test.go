package media

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// solid builds an opaque image of a given size, which is what most product
// photography is — the case that should become a JPEG.
func solid(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestNormaliseDownscalesOversizedImages(t *testing.T) {
	source := encodePNG(t, solid(3200, 2400))

	out, contentType, err := Normalise(source, "image/png")
	if err != nil {
		t.Fatalf("Normalise: %v", err)
	}
	if contentType != "image/jpeg" {
		t.Errorf("content type = %q, want image/jpeg — an opaque photo does not need PNG", contentType)
	}

	decoded, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("result does not decode: %v", err)
	}
	b := decoded.Bounds()
	if b.Dx() != MaxEdgePixels {
		t.Errorf("width = %d, want %d", b.Dx(), MaxEdgePixels)
	}
	// 3200x2400 is 4:3, so the height has to follow the width.
	if want := MaxEdgePixels * 2400 / 3200; b.Dy() != want {
		t.Errorf("height = %d, want %d — aspect ratio was not preserved", b.Dy(), want)
	}
	// Deliberately no assertion on byte size here. A synthetic gradient is
	// nearly free to store as PNG, so re-encoding it as JPEG genuinely does
	// come out larger — that says something about the fixture, not about the
	// resize. Real photography is the opposite, and TestNormaliseOnRealPhoto
	// in the manual harness covers it.
}

// A cut-out with a transparent background must not be flattened onto an
// assumed colour, which is what encoding it as JPEG would do.
func TestNormaliseKeepsTransparencyAsPNG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2000, 2000))
	for y := 0; y < 2000; y++ {
		for x := 0; x < 2000; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 0})
		}
	}

	out, contentType, err := Normalise(encodePNG(t, img), "image/png")
	if err != nil {
		t.Fatalf("Normalise: %v", err)
	}
	if contentType != "image/png" {
		t.Fatalf("content type = %q, want image/png", contentType)
	}
	decoded, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("result does not decode: %v", err)
	}
	if _, _, _, a := decoded.At(10, 10).RGBA(); a != 0 {
		t.Errorf("alpha = %d, want 0 — transparency was lost", a)
	}
}

// An image already within the limit keeps its pixels, so an admin who uploads
// a correctly sized asset gets back what they uploaded.
func TestNormaliseLeavesSmallImagesAtTheirSize(t *testing.T) {
	source := encodePNG(t, solid(400, 300))

	out, _, err := Normalise(source, "image/png")
	if err != nil {
		t.Fatalf("Normalise: %v", err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("result does not decode: %v", err)
	}
	if b := decoded.Bounds(); b.Dx() != 400 || b.Dy() != 300 {
		t.Errorf("size = %dx%d, want 400x300", b.Dx(), b.Dy())
	}
}

// WebP and AVIF cannot be decoded by the standard library. They must pass
// through rather than fail the upload.
func TestNormalisePassesThroughFormatsItCannotDecode(t *testing.T) {
	source := []byte("RIFF????WEBPVP8 not really a webp")

	out, contentType, err := Normalise(source, "image/webp")
	if err != nil {
		t.Fatalf("Normalise: %v", err)
	}
	if contentType != "image/webp" || !bytes.Equal(out, source) {
		t.Errorf("webp was altered: type %q, %d bytes", contentType, len(out))
	}
}
