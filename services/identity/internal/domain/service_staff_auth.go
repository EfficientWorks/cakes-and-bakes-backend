package domain

import (
	"context"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/services/identity/identityapi"
)

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
