package domain

import (
	"context"
	"errors"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/store"
)

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
