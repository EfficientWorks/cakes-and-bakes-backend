package domain

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/store"
)

// CreatePOSCustomerInput defines the fields to create a walk-in / POS customer.
type CreatePOSCustomerInput struct {
	BranchID        string  `json:"branchId"`
	FirstName       string  `json:"firstName"`
	LastName        string  `json:"lastName"`
	Name            string  `json:"name"`
	Email           string  `json:"email"`
	MobileNumber    string  `json:"mobileNumber"`
	Address         string  `json:"address"`
	Street          string  `json:"street"`
	City            string  `json:"city"`
	State           string  `json:"state"`
	Postcode        string  `json:"postcode"`
	DateOfBirth     *string `json:"dob"`
	AnniversaryDate *string `json:"anniversaryDate"`
}

// resolveStaffBranch extracts the branch from the staff claims or validates the explicit branch ID.
func resolveStaffBranch(claims *auth.StaffClaims, explicitBranchID string) (string, error) {
	if claims == nil {
		return "", apperr.Unauthenticated("Staff authentication required.")
	}

	explicit := strings.TrimSpace(explicitBranchID)
	if explicit != "" {
		if tenant.IsBrandScoped(claims) {
			return explicit, nil
		}
		for _, ra := range claims.RoleAssignments {
			if ra.BranchID != nil && *ra.BranchID == explicit {
				return explicit, nil
			}
		}
		return "", apperr.PermissionDenied("You do not have access to this outlet.")
	}

	// Fallback to sole/assigned branch from token claims
	for _, ra := range claims.RoleAssignments {
		if ra.BranchID != nil && *ra.BranchID != "" {
			return *ra.BranchID, nil
		}
	}

	return "", apperr.Validation("An outlet is required.").
		WithFields(apperr.FieldError{Field: "branchId", Code: "REQUIRED", Message: "Required."})
}

// CreatePOSCustomer creates a new customer tied to the staff member's branch.
// It does not set a password, does not store a password hash, and sends no welcome email.
func (s *Service) CreatePOSCustomer(ctx context.Context, claims *auth.StaffClaims, in CreatePOSCustomerInput) (*identityapi.Customer, error) {
	branchID, err := resolveStaffBranch(claims, in.BranchID)
	if err != nil {
		return nil, err
	}

	firstName := strings.TrimSpace(in.FirstName)
	lastName := strings.TrimSpace(in.LastName)

	if firstName == "" && strings.TrimSpace(in.Name) != "" {
		parts := strings.Fields(strings.TrimSpace(in.Name))
		if len(parts) > 0 {
			firstName = parts[0]
			if len(parts) > 1 {
				lastName = strings.Join(parts[1:], " ")
			}
		}
	}

	if firstName == "" {
		return nil, apperr.Validation("First name is required.").
			WithFields(apperr.FieldError{Field: "firstName", Code: "REQUIRED", Message: "Required."})
	}

	var emailPtr *string
	cleanEmail := normaliseEmail(in.Email)
	if cleanEmail != "" {
		// Unique within this store's POS customers only, like the mobile.
		if _, err := s.store.GetPOSCustomerByEmail(ctx, cleanEmail, branchID); err == nil {
			return nil, errEmailTaken()
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, apperr.Internal(err)
		}
		emailPtr = &cleanEmail
	}

	var mobilePtr *string
	mobile := normaliseMobile(in.MobileNumber)
	if mobile == "" {
		return nil, apperr.Validation("Mobile number is required.").
			WithFields(apperr.FieldError{Field: "mobileNumber", Code: "REQUIRED", Message: "Required."})
	}
	if len(mobile) < 7 || len(mobile) > 15 {
		return nil, apperr.Validation("Mobile number must be 7 to 15 digits.").
			WithFields(apperr.FieldError{Field: "mobileNumber", Code: "INVALID", Message: "7 to 15 digits."})
	}
	// Unique within this store's POS customers only: the same number may belong
	// to a website account or to a customer at another store.
	if existing, err := s.mobileTaken(ctx, branchID, mobile, ""); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, mobileConflict(existing)
	}
	mobilePtr = &mobile

	customer, err := s.store.CreatePOSCustomer(ctx, branchID, firstName, lastName, emailPtr, mobilePtr)
	if err != nil {
		// Another till added the same number at this store a moment ago.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "idx_customers_pos_mobile_per_branch" {
			if existing, lookupErr := s.mobileTaken(ctx, branchID, mobile, ""); lookupErr == nil && existing != nil {
				return nil, mobileConflict(existing)
			}
			return nil, errMobileExists()
		}
		return nil, apperr.Internal(err)
	}

	city := strings.TrimSpace(in.City)
	postcode := strings.TrimSpace(in.Postcode)
	address := strings.TrimSpace(in.Address)
	street := strings.TrimSpace(in.Street)
	state := strings.TrimSpace(in.State)
	if city != "" || postcode != "" || address != "" || street != "" || state != "" {
		streetVal := street
		if streetVal == "" {
			streetVal = address
		}
		if streetVal == "" {
			streetVal = city
		}
		addr, err := s.store.CreateCustomerAddress(ctx, identityapi.CustomerAddress{
			CustomerID: customer.ID,
			Address:    address,
			Street:     streetVal,
			City:       city,
			State:      state,
			Postcode:   postcode,
			Country:    "US",
			IsDefault:  true,
		})
		if err != nil {
			slog.Error("failed to create pos customer address", "customer_id", customer.ID, "error", err)
			return nil, apperr.Internal(fmt.Errorf("create customer address: %w", err))
		}
		if addr != nil {
			applyAddress(customer, *addr)
		}
	}

	// Apply date of birth and anniversary date if provided at creation time.
	if in.DateOfBirth != nil || in.AnniversaryDate != nil {
		var dobTime, annivTime *time.Time
		if in.DateOfBirth != nil {
			raw := strings.TrimSpace(*in.DateOfBirth)
			if raw != "" {
				if t, err := time.Parse("2006-01-02", raw); err == nil {
					dobTime = &t
					customer.DateOfBirth = &t
				}
			}
		}
		if in.AnniversaryDate != nil {
			raw := strings.TrimSpace(*in.AnniversaryDate)
			if raw != "" {
				if t, err := time.Parse("2006-01-02", raw); err == nil {
					annivTime = &t
					customer.AnniversaryDate = &t
				}
			}
		}
		if dobTime != nil || annivTime != nil {
			_, _ = s.store.UpdatePOSCustomer(ctx, customer.ID, customer.FirstName, customer.LastName, nonEmptyPtr(customer.Email), customer.MobileNumber, dobTime, annivTime)
		}
	}

	return customer, nil
}

// ListPOSCustomers returns customers from the staff member's branch with customer_type = 'pos'.
func (s *Service) ListPOSCustomers(ctx context.Context, claims *auth.StaffClaims, explicitBranchID, search string, page, limit int) ([]identityapi.Customer, int, error) {
	branchID, err := resolveStaffBranch(claims, explicitBranchID)
	if err != nil {
		return nil, 0, err
	}

	customers, total, err := s.store.ListPOSCustomers(ctx, branchID, search, page, limit)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}

	return customers, total, nil
}

// GetPOSCustomer retrieves a customer by ID, ensuring they belong to the caller's branch and have customer_type = 'pos'.
func (s *Service) GetPOSCustomer(ctx context.Context, claims *auth.StaffClaims, customerID string) (*identityapi.Customer, error) {
	id := strings.TrimSpace(customerID)
	if id == "" {
		return nil, apperr.NotFound("Customer not found.")
	}

	branchID, err := resolveStaffBranch(claims, "")
	if err != nil && !tenant.IsBrandScoped(claims) {
		return nil, err
	}

	customer, err := s.store.GetPOSCustomerByID(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, apperr.NotFound("Customer not found.")
		}
		return nil, apperr.Internal(err)
	}

	if customer.CustomerType != "pos" {
		return nil, apperr.NotFound("Customer not found.")
	}

	// Enforce branch isolation unless brand-scoped
	if !tenant.IsBrandScoped(claims) {
		if customer.BranchID == nil || *customer.BranchID != branchID {
			return nil, apperr.PermissionDenied("You do not have access to this customer.")
		}
	}

	// Populate primary address if available
	if addrs, addrErr := s.store.ListCustomerAddresses(ctx, customer.ID); addrErr == nil && len(addrs) > 0 {
		// Prefer default address, otherwise first
		chosen := addrs[0]
		for _, a := range addrs {
			if a.IsDefault {
				chosen = a
				break
			}
		}
		applyAddress(customer, chosen)
	}
	return customer, nil
}

// applyAddress puts a customer's address row on the customer. `address` is the
// full line the POS shows; when only a street was saved it stands in for it.
func applyAddress(customer *identityapi.Customer, a identityapi.CustomerAddress) {
	nonEmpty := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	customer.Address = nonEmpty(a.Address)
	if customer.Address == nil {
		customer.Address = nonEmpty(a.Street)
	}
	customer.Street = nonEmpty(a.Street)
	customer.City = nonEmpty(a.City)
	customer.State = nonEmpty(a.State)
	customer.Postcode = nonEmpty(a.Postcode)
}

// posCustomerEmail resolves the email an update asks for: nil keeps the
// customer's own, "" clears it, anything else must look like an email and not
// belong to a different customer.
func (s *Service) posCustomerEmail(ctx context.Context, asked *string, existing *identityapi.Customer) (*string, error) {
	if asked == nil {
		return nonEmptyPtr(existing.Email), nil
	}
	email := normaliseEmail(*asked)
	if email == "" {
		return nil, nil
	}
	if !strings.Contains(email, "@") || strings.ContainsAny(email, " \t") {
		return nil, apperr.Validation("Enter a valid email address.").
			WithFields(apperr.FieldError{Field: "email", Code: "INVALID", Message: "Not an email address."})
	}
	branchID := ""
	if existing.BranchID != nil {
		branchID = *existing.BranchID
	}
	other, err := s.store.GetPOSCustomerByEmail(ctx, email, branchID)
	if err == nil && other.ID != existing.ID {
		return nil, errEmailTaken()
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, apperr.Internal(err)
	}
	return &email, nil
}

func errEmailTaken() error {
	return apperr.New("EMAIL_ALREADY_EXISTS", apperr.CategoryConflict, "A customer with this email already exists at this store.").
		WithFields(apperr.FieldError{Field: "email", Code: "TAKEN", Message: "Already used by another customer."})
}

func nonEmptyPtr(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// UpdatePOSCustomerInput defines the fields that may be updated on a POS customer.
type UpdatePOSCustomerInput struct {
	FirstName string
	// nil leaves the last name alone; a pointer to "" clears it.
	LastName     *string
	MobileNumber string
	Phone        string
	// nil keeps the email; a pointer to "" clears it.
	Email           *string
	DateOfBirth     *string // ISO-8601 date string (YYYY-MM-DD), nil means no change
	AnniversaryDate *string // ISO-8601 date string (YYYY-MM-DD), nil means no change
	Postcode        *string
	Address         *string
	Street          *string
	City            *string
	State           *string
}

// UpdatePOSCustomer updates name, mobile number, and anniversary date for an existing POS customer.
func (s *Service) UpdatePOSCustomer(
	ctx context.Context,
	claims *auth.StaffClaims,
	customerID string,
	in UpdatePOSCustomerInput,
) (*identityapi.Customer, error) {
	id := strings.TrimSpace(customerID)
	if id == "" {
		return nil, apperr.NotFound("Customer not found.")
	}

	branchID, err := resolveStaffBranch(claims, "")
	if err != nil && !tenant.IsBrandScoped(claims) {
		return nil, err
	}

	existing, err := s.store.GetPOSCustomerByID(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, apperr.NotFound("Customer not found.")
		}
		return nil, apperr.Internal(err)
	}

	if existing.CustomerType != "pos" {
		return nil, apperr.NotFound("Customer not found.")
	}

	if !tenant.IsBrandScoped(claims) {
		if existing.BranchID == nil || *existing.BranchID != branchID {
			return nil, apperr.PermissionDenied(
				"You do not have access to this customer.",
			)
		}
	}

	// First name
	firstName := strings.TrimSpace(in.FirstName)
	if firstName == "" {
		firstName = existing.FirstName
	}

	// Last name: absent = keep, present (even empty/null) = set to that.
	lastName := existing.LastName
	if in.LastName != nil {
		lastName = strings.TrimSpace(*in.LastName)
	}

	// Mobile number
	mobile := normaliseMobile(in.MobileNumber)
	if mobile == "" {
		mobile = normaliseMobile(in.Phone)
	}

	var mobilePtr *string
	if mobile != "" {
		if len(mobile) < 7 || len(mobile) > 15 {
			return nil, apperr.Validation("Mobile number must be 7 to 15 digits.").
				WithFields(apperr.FieldError{Field: "mobileNumber", Code: "INVALID", Message: "7 to 15 digits."})
		}
		if other, err := s.mobileTaken(ctx, posCustomerScope(existing), mobile, existing.ID); err != nil {
			return nil, err
		} else if other != nil {
			return nil, mobileConflict(other)
		}
		mobilePtr = &mobile
	} else {
		mobilePtr = existing.MobileNumber
	}

	// Email: unique among this store's POS customers, so one that belongs to
	// someone else here is refused rather than silently dropped.
	emailPtr, err := s.posCustomerEmail(ctx, in.Email, existing)
	if err != nil {
		return nil, err
	}

	// Date of birth
	var dateOfBirth *time.Time

	if in.DateOfBirth != nil {
		raw := strings.TrimSpace(*in.DateOfBirth)

		if raw != "" {
			t, parseErr := time.Parse("2006-01-02", raw)
			if parseErr != nil {
				return nil, apperr.Validation(
					"Invalid dob format; expected YYYY-MM-DD.",
				).WithFields(
					apperr.FieldError{
						Field:   "dob",
						Code:    "INVALID_FORMAT",
						Message: "Expected YYYY-MM-DD.",
					},
				)
			}

			dateOfBirth = &t
		}
	} else {
		dateOfBirth = existing.DateOfBirth
	}

	// Anniversary date
	var anniversaryDate *time.Time

	if in.AnniversaryDate != nil {
		raw := strings.TrimSpace(*in.AnniversaryDate)

		if raw != "" {
			t, parseErr := time.Parse("2006-01-02", raw)
			if parseErr != nil {
				return nil, apperr.Validation(
					"Invalid anniversaryDate format; expected YYYY-MM-DD.",
				).WithFields(
					apperr.FieldError{
						Field:   "anniversaryDate",
						Code:    "INVALID_FORMAT",
						Message: "Expected YYYY-MM-DD.",
					},
				)
			}

			anniversaryDate = &t
		}
	} else {
		anniversaryDate = existing.AnniversaryDate
	}

	// Address fields
	var postCode, city, address, street, state *string

	if in.Postcode != nil {
		value := strings.TrimSpace(*in.Postcode)
		postCode = &value
	} else if existing.Postcode != nil {
		postCode = existing.Postcode
	}

	if in.City != nil {
		value := strings.TrimSpace(*in.City)
		city = &value
	} else if existing.City != nil {
		city = existing.City
	}

	if in.Address != nil {
		value := strings.TrimSpace(*in.Address)
		address = &value
	} else if existing.Address != nil {
		address = existing.Address
	}

	if in.Street != nil {
		value := strings.TrimSpace(*in.Street)
		street = &value
	} else if existing.Street != nil {
		street = existing.Street
	}

	if in.State != nil {
		value := strings.TrimSpace(*in.State)
		state = &value
	} else if existing.State != nil {
		state = existing.State
	}

	// Update customer table
	customer, err := s.store.UpdatePOSCustomer(
		ctx,
		id,
		firstName,
		lastName,
		emailPtr,
		mobilePtr,
		dateOfBirth,
		anniversaryDate,
	)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, apperr.NotFound("Customer not found.")
		}
		if errors.Is(err, store.ErrEmailTaken) {
			return nil, errEmailTaken()
		}

		return nil, apperr.Internal(err)
	}

	// Update customer_addresses table
	err = s.store.UpdatePOSCustomerAddress(
		ctx,
		id,
		postCode,
		city,
		address,
		street,
		state,
	)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, apperr.NotFound("Customer address not found.")
		}

		return nil, apperr.Internal(err)
	}

	// Populate primary/default address in response
	if addrs, addrErr := s.store.ListCustomerAddresses(ctx, customer.ID); addrErr == nil && len(addrs) > 0 {
		chosen := addrs[0]

		for _, a := range addrs {
			if a.IsDefault {
				chosen = a
				break
			}
		}

		applyAddress(customer, chosen)
	}

	return customer, nil
}

// DeletePOSCustomer soft-deletes a POS customer if found and branch-authorized.
func (s *Service) DeletePOSCustomer(ctx context.Context, claims *auth.StaffClaims, customerID string) error {
	id := strings.TrimSpace(customerID)
	if id == "" {
		return apperr.NotFound("Customer not found.")
	}

	branchID, err := resolveStaffBranch(claims, "")
	if err != nil && !tenant.IsBrandScoped(claims) {
		return err
	}

	customer, err := s.store.GetPOSCustomerByID(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return apperr.NotFound("Customer not found.")
		}
		return apperr.Internal(err)
	}

	if customer.CustomerType != "pos" {
		return apperr.NotFound("Customer not found.")
	}

	// Enforce branch isolation unless brand-scoped
	if !tenant.IsBrandScoped(claims) {
		if customer.BranchID == nil || *customer.BranchID != branchID {
			return apperr.PermissionDenied("You do not have access to this customer.")
		}
	}

	if err := s.store.DeletePOSCustomerByID(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return apperr.NotFound("Customer not found.")
		}
		return apperr.Internal(err)
	}

	return nil
}

// normaliseMobile keeps only the digits. Spaces, dashes, brackets and the
// leading + all vary with who typed the number and mean nothing for whether
// two numbers are the same phone.
func normaliseMobile(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// mobileTaken reports whether another live customer in the same scope already
// has this number: that store's POS customers when branchID is set, the
// website accounts when it is empty. exceptID lets an update keep its own
// number without tripping over itself.
func (s *Service) mobileTaken(ctx context.Context, branchID, mobile, exceptID string) (*identityapi.Customer, error) {
	existing, err := s.store.GetCustomerByMobile(ctx, mobile, branchID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if existing.ID == exceptID {
		return nil, nil
	}
	return existing, nil
}

func mobileConflict(existing *identityapi.Customer) error {
	// The id goes back so the till can offer "use this customer" instead of
	// making staff search for who it was.
	return apperr.New("MOBILE_ALREADY_EXISTS", apperr.CategoryConflict,
		"A customer with this mobile number already exists.").
		WithFields(apperr.FieldError{Field: "mobileNumber", Code: "DUPLICATE", Message: "Already in use."}).
		WithMeta(map[string]any{
			"existingCustomerId": existing.ID,
			"existingName":       strings.TrimSpace(existing.FirstName + " " + existing.LastName),
		})
}

// posCustomerScope is the uniqueness scope of an existing customer: its store
// for a POS customer, the website accounts otherwise.
func posCustomerScope(c *identityapi.Customer) string {
	if c.CustomerType == "pos" && c.BranchID != nil {
		return *c.BranchID
	}
	return ""
}
