package api

import (
	"log/slog"
	"net"
	"net/http"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/httpx"
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

func staffClaims(r *http.Request) *auth.StaffClaims {
	c, _ := auth.StaffFromContext(r.Context())
	return c
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return "", false
	}
	return strings.TrimPrefix(h, "Bearer "), true
}
