package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/media"
)

/*
 * Product video upload and delivery.
 *
 * The same two-step shape as images: ask for a ticket, post the bytes to the
 * ticket's uploadUrl, then save the ticket's deliveryUrl on the product as
 * videoUrl. A product has at most one video.
 */

// videoTicketResponse is what the admin needs to upload one video.
type videoTicketResponse struct {
	VideoID string `json:"videoId"`
	// Where the admin posts the raw file (staff auth).
	UploadURL string `json:"uploadUrl"`
	// The value to save on the product as videoUrl.
	DeliveryURL string `json:"deliveryUrl"`
	// Largest accepted file, in bytes.
	MaxBytes int64 `json:"maxBytes"`
}

func (h *Handler) videoStoreOr503(w http.ResponseWriter, r *http.Request) bool {
	if h.videos == nil {
		httpx.WriteError(w, r, apperr.New("VIDEO_STORAGE_NOT_CONFIGURED", apperr.CategoryUpstream,
			"Video uploads are not set up. Set PUBLIC_BASE_URL."))
		return false
	}
	return true
}

// CreateVideoUploadTicket issues an upload URL for one product video.
//
//	@Summary		Get a product video upload URL
//	@Description	Returns a single-use uploadUrl (POST the raw file to it, staff auth) and the deliveryUrl to save on the product as videoUrl. MP4, MOV or WebM, up to 50 MB. A product has one video.
//	@Tags			admin-catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Success		201	{object}	httpx.Envelope{data=videoTicketResponse}
//	@Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Failure		503	{object}	httpx.Envelope	"VIDEO_STORAGE_NOT_CONFIGURED"
//	@Router			/admin/videos/upload-url [post]
func (h *Handler) CreateVideoUploadTicket(w http.ResponseWriter, r *http.Request) {
	if !h.videoStoreOr503(w, r) {
		return
	}
	ticket, err := h.videos.CreateUploadTicket(r.Context())
	if err != nil {
		httpx.WriteError(w, r, apperr.Internal(err))
		return
	}
	httpx.Created(w, videoTicketResponse{
		VideoID: ticket.ID, UploadURL: ticket.UploadURL, DeliveryURL: ticket.DeliveryURL, MaxBytes: media.MaxVideoBytes,
	})
}

// UploadVideoContent receives the video bytes for a ticket.
//
//	@Summary		Upload product video bytes
//	@Description	The raw file as the request body (not multipart), for a videoId from /admin/videos/upload-url. MP4, MOV or WebM (checked from the file itself), up to 50 MB.
//	@Tags			admin-catalog
//	@Accept			octet-stream
//	@Produce		json
//	@Security		BearerAuth
//	@Param			videoId	path	string	true	"Video id from the upload ticket"
//	@Success		204	"No Content"
//	@Failure		400	{object}	httpx.Envelope	"UNSUPPORTED_VIDEO_TYPE, VIDEO_TOO_LARGE or INVALID_VIDEO_ID"
//	@Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Router			/admin/videos/{videoId}/content [post]
func (h *Handler) UploadVideoContent(w http.ResponseWriter, r *http.Request) {
	if !h.videoStoreOr503(w, r) {
		return
	}
	body := http.MaxBytesReader(w, r.Body, media.MaxVideoBytes+1)
	defer body.Close()
	if err := h.videos.Receive(r.Context(), chi.URLParam(r, "videoId"), body); err != nil {
		var tooBig *http.MaxBytesError
		switch {
		case errors.Is(err, media.ErrVideoTooLarge), errors.As(err, &tooBig):
			httpx.WriteError(w, r, apperr.Wrap(err, "VIDEO_TOO_LARGE", apperr.CategoryValidation,
				"That video is larger than 50 MB. Please upload a shorter or smaller file."))
		case errors.Is(err, media.ErrUnsupportedVideo):
			httpx.WriteError(w, r, apperr.Wrap(err, "UNSUPPORTED_VIDEO_TYPE", apperr.CategoryValidation,
				"That file is not a supported video. Use MP4, MOV or WebM."))
		case errors.Is(err, media.ErrInvalidID):
			httpx.WriteError(w, r, apperr.Wrap(err, "INVALID_VIDEO_ID", apperr.CategoryValidation,
				"That upload link is not valid. Request a new one."))
		default:
			httpx.WriteError(w, r, apperr.Wrap(err, "VIDEO_UPLOAD_FAILED", apperr.CategoryUpstream,
				"The video could not be stored. Please try again."))
		}
		return
	}
	httpx.NoContent(w)
}

// DeleteVideo removes a stored video. Detach it from the product first
// (PATCH the product with videoUrl ""), as with images.
//
//	@Summary		Delete a product video
//	@Tags			admin-catalog
//	@Produce		json
//	@Security		BearerAuth
//	@Param			videoId	path	string	true	"Video id"
//	@Success		204	"No Content"
//	@Router			/admin/videos/{videoId} [delete]
func (h *Handler) DeleteVideo(w http.ResponseWriter, r *http.Request) {
	if !h.videoStoreOr503(w, r) {
		return
	}
	if err := h.videos.Delete(r.Context(), chi.URLParam(r, "videoId")); err != nil {
		if errors.Is(err, media.ErrInvalidID) {
			httpx.WriteError(w, r, apperr.Validation("That video id is not valid."))
			return
		}
		httpx.WriteError(w, r, apperr.Internal(err))
		return
	}
	httpx.NoContent(w)
}

// ServeVideo streams a stored video, honouring Range requests.
//
//	@Summary		Serve a product video
//	@Description	Public. Supports Range requests (needed for playback and seeking).
//	@Tags			storefront
//	@Produce		video/mp4
//	@Param			videoId	path	string	true	"Video id"
//	@Success		200	{file}	binary
//	@Success		206	{file}	binary
//	@Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
//	@Router			/media/videos/{videoId} [get]
func (h *Handler) ServeVideo(w http.ResponseWriter, r *http.Request) {
	if h.videos == nil {
		httpx.WriteError(w, r, apperr.NotFound("Video not found."))
		return
	}
	id := chi.URLParam(r, "videoId")
	// Stored in R2: the file is served from its public host, which handles
	// Range requests itself. Old links that point here are redirected.
	if public, ok := h.videos.(media.PublicDelivery); ok {
		http.Redirect(w, r, public.PublicURL(id), http.StatusMovedPermanently)
		return
	}
	db, ok := h.videos.(*media.VideoStore)
	if !ok {
		httpx.WriteError(w, r, apperr.NotFound("Video not found."))
		return
	}
	info, err := db.Stat(r.Context(), id)
	if err != nil {
		httpx.WriteError(w, r, apperr.NotFound("Video not found."))
		return
	}
	// Set before ServeContent so it does not read the start of the file to
	// sniff a type. An id is never reused, so the bytes behind it never change.
	w.Header().Set("Content-Type", info.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, "", info.ModTime, db.NewReader(r.Context(), id, info.Size))
}
