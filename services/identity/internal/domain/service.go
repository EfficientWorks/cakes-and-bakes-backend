package domain

import (
	"context"
	"errors"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	mail "bakehouse/pkg/email"
	"bakehouse/pkg/ratelimit"
	"bakehouse/pkg/sealbox"
	"bakehouse/pkg/tenant"
	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/store"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	otpTTL           = 10 * time.Minute
	otpMaxAttempts   = 5
	passwordResetTTL = time.Hour
	loginRateLimit   = 10
	loginRateWindow  = 5 * time.Minute
	otpResendLimit   = 3
	otpResendWindow  = 10 * time.Minute
)

// Service is the identity module's business logic: customer auth/account
// self-service and staff auth. It owns no HTTP concerns.
type Service struct {
	store *store.Store
	// Few-second cache behind the per-request POS / KDS session check.
	sessions       sessionCache
	customerTokens *auth.Issuer
	staffTokens    *auth.Issuer
	emailer        mail.Emailer
	limiter        *ratelimit.Limiter
	// Both are set after construction: the franchise service is built later,
	// and identity needs one lookup from each. Nil until then, which the
	// callers check rather than assume.
	branches BranchResolver
	scopes   ScopeStatusReader
	// Stores vs central kitchens by Location ID (KDS). Nil until wired.
	locations LocationResolver
	// Seals till passcodes so admin can reveal them. Nil turns that off.
	passcodes *sealbox.Box

	accessTTL       time.Duration
	refreshTTL      time.Duration
	staffAccessTTL  time.Duration
	staffRefreshTTL time.Duration
}

// UseBranchResolver lets the POS login turn a Business ID into an outlet.
//
// Wired at start-up rather than passed to NewService because the franchise
// service is constructed after identity, and only this one feature needs it.
func (s *Service) UseBranchResolver(r BranchResolver) { s.branches = r }

func NewService(
	store *store.Store,
	customerTokens, staffTokens *auth.Issuer,
	emailer mail.Emailer,
	limiter *ratelimit.Limiter,
	accessTTL, refreshTTL, staffAccessTTL, staffRefreshTTL time.Duration,
) *Service {
	return &Service{
		store: store, customerTokens: customerTokens, staffTokens: staffTokens,
		emailer: emailer, limiter: limiter,
		accessTTL: accessTTL, refreshTTL: refreshTTL,
		staffAccessTTL: staffAccessTTL, staffRefreshTTL: staffRefreshTTL,
	}
}

// --- Customer registration & verification --------------------------------

// RegisterStep1 creates the account shell (email + password), sends an OTP,
// and returns a short-lived registration token so steps 2/3 can identify the
// account before it's fully verified.
func (s *Service) RegisterStep1(ctx context.Context, email, password, confirmPassword string) (registrationToken string, err error) {
	if password != confirmPassword {
		return "", apperr.Validation("Password and confirmation do not match.").
			WithFields(apperr.FieldError{Field: "confirmPassword", Code: "MISMATCH", Message: "Does not match password."})
	}
	if err := validatePasswordStrength(password); err != nil {
		return "", err
	}
	email = normaliseEmail(email)

	if _, err := s.store.GetCustomerByEmail(ctx, email); err == nil {
		return "", apperr.New("EMAIL_ALREADY_EXISTS", apperr.CategoryConflict, "An account with this email already exists.")
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", apperr.Internal(err)
	}

	hash, err := auth.HashSecret(password)
	if err != nil {
		return "", apperr.Internal(err)
	}
	customer, err := s.store.CreateCustomer(ctx, email, hash)
	if err != nil {
		return "", apperr.Internal(err)
	}

	if err := s.issueAndSendOTP(ctx, customer.ID, customer.Email, mail.TemplateAccountVerifyOTP); err != nil {
		return "", err
	}

	tok, _, err := s.customerTokens.IssueCustomerAccessToken(customer.ID, customer.Email, false, 30*time.Minute)
	if err != nil {
		return "", apperr.Internal(err)
	}
	return tok, nil
}

// RegisterStep2 fills in personal details for the account named by the
// registration token issued in step 1.
func (s *Service) RegisterStep2(ctx context.Context, registrationToken, firstName, lastName, mobile string) error {
	claims, err := s.customerTokens.ParseCustomerAccessToken(registrationToken)
	if err != nil {
		return err
	}
	return s.saveCustomerProfile(ctx, claims.CustomerID, firstName, lastName, mobile)
}

// saveCustomerProfile writes name and mobile for a storefront account.
//
// One live account per mobile number (idx_customers_mobile). The number is
// checked before the write so the usual case is a clear answer rather than
// a constraint error, and the write's own unique violation is translated
// too, for the race where two registrations claim the same number at once.
// Unlike the till, the shop is not told whose number it is.
func (s *Service) saveCustomerProfile(ctx context.Context, customerID, firstName, lastName, mobile string) error {
	var mobilePtr *string
	if mobile = strings.TrimSpace(mobile); mobile != "" {
		if digits := normaliseMobile(mobile); digits != "" {
			if existing, err := s.mobileTaken(ctx, "", digits, customerID); err != nil {
				return err
			} else if existing != nil {
				return errMobileExists()
			}
		}
		mobilePtr = &mobile
	}
	if err := s.store.UpdateCustomerProfile(ctx, customerID, strings.TrimSpace(firstName), strings.TrimSpace(lastName), mobilePtr); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return apperr.NotFound("Account not found.")
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && (pgErr.ConstraintName == "idx_customers_online_mobile" || pgErr.ConstraintName == "idx_customers_mobile") {
			return errMobileExists()
		}
		return apperr.Internal(err)
	}
	return nil
}

// errMobileExists is the 409 a storefront caller gets for a number that is
// already on another account.
func errMobileExists() error {
	return apperr.New("MOBILE_ALREADY_EXISTS", apperr.CategoryConflict,
		"This mobile number is already registered to another account.").
		WithFields(apperr.FieldError{Field: "mobileNumber", Code: "DUPLICATE", Message: "Mobile number already exists."})
}

// RegisterStep3 saves the optional address step and returns the (still
// unverified) customer along with a fresh access token.
func (s *Service) RegisterStep3(ctx context.Context, registrationToken string, addr identityapi.CustomerAddress) (*auth.TokenPair, *identityapi.Customer, error) {
	claims, err := s.customerTokens.ParseCustomerAccessToken(registrationToken)
	if err != nil {
		return nil, nil, err
	}
	if addr.Street != "" {
		addr.CustomerID = claims.CustomerID
		addr.IsDefault = true
		if _, err := s.store.CreateCustomerAddress(ctx, addr); err != nil {
			return nil, nil, apperr.Internal(err)
		}
	}
	customer, err := s.store.GetCustomerByID(ctx, claims.CustomerID)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	pair, err := s.issueCustomerTokenPair(ctx, customer)
	if err != nil {
		return nil, nil, err
	}
	return pair, customer, nil
}

func (s *Service) issueAndSendOTP(ctx context.Context, customerID, email, template string) error {
	code, err := auth.NewOTP()
	if err != nil {
		return apperr.Internal(err)
	}
	hash, err := auth.HashSecret(code)
	if err != nil {
		return apperr.Internal(err)
	}
	if err := s.store.SetCustomerOTP(ctx, customerID, hash, time.Now().Add(otpTTL)); err != nil {
		return apperr.Internal(err)
	}
	if err := s.emailer.Send(ctx, mail.Email{
		To:       email,
		Template: template,
		Data:     map[string]interface{}{"otp": code, "expiresInMinutes": int(otpTTL.Minutes())},
	}); err != nil {
		return apperr.Wrap(err, "EMAIL_SEND_FAILED", apperr.CategoryUpstream, "Could not send verification email. Please try again.")
	}
	return nil
}

// VerifyOTP checks the code against the stored hash and, on success, marks
// the account verified and returns a full token pair.
func (s *Service) VerifyOTP(ctx context.Context, email, otp string) (*auth.TokenPair, *identityapi.Customer, error) {
	email = normaliseEmail(email)
	customer, err := s.store.GetCustomerByEmail(ctx, email)
	if err != nil {
		return nil, nil, apperr.New("OTP_INVALID", apperr.CategoryValidation, "Invalid or expired code.")
	}

	hash, expiresAt, attempts, err := s.store.GetCustomerOTP(ctx, customer.ID)
	if err != nil {
		return nil, nil, apperr.Internal(err)
	}
	if attempts >= otpMaxAttempts {
		return nil, nil, apperr.New("OTP_LOCKED", apperr.CategoryRateLimited, "Too many attempts. Request a new code.")
	}
	if hash == "" || time.Now().After(expiresAt) {
		return nil, nil, apperr.New("OTP_EXPIRED", apperr.CategoryValidation, "This code has expired. Request a new one.")
	}
	ok, err := auth.VerifySecret(hash, otp)
	if err != nil || !ok {
		_ = s.store.IncrementCustomerOTPAttempts(ctx, customer.ID)
		return nil, nil, apperr.New("OTP_INVALID", apperr.CategoryValidation, "Invalid or expired code.")
	}

	if err := s.store.MarkCustomerVerified(ctx, customer.ID); err != nil {
		return nil, nil, apperr.Internal(err)
	}
	customer.IsVerified = true

	// Best-effort welcome email — does not block token issuance.
	_ = s.emailer.Send(ctx, mail.Email{
		To:       customer.Email,
		Template: mail.TemplateCustomerWelcome,
		Data:     map[string]interface{}{"firstName": customer.FirstName, "lastName": customer.LastName},
	})

	pair, err := s.issueCustomerTokenPair(ctx, customer)
	if err != nil {
		return nil, nil, err
	}
	return pair, customer, nil
}

// ResendOTP re-issues a verification code, subject to per-email rate limiting.
func (s *Service) ResendOTP(ctx context.Context, email string) error {
	email = normaliseEmail(email)
	if ok, err := s.limiter.Allow(ctx, "otp-resend:"+email, otpResendLimit, otpResendWindow); err == nil && !ok {
		return apperr.RateLimited("Too many code requests. Please wait before trying again.")
	}
	customer, err := s.store.GetCustomerByEmail(ctx, email)
	if err != nil {
		// Always behave as success to avoid confirming which emails exist.
		return nil
	}
	return s.issueAndSendOTP(ctx, customer.ID, customer.Email, mail.TemplateAccountVerifyOTP)
}

// --- Login / session ------------------------------------------------------

func (s *Service) Login(ctx context.Context, email, password, ip string) (*auth.TokenPair, *identityapi.Customer, error) {
	email = normaliseEmail(email)
	if ok, err := s.limiter.Allow(ctx, "login:"+ip, loginRateLimit, loginRateWindow); err == nil && !ok {
		return nil, nil, apperr.RateLimited("Too many login attempts. Please try again later.")
	}

	customer, err := s.store.GetCustomerByEmail(ctx, email)
	if err != nil {
		return nil, nil, apperr.New("INVALID_CREDENTIALS", apperr.CategoryAuthentication, "Incorrect email or password.")
	}
	ok, err := auth.VerifySecret(customer.PasswordHash, password)
	if err != nil || !ok {
		return nil, nil, apperr.New("INVALID_CREDENTIALS", apperr.CategoryAuthentication, "Incorrect email or password.")
	}
	if !customer.IsVerified {
		return nil, nil, apperr.New("ACCOUNT_NOT_VERIFIED", apperr.CategoryAuthorization, "Please verify your email before logging in.")
	}

	pair, err := s.issueCustomerTokenPair(ctx, customer)
	if err != nil {
		return nil, nil, err
	}
	return pair, customer, nil
}

func (s *Service) issueCustomerTokenPair(ctx context.Context, c *identityapi.Customer) (*auth.TokenPair, error) {
	access, exp, err := s.customerTokens.IssueCustomerAccessToken(c.ID, c.Email, c.IsVerified, s.accessTTL)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	refresh, err := s.storeRefreshToken(ctx, identityapi.SubjectCustomer, c.ID, s.refreshTTL)
	if err != nil {
		return nil, err
	}
	return &auth.TokenPair{AccessToken: access, RefreshToken: refresh, ExpiresAt: exp}, nil
}

func (s *Service) storeRefreshToken(ctx context.Context, subjectType, subjectID string, ttl time.Duration) (string, error) {
	refresh, err := auth.NewOpaqueToken()
	if err != nil {
		return "", apperr.Internal(err)
	}
	if _, err := s.store.CreateRefreshToken(ctx, subjectType, subjectID, auth.HashOpaqueToken(refresh), time.Now().Add(ttl), ""); err != nil {
		return "", apperr.Internal(err)
	}
	return refresh, nil
}

// RefreshCustomer rotates a customer refresh token: the old one is revoked
// and a new pair issued, so a stolen-but-unused token can't be replayed
// indefinitely once the legitimate client rotates past it.
func (s *Service) RefreshCustomer(ctx context.Context, refreshToken string) (*auth.TokenPair, error) {
	row, err := s.store.GetRefreshTokenByHash(ctx, identityapi.SubjectCustomer, auth.HashOpaqueToken(refreshToken))
	if err != nil || row.RevokedAt != nil || time.Now().After(row.ExpiresAt) {
		return nil, apperr.Unauthenticated("Session expired. Please log in again.")
	}
	customer, err := s.store.GetCustomerByID(ctx, row.SubjectID)
	if err != nil {
		return nil, apperr.Unauthenticated("Session expired. Please log in again.")
	}
	pair, err := s.issueCustomerTokenPair(ctx, customer)
	if err != nil {
		return nil, err
	}
	_ = s.store.RevokeRefreshToken(ctx, row.ID, "")
	return pair, nil
}

// LogoutCustomer revokes a single refresh token (this device only).
func (s *Service) LogoutCustomer(ctx context.Context, refreshToken string) error {
	row, err := s.store.GetRefreshTokenByHash(ctx, identityapi.SubjectCustomer, auth.HashOpaqueToken(refreshToken))
	if err != nil {
		return nil // already gone; logout is idempotent
	}
	return s.store.RevokeRefreshToken(ctx, row.ID, "")
}

func normaliseEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

func validatePasswordStrength(password string) error {
	if len(password) < 8 {
		return apperr.New("PASSWORD_TOO_WEAK", apperr.CategoryValidation, "Password must be at least 8 characters.").
			WithFields(apperr.FieldError{Field: "password", Code: "TOO_SHORT", Message: "Minimum 8 characters."})
	}
	return nil
}

// --- Password reset --------------------------------------------------------

// ForgotPassword always reports success, even for an unknown email, so the
// response cannot be used to enumerate registered addresses.
func (s *Service) ForgotPassword(ctx context.Context, email string) error {
	email = normaliseEmail(email)
	if ok, err := s.limiter.Allow(ctx, "otp-resend:"+email, otpResendLimit, otpResendWindow); err == nil && !ok {
		return nil
	}
	customer, err := s.store.GetCustomerByEmail(ctx, email)
	if err != nil {
		return nil
	}
	return s.issueAndSendOTP(ctx, customer.ID, customer.Email, mail.TemplatePasswordResetOTP)
}

// VerifyResetOTP checks the reset code and returns a short-lived reset token
// to present to ResetPasswordForEmail.
func (s *Service) VerifyResetOTP(ctx context.Context, email, otp string) (string, error) {
	email = normaliseEmail(email)
	customer, err := s.store.GetCustomerByEmail(ctx, email)
	if err != nil {
		return "", apperr.New("OTP_INVALID", apperr.CategoryValidation, "Invalid or expired code.")
	}
	hash, expiresAt, attempts, err := s.store.GetCustomerOTP(ctx, customer.ID)
	if err != nil {
		return "", apperr.Internal(err)
	}
	if attempts >= otpMaxAttempts {
		return "", apperr.New("OTP_LOCKED", apperr.CategoryRateLimited, "Too many attempts. Request a new code.")
	}
	if hash == "" || time.Now().After(expiresAt) {
		return "", apperr.New("OTP_EXPIRED", apperr.CategoryValidation, "This code has expired. Request a new one.")
	}
	ok, err := auth.VerifySecret(hash, otp)
	if err != nil || !ok {
		_ = s.store.IncrementCustomerOTPAttempts(ctx, customer.ID)
		return "", apperr.New("OTP_INVALID", apperr.CategoryValidation, "Invalid or expired code.")
	}

	resetToken, err := auth.NewOpaqueToken()
	if err != nil {
		return "", apperr.Internal(err)
	}
	if err := s.store.SetCustomerPasswordResetToken(ctx, customer.ID, auth.HashOpaqueToken(resetToken), time.Now().Add(passwordResetTTL)); err != nil {
		return "", apperr.Internal(err)
	}
	return resetToken, nil
}

// ResetPasswordForEmail completes a reset: email + resetToken together
// identify the account, mirroring the refresh-token verification pattern.
func (s *Service) ResetPasswordForEmail(ctx context.Context, email, resetToken, newPassword, confirmPassword string) error {
	if newPassword != confirmPassword {
		return apperr.Validation("Password and confirmation do not match.").
			WithFields(apperr.FieldError{Field: "confirmPassword", Code: "MISMATCH", Message: "Does not match password."})
	}
	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}
	email = normaliseEmail(email)
	customer, err := s.store.GetCustomerByEmail(ctx, email)
	if err != nil {
		return apperr.New("RESET_TOKEN_INVALID", apperr.CategoryValidation, "This reset link is invalid or has expired.")
	}
	storedHash, expiresAt, err := s.store.GetCustomerPasswordResetToken(ctx, customer.ID)
	if err != nil {
		return apperr.Internal(err)
	}
	if storedHash == "" || time.Now().After(expiresAt) || storedHash != auth.HashOpaqueToken(resetToken) {
		return apperr.New("RESET_TOKEN_INVALID", apperr.CategoryValidation, "This reset link is invalid or has expired.")
	}
	hash, err := auth.HashSecret(newPassword)
	if err != nil {
		return apperr.Internal(err)
	}
	if err := s.store.UpdateCustomerPassword(ctx, customer.ID, hash); err != nil {
		return apperr.Internal(err)
	}
	_ = s.store.RevokeAllRefreshTokens(ctx, identityapi.SubjectCustomer, customer.ID)
	return nil
}

// --- Account self-service --------------------------------------------------

func (s *Service) GetProfile(ctx context.Context, customerID string) (*identityapi.Customer, error) {
	c, err := s.store.GetCustomerByID(ctx, customerID)
	if err != nil {
		return nil, apperr.NotFound("Account not found.")
	}
	return c, nil
}

func (s *Service) UpdateProfile(ctx context.Context, customerID, firstName, lastName, mobile string) (*identityapi.Customer, error) {
	if err := s.saveCustomerProfile(ctx, customerID, firstName, lastName, mobile); err != nil {
		return nil, err
	}
	return s.store.GetCustomerByID(ctx, customerID)
}

func (s *Service) ChangePassword(ctx context.Context, customerID, currentPassword, newPassword, confirmPassword string) error {
	customer, err := s.store.GetCustomerByID(ctx, customerID)
	if err != nil {
		return apperr.NotFound("Account not found.")
	}
	ok, err := auth.VerifySecret(customer.PasswordHash, currentPassword)
	if err != nil || !ok {
		return apperr.New("WRONG_CURRENT_PASSWORD", apperr.CategoryAuthentication, "Current password is incorrect.")
	}
	if newPassword != confirmPassword {
		return apperr.Validation("Password and confirmation do not match.")
	}
	if err := validatePasswordStrength(newPassword); err != nil {
		return err
	}
	hash, err := auth.HashSecret(newPassword)
	if err != nil {
		return apperr.Internal(err)
	}
	return s.store.UpdateCustomerPassword(ctx, customerID, hash)
}

func (s *Service) DeleteAccount(ctx context.Context, customerID, password string) error {
	customer, err := s.store.GetCustomerByID(ctx, customerID)
	if err != nil {
		return apperr.NotFound("Account not found.")
	}
	ok, err := auth.VerifySecret(customer.PasswordHash, password)
	if err != nil || !ok {
		return apperr.New("INVALID_CREDENTIALS", apperr.CategoryAuthentication, "Incorrect password.")
	}
	if err := s.store.SoftDeleteCustomer(ctx, customerID); err != nil {
		return apperr.Internal(err)
	}
	_ = s.store.RevokeAllRefreshTokens(ctx, identityapi.SubjectCustomer, customerID)
	return nil
}

func (s *Service) SetMarketingOptIn(ctx context.Context, customerID string, optIn bool, ip string) error {
	return s.store.SetCustomerMarketingOptIn(ctx, customerID, optIn, ip)
}

// --- Address book (ACC-06) --------------------------------------------------

func (s *Service) ListAddresses(ctx context.Context, customerID string) ([]identityapi.CustomerAddress, error) {
	return s.store.ListCustomerAddresses(ctx, customerID)
}

func (s *Service) CreateAddress(ctx context.Context, customerID string, a identityapi.CustomerAddress) (*identityapi.CustomerAddress, error) {
	a.CustomerID = customerID
	if a.Country == "" {
		a.Country = "US"
	}
	count, err := s.store.CountCustomerAddresses(ctx, customerID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if count == 0 {
		a.IsDefault = true // first address is always the default
	}
	out, err := s.store.CreateCustomerAddress(ctx, a)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, apperr.NotFound("Account not found.")
		}
		return nil, apperr.Internal(err)
	}
	return out, nil
}

func (s *Service) UpdateAddress(ctx context.Context, customerID, addressID string, a identityapi.CustomerAddress) (*identityapi.CustomerAddress, error) {
	if err := s.store.UpdateCustomerAddress(ctx, customerID, addressID, a); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, apperr.NotFound("Address not found.")
		}
		return nil, apperr.Internal(err)
	}
	return s.store.GetCustomerAddress(ctx, customerID, addressID)
}

func (s *Service) DeleteAddress(ctx context.Context, customerID, addressID string) error {
	addr, err := s.store.GetCustomerAddress(ctx, customerID, addressID)
	if err != nil {
		return apperr.NotFound("Address not found.")
	}
	if addr.IsDefault {
		count, err := s.store.CountCustomerAddresses(ctx, customerID)
		if err == nil && count > 1 {
			return apperr.New("CANNOT_DELETE_DEFAULT", apperr.CategoryValidation, "Set another address as default before deleting this one.")
		}
	}
	if err := s.store.DeleteCustomerAddress(ctx, customerID, addressID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return apperr.NotFound("Address not found.")
		}
		return apperr.Internal(err)
	}
	return nil
}

func (s *Service) SetDefaultAddress(ctx context.Context, customerID, addressID string) error {
	if err := s.store.SetDefaultAddress(ctx, customerID, addressID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return apperr.NotFound("Address not found.")
		}
		return apperr.Internal(err)
	}
	return nil
}

// --- Recipient book (ACC-07) -------------------------------------------------

func (s *Service) ListRecipients(ctx context.Context, customerID string) ([]identityapi.CustomerRecipient, error) {
	return s.store.ListCustomerRecipients(ctx, customerID)
}

func (s *Service) CreateRecipient(ctx context.Context, customerID string, r identityapi.CustomerRecipient) (*identityapi.CustomerRecipient, error) {
	r.CustomerID = customerID
	out, err := s.store.CreateCustomerRecipient(ctx, r)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, apperr.NotFound("Account not found.")
		}
		return nil, apperr.Internal(err)
	}
	return out, nil
}

func (s *Service) UpdateRecipient(ctx context.Context, customerID, recipientID string, r identityapi.CustomerRecipient) error {
	if err := s.store.UpdateCustomerRecipient(ctx, customerID, recipientID, r); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return apperr.NotFound("Recipient not found.")
		}
		return apperr.Internal(err)
	}
	return nil
}

func (s *Service) DeleteRecipient(ctx context.Context, customerID, recipientID string) error {
	if err := s.store.DeleteCustomerRecipient(ctx, customerID, recipientID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return apperr.NotFound("Recipient not found.")
		}
		return apperr.Internal(err)
	}
	return nil
}

// --- Staff auth (RBAC, Section 6) -------------------------------------------

// StaffLogin authenticates a staff member and returns a token pair whose
// access token carries every role assignment they hold, so the client and
// every downstream permission check can resolve scope per request.
func (s *Service) StaffLogin(ctx context.Context, email, password, ip string) (*auth.TokenPair, *identityapi.StaffUser, []identityapi.StaffRoleAssignment, error) {
	email = normaliseEmail(email)
	// if ok, err := s.limiter.Allow(ctx, "staff-login:"+ip, loginRateLimit, loginRateWindow); err == nil && !ok {
	// 	return nil, nil, nil, apperr.RateLimited("Too many login attempts. Please try again later.")
	// }

	staff, err := s.store.GetStaffByEmail(ctx, email)
	if err != nil {
		return nil, nil, nil, apperr.New("INVALID_CREDENTIALS", apperr.CategoryAuthentication, "Incorrect email or password.")
	}
	ok, err := auth.VerifySecret(staff.PasswordHash, password)
	if err != nil || !ok {
		return nil, nil, nil, apperr.New("INVALID_CREDENTIALS", apperr.CategoryAuthentication, "Incorrect email or password.")
	}
	if !staff.IsActive {
		return nil, nil, nil, apperr.New("ACCOUNT_DEACTIVATED", apperr.CategoryAuthorization, "This staff account has been deactivated.")
	}

	assignments, err := s.store.ListStaffRoleAssignments(ctx, staff.ID)
	if err != nil {
		return nil, nil, nil, apperr.Internal(err)
	}
	if len(assignments) == 0 {
		return nil, nil, nil, apperr.New("NO_ROLE_ASSIGNED", apperr.CategoryAuthorization, "This account has no role assigned. Contact your administrator.")
	}
	if err := s.assertScopeActive(ctx, assignments); err != nil {
		return nil, nil, nil, err
	}

	pair, err := s.issueStaffTokenPair(ctx, staff, assignments)
	if err != nil {
		return nil, nil, nil, err
	}
	return pair, staff, assignments, nil
}

func (s *Service) issueStaffTokenPair(ctx context.Context, u *identityapi.StaffUser, assignments []identityapi.StaffRoleAssignment) (*auth.TokenPair, error) {
	return s.issueStaffTokenPairAt(ctx, u, assignments, auth.StaffLocation{})
}

// issueStaffTokenPairAt issues a pair for a session tied to a store or
// central kitchen (POS / KDS login). The location goes on both tokens: the
// access token for the per-request check, the refresh token so a refresh
// keeps it.
func (s *Service) issueStaffTokenPairAt(ctx context.Context, u *identityapi.StaffUser, assignments []identityapi.StaffRoleAssignment, loc auth.StaffLocation) (*auth.TokenPair, error) {
	roles := toRoleAssignments(assignments)
	access, exp, err := s.staffTokens.IssueStaffAccessTokenAt(u.ID, u.Email, derefString(u.BusinessID), roles, s.staffAccessTTL, loc)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	refresh, err := auth.NewOpaqueToken()
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if _, err := s.store.CreateRefreshTokenAt(ctx, identityapi.SubjectStaff, u.ID, auth.HashOpaqueToken(refresh),
		time.Now().Add(s.staffRefreshTTL), "", loc.BranchID, loc.KitchenID); err != nil {
		return nil, apperr.Internal(err)
	}
	return &auth.TokenPair{AccessToken: access, RefreshToken: refresh, ExpiresAt: exp}, nil
}

func toRoleAssignments(rows []identityapi.StaffRoleAssignment) []auth.RoleAssignment {
	out := make([]auth.RoleAssignment, 0, len(rows))
	for _, r := range rows {
		out = append(out, auth.RoleAssignment{
			Role: r.Role, ScopeType: r.ScopeType, BranchID: r.BranchID,
			CentralKitchenID: r.CentralKitchenID,
		})
	}
	return out
}

// RefreshStaff rotates a staff refresh token and re-materialises current
// role assignments, so a permission change takes effect on next refresh, at
// most staffAccessTTL after being granted.
func (s *Service) RefreshStaff(ctx context.Context, refreshToken string) (*auth.TokenPair, error) {
	row, err := s.store.GetRefreshTokenByHash(ctx, identityapi.SubjectStaff, auth.HashOpaqueToken(refreshToken))
	if err != nil || row.RevokedAt != nil || time.Now().After(row.ExpiresAt) {
		return nil, apperr.Unauthenticated("Session expired. Please log in again.")
	}
	staff, err := s.store.GetStaffByID(ctx, row.SubjectID)
	if err != nil {
		return nil, apperr.Unauthenticated("Session expired. Please log in again.")
	}
	if !staff.IsActive {
		_ = s.store.RevokeRefreshToken(ctx, row.ID, "")
		return nil, errSessionDeactivated()
	}
	// A POS / KDS session stays tied to the store it signed in to, and ends
	// when that store stops trading.
	loc := auth.StaffLocation{BranchID: row.LocationBranchID, KitchenID: row.LocationKitchenID}
	if err := s.assertLocationTrading(ctx, loc); err != nil {
		_ = s.store.RevokeRefreshToken(ctx, row.ID, "")
		return nil, err
	}
	assignments, err := s.store.ListStaffRoleAssignments(ctx, staff.ID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	pair, err := s.issueStaffTokenPairAt(ctx, staff, assignments, loc)
	if err != nil {
		return nil, err
	}
	_ = s.store.RevokeRefreshToken(ctx, row.ID, "")
	return pair, nil
}

func (s *Service) LogoutStaff(ctx context.Context, refreshToken string) error {
	row, err := s.store.GetRefreshTokenByHash(ctx, identityapi.SubjectStaff, auth.HashOpaqueToken(refreshToken))
	if err != nil {
		return nil
	}
	return s.store.RevokeRefreshToken(ctx, row.ID, "")
}

// CreateStaffUser provisions a new staff account with one initial role
// assignment (ADM-18 staff invite). A production invite flow would email a
// set-password link instead of accepting a password directly; the handler
// layer can add that later without changing this signature's shape.
// validateRoleScope enforces the pairing the database also enforces, early
// enough to explain itself.
//
//	brand      — no branch (head office sees everything)
//	branch     — a branch
//
// The franchise scope was retired in migration 0070; branches carry an
// ownership_type ("corporate_store" / "franchise_store") instead.
func validateRoleScope(role identityapi.StaffRoleAssignment) error {
	if role.Role == "" {
		return apperr.Validation("A role is required.").
			WithFields(apperr.FieldError{Field: "role", Code: "REQUIRED", Message: "Choose a role."})
	}
	if !tenant.IsKnownRole(role.Role) {
		return apperr.Validation(role.Role + " is not a role.").
			WithFields(apperr.FieldError{Field: "role", Code: "UNKNOWN", Message: "Choose one of the listed roles."})
	}

	// Central-kitchen staff work the KDS at a store or at a central kitchen,
	// and a central-kitchen scope belongs to nobody else.
	if role.Role == tenant.RoleCentralKitchenStaff &&
		role.ScopeType != tenant.ScopeBranch && role.ScopeType != tenant.ScopeCentralKitchen {
		return apperr.Validation("Central kitchen staff are assigned to a store or to a corporate office.").
			WithFields(apperr.FieldError{Field: "scopeType", Code: "SCOPE_MISMATCH", Message: "branch or central_kitchen."})
	}
	if role.ScopeType == tenant.ScopeCentralKitchen && role.Role != tenant.RoleCentralKitchenStaff {
		return apperr.Validation("Only central kitchen staff can be assigned to a corporate office.").
			WithFields(apperr.FieldError{Field: "role", Code: "SCOPE_MISMATCH", Message: "Use central_kitchen_staff."})
	}

	switch role.ScopeType {
	case tenant.ScopeCentralKitchen:
		if role.CentralKitchenID == nil || *role.CentralKitchenID == "" {
			return apperr.Validation("Choose which corporate office this person works at.").
				WithFields(apperr.FieldError{Field: "centralKitchenId", Code: "REQUIRED", Message: "Required for a corporate-office role."})
		}
		if role.BranchID != nil && *role.BranchID != "" {
			return apperr.Validation("A corporate-office role is not tied to one store.").
				WithFields(apperr.FieldError{Field: "branchId", Code: "SCOPE_MISMATCH", Message: "Remove the store."})
		}
	case tenant.ScopeBrand:
		if role.BranchID != nil {
			return apperr.Validation("A brand-scoped role covers the whole network and cannot be tied to one outlet.").
				WithFields(apperr.FieldError{Field: "scopeType", Code: "SCOPE_MISMATCH", Message: "Remove the outlet."})
		}
	case tenant.ScopeFranchise:
		return apperr.Validation("Franchise-wide roles are no longer supported. Assign the role to each outlet instead.").
			WithFields(apperr.FieldError{Field: "scopeType", Code: "UNSUPPORTED", Message: "Use brand or branch."})
	case tenant.ScopeBranch:
		if role.BranchID == nil || *role.BranchID == "" {
			return apperr.Validation("Choose which outlet this person works at.").
				WithFields(apperr.FieldError{Field: "branchId", Code: "REQUIRED", Message: "Required for an outlet-scoped role."})
		}
		if role.CentralKitchenID != nil && *role.CentralKitchenID != "" {
			return apperr.Validation("A store role is not tied to a corporate office.").
				WithFields(apperr.FieldError{Field: "centralKitchenId", Code: "SCOPE_MISMATCH", Message: "Remove the corporate office."})
		}
	default:
		return apperr.Validation("Choose whether this role covers the brand or one outlet.").
			WithFields(apperr.FieldError{Field: "scopeType", Code: "REQUIRED", Message: "brand or branch."})
	}
	return nil
}

// mayGrant decides whether the caller is allowed to hand out this role.
//
// Two rules, both about not letting somebody appoint above themselves:
//
//   - Only a super admin creates a super admin. Otherwise the permission to
//     manage a team is also the permission to seize the network.
//   - Only a brand-scoped caller creates a brand-scoped role. An outlet-level
//     manager may staff outlets they hold, not the whole company.
//
// Anything else is left to validateRoleScope and the scope checks on the
// route — this is the ceiling, not the whole policy.
func mayGrant(claims *auth.StaffClaims, role identityapi.StaffRoleAssignment) error {
	if role.Role == "super_admin" && !hasRole(claims, "super_admin") {
		return apperr.PermissionDenied("Only a super admin can create another super admin.")
	}
	if role.ScopeType == "brand" && !tenant.IsBrandScoped(claims) {
		return apperr.PermissionDenied("Only brand-wide staff can create a brand-wide role. Choose an outlet instead.")
	}
	if role.ScopeType == tenant.ScopeBranch && role.BranchID != nil && !tenant.CanAccessBranch(claims, *role.BranchID) {
		return apperr.PermissionDenied("You can only assign roles at outlets you manage.")
	}
	if role.ScopeType == tenant.ScopeCentralKitchen && !tenant.IsBrandScoped(claims) {
		return apperr.PermissionDenied("Only brand-wide staff can assign corporate-office roles.")
	}
	return nil
}

// assertCentralKitchenExists refuses a role at a kitchen that is not there.
func (s *Service) assertCentralKitchenExists(ctx context.Context, role identityapi.StaffRoleAssignment) error {
	if role.ScopeType != tenant.ScopeCentralKitchen || role.CentralKitchenID == nil || s.locations == nil {
		return nil
	}
	active, err := s.locations.CentralKitchenActive(ctx, *role.CentralKitchenID)
	if err != nil {
		return apperr.Internal(err)
	}
	// Another business's kitchen reads as unknown, as its stores do.
	if active {
		if active, err = s.store.CentralKitchenInBusiness(ctx, *role.CentralKitchenID); err != nil {
			return apperr.Internal(err)
		}
	}
	if !active {
		return apperr.Validation("That corporate office does not exist or is inactive.").
			WithFields(apperr.FieldError{Field: "centralKitchenId", Code: "UNKNOWN", Message: "Unknown or inactive corporate office."})
	}
	return nil
}

func hasRole(claims *auth.StaffClaims, role string) bool {
	if claims == nil {
		return false
	}
	for _, r := range claims.RoleAssignments {
		if r.Role == role {
			return true
		}
	}
	return false
}

// CreateStaffUserAsSystem provisions an account with no caller to check.
//
// Only for bootstrapping: the first super admin has to come from somewhere,
// and at that point there is nobody to have authorised it. Every request-borne
// path must use CreateStaffUser, which enforces who may appoint whom.
func (s *Service) CreateStaffUserAsSystem(ctx context.Context, email, password, firstName, lastName string, role identityapi.StaffRoleAssignment) (*identityapi.StaffUser, error) {
	return s.createStaffUser(ctx, "", email, password, firstName, lastName, role)
}

// CreateBusinessOwner gives a newly registered business its first login: a
// super admin over the whole business. The platform API calls it; there is
// no staff caller to authorise.
func (s *Service) CreateBusinessOwner(ctx context.Context, businessID, email, password, firstName, lastName string) (*identityapi.StaffUser, error) {
	if strings.TrimSpace(businessID) == "" {
		return nil, apperr.Validation("A business is required.")
	}
	if err := validateOwnerFields(email, password, firstName, lastName); err != nil {
		return nil, err
	}
	return s.createStaffUser(ctx, businessID, email, password, firstName, lastName, identityapi.StaffRoleAssignment{
		Role:      tenant.RoleSuperAdmin,
		ScopeType: tenant.ScopeBrand,
	})
}

// validateOwnerFields checks the details of a business's first login, so a
// typo is a 400 naming the field rather than an account nobody can use.
func validateOwnerFields(email, password, firstName, lastName string) error {
	var fields []apperr.FieldError
	if !strings.Contains(email, "@") {
		fields = append(fields, apperr.FieldError{Field: "email", Code: "INVALID", Message: "Enter an email address."})
	}
	if len(password) < 12 {
		fields = append(fields, apperr.FieldError{Field: "password", Code: "TOO_SHORT", Message: "At least 12 characters."})
	}
	if strings.TrimSpace(firstName) == "" {
		fields = append(fields, apperr.FieldError{Field: "firstName", Code: "REQUIRED", Message: "Required."})
	}
	if strings.TrimSpace(lastName) == "" {
		fields = append(fields, apperr.FieldError{Field: "lastName", Code: "REQUIRED", Message: "Required."})
	}
	if len(fields) > 0 {
		return apperr.Validation("Check the owner's details.").WithFields(fields...)
	}
	return nil
}

func (s *Service) CreateStaffUser(ctx context.Context, claims *auth.StaffClaims, email, password, firstName, lastName string, role identityapi.StaffRoleAssignment) (*identityapi.StaffUser, error) {
	return s.CreateStaffUserWithPasscode(ctx, claims, email, password, firstName, lastName, role, "")
}

/*
 * CreateStaffUserWithPasscode is CreateStaffUser plus an optional POS/KDS
 * passcode, validated (exactly 4 digits, free at the person's location) before
 * anything is written, so a bad code never leaves a half-created account.
 */
func (s *Service) CreateStaffUserWithPasscode(ctx context.Context, claims *auth.StaffClaims, email, password, firstName, lastName string, role identityapi.StaffRoleAssignment, passcode string) (*identityapi.StaffUser, error) {
	if err := mayGrant(claims, role); err != nil {
		return nil, err
	}
	if err := validateRoleScope(role); err != nil {
		return nil, err
	}
	if err := s.assertCentralKitchenExists(ctx, role); err != nil {
		return nil, err
	}
	passcode = strings.TrimSpace(passcode)
	if passcode != "" {
		if err := validatePasscodeFormat(passcode); err != nil {
			return nil, err
		}
		if err := s.assertPasscodeFree(ctx, []identityapi.StaffRoleAssignment{role}, "", passcode); err != nil {
			return nil, err
		}
	}
	// New staff join the business of whoever adds them.
	staff, err := s.createStaffUser(ctx, claimsBusinessID(claims), email, password, firstName, lastName, role)
	if err != nil {
		return nil, err
	}
	if passcode != "" {
		if err := s.storePasscode(ctx, staff.ID, passcode); err != nil {
			return nil, apperr.Internal(err)
		}
	}
	return staff, nil
}

func (s *Service) createStaffUser(ctx context.Context, businessID, email, password, firstName, lastName string, role identityapi.StaffRoleAssignment) (*identityapi.StaffUser, error) {
	email = normaliseEmail(email)
	if _, err := s.store.GetStaffByEmail(ctx, email); err == nil {
		return nil, apperr.New("EMAIL_ALREADY_EXISTS", apperr.CategoryConflict, "A staff account with this email already exists.")
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, apperr.Internal(err)
	}
	// The scope is checked here so a mismatch is a 400 that says which field
	// is wrong, rather than a 500 from a CHECK constraint the caller cannot
	// see. A branch manager with no branch is the common mistake, and until
	// this existed it produced "an unexpected error occurred".
	if err := validateRoleScope(role); err != nil {
		return nil, err
	}

	hash, err := auth.HashSecret(password)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	// One transaction: the account and the role that makes it usable are
	// written together. They used to be two statements, so a rejected role
	// left an account behind that could sign in and do nothing.
	staff, err := s.store.CreateStaffUserWithRole(ctx, businessID, email, hash, firstName, lastName, role)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	// No invite email: admin hands over the first password or passcode.
	return staff, nil
}

func (s *Service) AddStaffRoleAssignment(ctx context.Context, staffID string, role identityapi.StaffRoleAssignment) (*identityapi.StaffRoleAssignment, error) {
	role.StaffUserID = staffID
	out, err := s.store.CreateStaffRoleAssignment(ctx, role)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

// ListStaffNetwork returns the staff directory (SA-16), scoped to what the
// caller may see: brand-scoped roles (super admin, finance) see everyone;
// everyone else only sees staff who share at least one branch with one of
// the caller's own role assignments.
func (s *Service) ListStaffNetwork(ctx context.Context, claims *auth.StaffClaims, f identityapi.StaffDirectoryFilter) ([]identityapi.StaffDirectoryEntry, error) {
	all, err := s.store.ListStaffNetwork(ctx, f)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if tenant.IsBrandScoped(claims) {
		return all, nil
	}
	out := make([]identityapi.StaffDirectoryEntry, 0, len(all))
	for _, e := range all {
		for _, role := range e.Roles {
			if role.BranchID != nil && tenant.CanAccessBranch(claims, *role.BranchID) {
				out = append(out, e)
				break
			}
		}
	}
	return out, nil
}

func (s *Service) DeactivateStaffUser(ctx context.Context, staffID string) error {
	// Not found for another business's staff, so their sessions are left alone.
	if err := s.store.DeactivateStaffUser(ctx, staffID); err != nil {
		return staffNotFoundOrInternal(err)
	}
	_ = s.store.RevokeAllRefreshTokens(ctx, identityapi.SubjectStaff, staffID)
	return nil
}

// UpdateStaffUser changes a staff member's details, role and scope.
//
// The gap this fills: staff could be invited and deactivated, but never
// edited — so a branch manager moving branches, or a viewer being promoted to
// owner, had no path at all (docs/api-gaps.md §1.4 in the admin repo).
//
// Role and scope are replaced together because they are one decision: a role
// without its scope is meaningless, and applying them separately would leave a
// window where someone is, say, a branch manager of nothing.
func (s *Service) UpdateStaffUser(
	ctx context.Context,
	claims *auth.StaffClaims,
	staffID, firstName, lastName, role, scopeType string,
	branchID *string,
) error {
	return s.UpdateStaffUserKDS(ctx, claims, staffID, firstName, lastName, role, scopeType, branchID, nil, nil)
}

/*
 * UpdateStaffUserKDS is UpdateStaffUser plus the two KDS additions: a
 * central-kitchen scope, and the passcode (nil leaves it, "" removes POS/KDS
 * access, four digits sets it).
 */
func (s *Service) UpdateStaffUserKDS(
	ctx context.Context,
	claims *auth.StaffClaims,
	staffID, firstName, lastName, role, scopeType string,
	branchID, centralKitchenID, passcode *string,
) error {
	// Validate the new role before touching anything, so a refused role
	// change does not leave a half-applied profile edit behind. The same
	// ceiling as an invite applies: this used to skip mayGrant, which let
	// anyone with staff.manage promote an account to brand-wide super admin.
	var assignment identityapi.StaffRoleAssignment
	if role != "" {
		switch scopeType {
		case tenant.ScopeBrand:
			branchID, centralKitchenID = nil, nil
		case tenant.ScopeBranch:
			centralKitchenID = nil
		case tenant.ScopeCentralKitchen:
			branchID = nil
		}
		assignment = identityapi.StaffRoleAssignment{Role: role, ScopeType: scopeType, BranchID: branchID, CentralKitchenID: centralKitchenID}
		if err := validateRoleScope(assignment); err != nil {
			return err
		}
		if err := mayGrant(claims, assignment); err != nil {
			return err
		}
		if err := s.assertCentralKitchenExists(ctx, assignment); err != nil {
			return err
		}
	}
	if passcode != nil && strings.TrimSpace(*passcode) != "" {
		code := strings.TrimSpace(*passcode)
		if err := validatePasscodeFormat(code); err != nil {
			return err
		}
		scopes := []identityapi.StaffRoleAssignment{assignment}
		if role == "" {
			current, err := s.store.ListStaffRoleAssignments(ctx, staffID)
			if err != nil {
				return staffNotFoundOrInternal(err)
			}
			scopes = current
		}
		if err := s.assertPasscodeFree(ctx, scopes, staffID, code); err != nil {
			return err
		}
	}
	if err := s.store.UpdateStaffProfile(ctx, staffID, firstName, lastName); err != nil {
		return staffNotFoundOrInternal(err)
	}
	if role != "" {
		if err := s.store.ReplaceStaffRoleAssignment(ctx, staffID, role, scopeType, branchID, centralKitchenID); err != nil {
			return staffNotFoundOrInternal(err)
		}
	}
	if passcode != nil {
		if err := s.storePasscode(ctx, staffID, *passcode); err != nil {
			return staffNotFoundOrInternal(err)
		}
	}
	return nil
}

// ReactivateStaffUser restores access to a previously deactivated account.
func (s *Service) ReactivateStaffUser(ctx context.Context, staffID string) error {
	if err := s.store.ReactivateStaffUser(ctx, staffID); err != nil {
		return staffNotFoundOrInternal(err)
	}
	return nil
}

// staffNotFoundOrInternal keeps the two staff writes from each spelling the
// same not-found translation.
func staffNotFoundOrInternal(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return apperr.NotFound("Staff member not found.")
	}
	return apperr.Internal(err)
}
