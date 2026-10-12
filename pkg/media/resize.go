package media

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"

	// Registers the GIF decoder with image.Decode. Imported for that side
	// effect only — nothing here encodes a GIF.
	_ "image/gif"
)

// MaxEdgePixels is the longest side an image is stored at.
//
// The storefront's largest rendition is a full-bleed hero on a retina display,
// which is comfortably under this. Anything larger is detail no visitor will
// ever see, paid for on every page load — and product photography arrives
// enormous: the shots harvested from the live site are 5.8 MB each, so eight
// category tiles would be 46 MB of downloads for images displayed 400px wide.
const MaxEdgePixels = 1600

// JPEGQuality is the encode quality for photographs. 82 is the usual knee in
// the curve — visually indistinguishable from 100 at this size, at a fraction
// of the bytes.
const JPEGQuality = 82

// Normalise downscales an oversized image and re-encodes it.
//
// Returns the bytes to store and their content type. Anything it cannot decode
// — WebP and AVIF, which the standard library does not read — is passed
// through untouched rather than rejected: those formats are already compressed
// far better than what arrives from a camera, so there is little to gain and a
// working upload to lose.
//
// The format decision is made on transparency, not on what came in. A photo
// on an opaque background becomes JPEG, which is dramatically smaller. One
// with an alpha channel stays PNG, because flattening it onto an assumed
// background colour would put a white box behind a product cut-out.
func Normalise(data []byte, contentType string) ([]byte, string, error) {
	switch contentType {
	case "image/jpeg", "image/png", "image/gif":
	default:
		return data, contentType, nil
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		// Undecodable but allowlisted: store what we were given rather than
		// failing the upload over a file the browser will probably render.
		return data, contentType, nil
	}

	resized := downscale(src, MaxEdgePixels)
	transparent := hasAlpha(resized)

	var out bytes.Buffer
	if transparent {
		if err := png.Encode(&out, resized); err != nil {
			return nil, "", fmt.Errorf("media: re-encode png: %w", err)
		}
		return out.Bytes(), "image/png", nil
	}
	if err := jpeg.Encode(&out, resized, &jpeg.Options{Quality: JPEGQuality}); err != nil {
		return nil, "", fmt.Errorf("media: re-encode jpeg: %w", err)
	}

	// A small source re-encoded can come out larger than it went in — an
	// already-optimised JPEG being the usual case. Keep whichever is smaller,
	// as long as the original did not need resizing.
	if resized == src && out.Len() >= len(data) {
		return data, contentType, nil
	}
	return out.Bytes(), "image/jpeg", nil
}

// downscale shrinks an image so its longest edge is at most maxEdge,
// preserving aspect ratio. An image already within the limit is returned as
// it is, so the common case costs nothing.
//
// Each destination pixel is the average of the source pixels it covers — a
// box filter. Nearest-neighbour, which is all image/draw offers, aliases badly
// on the fine detail in a photograph (piped icing, sprinkles), and this is
// only a few lines more.
func downscale(src image.Image, maxEdge int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxEdge && h <= maxEdge {
		return src
	}

	scale := float64(maxEdge) / float64(w)
	if h > w {
		scale = float64(maxEdge) / float64(h)
	}
	dw, dh := int(float64(w)*scale), int(float64(h)*scale)
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		// The source rows this destination row averages over.
		y0 := b.Min.Y + y*h/dh
		y1 := b.Min.Y + (y+1)*h/dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < dw; x++ {
			x0 := b.Min.X + x*w/dw
			x1 := b.Min.X + (x+1)*w/dw
			if x1 <= x0 {
				x1 = x0 + 1
			}

			// Accumulated in 64 bits: a box can cover thousands of pixels of
			// 16-bit channels, which overflows anything narrower.
			var rs, gs, bs, as, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					r, g, bl, a := src.At(sx, sy).RGBA()
					rs += uint64(r)
					gs += uint64(g)
					bs += uint64(bl)
					as += uint64(a)
					n++
				}
			}
			dst.Set(x, y, color.RGBA64{
				R: uint16(rs / n), G: uint16(gs / n), B: uint16(bs / n), A: uint16(as / n),
			})
		}
	}
	return dst
}

// hasAlpha reports whether any pixel is not fully opaque.
//
// Asked of the image rather than the format, because the overwhelming majority
// of PNGs coming out of a camera or a design tool are opaque and belong in a
// JPEG — it is only the genuine cut-outs that need to stay PNG.
func hasAlpha(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return !o.Opaque()
	}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0xffff {
				return true
			}
		}
	}
	return false
}
