package invoice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/tenant"
	"bakehouse/services/admin/adminapi"
)

/*
 * ListInvoices returns one page, newest first, with the total behind it.
 *
 * Paged in the database rather than in the console: the list is expected to
 * grow without limit, and a screen that fetches every invoice to show ten of
 * them gets slower every week it is used.
 */
func (s *Repo) ListInvoices(ctx context.Context, f adminapi.InvoiceListFilter) ([]adminapi.Invoice, int, error) {
	if f.Page <= 0 {
		f.Page = 1
	}
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 10
	}

	where := []string{"TRUE"}
	args := []any{}
	if tenant.Scoped(ctx) {
		args = append(args, tenant.BusinessParam(ctx))
		where = append(where, fmt.Sprintf("i.business_id IS NOT DISTINCT FROM $%d", len(args)))
	}

	if f.Search != "" {
		// The billed party lives on the invoice row now (0053), not in a
		// joined customers/branches table — the old c./bb. aliases no longer
		// exist and made every search fail.
		args = append(args, "%"+f.Search+"%")
		n := len(args)
		where = append(where, fmt.Sprintf(
			`(i.reference ILIKE $%d
			  OR i.customer_first_name ILIKE $%d OR i.customer_last_name ILIKE $%d OR i.customer_email ILIKE $%d
			  OR (i.customer_first_name || ' ' || i.customer_last_name) ILIKE $%d
			  OR b.name ILIKE $%d)`, n, n, n, n, n, n))
	}
	if f.Kind != "" {
		args = append(args, f.Kind)
		where = append(where, fmt.Sprintf("i.kind = $%d", len(args)))
	}
	if f.Status != "" {
		args = append(args, f.Status)
		where = append(where, fmt.Sprintf("i.status = $%d", len(args)))
	}
	if f.BranchID != "" {
		args = append(args, f.BranchID)
		where = append(where, fmt.Sprintf("i.branch_id = $%d", len(args)))
	}
	clause := strings.Join(where, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+invoiceFrom+` WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, f.Limit, (f.Page-1)*f.Limit)
	q := `SELECT` + invoiceColumns + invoiceFrom + ` WHERE ` + clause +
		fmt.Sprintf(` ORDER BY i.issued_date DESC, i.created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []adminapi.Invoice{}
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *inv)
	}
	return out, total, rows.Err()
}

// GetInvoice returns one invoice with its lines. Another business's invoice
// is pgx.ErrNoRows, as if it did not exist.
func (s *Repo) GetInvoice(ctx context.Context, id string) (*adminapi.Invoice, error) {
	row := s.pool.QueryRow(
		ctx,
		`SELECT `+invoiceColumns+invoiceFrom+` WHERE i.id = $1 AND `+inBusiness("i.business_id", 2),
		append([]any{id}, businessArgs(ctx)...)...,
	)

	inv, err := scanInvoice(row)
	if err != nil {
		return nil, err
	}

	itemRows, err := s.pool.Query(ctx, `
        SELECT
            ii.id,
            COALESCE(ii.product_id::text, ''),
            ii.product_name,
            ii.variant_name,
            ii.modifiers,
            ii.quantity,
            ii.unit_price_cents,
            ii.line_total_cents,
            COALESCE(p.disclaimer, '')
        FROM invoice_items ii
        LEFT JOIN products p ON p.id = ii.product_id
        WHERE ii.invoice_id = $1
        ORDER BY ii.sort_order, ii.id
    `, id)
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()

	for itemRows.Next() {
		var it adminapi.InvoiceItem
		var mods []byte

		if err := itemRows.Scan(
			&it.ID,
			&it.ProductID,
			&it.ProductName,
			&it.VariantName,
			&mods,
			&it.Quantity,
			&it.UnitPriceCents,
			&it.LineTotalCents,
			&it.Disclaimer,
		); err != nil {
			return nil, err
		}

		_ = json.Unmarshal(mods, &it.Modifiers)
		inv.Items = append(inv.Items, it)
	}

	// The disclaimer is the invoice's own business's: every business keeps
	// its own site_content, and an unfiltered read would pick any one of them.
	var disclaimer *string
	if err := s.pool.QueryRow(
		ctx,
		`SELECT value FROM site_content
		  WHERE key = 'disclaimer'
		    AND business_id IS NOT DISTINCT FROM (SELECT business_id FROM invoices WHERE id = $1)`,
		id,
	).Scan(&disclaimer); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		// No disclaimer configured is not a broken invoice.
		return nil, err
	}

	inv.Disclaimer = disclaimer

	return inv, itemRows.Err()
}
