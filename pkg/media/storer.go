// Package media stores the images an admin uploads — product photography,
// category tiles, modifier-option thumbnails.
//
// Every caller depends on the Storer interface rather than on Cloudflare
// directly, the same way the codebase treats email.Emailer and geo.Geocoder:
// a provider can be swapped, and local dev works without an account.
package media

import (
	"context"
	"io"
)

// UploadTicket is a one-time permission to upload one image.
//
// The browser posts the file straight to UploadURL. The bytes never pass
// through this server, which is the point — a product photo can be several
// megabytes, and proxying it would mean request-size limits, memory pressure
// and a slower upload for no benefit. It also keeps the provider API token on
// the server, where the browser cannot read it.
//
// The ticket is short-lived and good for a single upload.
type UploadTicket struct {
	// ID is the provider's id for the image that will exist once the upload
	// completes. Known up front, so the caller can record the delivery URL
	// without waiting for the browser to finish.
	ID string
	// UploadURL is where the browser sends the file. Single-use.
	UploadURL string
	// DeliveryURL is where the finished image will be served from. Valid only
	// after the upload succeeds.
	DeliveryURL string
}

// Storer issues upload tickets and removes images.
//
// There is deliberately no Upload(bytes) method. Uploads go browser-to-
// provider; this server only ever brokers permission and records the result.
type Storer interface {
	// CreateUploadTicket reserves an id and returns a one-time upload URL.
	CreateUploadTicket(ctx context.Context) (*UploadTicket, error)
	// Delete removes an image permanently. Deleting one that is already gone
	// is not an error — the caller's intent is satisfied either way.
	Delete(ctx context.Context, imageID string) error
	// DeliveryURL builds the public URL for an image id and named variant.
	// Pure string work: no network call, so it is safe to use in a loop.
	DeliveryURL(imageID, variant string) string
	// Enabled reports whether a provider is actually configured. Handlers use
	// it to answer with a clear "not configured" rather than a confusing
	// failure from the provider.
	Enabled() bool
}

// Variants are the named renditions configured in the provider's dashboard.
//
// Cloudflare Images resizes on delivery according to these, so the same
// original serves a grid thumbnail and a full-bleed hero without the admin
// uploading twice. `public` is the one every account has by default; the rest
// have to exist in the dashboard before they resolve.
const (
	VariantPublic    = "public"
	VariantThumbnail = "thumbnail"
	VariantCard      = "card"
)

// DisabledStorer stands in when no provider is configured.
//
// It refuses uploads rather than pretending to accept them: an admin who
// uploads a photo and is told it worked, only to find a broken image on the
// storefront, is worse off than one told plainly that image hosting is not set
// up yet. That is exactly the state this project was already in — product
// URLs pointed at a media host that had never existed.
type DisabledStorer struct{}

func (DisabledStorer) CreateUploadTicket(context.Context) (*UploadTicket, error) {
	return nil, ErrNotConfigured
}

func (DisabledStorer) Delete(context.Context, string) error { return ErrNotConfigured }

func (DisabledStorer) DeliveryURL(string, string) string { return "" }

func (DisabledStorer) Enabled() bool { return false }

// VideoStorer stores product videos. VideoStore keeps them in Postgres and
// serves them itself; R2VideoStore keeps them in R2, served from its public
// host.
type VideoStorer interface {
	CreateUploadTicket(ctx context.Context) (*UploadTicket, error)
	Receive(ctx context.Context, id string, r io.Reader) error
	Delete(ctx context.Context, id string) error
}
