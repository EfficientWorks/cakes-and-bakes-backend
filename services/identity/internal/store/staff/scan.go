package staff

import (
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/store/shared"
)

func scanStaff(row pgx.Row) (*identityapi.StaffUser, error) {
	var u identityapi.StaffUser
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FirstName, &u.LastName, &u.IsActive, &u.CreatedAt, &u.UpdatedAt, &u.BusinessID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.ErrNotFound
	}
	return &u, err
}
