package printtemplate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store/shared"
)

func (s *Repo) GetPrintReceiptTemplate(ctx context.Context, branchID string) (*franchiseapi.PrintReceiptTemplate, error) {
	var t franchiseapi.PrintReceiptTemplate
	var alignment []byte
	err := s.pool.QueryRow(ctx, receiptTemplateSelect, branchID).Scan(
		&t.ID, &t.BranchID, &t.IsActive,
		&t.ShowCustomerName, &t.ShowOrderNotes, &t.ShowProductComments, &t.ShowAddonPrice,
		&t.IncludeCustomerReceipt, &alignment, &t.DateTimeFormat,
		&t.BusinessName, &t.MobileNo, &t.Address, &t.GSTVATNumber, &t.BusinessLogo,
		&t.ShowTime, &t.TextWeight, &t.CreatedAt, &t.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, shared.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(alignment, &t.ColumnAlignment)
	return &t, nil
}

func (s *Repo) getSimplePrintTemplate(ctx context.Context, table, receiptType, branchID string) (*franchiseapi.PrintSimpleTemplate, error) {
	var t franchiseapi.PrintSimpleTemplate
	t.ReceiptType = receiptType
	err := s.pool.QueryRow(ctx, simpleTemplateSelect(table), branchID).Scan(
		&t.ID, &t.BranchID, &t.IsActive, &t.ShowTime, &t.TextWeight, &t.CreatedAt, &t.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, shared.ErrNotFound
	}
	return &t, err
}

func (s *Repo) GetPrintKOTTemplate(ctx context.Context, branchID string) (*franchiseapi.PrintSimpleTemplate, error) {
	return s.getSimplePrintTemplate(ctx, "print_kot_templates", franchiseapi.ReceiptTypeKOT, branchID)
}

func (s *Repo) GetPrintCancelTemplate(ctx context.Context, branchID string) (*franchiseapi.PrintSimpleTemplate, error) {
	return s.getSimplePrintTemplate(ctx, "print_cancel_templates", franchiseapi.ReceiptTypeCancel, branchID)
}

// UpdatePrintTemplateField sets exactly one column on exactly one table, for
// exactly one branch. table and column are never taken from the request —
// they only ever come from printTemplateFieldRegistry in the domain package,
// a hardcoded whitelist — so building the query with fmt.Sprintf here is
// safe: an identifier can't be parameterised with $1 the way a value can,
// and nothing user-supplied ever reaches this string.
func (s *Repo) UpdatePrintTemplateField(ctx context.Context, table, column, branchID string, value interface{}) error {
	q := fmt.Sprintf(`UPDATE %s SET %s = $2, updated_at = NOW() WHERE branch_id = $1`, table, column)
	tag, err := s.pool.Exec(ctx, q, branchID, value)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

// EnsureDefaultPrintTemplates gives a newly created branch all three rows —
// receipt, kot, cancel — at column defaults, so GET never 404s. The receipt
// row's business_name/mobile_no/address seed from the branch itself; kot and
// cancel have no such fields to seed.
func (s *Repo) EnsureDefaultPrintTemplates(ctx context.Context, branchID, businessName, address, mobileNo string) error {
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO print_receipt_templates (branch_id, business_name, address, mobile_no)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (branch_id) DO NOTHING`,
		branchID, businessName, address, mobileNo); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO print_kot_templates (branch_id) VALUES ($1)
		ON CONFLICT (branch_id) DO NOTHING`, branchID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO print_cancel_templates (branch_id) VALUES ($1)
		ON CONFLICT (branch_id) DO NOTHING`, branchID)
	return err
}
