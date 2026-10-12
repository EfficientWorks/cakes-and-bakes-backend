package kitchen

import (
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store/shared"
)

func scanCentralKitchen(row pgx.Row) (*franchiseapi.CentralKitchen, error) {
	var ck franchiseapi.CentralKitchen
	err := row.Scan(&ck.ID, &ck.Name, &ck.BusinessID, &ck.Street, &ck.City, &ck.State, &ck.Postcode, &ck.Country,
		&ck.Phone, &ck.Email, &ck.Timezone, &ck.Status,
		&ck.Manager.Name, &ck.Manager.Phone, &ck.Manager.Email,
		&ck.CreatedAt, &ck.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	ck.BranchIDs = []string{}
	ck.Branches = []franchiseapi.BranchRef{}
	return &ck, nil
}
