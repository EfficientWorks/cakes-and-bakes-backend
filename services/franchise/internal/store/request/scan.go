package request

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store/shared"
)

func scanBranchRequest(row pgx.Row) (*franchiseapi.BranchRequest, error) {
	var r franchiseapi.BranchRequest
	var storeName string
	err := row.Scan(
		&r.ID, &r.BranchID,
		&r.RequestedBy, &r.RequestedAt, &r.Role,
		&r.Type, &r.Title, &r.Description, &r.FieldsToChange, &r.Status,
		&r.ApprovedAt, &r.ApprovedBy,
		&r.RejectedAt, &r.RejectedBy, &r.RejectionNote,
		&r.CompletedAt, &r.CompletedBy,
		&r.CreatedAt, &r.UpdatedAt,
		&r.ModifierType, &r.ModifierName,
		&storeName,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.ErrNotFound
		}
		return nil, fmt.Errorf("scan branch request: %w", err)
	}
	r.Store = storeName
	r.BranchName = storeName
	if r.FieldsToChange == nil {
		r.FieldsToChange = []string{}
	}
	return &r, nil
}
