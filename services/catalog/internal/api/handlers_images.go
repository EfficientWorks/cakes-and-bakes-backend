package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/media"
)

// uploadTicketResponse is what the admin UI needs to upload one image.
type uploadTicketResponse struct {
	// ImageID is the provider's id. Store it if you ever want to delete the
	// image; the delivery URL alone is not enough to do that.
	ImageID string `json:"imageId"`
	// UploadURL is where the browser PUTs/POSTs the file. Single-use and
	// short-lived, so it is fetched per upload rather than cached.
	UploadURL string `json:"uploadUrl"`
	// DeliveryURL is where the image will be served once uploaded. This is
	// the value that goes into a product's `images[].url`.
	DeliveryURL string `json:"deliveryUrl"`
	// Variants names the renditions configured on the account. The same
	// image is available at each by swapping the last path segment.
	Variants []string `json:"variants"`
}

// CreateImageUploadTicket issues a one-time direct-upload URL.
//
//	@Summary		Get an image upload URL
//	@Description	Returns a single-use URL the browser posts the file straight to, so the image never passes through this server. Record `deliveryUrl` against the product, category or modifier option.
//	@Tags			admin-catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Success		201	{object}	httpx.Envelope{data=uploadTicketResponse}
//	@Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Failure		503	{object}	httpx.Envelope	"IMAGE_STORAGE_NOT_CONFIGURED"
//	@Router			/admin/images/upload-url [post]
func (h *Handler) CreateImageUploadTicket(w http.ResponseWriter, r *http.Request) {
	ticket, err := h.media.CreateUploadTicket(r.Context())
	if err != nil {
		httpx.WriteError(w, r, imageStorageError(err))
		return
	}

	httpx.Created(w, uploadTicketResponse{
		ImageID:     ticket.ID,
		UploadURL:   ticket.UploadURL,
		DeliveryURL: ticket.DeliveryURL,
		Variants:    []string{media.VariantPublic, media.VariantThumbnail, media.VariantCard},
	})
}

// DeleteImage removes an uploaded image from the provider.
//
// It does not touch the catalogue: a product still naming this image would be
// left pointing at nothing. Callers detach the image from the product first,
// then delete it — doing it the other way round leaves a broken storefront in
// the window between the two calls.
//
//	@Summary		Delete an uploaded image
//	@Description	Removes the image from storage. Detach it from any product, category or modifier option first — this does not update the catalogue.
//	@Tags			admin-catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			imageId	path	string	true	"Provider image id"
//	@Success		204	"No Content"
//	@Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Failure		503	{object}	httpx.Envelope	"IMAGE_STORAGE_NOT_CONFIGURED"
//	@Router			/admin/images/{imageId} [delete]
func (h *Handler) DeleteImage(w http.ResponseWriter, r *http.Request) {
	if err := h.media.Delete(r.Context(), chi.URLParam(r, "imageId")); err != nil {
		httpx.WriteError(w, r, imageStorageError(err))
		return
	}
	httpx.NoContent(w)
}

// imageStorageError separates "nobody set this up" from "the provider failed".
//
// The first is a deployment gap an operator fixes by adding three environment
// variables; the second is a transient upstream problem. Reporting both the
// same way sends someone hunting for an outage that is really a missing
// config value.
func imageStorageError(err error) error {
	if errors.Is(err, media.ErrNotConfigured) {
		return apperr.Wrap(err, "IMAGE_STORAGE_NOT_CONFIGURED", apperr.CategoryUpstream,
			"Image uploads are not set up. Set the R2_* settings (or PUBLIC_BASE_URL to store images in the database).")
	}
	return apperr.Wrap(err, "IMAGE_UPLOAD_FAILED", apperr.CategoryUpstream,
		"The image could not be prepared for upload. Please try again.")
}

// UploadImageContent receives an image for a ticket this server issued.
//
// Only exists for storers that take the bytes themselves — local disk. With
// Cloudflare the browser posts straight to Cloudflare and this route is never
// reached, which is why it type-asserts rather than widening the Storer
// interface for a capability most providers do not have.
//
// The body is the raw image, not multipart: the admin already knows the id, so
// there is no second field to carry and a plain PUT-style body keeps the
// client simple.
//
//	@Summary		Upload image bytes (local storage only)
//	@Description	Posts the file for an id returned by /admin/images/upload-url. Only used when the server stores images itself; with a CDN provider the browser uploads directly instead.
//	@Tags			admin-catalog
//	@Accept			octet-stream
//	@Produce		json
//	@Security		BearerAuth
//	@Param			imageId	path	string	true	"Image id from the upload ticket"
//	@Success		204	"No Content"
//	@Failure		400	{object}	httpx.Envelope	"UNSUPPORTED_IMAGE_TYPE or IMAGE_TOO_LARGE"
//	@Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Router			/admin/images/{imageId}/content [post]
func (h *Handler) UploadImageContent(w http.ResponseWriter, r *http.Request) {
	receiver, ok := h.media.(media.Receiver)
	if !ok {
		httpx.WriteError(w, r, apperr.New("UPLOAD_NOT_ACCEPTED_HERE", apperr.CategoryValidation,
			"This server does not receive image uploads directly. Use the uploadUrl from the ticket."))
		return
	}

	// Bounded before anything reads it, so an oversized body is refused
	// rather than streamed to disk and deleted afterwards.
	body := http.MaxBytesReader(w, r.Body, media.MaxUploadBytes+1)
	defer body.Close()

	imageID := chi.URLParam(r, "imageId")
	if err := receiver.Receive(r.Context(), imageID, r.Header.Get("Content-Type"), body); err != nil {
		httpx.WriteError(w, r, uploadError(err))
		return
	}
	httpx.NoContent(w)
}

// ServeImage serves a stored image.
//
// Public and unauthenticated by design: these are product photographs on a
// storefront that anyone can browse, and requiring a token would mean every
// <img> carried one.
//
//	@Summary		Serve a stored image
//	@Description	Public. Only present when the server stores images itself; a CDN provider serves them from its own domain.
//	@Tags			storefront
//	@Produce		image/jpeg
//	@Param			imageId	path	string	true	"Image id"
//	@Success		200	{file}	binary
//	@Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
//	@Router			/media/{imageId} [get]
func (h *Handler) ServeImage(w http.ResponseWriter, r *http.Request) {
	// Stored in R2: send the browser to the public host. Links saved while
	// images lived in the database keep working this way.
	if public, ok := h.media.(media.PublicDelivery); ok {
		http.Redirect(w, r, public.PublicURL(chi.URLParam(r, "imageId")), http.StatusMovedPermanently)
		return
	}
	receiver, ok := h.media.(media.Receiver)
	if !ok {
		httpx.WriteError(w, r, apperr.NotFound("Image not found."))
		return
	}

	file, contentType, err := receiver.Open(chi.URLParam(r, "imageId"))
	if err != nil {
		httpx.WriteError(w, r, apperr.NotFound("Image not found."))
		return
	}
	defer file.Close()

	// Immutable: an id is never reused, so the bytes behind one cannot change
	// and the browser need never revalidate.
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = io.Copy(w, file)
}

// uploadError turns storage failures into something the admin can act on.
func uploadError(err error) error {
	switch {
	case errors.Is(err, media.ErrUnsupportedType):
		return apperr.Wrap(err, "UNSUPPORTED_IMAGE_TYPE", apperr.CategoryValidation,
			"That file is not a supported image. Use JPEG, PNG, WebP, AVIF or GIF.")
	case errors.Is(err, media.ErrTooLarge):
		return apperr.Wrap(err, "IMAGE_TOO_LARGE", apperr.CategoryValidation,
			"That image is larger than 10 MB. Please upload a smaller file.")
	case errors.Is(err, media.ErrInvalidID):
		return apperr.Wrap(err, "INVALID_IMAGE_ID", apperr.CategoryValidation,
			"That upload link is not valid. Request a new one.")
	default:
		return imageStorageError(err)
	}
}
