package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/httpx"
	"bakehouse/services/franchise/franchiseapi"
)

// createPrinterRequest adds a printer to an outlet.
type createPrinterRequest struct {
	// Optional: a till signed in to one outlet configures that one.
	BranchID string `json:"branchId,omitempty"`
	Name     string `json:"name"`
	// wifi or bluetooth.
	Kind string `json:"kind"`
	// Required for wifi.
	IPAddress string `json:"ipAddress,omitempty"`
	// Required for bluetooth.
	DeviceAddress string `json:"deviceAddress,omitempty"`
	// Connect it as it is added. For bluetooth this disconnects whichever
	// device the outlet was paired with.
	IsConnected bool `json:"isConnected,omitempty"`
}

// updatePrinterRequest edits a printer. Every field is optional — an absent
// one is left as it was, so renaming a printer cannot blank its address.
type updatePrinterRequest struct {
	Name          *string `json:"name,omitempty"`
	IPAddress     *string `json:"ipAddress,omitempty"`
	DeviceAddress *string `json:"deviceAddress,omitempty"`
	IsConnected   *bool   `json:"isConnected,omitempty"`
}

// @Summary		List the outlet's printers
// @Description	Wi-Fi and Bluetooth printers configured at one outlet. An outlet runs several Wi-Fi printers at once but pairs with only one Bluetooth device.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query		string	false	"Outlet (defaults to the token's own)"
// @Param			kind		query		string	false	"wifi or bluetooth"
// @Param			search		query		string	false	"Filter by name"
// @Success		200			{object}	httpx.Envelope
// @Failure		400			{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/pos/printers [get]
func (h *Handler) ListPrinters(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := h.svc.ListPrinters(r.Context(), staffClaims(r), franchiseapi.PrinterFilter{
		BranchID: q.Get("branchId"), Kind: q.Get("kind"), Search: q.Get("search"),
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, out)
}

// @Summary		Get one printer
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			printerId	path		string	true	"printerId"
// @Success		200			{object}	httpx.Envelope
// @Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/printers/{printerId} [get]
func (h *Handler) GetPrinter(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.GetPrinter(r.Context(), staffClaims(r), chi.URLParam(r, "printerId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, p)
}

// @Summary		Add a printer
// @Description	Names are unique per outlet, as are Wi-Fi IP addresses. Connecting a Bluetooth printer disconnects the outlet's previous one.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body		createPrinterRequest	true	"Request body"
// @Success		201		{object}	httpx.Envelope
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		409		{object}	httpx.Envelope	"PRINTER_NAME_TAKEN or PRINTER_IP_TAKEN"
// @Router			/pos/printers [post]
func (h *Handler) CreatePrinter(w http.ResponseWriter, r *http.Request) {
	var req createPrinterRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, err := h.svc.CreatePrinter(r.Context(), staffClaims(r), franchiseapi.PrinterInput{
		BranchID: req.BranchID, Name: &req.Name, Kind: req.Kind,
		IPAddress: &req.IPAddress, DeviceAddress: &req.DeviceAddress,
		IsConnected: &req.IsConnected,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, p)
}

// @Summary		Edit a printer
// @Description	Only the fields sent are changed. A printer's kind is fixed once created.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			printerId	path		string					true	"printerId"
// @Param			body		body		updatePrinterRequest	true	"Request body"
// @Success		200			{object}	httpx.Envelope
// @Failure		400			{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
// @Failure		409			{object}	httpx.Envelope	"PRINTER_NAME_TAKEN or PRINTER_IP_TAKEN"
// @Router			/pos/printers/{printerId} [put]
func (h *Handler) UpdatePrinter(w http.ResponseWriter, r *http.Request) {
	var req updatePrinterRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, err := h.svc.UpdatePrinter(r.Context(), staffClaims(r), chi.URLParam(r, "printerId"),
		franchiseapi.PrinterInput{
			Name: req.Name, IPAddress: req.IPAddress,
			DeviceAddress: req.DeviceAddress, IsConnected: req.IsConnected,
		})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, p)
}

// @Summary		Connect a printer
// @Description	For a Bluetooth printer this disconnects whichever one the outlet was paired with — a till talks to one device at a time. Wi-Fi printers have no such limit.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			printerId	path		string	true	"printerId"
// @Success		200			{object}	httpx.Envelope
// @Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/printers/{printerId}/connect [post]
func (h *Handler) ConnectPrinter(w http.ResponseWriter, r *http.Request) {
	h.setPrinterConnected(w, r, true)
}

// @Summary		Disconnect a printer
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			printerId	path		string	true	"printerId"
// @Success		200			{object}	httpx.Envelope
// @Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/printers/{printerId}/disconnect [post]
func (h *Handler) DisconnectPrinter(w http.ResponseWriter, r *http.Request) {
	h.setPrinterConnected(w, r, false)
}

func (h *Handler) setPrinterConnected(w http.ResponseWriter, r *http.Request, connected bool) {
	p, err := h.svc.SetPrinterConnected(r.Context(), staffClaims(r), chi.URLParam(r, "printerId"), connected)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, p)
}

// @Summary		Remove a printer
// @Description	Permanent. A printer that has been thrown away should not linger on a settings screen.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			printerId	path		string	true	"printerId"
// @Success		200			{object}	httpx.Envelope
// @Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404			{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/pos/printers/{printerId} [delete]
func (h *Handler) DeletePrinter(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "printerId")
	if err := h.svc.DeletePrinter(r.Context(), staffClaims(r), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"deleted": true, "id": id})
}
