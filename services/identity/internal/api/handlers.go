package api

import (
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/httpx"
	"bakehouse/pkg/tenant"
	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/domain"
)

type Handler struct {
	svc *domain.Service
}

func NewHandler(svc *domain.Service) *Handler { return &Handler{svc: svc} }

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.TrimSpace(strings.Split(fwd, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// --- Customer registration & verification ----------------------------------

type registerStep1Request struct {
	Email           string `json:"email"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirmPassword"`
}

// @Summary		Start registration (email and password)
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			body	body	registerStep1Request	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/auth/register [post]
func (h *Handler) RegisterStep1(w http.ResponseWriter, r *http.Request) {
	var req registerStep1Request
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	token, err := h.svc.RegisterStep1(r.Context(), req.Email, req.Password, req.ConfirmPassword)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, map[string]interface{}{
		"message":           "OTP sent to email. Please verify your account.",
		"registrationToken": token,
	})
}

type registerStep2Request struct {
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	MobileNumber string `json:"mobileNumber"`
}

// @Summary		Complete registration step 2 (profile)
// @Tags			auth
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	registerStep2Request	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		409	{object}	httpx.Envelope	"MOBILE_ALREADY_EXISTS — the mobile number is on another account"
// @Router			/auth/register/profile [post]
func (h *Handler) RegisterStep2(w http.ResponseWriter, r *http.Request) {
	token, ok := bearer(r)
	if !ok {
		httpx.WriteError(w, r, apperr.Unauthenticated("Missing registration token."))
		return
	}
	var req registerStep2Request
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.RegisterStep2(r.Context(), token, req.FirstName, req.LastName, req.MobileNumber); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]string{"message": "Profile saved. Complete the address step or verify your email."})
}

type registerStep3Request struct {
	Country  string `json:"country"`
	State    string `json:"state"`
	City     string `json:"city"`
	Postcode string `json:"postcode"`
	Address  string `json:"address"`
}

// @Summary		Complete registration step 3 (address)
// @Tags			auth
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	registerStep3Request	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/auth/register/address [post]
func (h *Handler) RegisterStep3(w http.ResponseWriter, r *http.Request) {
	token, ok := bearer(r)
	if !ok {
		httpx.WriteError(w, r, apperr.Unauthenticated("Missing registration token."))
		return
	}
	var req registerStep3Request
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pair, customer, err := h.svc.RegisterStep3(r.Context(), token, identityapi.CustomerAddress{
		Street: req.Address, City: req.City, State: req.State, Postcode: req.Postcode, Country: req.Country,
	})
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{
		"customerId":   customer.ID,
		"accessToken":  pair.AccessToken,
		"refreshToken": pair.RefreshToken,
	})
}

type verifyOTPRequest struct {
	Email string `json:"email"`
	OTP   string `json:"otp"`
}

// @Summary		Verify the account OTP
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			body	body	verifyOTPRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/auth/verify-otp [post]
func (h *Handler) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	var req verifyOTPRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pair, customer, err := h.svc.VerifyOTP(r.Context(), req.Email, req.OTP)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{
		"accessToken":  pair.AccessToken,
		"refreshToken": pair.RefreshToken,
		"customer":     customerDTO(customer),
	})
}

type resendOTPRequest struct {
	Email string `json:"email"`
}

// @Summary		Resend the account OTP
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			body	body	resendOTPRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/auth/resend-otp [post]
func (h *Handler) ResendOTP(w http.ResponseWriter, r *http.Request) {
	var req resendOTPRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.ResendOTP(r.Context(), req.Email); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]string{"message": "OTP resent."})
}

// --- Login / session ---------------------------------------------------------

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// @Summary		Log in as a customer
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			body	body	loginRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/auth/login [post]
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pair, customer, err := h.svc.Login(r.Context(), req.Email, req.Password, clientIP(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{
		"accessToken":  pair.AccessToken,
		"refreshToken": pair.RefreshToken,
		"customer":     customerDTO(customer),
	})
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// @Summary		Exchange a customer refresh token
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			body	body	refreshRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/auth/refresh [post]
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pair, err := h.svc.RefreshCustomer(r.Context(), req.RefreshToken)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{"accessToken": pair.AccessToken, "refreshToken": pair.RefreshToken})
}

// @Summary		Revoke a customer refresh token
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			body	body	refreshRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/auth/logout [post]
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = h.svc.LogoutCustomer(r.Context(), req.RefreshToken)
	httpx.OK(w, map[string]bool{"success": true})
}

// --- Password reset -----------------------------------------------------------

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

// @Summary		Request a password-reset OTP
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			body	body	forgotPasswordRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/auth/forgot-password [post]
func (h *Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.ForgotPassword(r.Context(), req.Email); err != nil {
		slog.ErrorContext(r.Context(), "forgot password email failed", "error", err)
	}
	httpx.OK(w, map[string]string{"message": "Reset OTP sent to email, if that address is registered."})
}

type verifyResetOTPRequest struct {
	Email string `json:"email"`
	OTP   string `json:"otp"`
}

// @Summary		Verify a password-reset OTP
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			body	body	verifyResetOTPRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/auth/verify-reset-otp [post]
func (h *Handler) VerifyResetOTP(w http.ResponseWriter, r *http.Request) {
	var req verifyResetOTPRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	token, err := h.svc.VerifyResetOTP(r.Context(), req.Email, req.OTP)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]string{"resetToken": token})
}

type resetPasswordRequest struct {
	Email           string `json:"email"`
	ResetToken      string `json:"resetToken"`
	NewPassword     string `json:"newPassword"`
	ConfirmPassword string `json:"confirmPassword"`
}

// @Summary		Set a new password using a reset token
// @Tags			auth
// @Accept			json
// @Produce		json
// @Param			body	body	resetPasswordRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/auth/reset-password [post]
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.ResetPasswordForEmail(r.Context(), req.Email, req.ResetToken, req.NewPassword, req.ConfirmPassword); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]string{"message": "Password updated. Please log in."})
}

// --- Account self-service ----------------------------------------------------

// @Summary		Get the signed-in customer profile
// @Tags			account
// @Produce		json
// @Security		BearerAuth
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account/profile [get]
func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	c, err := h.svc.GetProfile(r.Context(), claims.CustomerID)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, customerDTO(c))
}

type updateProfileRequest struct {
	FirstName    string `json:"firstName"`
	LastName     string `json:"lastName"`
	MobileNumber string `json:"mobileNumber"`
}

// @Summary		Update the customer profile
// @Tags			account
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	updateProfileRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		409	{object}	httpx.Envelope	"MOBILE_ALREADY_EXISTS — the mobile number is on another account"
// @Router			/account/profile [patch]
func (h *Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	var req updateProfileRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	c, err := h.svc.UpdateProfile(r.Context(), claims.CustomerID, req.FirstName, req.LastName, req.MobileNumber)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, customerDTO(c))
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
	ConfirmPassword string `json:"confirmPassword"`
}

// @Summary		Change the account password
// @Tags			account
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	changePasswordRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account/change-password [post]
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	var req changePasswordRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.ChangePassword(r.Context(), claims.CustomerID, req.CurrentPassword, req.NewPassword, req.ConfirmPassword); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

type deleteAccountRequest struct {
	Password string `json:"password"`
}

// @Summary		Delete the customer account
// @Tags			account
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	deleteAccountRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account [delete]
func (h *Handler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	var req deleteAccountRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.DeleteAccount(r.Context(), claims.CustomerID, req.Password); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]string{"message": "Account deleted."})
}

type marketingRequest struct {
	OptIn bool `json:"optIn"`
}

// @Summary		Set marketing consent
// @Tags			account
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	marketingRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account/marketing [patch]
func (h *Handler) SetMarketing(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	var req marketingRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.SetMarketingOptIn(r.Context(), claims.CustomerID, req.OptIn, clientIP(r)); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// --- Address book --------------------------------------------------------------

// @Summary		List saved addresses
// @Tags			account
// @Produce		json
// @Security		BearerAuth
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account/addresses [get]
func (h *Handler) ListAddresses(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	addrs, err := h.svc.ListAddresses(r.Context(), claims.CustomerID)
	if err != nil {
		httpx.WriteError(w, r, apperr.Internal(err))
		return
	}
	out := make([]addressDTOType, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, addressDTO(&a))
	}
	httpx.OK(w, out)
}

type addressRequest struct {
	Label    string `json:"label"`
	Address  string `json:"address,omitempty"`
	Street   string `json:"street,omitempty"`
	City     string `json:"city"`
	State    string `json:"state"`
	Postcode string `json:"postcode"`
	Country  string `json:"country"`
}

// @Summary		Add a saved address
// @Tags			account
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	addressRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account/addresses [post]
func (h *Handler) CreateAddress(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	var req addressRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	a, err := h.svc.CreateAddress(r.Context(), claims.CustomerID, addressFromRequest(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, addressDTO(a))
}

// @Summary		Update a saved address
// @Tags			account
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			addressId	path	string	true	"addressId"
// @Param			body	body	addressRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/account/addresses/{addressId} [patch]
func (h *Handler) UpdateAddress(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	id := chi.URLParam(r, "addressId")
	var req addressRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	a, err := h.svc.UpdateAddress(r.Context(), claims.CustomerID, id, addressFromRequest(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, addressDTO(a))
}

// @Summary		Delete a saved address
// @Tags			account
// @Produce		json
// @Security		BearerAuth
// @Param			addressId	path	string	true	"addressId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/account/addresses/{addressId} [delete]
func (h *Handler) DeleteAddress(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	id := chi.URLParam(r, "addressId")
	if err := h.svc.DeleteAddress(r.Context(), claims.CustomerID, id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// @Summary		Make an address the default
// @Tags			account
// @Produce		json
// @Security		BearerAuth
// @Param			addressId	path	string	true	"addressId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/account/addresses/{addressId}/set-default [post]
func (h *Handler) SetDefaultAddress(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	id := chi.URLParam(r, "addressId")
	if err := h.svc.SetDefaultAddress(r.Context(), claims.CustomerID, id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// --- Recipient book --------------------------------------------------------------

// @Summary		List saved gift recipients
// @Tags			account
// @Produce		json
// @Security		BearerAuth
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account/recipients [get]
func (h *Handler) ListRecipients(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	recipients, err := h.svc.ListRecipients(r.Context(), claims.CustomerID)
	if err != nil {
		httpx.WriteError(w, r, apperr.Internal(err))
		return
	}
	out := make([]recipientDTOType, 0, len(recipients))
	for _, rec := range recipients {
		out = append(out, recipientDTO(&rec))
	}
	httpx.OK(w, out)
}

type recipientRequest struct {
	FirstName     string                     `json:"firstName"`
	LastName      string                     `json:"lastName"`
	Phone         string                     `json:"phone"`
	Relationship  string                     `json:"relationship"`
	OccasionDates []identityapi.OccasionDate `json:"occasionDates"`
}

// @Summary		Add a saved gift recipient
// @Tags			account
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	recipientRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/account/recipients [post]
func (h *Handler) CreateRecipient(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	var req recipientRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	rec, err := h.svc.CreateRecipient(r.Context(), claims.CustomerID, recipientFromRequest(req))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, recipientDTO(rec))
}

// @Summary		Update a saved gift recipient
// @Tags			account
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			recipientId	path	string	true	"recipientId"
// @Param			body	body	recipientRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/account/recipients/{recipientId} [patch]
func (h *Handler) UpdateRecipient(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	id := chi.URLParam(r, "recipientId")
	var req recipientRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.UpdateRecipient(r.Context(), claims.CustomerID, id, recipientFromRequest(req)); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// @Summary		Delete a saved gift recipient
// @Tags			account
// @Produce		json
// @Security		BearerAuth
// @Param			recipientId	path	string	true	"recipientId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/account/recipients/{recipientId} [delete]
func (h *Handler) DeleteRecipient(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.CustomerFromContext(r.Context())
	id := chi.URLParam(r, "recipientId")
	if err := h.svc.DeleteRecipient(r.Context(), claims.CustomerID, id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

// --- Staff auth ---------------------------------------------------------------

type staffLoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type posBusinessIDRequest struct {
	BusinessID string `json:"businessId"`
}

type posLoginRequest struct {
	BusinessID string `json:"businessId"`
	Passcode   string `json:"passcode"`
}

// @Summary		Check a Business ID before asking for a passcode
// @Description	Step one of the till sign-in. Grants nothing — it only confirms the code and names the outlet, so the passcode screen can show which shop it is signing in to.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Param			body	body	posBusinessIDRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		401	{object}	httpx.Envelope	"INVALID_BUSINESS_ID"
// @Failure		429	{object}	httpx.Envelope	"RATE_LIMITED"
// @Router			/pos/auth/business-id [post]
func (h *Handler) POSValidateBusinessID(w http.ResponseWriter, r *http.Request) {
	var req posBusinessIDRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	outlet, err := h.svc.ValidateBusinessID(r.Context(), req.BusinessID, clientIP(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	// The same "outlet" object the login below returns. Step one and step two
	// describe the same shop, and a client that has to parse two shapes for it
	// will eventually parse one of them wrong.
	httpx.OK(w, map[string]interface{}{
		"outlet": map[string]string{
			"branchId":   outlet.BranchID,
			"businessId": outlet.BusinessID,
			"name":       outlet.Name,
			// KDS flow: "branch" or "central_kitchen". A till only signs in to
			// a branch; POS login refuses a central kitchen's Location ID.
			"type":             outlet.Type,
			"centralKitchenId": outlet.CentralKitchenID,
		},
	})
}

type staffPasscodeRequest struct {
	// Empty takes POS access away, which is why it is not required.
	Passcode string `json:"passcode"`
}

// @Summary		Sign in to a till with a Business ID and passcode
// @Tags			pos
// @Accept			json
// @Produce		json
// @Param			body	body	posLoginRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		401	{object}	httpx.Envelope	"INVALID_CREDENTIALS"
// @Failure		429	{object}	httpx.Envelope	"RATE_LIMITED"
// @Router			/pos/auth/login [post]
func (h *Handler) POSLogin(w http.ResponseWriter, r *http.Request) {
	var req posLoginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pair, staff, roles, outlet, err := h.svc.POSLogin(r.Context(), req.BusinessID, req.Passcode, clientIP(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{
		"accessToken":  pair.AccessToken,
		"refreshToken": pair.RefreshToken,
		"staff":        staffDTO(staff, roles),
		// The till writes every order against these, so it is told them once
		// at sign-in rather than having to ask.
		"outlet": map[string]interface{}{
			"branchId":   outlet.BranchID,
			"businessId": outlet.BusinessID,
			"name":       outlet.Name,
			"type":       outlet.Type,
			"tax": map[string]interface{}{
				"name":    outlet.TaxName,
				"ratePct": outlet.TaxRatePct, // null when no tax is set
			},
		},
	})
}

// @Summary		Set or clear a staff member's POS passcode
// @Tags			admin-staff
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			staffId	path	string	true	"staffId"
// @Param			body	body	staffPasscodeRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		409	{object}	httpx.Envelope	"PASSCODE_TAKEN"
// @Router			/admin/staff/{staffId}/passcode [put]
func (h *Handler) SetStaffPasscode(w http.ResponseWriter, r *http.Request) {
	var req staffPasscodeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	if err := h.svc.SetStaffPasscode(r.Context(), staffClaims(r), chi.URLParam(r, "staffId"), req.Passcode); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"ok": true})
}

// @Summary		Log in as staff
// @Tags			staff-auth
// @Accept			json
// @Produce		json
// @Param			body	body	staffLoginRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/staff/auth/login [post]
func (h *Handler) StaffLogin(w http.ResponseWriter, r *http.Request) {
	var req staffLoginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pair, staff, roles, err := h.svc.StaffLogin(r.Context(), req.Email, req.Password, clientIP(r))
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{
		"accessToken":  pair.AccessToken,
		"refreshToken": pair.RefreshToken,
		"staff":        staffDTO(staff, roles),
	})
}

// @Summary		Exchange a staff refresh token
// @Tags			staff-auth
// @Accept			json
// @Produce		json
// @Param			body	body	refreshRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/staff/auth/refresh [post]
func (h *Handler) StaffRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	pair, err := h.svc.RefreshStaff(r.Context(), req.RefreshToken)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{"accessToken": pair.AccessToken, "refreshToken": pair.RefreshToken})
}

// @Summary		Exchange a POS refresh token
// @Description	Same behaviour as /staff/auth/refresh — the till carries a staff token.
// @Tags			pos
// @Accept			json
// @Produce		json
// @Param			body	body	refreshRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/pos/auth/refresh [post]
func (h *Handler) POSRefresh(w http.ResponseWriter, r *http.Request) {
	h.StaffRefresh(w, r)
}

// @Summary		Revoke a staff refresh token
// @Tags			staff-auth
// @Accept			json
// @Produce		json
// @Param			body	body	refreshRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Router			/staff/auth/logout [post]
func (h *Handler) StaffLogout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	_ = h.svc.LogoutStaff(r.Context(), req.RefreshToken)
	httpx.OK(w, map[string]bool{"success": true})
}

type staffInviteRequest struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Role      string `json:"role"`
	ScopeType string `json:"scopeType"`
	// Accepted and ignored so older consoles still sending it are not
	// refused by the strict decoder; franchise scope no longer exists.
	FranchiseID *string `json:"franchiseId,omitempty"`
	BranchID    *string `json:"branchId,omitempty"`
	// KDS: the corporate office (central kitchen) for scopeType
	// "central_kitchen". Only with role central_kitchen_staff.
	CentralKitchenID *string `json:"centralKitchenId,omitempty"`
	// KDS/POS: optional 4-digit passcode set together with the account.
	Passcode string `json:"passcode,omitempty" example:"1234"`

	MaxRefundCents      *int64   `json:"maxRefundCents,omitempty"`
	MaxDiscountPct      *float64 `json:"maxDiscountPct,omitempty"`
	OrderVoidWindowMins *int     `json:"orderVoidWindowMins,omitempty"`
}

// @Summary		Invite a staff member
// @Tags			admin-staff
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	staffInviteRequest	true	"Request body"
// @Success		201	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Router			/admin/staff/invite [post]
func (h *Handler) InviteStaff(w http.ResponseWriter, r *http.Request) {
	var req staffInviteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	staff, err := h.svc.CreateStaffUserWithPasscode(r.Context(), staffClaims(r), req.Email, req.Password, req.FirstName, req.LastName, identityapi.StaffRoleAssignment{
		Role: req.Role, ScopeType: req.ScopeType, BranchID: req.BranchID, CentralKitchenID: req.CentralKitchenID,
		MaxRefundCents: req.MaxRefundCents, MaxDiscountPct: req.MaxDiscountPct, OrderVoidWindowMins: req.OrderVoidWindowMins,
	}, req.Passcode)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.Created(w, map[string]interface{}{"id": staff.ID, "email": staff.Email})
}

// @Summary		Deactivate a staff member
// @Tags			admin-staff
// @Produce		json
// @Security		BearerAuth
// @Param			staffId	path	string	true	"staffId"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/staff/{staffId}/deactivate [post]
func (h *Handler) DeactivateStaff(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "staffId")
	if err := h.svc.DeactivateStaffUser(r.Context(), id); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"success": true})
}

func staffClaims(r *http.Request) *auth.StaffClaims {
	c, _ := auth.StaffFromContext(r.Context())
	return c
}

// ListStaff serves the network-wide staff directory (SA-16).
//
//	@Summary		List staff in scope
//	@Tags			admin-staff
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	httpx.Envelope
//	@Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
//	@Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
//	@Failure		403	{object}	httpx.Envelope	"PERMISSION_DENIED"
//	@Router			/admin/staff [get]
func (h *Handler) ListStaff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := identityapi.StaffDirectoryFilter{
		Role:             q.Get("role"),
		BranchID:         q.Get("branchId"),
		CentralKitchenID: q.Get("centralKitchenId"),
		Search:           q.Get("search"),
	}
	if v := q.Get("isActive"); v != "" {
		b := v == "true"
		filter.IsActive = &b
	}
	list, err := h.svc.ListStaffNetwork(r.Context(), staffClaims(r), filter)
	if err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	canReveal := tenant.HasPermission(staffClaims(r), tenant.PermStaffManage)
	out := make([]staffDirectoryDTOType, 0, len(list))
	for _, e := range list {
		dto := staffDirectoryDTO(&e)
		if canReveal {
			if code := h.svc.RevealPasscode(e.PasscodeSealed); code != "" {
				dto.Passcode = &code
			}
		}
		out = append(out, dto)
	}
	httpx.OK(w, out)
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return "", false
	}
	return strings.TrimPrefix(h, "Bearer "), true
}

type staffChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// @Summary		Change your own staff password
// @Tags			admin-staff
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	staffChangePasswordRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/admin/staff/me/change-password [post]
func (h *Handler) ChangeStaffPassword(w http.ResponseWriter, r *http.Request) {
	var req staffChangePasswordRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	claims := staffClaims(r)
	if claims == nil {
		httpx.WriteError(w, r, apperr.Unauthenticated("Sign in again to change your password."))
		return
	}
	if err := h.svc.ChangeStaffPassword(r.Context(), claims.StaffID, req.CurrentPassword, req.NewPassword); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"changed": true})
}

type staffUpdateProfileRequest struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
}

// @Summary		Update your own staff profile
// @Tags			staff-auth
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			body	body	staffUpdateProfileRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		400	{object}	httpx.Envelope	"VALIDATION_FAILED"
// @Failure		401	{object}	httpx.Envelope	"UNAUTHENTICATED"
// @Router			/admin/staff/me/profile [patch]
func (h *Handler) UpdateOwnStaffProfile(w http.ResponseWriter, r *http.Request) {
	var req staffUpdateProfileRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	claims := staffClaims(r)
	if claims == nil {
		httpx.WriteError(w, r, apperr.Unauthenticated("Sign in again to change your password."))
		return
	}
	if err := h.svc.UpdateOwnStaffProfile(r.Context(), claims.StaffID, req.FirstName, req.LastName, req.Email, req.Phone); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]bool{"changed": true})
}

type staffUpdateRequest struct {
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Role      string `json:"role"`
	ScopeType string `json:"scopeType"`
	// Accepted and ignored; see staffInviteRequest.
	FranchiseID *string `json:"franchiseId"`
	BranchID    *string `json:"branchId"`
	// KDS: corporate office for scopeType "central_kitchen".
	CentralKitchenID *string `json:"centralKitchenId,omitempty"`
	// KDS/POS: omit to keep, "" to remove, 4 digits to set.
	Passcode *string `json:"passcode,omitempty" example:"1234"`
}

// @Summary		Update a staff member's details, role and scope
// @Tags			admin-staff
// @Accept			json
// @Produce		json
// @Security		BearerAuth
// @Param			staffId	path	string	true	"staffId"
// @Param			body	body	staffUpdateRequest	true	"Request body"
// @Success		200	{object}	httpx.Envelope
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/staff/{staffId} [patch]
func (h *Handler) UpdateStaff(w http.ResponseWriter, r *http.Request) {
	var req staffUpdateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	id := chi.URLParam(r, "staffId")
	if err := h.svc.UpdateStaffUserKDS(r.Context(), staffClaims(r), id, req.FirstName, req.LastName,
		req.Role, req.ScopeType, req.BranchID, req.CentralKitchenID, req.Passcode); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{"success": true})
}

// @Summary		Restore access to a deactivated staff member
// @Tags			admin-staff
// @Produce		json
// @Security		BearerAuth
// @Param			staffId	path	string	true	"staffId"
// @Success		200	{object}	httpx.Envelope
// @Failure		404	{object}	httpx.Envelope	"NOT_FOUND"
// @Router			/admin/staff/{staffId}/reactivate [post]
func (h *Handler) ReactivateStaff(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ReactivateStaffUser(r.Context(), chi.URLParam(r, "staffId")); err != nil {
		httpx.WriteError(w, r, err)
		return
	}
	httpx.OK(w, map[string]interface{}{"success": true})
}
