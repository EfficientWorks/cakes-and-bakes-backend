package printer

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store/shared"
)

// ListPrinters returns one outlet's printers, newest first.
func (s *Repo) ListPrinters(ctx context.Context, f franchiseapi.PrinterFilter) ([]franchiseapi.Printer, error) {
	q := printerSelect + ` WHERE branch_id = $1`
	args := []interface{}{f.BranchID}

	if f.Kind != "" {
		args = append(args, f.Kind)
		q += ` AND kind = $` + strconv.Itoa(len(args))
	}
	if term := strings.TrimSpace(f.Search); term != "" {
		args = append(args, "%"+strings.ToLower(term)+"%")
		q += ` AND lower(name) LIKE $` + strconv.Itoa(len(args))
	}
	q += ` ORDER BY created_at DESC`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []franchiseapi.Printer{}
	for rows.Next() {
		p, err := scanPrinter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// GetPrinter reads one printer by id.
func (s *Repo) GetPrinter(ctx context.Context, id string) (*franchiseapi.Printer, error) {
	return scanPrinter(s.pool.QueryRow(ctx, printerSelect+` WHERE id = $1`, id))
}

/*
 * CreatePrinter adds a printer, honouring the one-Bluetooth-at-a-time rule.
 *
 * The disconnect and the insert share a transaction because the database
 * enforces "one connected Bluetooth printer per outlet" with a partial unique
 * index: doing them apart would leave a window where the index is violated
 * and the insert fails for a reason that has nothing to do with the request.
 */
func (s *Repo) CreatePrinter(ctx context.Context, in franchiseapi.PrinterInput, createdBy string) (*franchiseapi.Printer, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	connected := in.IsConnected != nil && *in.IsConnected
	if connected && in.Kind == franchiseapi.PrinterBluetooth {
		if err := disconnectOtherBluetooth(ctx, tx, in.BranchID, ""); err != nil {
			return nil, err
		}
	}

	q := `
		INSERT INTO pos_printers (branch_id, name, kind, ip_address, device_address, is_connected, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7)` + printerReturning
	p, err := scanPrinter(tx.QueryRow(ctx, q,
		in.BranchID, strings.TrimSpace(shared.Deref(in.Name)), in.Kind,
		shared.NilIfEmpty(strings.TrimSpace(shared.Deref(in.IPAddress))),
		shared.NilIfEmpty(strings.TrimSpace(shared.Deref(in.DeviceAddress))),
		connected, shared.NilIfEmpty(createdBy)))
	if err != nil {
		return nil, err
	}
	return p, tx.Commit(ctx)
}

// UpdatePrinter edits the fields the caller actually sent.
func (s *Repo) UpdatePrinter(ctx context.Context, id string, in franchiseapi.PrinterInput) (*franchiseapi.Printer, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	current, err := scanPrinter(tx.QueryRow(ctx, printerSelect+` WHERE id = $1`, id))
	if err != nil {
		return nil, err
	}

	if in.IsConnected != nil && *in.IsConnected && current.Kind == franchiseapi.PrinterBluetooth {
		if err := disconnectOtherBluetooth(ctx, tx, current.BranchID, id); err != nil {
			return nil, err
		}
	}

	// COALESCE rather than a built-up SET list: a nil argument leaves the
	// column as it was, which is the same rule the pointer fields encode.
	q := `
		UPDATE pos_printers
		   SET name           = COALESCE($2, name),
		       ip_address     = COALESCE($3, ip_address),
		       device_address = COALESCE($4, device_address),
		       is_connected   = COALESCE($5, is_connected),
		       updated_at     = NOW()
		 WHERE id = $1` + printerReturning
	p, err := scanPrinter(tx.QueryRow(ctx, q, id,
		shared.TrimmedPtr(in.Name), shared.TrimmedPtr(in.IPAddress), shared.TrimmedPtr(in.DeviceAddress), in.IsConnected))
	if err != nil {
		return nil, err
	}
	return p, tx.Commit(ctx)
}

// SetPrinterConnected connects or disconnects one printer.
func (s *Repo) SetPrinterConnected(ctx context.Context, id string, connected bool) (*franchiseapi.Printer, error) {
	return s.UpdatePrinter(ctx, id, franchiseapi.PrinterInput{IsConnected: &connected})
}

// DeletePrinter removes a printer outright. There is no soft delete: a
// printer that has been thrown away should not linger on a settings screen.
func (s *Repo) DeletePrinter(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM pos_printers WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

// disconnectOtherBluetooth drops whatever Bluetooth printer the outlet is
// currently paired with, so the caller can connect a different one.
func disconnectOtherBluetooth(ctx context.Context, tx pgx.Tx, branchID, exceptID string) error {
	_, err := tx.Exec(ctx, `
		UPDATE pos_printers
		   SET is_connected = FALSE, updated_at = NOW()
		 WHERE branch_id = $1
		   AND kind = 'bluetooth'
		   AND is_connected
		   AND ($2 = '' OR id <> $2::uuid)`, branchID, exceptID)
	return err
}
