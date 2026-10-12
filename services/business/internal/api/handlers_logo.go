package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/media"
	"bakehouse/services/business/businessapi"
)

// UseMedia sets where uploaded logos are stored: the same store as product
// images (R2 in production).
func (h *Handler) UseMedia(store media.Storer) { h.media = store }

// UploadLogo godoc
//
//	@Summary		Upload a business's logo
//	@Description	Stores the image (PNG, JPEG, WebP or AVIF, up to 10 MB; checked and resized like product photos) and makes it the business's logo on its shop, emails and invoices. A transparent PNG of the wordmark works best.
//	@Tags			platform
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		PlatformKey
//	@Param			businessId	path		string	true	"Business id"
//	@Param			file		formData	file	true	"The logo image"
//	@Success		200			{object}	httpx.Envelope{data=businessDTO}
//	@Failure		400			{object}	httpx.Envelope	"VALIDATION_FAILED — not an image, or too large"
//	@Failure		401			{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
//	@Failure		503			{object}	httpx.Envelope	"MEDIA_NOT_CONFIGURED"
//	@Router			/platform/businesses/{businessId}/logo [post]
func (h *Handler) UploadLogo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "businessId")
	if _, err := h.svc.GetBusiness(r.Context(), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	receiver, ok := h.media.(media.Receiver)
	if h.media == nil || !h.media.Enabled() || !ok {
		httpx.WriteError(w, r, apperr.New("MEDIA_NOT_CONFIGURED", apperr.CategoryUpstream,
			"Image uploads are not set up on this server. Set logoUrl to an https image address instead."))
		return
	}

	// The cap is enforced on what is read, not on the Content-Length a
	// client declares.
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxUploadBytes+(1<<20))
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.WriteError(w, r, apperr.Validation("Attach the logo as the \"file\" field (an image up to 10 MB).").
			WithFields(apperr.FieldError{Field: "file", Code: "REQUIRED", Message: "An image up to 10 MB."}))
		return
	}
	defer file.Close()

	ticket, err := h.media.CreateUploadTicket(r.Context())
	if err != nil {
		httpx.WriteError(w, r, apperr.Internal(err))
		return
	}
	// Receive checks the bytes are an image (not the declared type) and
	// resizes it before storing.
	if err := receiver.Receive(r.Context(), ticket.ID, header.Header.Get("Content-Type"), file); err != nil {
		httpx.WriteError(w, r, apperr.Validation("That file is not an image we can use (PNG, JPEG, WebP or AVIF).").
			WithFields(apperr.FieldError{Field: "file", Code: "INVALID", Message: "PNG, JPEG, WebP or AVIF."}))
		return
	}
	logo := ticket.DeliveryURL
	b, err := h.svc.UpdateBusiness(r.Context(), id, businessapi.BusinessPatch{LogoURL: &logo})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, toBusinessDTO(b))
}
