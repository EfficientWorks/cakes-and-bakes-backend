package api

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/media"
	"bakehouse/pkg/ratelimit"
)

// Customer photo uploads are public (a guest can send a custom-order form),
// so they are capped per IP as well as by size and type.
const (
	customerPhotoLimit  = 20
	customerPhotoWindow = time.Hour
)

// customerPhotoTypes are the formats a customer may upload. HEIC and anything
// else is refused: the kitchen has to be able to open it.
var customerPhotoTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}

// UseLimiter turns on per-IP throttling of public uploads. Nil is a no-op.
func (h *Handler) UseLimiter(l *ratelimit.Limiter) { h.limiter = l }

type customerPhotoDTO struct {
	// The upload id; send it (or url) back as a photo reference.
	ID string `json:"id" example:"9f2c4e0a6b1d4c3e8f7a6b5c4d3e2f1a"`
	// Where the photo is served from.
	URL string `json:"url" example:"https://api.example.com/v1/media/9f2c4e0a6b1d4c3e8f7a6b5c4d3e2f1a"`
}

// UploadCustomerPhoto stores a photo a customer wants printed on a cake.
//
//	@Summary		Upload a customer cake photo
//	@Description	Public. Send the image either as multipart/form-data (field "file") or as the raw request body with an image Content-Type. JPEG, PNG or WebP only (HEIC and others are refused), at most 10 MB; large images are scaled to 1600 px on the longest side. Rate limited per IP (20 an hour). Returns the id and url to attach to a custom order (photoUrls).
//	@Tags			storefront
//	@Accept			mpfd
//	@Produce		json
//	@Param			file	formData	file	false	"The image (multipart)"
//	@Success		201		{object}	httpx.Envelope{data=customerPhotoDTO}
//	@Failure		400		{object}	httpx.Envelope	"UNSUPPORTED_IMAGE_TYPE or IMAGE_TOO_LARGE or VALIDATION_FAILED"
//	@Failure		429		{object}	httpx.Envelope	"RATE_LIMITED"
//	@Failure		503		{object}	httpx.Envelope	"IMAGE_STORAGE_NOT_CONFIGURED"
//	@Router			/storefront/uploads/cake-photo [post]
func (h *Handler) UploadCustomerPhoto(w http.ResponseWriter, r *http.Request) {
	ip := requestIP(r)
	if h.limiter != nil {
		if ok, err := h.limiter.Allow(r.Context(), "cake-photo:"+ip, customerPhotoLimit, customerPhotoWindow); err == nil && !ok {
			httpx.WriteError(w, r, apperr.RateLimited("You have uploaded a lot of photos. Please try again later."))
			return
		}
	}
	receiver, ok := h.media.(media.Receiver)
	if !ok || !h.media.Enabled() {
		httpx.WriteError(w, r, apperr.New("IMAGE_STORAGE_NOT_CONFIGURED", apperr.CategoryUpstream,
			"Photo uploads are not available right now."))
		return
	}

	// Multipart overhead on top of the image itself.
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxUploadBytes+64<<10)
	var src io.Reader = r.Body
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); strings.HasPrefix(ct, "multipart/") {
		file, _, err := r.FormFile("file")
		if err != nil {
			httpx.WriteError(w, r, uploadReadError(err))
			return
		}
		defer file.Close()
		src = file
	}
	data, err := io.ReadAll(io.LimitReader(src, media.MaxUploadBytes+1))
	if err != nil {
		httpx.WriteError(w, r, uploadReadError(err))
		return
	}
	if len(data) > media.MaxUploadBytes {
		httpx.WriteError(w, r, uploadError(media.ErrTooLarge))
		return
	}
	detected := http.DetectContentType(data)
	if base, _, err := mime.ParseMediaType(detected); err == nil {
		detected = base
	}
	if len(data) == 0 || !customerPhotoTypes[detected] {
		httpx.WriteError(w, r, apperr.New("UNSUPPORTED_IMAGE_TYPE", apperr.CategoryValidation,
			"That file is not a supported photo. Use a JPEG, PNG or WebP image."))
		return
	}

	ticket, err := h.media.CreateUploadTicket(r.Context())
	if err != nil {
		httpx.WriteError(w, r, imageStorageError(err))
		return
	}
	if err := receiver.Receive(r.Context(), ticket.ID, detected, bytes.NewReader(data)); err != nil {
		httpx.WriteError(w, r, uploadError(err))
		return
	}
	if err := h.svc.RecordCustomerPhoto(r.Context(), ticket.ID, ticket.DeliveryURL, detected, len(data), ip); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, customerPhotoDTO{ID: ticket.ID, URL: ticket.DeliveryURL})
}

// uploadReadError: a body over the limit, or a form without its file.
func uploadReadError(err error) error {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return uploadError(media.ErrTooLarge)
	}
	return apperr.Validation("Attach the photo as the \"file\" field, or send the image as the request body.")
}

// requestIP is the caller's address for throttling: the first
// X-Forwarded-For entry (what the edge sets), else the socket address.
func requestIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
