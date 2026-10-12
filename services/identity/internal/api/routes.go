package api

import (
	"github.com/go-chi/chi/v5"

	"bakehouse/pkg/tenant"
)

// MountCustomerAuth wires customer sign-up and sign-in onto v1. Customers
// belong to one business, so these routes sit behind the business resolver.
// The account area is mounted separately via MountAccount.
func MountCustomerAuth(v1 chi.Router, h *Handler) {
	v1.Route("/auth", func(r chi.Router) {
		r.Post("/register", h.RegisterStep1)
		r.Post("/register/profile", h.RegisterStep2) // Authorization: Bearer <registrationToken>
		r.Post("/register/address", h.RegisterStep3) // Authorization: Bearer <registrationToken>
		r.Post("/verify-otp", h.VerifyOTP)
		r.Post("/resend-otp", h.ResendOTP)
		r.Post("/login", h.Login)
		r.Post("/refresh", h.Refresh)
		r.Post("/logout", h.Logout)
		r.Post("/forgot-password", h.ForgotPassword)
		r.Post("/verify-reset-otp", h.VerifyResetOTP)
		r.Post("/reset-password", h.ResetPassword)
	})
}

// MountStaffAuth wires staff, POS and KDS sign-in onto v1. A staff member's
// business comes from their own account, never from the request.
func MountStaffAuth(v1 chi.Router, h *Handler) {
	// The till signs in with an outlet code and a passcode rather than an
	// email and a password, so it gets its own unauthenticated route.
	v1.Route("/pos/auth", func(r chi.Router) {
		// Two steps, because the screen has two: the Business ID is checked on
		// its own so the operator is told about a wrong code before being asked
		// for a passcode there is no point typing.
		r.Post("/business-id", h.POSValidateBusinessID)
		r.Post("/login", h.POSLogin)
		r.Post("/refresh", h.POSRefresh) // same handler as /staff/auth/refresh
		r.Post("/logout", h.StaffLogout) // same handler as /staff/auth/logout
	})

	// The Kitchen Display signs in the same two-step way as the till, but a
	// Location ID may name a store or a central kitchen (corporate office).
	v1.Route("/kds/auth", func(r chi.Router) {
		r.Post("/business-id", h.KDSValidateBusinessID)
		r.Post("/login", h.KDSLogin)
		r.Post("/refresh", h.KDSRefresh) // same handler as /staff/auth/refresh
		r.Post("/logout", h.StaffLogout)
	})

	v1.Route("/staff/auth", func(r chi.Router) {
		r.Post("/login", h.StaffLogin)
		r.Post("/refresh", h.StaffRefresh)
		r.Post("/logout", h.StaffLogout)
	})
}

// MountAccount attaches profile/address/recipient routes onto the shared
// /account router (already behind auth.RequireCustomer — see
// cmd/api/main.go).
func MountAccount(account chi.Router, h *Handler) {
	account.Get("/profile", h.GetProfile)
	account.Patch("/profile", h.UpdateProfile)
	account.Post("/change-password", h.ChangePassword)
	account.Delete("/", h.DeleteAccount)
	account.Patch("/marketing", h.SetMarketing)

	account.Get("/addresses", h.ListAddresses)
	account.Post("/addresses", h.CreateAddress)
	account.Patch("/addresses/{addressId}", h.UpdateAddress)
	account.Delete("/addresses/{addressId}", h.DeleteAddress)
	account.Post("/addresses/{addressId}/set-default", h.SetDefaultAddress)

	account.Get("/recipients", h.ListRecipients)
	account.Post("/recipients", h.CreateRecipient)
	account.Patch("/recipients/{recipientId}", h.UpdateRecipient)
	account.Delete("/recipients/{recipientId}", h.DeleteRecipient)
}

// MountAdmin attaches staff-management routes onto the shared /admin router
// (already behind auth.RequireStaff — see cmd/api/main.go).
func MountAdmin(admin chi.Router, h *Handler) {
	admin.Route("/staff", func(r chi.Router) {
		r.Post("/me/change-password", h.ChangeStaffPassword)
		r.Patch("/me/profile", h.UpdateOwnStaffProfile)
		r.With(tenant.RequirePermission(tenant.PermStaffView)).Get("/", h.ListStaff)
		r.With(tenant.RequirePermission(tenant.PermStaffManage)).Post("/invite", h.InviteStaff)
		// A second credential on an existing account, for the till.
		r.With(tenant.RequirePermission(tenant.PermStaffManage)).Put("/{staffId}/passcode", h.SetStaffPasscode)
		r.With(tenant.RequirePermission(tenant.PermStaffManage)).Post("/{staffId}/deactivate", h.DeactivateStaff)
		r.With(tenant.RequirePermission(tenant.PermStaffManage)).Post("/{staffId}/reactivate", h.ReactivateStaff)
		r.With(tenant.RequirePermission(tenant.PermStaffManage)).Patch("/{staffId}", h.UpdateStaff)
	})
}

// MountPOS attaches customer endpoints to the till's router.
func MountPOS(pos chi.Router, h *Handler) {
	pos.Route("/customers", func(r chi.Router) {
		r.Post("/", h.CreatePOSCustomer)
		r.Get("/", h.ListPOSCustomers)
		r.Get("/{customerId}", h.GetPOSCustomer)
		r.Put("/{customerId}", h.UpdatePOSCustomer)
		r.Delete("/{customerId}", h.DeletePOSCustomer)
	})
}
