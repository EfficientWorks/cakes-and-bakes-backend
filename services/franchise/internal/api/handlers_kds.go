package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/httpx"
	"bakehouse/services/franchise/franchiseapi"
)

// ============================================================== central kitchens

// centralKitchenRequest is a Corporate Office being created or edited. Every
// field is optional on PATCH; on POST the address fields and Location ID are
// required.
type centralKitchenRequest struct {
	Name       *string `json:"name,omitempty" example:"BB East Coast Office"`
	BusinessID *string `json:"businessId,omitempty" example:"ECO001"`
	Street     *string `json:"street,omitempty" example:"24 Oak Tree Rd"`
	City       *string `json:"city,omitempty" example:"Edison"`
	State      *string `json:"state,omitempty" example:"NJ"`
	Postcode   *string `json:"postcode,omitempty" example:"08820"`
	Country    *string `json:"country,omitempty" example:"US"`
	Phone      *string `json:"phone,omitempty" example:"7329034600"`
	Email      *string `json:"email,omitempty" example:"eastcoastoffice@bakedbouquet.com"`
	Timezone   *string `json:"timezone,omitempty" example:"America/New_York"`
	// active or inactive.
	Status *string `json:"status,omitempty" example:"active"`

	ManagerName  *string `json:"managerName,omitempty" example:"Olivia Carter"`
	ManagerPhone *string `json:"managerPhone,omitempty" example:"7329034600"`
	ManagerEmail *string `json:"managerEmail,omitempty" example:"olivia@bakedbouquet.com"`

	// Store Assignment. Replaces the list when sent; omit to leave it.
	BranchIDs *[]string `json:"branchIds,omitempty"`
}

func (r centralKitchenRequest) toInput() franchiseapi.CentralKitchenInput {
	return franchiseapi.CentralKitchenInput{
		Name: r.Name, BusinessID: r.BusinessID, Street: r.Street, City: r.City, State: r.State,
		Postcode: r.Postcode, Country: r.Country, Phone: r.Phone, Email: r.Email, Timezone: r.Timezone,
		Status: r.Status, ManagerName: r.ManagerName, ManagerPhone: r.ManagerPhone,
		ManagerEmail: r.ManagerEmail, BranchIDs: r.BranchIDs,
	}
}

// @Summary		List corporate offices (central kitchens)
// @Tags			admin-central-kitchens
// @Produce		json
// @Security		BearerAuth
// @Param			search	query	string	false	"Name, Location ID, email, phone or manager"
// @Param			status	query	string	false	"active, inactive or all"
// @Param			page	query	int		false	"Page (default 1)"
// @Param			limit	query	int		false	"Page size (default 10, max 200)"
// @Success		200	{object}	httpx.Envelope{data=[]franchiseapi.CentralKitchen}
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/central-kitchens [get]
func (h *Handler) ListCentralKitchens(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 10
	}
	list, total, err := h.svc.ListCentralKitchens(r.Context(), staffClaims(r), franchiseapi.CentralKitchenFilter{
		Search: q.Get("search"), Status: q.Get("status"), Page: page, Limit: limit,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OKWithMeta(w, list, httpx.Meta{
		Page: page, PerPage: limit, Total: total, TotalPages: (total + limit - 1) / limit,
	})
}

// @Summary		Get a corporate office (central kitchen)
// @Tags			admin-central-kitchens
// @Produce		json
// @Security		BearerAuth
// @Param			centralKitchenId	path	string	true	"centralKitchenId"
// @Success		200	{object}	httpx.Envelope{data=franchiseapi.CentralKitchen}
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/central-kitchens/{centralKitchenId} [get]
func (h *Handler) GetCentralKitchen(w http.ResponseWriter, r *http.Request) {
	ck, err := h.svc.GetCentralKitchen(r.Context(), staffClaims(r), chi.URLParam(r, "centralKitchenId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, ck)
}

// @Summary		Create a corporate office (central kitchen)
// @Description	Creates the kitchen, its store assignment and default KDS settings. The Location ID must not be used by any store or other corporate office.
// @Tags			admin-central-kitchens
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	centralKitchenRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope{data=franchiseapi.CentralKitchen}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		409	{object}	httpx.Envelope	"LOCATION_ID_TAKEN"
// @Router			/admin/central-kitchens [post]
func (h *Handler) CreateCentralKitchen(w http.ResponseWriter, r *http.Request) {
	var req centralKitchenRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ck, err := h.svc.CreateCentralKitchen(r.Context(), staffClaims(r), req.toInput())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, ck)
}

// @Summary		Update a corporate office (central kitchen)
// @Description	Only the fields sent change. branchIds replaces the store assignment.
// @Tags			admin-central-kitchens
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			centralKitchenId	path	string	true	"centralKitchenId"
// @Param			body	body	centralKitchenRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope{data=franchiseapi.CentralKitchen}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Failure		409	{object}	httpx.Envelope	"LOCATION_ID_TAKEN"
// @Router			/admin/central-kitchens/{centralKitchenId} [patch]
func (h *Handler) UpdateCentralKitchen(w http.ResponseWriter, r *http.Request) {
	var req centralKitchenRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	ck, err := h.svc.UpdateCentralKitchen(r.Context(), staffClaims(r), chi.URLParam(r, "centralKitchenId"), req.toInput())
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, ck)
}

// @Summary		Delete a corporate office (central kitchen)
// @Description	Soft delete: the kitchen stops resolving on the KDS login and drops out of lists.
// @Tags			admin-central-kitchens
// @Produce		json
// @Security		BearerAuth
// @Param			centralKitchenId	path	string	true	"centralKitchenId"
// @Success		200	{object}	httpx.Envelope
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/central-kitchens/{centralKitchenId} [delete]
func (h *Handler) DeleteCentralKitchen(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "centralKitchenId")
	if err := h.svc.DeleteCentralKitchen(r.Context(), staffClaims(r), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"deleted": true, "id": id})
}

// ============================================================== KDS settings

// kdsSettingsRequest changes a location's KDS settings. Every field is
// optional; only what is sent changes.
type kdsSettingsRequest struct {
	// Turns on KDS-driven fulfilment at this location (see KDS flow doc).
	KDSEnabled            *bool `json:"kdsEnabled,omitempty"`
	ShowPickup            *bool `json:"showPickup,omitempty"`
	ShowDelivery          *bool `json:"showDelivery,omitempty"`
	ShowTakeaway          *bool `json:"showTakeaway,omitempty"`
	ShowOrdersBasedOnTime *bool `json:"showOrdersBasedOnTime,omitempty"`
	// Minutes before (slot start − longest item prep time) that an order appears.
	OrderAppearMinutes   *int    `json:"orderAppearMinutes,omitempty" example:"15"`
	PlaySoundOnNewTicket *bool   `json:"playSoundOnNewTicket,omitempty"`
	TicketLayout         *string `json:"ticketLayout,omitempty" example:"one_rail"`
	TicketColumns        *int    `json:"ticketColumns,omitempty" example:"4"`
	HeaderColorsEnabled  *bool   `json:"headerColorsEnabled,omitempty"`
	PickupColor          *string `json:"pickupColor,omitempty" example:"green"`
	DeliveryColor        *string `json:"deliveryColor,omitempty" example:"purple"`
	TakeawayColor        *string `json:"takeawayColor,omitempty" example:"blue"`
	DarkTheme            *bool   `json:"darkTheme,omitempty"`
	CalendarLowFrom      *int    `json:"calendarLowFrom,omitempty" example:"1"`
	CalendarLowTo        *int    `json:"calendarLowTo,omitempty" example:"3"`
	CalendarMediumFrom   *int    `json:"calendarMediumFrom,omitempty" example:"4"`
	CalendarMediumTo     *int    `json:"calendarMediumTo,omitempty" example:"8"`
	CalendarHighFrom     *int    `json:"calendarHighFrom,omitempty" example:"9"`
}

// @Summary		Get the KDS settings of the signed-in location
// @Description	A KDS signed in to a store or a central kitchen gets its own settings with no parameters. Brand-wide staff name the location with branchId or centralKitchenId.
// @Tags			kds
// @Produce		json
// @Security		BearerAuth
// @Param			branchId			query	string	false	"Store"
// @Param			centralKitchenId	query	string	false	"Central kitchen"
// @Success		200	{object}	httpx.Envelope{data=franchiseapi.KDSSettings}
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/kds/settings [get]
func (h *Handler) GetKDSSettings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	k, err := h.svc.GetKDSSettings(r.Context(), staffClaims(r), q.Get("branchId"), q.Get("centralKitchenId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, k)
}

// @Summary		Update KDS settings by id
// @Tags			kds
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			settingsId	path	string	true	"settingsId"
// @Param			body	body	kdsSettingsRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope{data=franchiseapi.KDSSettings}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/kds/settings/{settingsId} [patch]
func (h *Handler) UpdateKDSSettings(w http.ResponseWriter, r *http.Request) {
	var req kdsSettingsRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	k, err := h.svc.UpdateKDSSettings(r.Context(), staffClaims(r), chi.URLParam(r, "settingsId"), franchiseapi.KDSSettingsPatch{
		KDSEnabled: req.KDSEnabled, ShowPickup: req.ShowPickup, ShowDelivery: req.ShowDelivery,
		ShowTakeaway: req.ShowTakeaway, ShowOrdersBasedOnTime: req.ShowOrdersBasedOnTime,
		OrderAppearMinutes: req.OrderAppearMinutes, PlaySoundOnNewTicket: req.PlaySoundOnNewTicket,
		TicketLayout: req.TicketLayout, TicketColumns: req.TicketColumns,
		HeaderColorsEnabled: req.HeaderColorsEnabled, PickupColor: req.PickupColor,
		DeliveryColor: req.DeliveryColor, TakeawayColor: req.TakeawayColor, DarkTheme: req.DarkTheme,
		CalendarLowFrom: req.CalendarLowFrom, CalendarLowTo: req.CalendarLowTo,
		CalendarMediumFrom: req.CalendarMediumFrom, CalendarMediumTo: req.CalendarMediumTo,
		CalendarHighFrom: req.CalendarHighFrom,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, k)
}

// ============================================================== KDS printers

type createKDSPrinterRequest struct {
	// Optional for staff signed in to one location.
	BranchID         string `json:"branchId,omitempty"`
	CentralKitchenID string `json:"centralKitchenId,omitempty"`
	Name             string `json:"name" example:"Epson TM-T88VI (Kitchen)"`
	// wifi (default), lan or bluetooth.
	Kind          string `json:"kind,omitempty" example:"wifi"`
	IPAddress     string `json:"ipAddress,omitempty" example:"192.168.0.223"`
	Port          *int   `json:"port,omitempty" example:"9100"`
	DeviceAddress string `json:"deviceAddress,omitempty"`
	IsConnected   bool   `json:"isConnected,omitempty"`
}

type updateKDSPrinterRequest struct {
	Name          *string `json:"name,omitempty"`
	IPAddress     *string `json:"ipAddress,omitempty"`
	Port          *int    `json:"port,omitempty"`
	DeviceAddress *string `json:"deviceAddress,omitempty"`
	IsConnected   *bool   `json:"isConnected,omitempty"`
}

// @Summary		List KDS printers
// @Tags			kds
// @Produce		json
// @Security		BearerAuth
// @Param			branchId			query	string	false	"Store"
// @Param			centralKitchenId	query	string	false	"Central kitchen"
// @Param			kind				query	string	false	"wifi, lan or bluetooth"
// @Param			search				query	string	false	"Filter by name"
// @Success		200	{object}	httpx.Envelope{data=[]franchiseapi.KDSPrinter}
// @Router			/kds/printers [get]
func (h *Handler) ListKDSPrinters(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	out, err := h.svc.ListKDSPrinters(r.Context(), staffClaims(r), q.Get("branchId"), q.Get("centralKitchenId"), q.Get("kind"), q.Get("search"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, out)
}

// @Summary		Get a KDS printer
// @Tags			kds
// @Produce		json
// @Security		BearerAuth
// @Param			printerId	path	string	true	"printerId"
// @Success		200	{object}	httpx.Envelope{data=franchiseapi.KDSPrinter}
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/kds/printers/{printerId} [get]
func (h *Handler) GetKDSPrinter(w http.ResponseWriter, r *http.Request) {
	p, err := h.svc.GetKDSPrinter(r.Context(), staffClaims(r), chi.URLParam(r, "printerId"))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, p)
}

// @Summary		Add a KDS printer
// @Tags			kds
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	createKDSPrinterRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope{data=franchiseapi.KDSPrinter}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		409	{object}	httpx.Envelope	"PRINTER_NAME_TAKEN or PRINTER_IP_TAKEN"
// @Router			/kds/printers [post]
func (h *Handler) CreateKDSPrinter(w http.ResponseWriter, r *http.Request) {
	var req createKDSPrinterRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, err := h.svc.CreateKDSPrinter(r.Context(), staffClaims(r), req.BranchID, req.CentralKitchenID, franchiseapi.KDSPrinterInput{
		Name: &req.Name, Kind: req.Kind, IPAddress: &req.IPAddress, Port: req.Port,
		DeviceAddress: &req.DeviceAddress, IsConnected: &req.IsConnected,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, p)
}

// @Summary		Edit a KDS printer
// @Tags			kds
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			printerId	path	string	true	"printerId"
// @Param			body	body	updateKDSPrinterRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope{data=franchiseapi.KDSPrinter}
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/kds/printers/{printerId} [put]
func (h *Handler) UpdateKDSPrinter(w http.ResponseWriter, r *http.Request) {
	var req updateKDSPrinterRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	p, err := h.svc.UpdateKDSPrinter(r.Context(), staffClaims(r), chi.URLParam(r, "printerId"), franchiseapi.KDSPrinterInput{
		Name: req.Name, IPAddress: req.IPAddress, Port: req.Port, DeviceAddress: req.DeviceAddress, IsConnected: req.IsConnected,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, p)
}

// @Summary		Connect a KDS printer
// @Tags			kds
// @Produce		json
// @Security		BearerAuth
// @Param			printerId	path	string	true	"printerId"
// @Success		200	{object}	httpx.Envelope{data=franchiseapi.KDSPrinter}
// @Router			/kds/printers/{printerId}/connect [post]
func (h *Handler) ConnectKDSPrinter(w http.ResponseWriter, r *http.Request) {
	h.setKDSPrinterConnected(w, r, true)
}

// @Summary		Disconnect a KDS printer
// @Tags			kds
// @Produce		json
// @Security		BearerAuth
// @Param			printerId	path	string	true	"printerId"
// @Success		200	{object}	httpx.Envelope{data=franchiseapi.KDSPrinter}
// @Router			/kds/printers/{printerId}/disconnect [post]
func (h *Handler) DisconnectKDSPrinter(w http.ResponseWriter, r *http.Request) {
	h.setKDSPrinterConnected(w, r, false)
}

func (h *Handler) setKDSPrinterConnected(w http.ResponseWriter, r *http.Request, connected bool) {
	p, err := h.svc.SetKDSPrinterConnected(r.Context(), staffClaims(r), chi.URLParam(r, "printerId"), connected)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, p)
}

// @Summary		Remove a KDS printer
// @Tags			kds
// @Produce		json
// @Security		BearerAuth
// @Param			printerId	path	string	true	"printerId"
// @Success		200	{object}	httpx.Envelope
// @Router			/kds/printers/{printerId} [delete]
func (h *Handler) DeleteKDSPrinter(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "printerId")
	if err := h.svc.DeleteKDSPrinter(r.Context(), staffClaims(r), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]any{"deleted": true, "id": id})
}
