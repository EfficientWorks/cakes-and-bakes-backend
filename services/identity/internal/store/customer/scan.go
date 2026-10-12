package customer

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/identity/identityapi"
	"bakehouse/services/identity/internal/store/shared"
)

func scanCustomer(row pgx.Row) (*identityapi.Customer, error) {
	var c identityapi.Customer
	var email *string
	err := row.Scan(&c.ID, &c.BranchID, &c.CustomerType, &email, &c.PasswordHash, &c.FirstName, &c.LastName, &c.MobileNumber,
		&c.IsVerified, &c.MarketingOptIn, &c.MarketingOptInAt, &c.DateOfBirth, &c.AnniversaryDate, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.ErrNotFound
		}
		return nil, fmt.Errorf("scan customer: %w", err)
	}
	if email != nil {
		c.Email = *email
	}
	return &c, nil
}
