package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/httpx"
	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/domain"
)

type createPOSCustomerRequest struct {
	BranchID        string  `json:"branchId,omitempty"`
	FirstName       string  `json:"firstName,omitempty"`
	LastName        string  `json:"lastName,omitempty"`
	Name            string  `json:"name,omitempty"`
	Email           string  `json:"email,omitempty"`
	MobileNumber    string  `json:"mobileNumber" example:"9876543210"`
	Address         string  `json:"address,omitempty"`
	Street          string  `json:"street,omitempty"`
	City            string  `json:"city,omitempty"`
	State           string  `json:"state,omitempty"`
	Postcode        string  `json:"postcode,omitempty"`
	DOB             *string `json:"dob,omitempty"`
	AnniversaryDate *string `json:"anniversaryDate,omitempty"`
}

type updatePOSCustomerRequest struct {
	FirstName string `json:"firstName,omitempty"`
	// Key absent keeps the current last name; "" or null clears it.
	LastName     httpx.OptionalString `json:"lastName,omitempty" swaggertype:"string"`
	MobileNumber string               `json:"mobileNumber,omitempty"`
	Phone        string               `json:"phone,omitempty"`
	// Key absent keeps the current email; "" or null clears it. Must not
	// belong to another customer.
	Email           httpx.OptionalString `json:"email,omitempty" swaggertype:"string"`
	DOB             *string              `json:"dob"`
	AnniversaryDate *string              `json:"anniversaryDate"`
	Postcode        *string              `json:"postcode,omitempty"`
	Address         *string              `json:"address,omitempty"`
	Street          *string              `json:"street,omitempty"`
	City            *string              `json:"city,omitempty"`
	State           *string              `json:"state,omitempty"`
}

type posCustomerDTOType struct {
	ID              string  `json:"id"`
	BranchID        *string `json:"branchId,omitempty"`
	CustomerType    string  `json:"customerType"`
	FirstName       string  `json:"firstName"`
	LastName        string  `json:"lastName"`
	Email           *string `json:"email"`
	MobileNumber    *string `json:"mobileNumber,omitempty"`
	Address         *string `json:"address,omitempty"`
	Street          *string `json:"street,omitempty"`
	City            *string `json:"city,omitempty"`
	State           *string `json:"state,omitempty"`
	Postcode        *string `json:"postcode,omitempty"`
	DOB             *string `json:"dob"`
	AnniversaryDate *string `json:"anniversaryDate"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
}

func posCustomerDTO(c *identityapi.Customer) posCustomerDTOType {
	var emailPtr *string
	if strings.TrimSpace(c.Email) != "" {
		trimmed := strings.TrimSpace(c.Email)
		emailPtr = &trimmed
	}
	var dob *string
	if c.DateOfBirth != nil {
		s := c.DateOfBirth.Format("2006-01-02")
		dob = &s
	}
	var anniversaryDate *string
	if c.AnniversaryDate != nil {
		s := c.AnniversaryDate.Format("2006-01-02")
		anniversaryDate = &s
	}
	return posCustomerDTOType{
		ID:              c.ID,
		BranchID:        c.BranchID,
		CustomerType:    c.CustomerType,
		FirstName:       c.FirstName,
		LastName:        c.LastName,
		Email:           emailPtr,
		MobileNumber:    c.MobileNumber,
		Address:         c.Address,
		Street:          c.Street,
		City:            c.City,
		State:           c.State,
		Postcode:        c.Postcode,
		DOB:             dob,
		AnniversaryDate: anniversaryDate,
		CreatedAt:       c.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       c.UpdatedAt.Format(time.RFC3339),
	}
}

// @Summary		Create a POS walk-in customer
// @Description	Creates a customer in the customers table with customer_type = 'pos' and branch_id set to the current branch. No password is created, and no welcome email is sent.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body		createPOSCustomerRequest	true	"Request body"
// @Success		201		{object}	httpx.Envelope{data=posCustomerDTOType}
// @Failure		400		{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		403		{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		409		{object}	httpx.Envelope	"EMAIL_ALREADY_EXISTS"
// @Router			/pos/customers [post]
func (h *Handler) CreatePOSCustomer(w http.ResponseWriter, r *http.Request) {
	var req createPOSCustomerRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	cust, err := h.svc.CreatePOSCustomer(r.Context(), staffClaims(r), domain.CreatePOSCustomerInput{
		BranchID:        req.BranchID,
		FirstName:       req.FirstName,
		LastName:        req.LastName,
		Name:            req.Name,
		Email:           req.Email,
		MobileNumber:    req.MobileNumber,
		Address:         req.Address,
		Street:          req.Street,
		City:            req.City,
		State:           req.State,
		Postcode:        req.Postcode,
		DateOfBirth:     req.DOB,
		AnniversaryDate: req.AnniversaryDate,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	httpx.Created(w, posCustomerDTO(cust))
}

// @Summary		List POS customers for the current branch
// @Description	Returns POS customers (customer_type = 'pos') belonging to the current logged-in user's branch.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			branchId	query		string	false	"Branch ID (defaults to current token's branch)"
// @Param			search		query		string	false	"Search by name, email, or mobile"
// @Param			page		query		int		false	"Page number (default 1)"
// @Param			limit		query		int		false	"Page size (default 25)"
// @Success		200			{object}	httpx.Envelope{data=[]posCustomerDTOType,meta=httpx.Meta}
// @Failure		401			{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/pos/customers [get]
func (h *Handler) ListPOSCustomers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page, _ := strconv.Atoi(q.Get("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 25
	}

	customers, total, err := h.svc.ListPOSCustomers(r.Context(), staffClaims(r), q.Get("branchId"), q.Get("search"), page, limit)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	out := make([]posCustomerDTOType, 0, len(customers))
	for _, c := range customers {
		out = append(out, posCustomerDTO(&c))
	}

	totalPages := 0
	if limit > 0 {
		totalPages = (total + limit - 1) / limit
	}

	httpx.OKWithMeta(w, out, httpx.Meta{
		Page:       page,
		PerPage:    limit,
		Total:      total,
		TotalPages: totalPages,
	})
}

// @Summary		Get a POS customer by ID
// @Description	Gets a POS customer by ID, ensuring they belong to the current logged-in user's branch and have customer_type = 'pos'.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			customerId	path		string	true	"Customer ID"
// @Success		200			{object}	httpx.Envelope{data=posCustomerDTOType}
// @Failure		401			{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404			{object}	httpx.Envelope	"CUSTOMER_NOT_FOUND"
// @Router			/pos/customers/{customerId} [get]
func (h *Handler) GetPOSCustomer(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "customerId")
	if customerID == "" {
		customerID = chi.URLParam(r, "customer_id")
	}

	cust, err := h.svc.GetPOSCustomer(r.Context(), staffClaims(r), strings.TrimSpace(customerID))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	httpx.OK(w, posCustomerDTO(cust))
}

// @Summary		Update a POS customer
// @Description	Updates name, email, mobile number, address and dates for an existing POS customer. Omitted fields keep their value. email must not belong to another customer (409 EMAIL_ALREADY_EXISTS).
// @Tags			pos
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			customerId	path		string						true	"Customer ID"
// @Param			body		body		updatePOSCustomerRequest	true	"Request body"
// @Success		200			{object}	httpx.Envelope{data=posCustomerDTOType}
// @Failure		400			{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401			{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404			{object}	httpx.Envelope	"CUSTOMER_NOT_FOUND"
// @Failure		409			{object}	httpx.Envelope	"EMAIL_ALREADY_EXISTS"
// @Router			/pos/customers/{customerId} [put]
func (h *Handler) UpdatePOSCustomer(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "customerId")
	if customerID == "" {
		customerID = chi.URLParam(r, "customer_id")
	}

	var req updatePOSCustomerRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	cust, err := h.svc.UpdatePOSCustomer(r.Context(), staffClaims(r), strings.TrimSpace(customerID), domain.UpdatePOSCustomerInput{
		FirstName:       req.FirstName,
		LastName:        req.LastName.Ptr(),
		MobileNumber:    req.MobileNumber,
		Phone:           req.Phone,
		Email:           req.Email.Ptr(),
		DateOfBirth:     req.DOB,
		AnniversaryDate: req.AnniversaryDate,
		Postcode:        req.Postcode,
		City:            req.City,
		State:           req.State,
		Address:         req.Address,
		Street:          req.Street,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	httpx.OK(w, posCustomerDTO(cust))
}

// @Summary		Delete a POS customer by ID
// @Description	Soft-deletes a POS customer by ID, ensuring they belong to the current logged-in user's branch.
// @Tags			pos
// @Produce		json
// @Security		BearerAuth
// @Param			customerId	path		string	true	"Customer ID"
// @Success		200			{object}	httpx.Envelope
// @Failure		401			{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403			{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404			{object}	httpx.Envelope	"CUSTOMER_NOT_FOUND"
// @Router			/pos/customers/{customerId} [delete]
func (h *Handler) DeletePOSCustomer(w http.ResponseWriter, r *http.Request) {
	customerID := chi.URLParam(r, "customerId")
	if customerID == "" {
		customerID = chi.URLParam(r, "customer_id")
	}

	if err := h.svc.DeletePOSCustomer(r.Context(), staffClaims(r), strings.TrimSpace(customerID)); err != nil {
		httpx.WriteError(w, r, err)
		return
	}

	httpx.OK(w, map[string]bool{"success": true})
}
