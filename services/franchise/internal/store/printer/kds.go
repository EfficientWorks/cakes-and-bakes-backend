package printer

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store/shared"
)

// ListKDSPrinters returns one location's printers, newest first.
func (s *Repo) ListKDSPrinters(ctx context.Context, scope franchiseapi.KDSScope, kind, search string) ([]franchiseapi.KDSPrinter, error) {
	q := kdsPrinterSelect
	args := []any{}
	if scope.Type == franchiseapi.LocationCentralKitchen {
		args = append(args, scope.CentralKitchenID)
		q += ` WHERE central_kitchen_id = $1`
	} else {
		args = append(args, scope.BranchID)
		q += ` WHERE branch_id = $1`
	}
	if kind != "" {
		args = append(args, kind)
		q += ` AND kind = $` + strconv.Itoa(len(args))
	}
	if term := strings.TrimSpace(search); term != "" {
		args = append(args, "%"+strings.ToLower(term)+"%")
		q += ` AND lower(name) LIKE $` + strconv.Itoa(len(args))
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []franchiseapi.KDSPrinter{}
	for rows.Next() {
		p, err := scanKDSPrinter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetKDSPrinter reads one printer.
func (s *Repo) GetKDSPrinter(ctx context.Context, id string) (*franchiseapi.KDSPrinter, error) {
	return scanKDSPrinter(s.pool.QueryRow(ctx, kdsPrinterSelect+` WHERE id = $1`, id))
}

// CreateKDSPrinter adds a printer. Connecting a Bluetooth printer drops the
// location's previous one, as on the POS.
func (s *Repo) CreateKDSPrinter(ctx context.Context, scope franchiseapi.KDSScope, in franchiseapi.KDSPrinterInput, createdBy string) (*franchiseapi.KDSPrinter, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	connected := in.IsConnected != nil && *in.IsConnected
	if connected && in.Kind == franchiseapi.PrinterBluetooth {
		if err := disconnectOtherKDSBluetooth(ctx, tx, scope, ""); err != nil {
			return nil, err
		}
	}
	port := 9100
	if in.Port != nil && *in.Port > 0 {
		port = *in.Port
	}
	var branchID, kitchenID any
	if scope.Type == franchiseapi.LocationCentralKitchen {
		kitchenID = scope.CentralKitchenID
	} else {
		branchID = scope.BranchID
	}
	p, err := scanKDSPrinter(tx.QueryRow(ctx, `
		INSERT INTO kds_printers (scope_type, branch_id, central_kitchen_id, name, kind, ip_address, port,
			device_address, is_connected, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`+kdsPrinterReturning,
		scope.Type, branchID, kitchenID, strings.TrimSpace(shared.Deref(in.Name)), in.Kind,
		shared.NilIfEmpty(strings.TrimSpace(shared.Deref(in.IPAddress))), port,
		shared.NilIfEmpty(strings.TrimSpace(shared.Deref(in.DeviceAddress))), connected, shared.NilIfEmpty(createdBy)))
	if err != nil {
		return nil, err
	}
	return p, tx.Commit(ctx)
}

// UpdateKDSPrinter edits the fields sent.
func (s *Repo) UpdateKDSPrinter(ctx context.Context, id string, in franchiseapi.KDSPrinterInput) (*franchiseapi.KDSPrinter, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := scanKDSPrinter(tx.QueryRow(ctx, kdsPrinterSelect+` WHERE id = $1`, id))
	if err != nil {
		return nil, err
	}
	if in.IsConnected != nil && *in.IsConnected && current.Kind == franchiseapi.PrinterBluetooth {
		scope := franchiseapi.KDSScope{Type: current.ScopeType}
		if current.BranchID != nil {
			scope.BranchID = *current.BranchID
		}
		if current.CentralKitchenID != nil {
			scope.CentralKitchenID = *current.CentralKitchenID
		}
		if err := disconnectOtherKDSBluetooth(ctx, tx, scope, id); err != nil {
			return nil, err
		}
	}
	p, err := scanKDSPrinter(tx.QueryRow(ctx, `
		UPDATE kds_printers
		   SET name           = COALESCE($2, name),
		       ip_address     = COALESCE($3, ip_address),
		       port           = COALESCE($4, port),
		       device_address = COALESCE($5, device_address),
		       is_connected   = COALESCE($6, is_connected),
		       updated_at     = NOW()
		 WHERE id = $1`+kdsPrinterReturning,
		id, shared.TrimmedPtr(in.Name), shared.TrimmedPtr(in.IPAddress), in.Port, shared.TrimmedPtr(in.DeviceAddress), in.IsConnected))
	if err != nil {
		return nil, err
	}
	return p, tx.Commit(ctx)
}

// DeleteKDSPrinter removes a printer outright.
func (s *Repo) DeleteKDSPrinter(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM kds_printers WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func disconnectOtherKDSBluetooth(ctx context.Context, tx pgx.Tx, scope franchiseapi.KDSScope, exceptID string) error {
	col, val := "branch_id", scope.BranchID
	if scope.Type == franchiseapi.LocationCentralKitchen {
		col, val = "central_kitchen_id", scope.CentralKitchenID
	}
	_, err := tx.Exec(ctx, `
		UPDATE kds_printers SET is_connected = FALSE, updated_at = NOW()
		 WHERE `+col+` = $1 AND kind = 'bluetooth' AND is_connected
		   AND ($2 = '' OR id <> $2::uuid)`, val, exceptID)
	return err
}
