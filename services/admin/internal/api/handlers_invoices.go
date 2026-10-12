package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/httpx"
	"bakehouse/services/admin/adminapi"
)

// @Summary		List invoices
// @Description	Paged in the database, newest first. Search matches the reference, the billed party and the store. A branch-scoped account only ever sees its own, whatever it asks for.
// @Tags			admin-invoices
// @Produce		json
// @Security		BearerAuth
// @Param			search		query		string	false	"Reference, customer or store"
// @Param			kind		query		string	false	"customer or store"
// @Param			status		query		string	false	"unpaid, paid or void"
// @Param			branchId	query		string	false	"Supplying store"
// @Param			page		query		int		false	"Page number (default 1)"
// @Param			limit		query		int		false	"Page size (default 10, max 100)"
// @Success		200			{object}	httpx.Envelope
// @Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/invoices [get]
func (h *Handler) ListInvoices(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))

	list, total, err := h.svc.ListInvoices(r.Context(), staffClaims(r), adminapi.InvoiceListFilter{
		Search:   q.Get("search"),
		Kind:     q.Get("kind"),
		Status:   q.Get("status"),
		BranchID: q.Get("branchId"),
		Page:     page,
		Limit:    limit,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 10
	}
	httpx.OKWithMeta(w, list, map[string]int{
		"page": page, "perPage": limit, "total": total,
		"totalPages": (total + limit - 1) / limit,
	})
}

// @Summary		Get one invoice
// @Tags			admin-invoices
// @Produce		json
// @Security		BearerAuth
// @Param			invoiceId	path		string	true	"invoiceId"
// @Success		200			{object}	httpx.Envelope
// @Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/invoices/{invoiceId} [get]
func (h *Handler) GetInvoice(w http.ResponseWriter, r *http.Request) {
	inv, err := h.svc.GetInvoice(r.Context(), staffClaims(r), chi.URLParam(r, "invoiceId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	// disclaimer := h.svc.GetContentByKey(r.Context(), "disclaimer")
	// if disclaimer != nil {
	// 	inv.Disclaimer = disclaimer.Value
	// }
	// inv.Disclaimer = disclaimer.Value

	httpx.OK(w, inv)
}

// @Summary		Raise an invoice
// @Description	For a customer or for another store. Totals are computed here from the lines and the supplying store's tax rate — the console's preview follows the server, not the other way round. Set send=true to email it on creation.
// @Tags			admin-invoices
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body		adminapi.CreateInvoiceInput	true	"Request body"
// @Success		201		{object}	httpx.Envelope
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/invoices [post]
func (h *Handler) CreateInvoice(w http.ResponseWriter, r *http.Request) {
	var req adminapi.CreateInvoiceInput
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	inv, err := h.svc.CreateInvoice(r.Context(), staffClaims(r), req)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, inv)
}

type invoiceStatusRequest struct {
	// unpaid, paid or void.
	Status string `json:"status"`
	// When it was paid (used with status=paid). Optional; defaults to now.
	PaidAt *string `json:"paidAt,omitempty" example:"2026-09-16T10:33:52Z"`
}

type sendInvoiceRequest struct {
	// When it was sent. Optional; defaults to now.
	SentAt *string `json:"sentAt,omitempty" example:"2026-09-16T10:33:52Z"`
}

// @Summary		Settle or reopen an invoice
// @Description	"Close invoice" on the detail screen sends status=paid. Money arriving by bank transfer is not something the system can observe, so somebody has to say so.
// @Tags			admin-invoices
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			invoiceId	path		string					true	"invoiceId"
// @Param			body		body		invoiceStatusRequest	true	"Request body"
// @Success		200			{object}	httpx.Envelope
// @Failure		400			{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/invoices/{invoiceId}/status [post]
func (h *Handler) SetInvoiceStatus(w http.ResponseWriter, r *http.Request) {
	var req invoiceStatusRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	r, err := httpx.WithEventTime(r, "paidAt", req.PaidAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	inv, err := h.svc.SetInvoiceStatus(r.Context(), staffClaims(r), chi.URLParam(r, "invoiceId"), req.Status)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, inv)
}

// @Summary		Send an invoice
// @Description	Emails it to whoever it bills. Useful for resending, or for an invoice raised without send=true.
// @Tags			admin-invoices
// @Produce		json
// @Security		BearerAuth
// @Param			invoiceId	path		string	true	"invoiceId"
// @Success		200			{object}	httpx.Envelope
// @Failure		400			{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
// @Param			body	body	sendInvoiceRequest	false	"Optional: sentAt"
// @Router			/admin/invoices/{invoiceId}/send [post]
func (h *Handler) SendInvoice(w http.ResponseWriter, r *http.Request) {
	// Optional body: {"sentAt": "..."}.
	var req sendInvoiceRequest
	_ = httpx.DecodeJSON(r, &req)
	r, err := httpx.WithEventTime(r, "sentAt", req.SentAt)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	inv, err := h.svc.SendInvoice(r.Context(), staffClaims(r), chi.URLParam(r, "invoiceId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, inv)
}

// MountAdminInvoices attaches the invoice routes onto the shared /admin router.
func MountAdminInvoices(admin chi.Router, h *Handler) {
	admin.Route("/invoices", func(r chi.Router) {
		r.Get("/", h.ListInvoices)
		r.Post("/", h.CreateInvoice)
		r.Get("/{invoiceId}", h.GetInvoice)
		r.Post("/{invoiceId}/status", h.SetInvoiceStatus)
		r.Post("/{invoiceId}/send", h.SendInvoice)
	})
}
