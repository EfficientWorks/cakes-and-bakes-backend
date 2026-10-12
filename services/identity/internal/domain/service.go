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
