package printer

import (
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store/shared"
)

func scanPrinter(row pgx.Row) (*franchiseapi.Printer, error) {
	var p franchiseapi.Printer
	if err := row.Scan(&p.ID, &p.BranchID, &p.Name, &p.Kind,
		&p.IPAddress, &p.DeviceAddress, &p.IsConnected, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, shared.ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

func scanKDSPrinter(row pgx.Row) (*franchiseapi.KDSPrinter, error) {
	var p franchiseapi.KDSPrinter
	err := row.Scan(&p.ID, &p.ScopeType, &p.BranchID, &p.CentralKitchenID, &p.Name, &p.Kind,
		&p.IPAddress, &p.Port, &p.DeviceAddress, &p.IsConnected, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}
