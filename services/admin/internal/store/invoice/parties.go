package invoice

import (
	"context"
	"regexp"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/admin/adminapi"
)

// BranchTax reads the outlet's tax rate and what it calls it — the two things
// an invoice needs to price itself.
func (s *Repo) BranchTax(ctx context.Context, branchID string) (float64, string, error) {
	var rate *float64
	var name string
	err := s.pool.QueryRow(ctx,
		`SELECT tax_rate_pct, coalesce(tax_name, 'Tax') FROM branches WHERE id::text = $1`, branchID).
		Scan(&rate, &name)
	if err != nil {
		return 0, "", err
	}
	if rate == nil {
		return 0, name, nil
	}
	return *rate, name, nil
}

// CustomerContact reads a customer's contact details for an invoice's
// "Billed to" block.
func (s *Repo) CustomerContact(ctx context.Context, customerID string) (adminapi.InvoiceCustomerInput, error) {
	var p adminapi.InvoiceCustomerInput
	if !uuidPattern.MatchString(customerID) {
		return p, pgx.ErrNoRows
	}
	err := s.pool.QueryRow(ctx, `
		SELECT coalesce(first_name,''), coalesce(last_name,''), coalesce(email,''), coalesce(mobile_number,'')
		FROM customers WHERE id = $1 AND deleted_at IS NULL`, customerID).
		Scan(&p.FirstName, &p.LastName, &p.Email, &p.Phone)
	return p, err
}

// BranchContact reads an outlet's details for a store-to-store invoice.
func (s *Repo) BranchContact(ctx context.Context, branchID string) (adminapi.InvoiceCustomerInput, error) {
	var p adminapi.InvoiceCustomerInput
	err := s.pool.QueryRow(ctx, `
		SELECT name, coalesce(email,''), coalesce(phone,''), coalesce(street,''), coalesce(city,''), coalesce(state,'')
		FROM branches WHERE id::text = $1`, branchID).
		Scan(&p.FirstName, &p.Email, &p.Phone, &p.Address, &p.City, &p.State)
	return p, err
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
