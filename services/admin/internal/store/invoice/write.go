package invoice

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/timex"
	"bakehouse/services/admin/adminapi"
	"bakehouse/services/admin/internal/store/shared"
)

// CreateInvoice writes the invoice and its lines in one transaction.
//
// The reference is taken from a sequence inside that transaction, so two
// people raising an invoice at the same moment cannot be handed the same
// number — which a count of existing rows would happily do.
func (s *Repo) CreateInvoice(ctx context.Context, in adminapi.CreateInvoiceInput, totals adminapi.Invoice, createdBy string) (string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var reference string
	if err := tx.QueryRow(ctx, `SELECT 'INV-' || nextval('invoice_reference_seq')`).Scan(&reference); err != nil {
		return "", err
	}

	// The supplying outlet must exist; this used to be implied by looking up
	// its franchise, and pgx.ErrNoRows is still what callers get if it doesn't
	// — or if it is another business's. The invoice belongs to the outlet's
	// business, which the filter makes the caller's own on a staff request.
	var businessID *string
	if err := tx.QueryRow(ctx, `SELECT business_id FROM branches WHERE id = $1 AND `+inBusiness("business_id", 2),
		append([]any{in.BranchID}, businessArgs(ctx)...)...).Scan(&businessID); err != nil {
		return "", err
	}

	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO invoices (reference, kind, branch_id,
			issued_date, message, bank_name, account_number, sort_code,
			subtotal_cents, discount_cents, discount_pct, tax_cents, tax_name, total_cents, created_by,
			customer_first_name, customer_last_name, customer_email, customer_phone, customer_city, customer_state, customer_address, receiver_first_name, receiver_last_name, receiver_email, receiver_phone, receiver_city, receiver_state, receiver_address, is_gift, business_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31)
		RETURNING id`,
		reference, in.Kind, in.BranchID,
		in.IssuedDate, in.Message, in.BankName, in.AccountNumber, in.SortCode,
		totals.SubtotalCents, totals.DiscountCents, totals.DiscountPct,
		totals.TaxCents, totals.TaxName, totals.TotalCents, shared.NilIfBlank(createdBy),
		in.BilledTo.FirstName, in.BilledTo.LastName, in.BilledTo.Email, in.BilledTo.Phone, in.BilledTo.City, in.BilledTo.State, in.BilledTo.Address, in.Receiver.FirstName, in.Receiver.LastName, in.Receiver.Email, in.Receiver.Phone, in.Receiver.City, in.Receiver.State, in.Receiver.Address, in.IsGift, businessID).Scan(&id)
	if err != nil {
		return "", err
	}

	for i, item := range in.Items {
		mods, _ := json.Marshal(orEmptyModifiers(item.Modifiers))
		if _, err := tx.Exec(ctx, `
			INSERT INTO invoice_items (invoice_id, product_id, product_name, variant_name, modifiers,
				quantity, unit_price_cents, line_total_cents, sort_order)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			id, shared.NilIfBlank(item.ProductID), item.ProductName, item.VariantName, mods,
			item.Quantity, item.UnitPriceCents, item.UnitPriceCents*int64(item.Quantity), i); err != nil {
			return "", err
		}
	}

	return id, tx.Commit(ctx)
}

// SetInvoiceStatus marks an invoice paid, unpaid or void.
func (s *Repo) SetInvoiceStatus(ctx context.Context, id, status string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE invoices
		   SET status = $2,
		       -- paidAt from the request wins; else keep an existing stamp; else now.
		       paid_at = CASE WHEN $2 = 'paid' THEN COALESCE($3::timestamptz, paid_at, NOW()) ELSE NULL END,
		       updated_at = NOW()
		 WHERE id = $1 AND `+inBusiness("business_id", 4),
		append([]any{id, status, timex.EventTimeFrom(ctx)}, businessArgs(ctx)...)...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// MarkInvoiceSent records that the document went out.
func (s *Repo) MarkInvoiceSent(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE invoices SET sent_at = COALESCE($2::timestamptz, NOW()), updated_at = NOW()
		  WHERE id = $1 AND `+inBusiness("business_id", 3),
		append([]any{id, timex.EventTimeFrom(ctx)}, businessArgs(ctx)...)...)
	return err
}
