package customer

import (
	"context"
	"fmt"

	"bakehouse/pkg/tenant"
	"bakehouse/services/admin/adminapi"
	"bakehouse/services/admin/internal/store/shared"
)

func (s *Repo) ListCustomers(ctx context.Context, f adminapi.CustomerListFilter) ([]adminapi.CustomerRecord, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 25
	}

	where := []string{"c.deleted_at IS NULL"}
	args := []interface{}{}
	if tenant.Scoped(ctx) {
		args = append(args, tenant.BusinessParam(ctx))
		where = append(where, fmt.Sprintf("c.business_id IS NOT DISTINCT FROM $%d", len(args)))
	}
	if f.Search != "" {
		args = append(args, "%"+f.Search+"%")
		idx := len(args)
		where = append(where, fmt.Sprintf("(coalesce(c.email, '') ILIKE $%d OR coalesce(c.first_name, '') ILIKE $%d OR coalesce(c.last_name, '') ILIKE $%d OR coalesce(c.mobile_number, '') ILIKE $%d)", idx, idx, idx, idx))
	}
	var branchArgIdx int
	if f.BranchID != "" {
		args = append(args, f.BranchID)
		branchArgIdx = len(args)
		where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM (SELECT * FROM orders WHERE deleted_at IS NULL) o2 WHERE o2.customer_id = c.id AND o2.branch_id = $%d)", branchArgIdx))
	}
	/*
	 * Where the customer actually buys.
	 *
	 * Asked of their orders rather than stored on the record, because it is
	 * not a property of the person: somebody who ordered online at Christmas
	 * and walked in the following week belongs to both, and a column would
	 * have to pick one and be wrong half the time.
	 */
	if f.Source == "web" || f.Source == "pos" {
		args = append(args, f.Source)
		where = append(where, fmt.Sprintf(
			"EXISTS (SELECT 1 FROM orders o3 WHERE o3.deleted_at IS NULL AND o3.customer_id = c.id AND o3.channel = $%d)",
			len(args)))
	}

	whereClause := where[0]
	for i := 1; i < len(where); i++ {
		whereClause += " AND " + where[i]
	}

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM customers c WHERE "+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	var statsCond string
	if branchArgIdx > 0 {
		statsCond = fmt.Sprintf("o.customer_id = c.id AND o.status <> 'cancelled' AND o.branch_id = $%d::uuid", branchArgIdx)
	} else {
		statsCond = "o.customer_id = c.id AND o.status <> 'cancelled'"
	}

	args = append(args, f.Limit, (f.Page-1)*f.Limit)
	q := fmt.Sprintf(`
		SELECT c.id, coalesce(c.email, ''), coalesce(c.first_name, ''), coalesce(c.last_name, ''), c.mobile_number, c.is_verified, c.created_at,
			coalesce(stats.total_orders,0), coalesce(stats.total_spend,0), stats.last_order_at,
			coalesce((SELECT json_agg(ct.tag) FROM customer_tags ct WHERE ct.customer_id = c.id), '[]')
		FROM customers c
		LEFT JOIN LATERAL (
			SELECT count(*) AS total_orders, coalesce(sum(total_cents),0) AS total_spend, max(created_at) AS last_order_at
			FROM (SELECT * FROM orders WHERE deleted_at IS NULL) o WHERE %s
		) stats ON true
		WHERE `+whereClause+`
		ORDER BY c.created_at DESC
		LIMIT $%d OFFSET $%d`, statsCond, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []adminapi.CustomerRecord
	for rows.Next() {
		var c adminapi.CustomerRecord
		var tagsRaw []byte
		if err := rows.Scan(&c.ID, &c.Email, &c.FirstName, &c.LastName, &c.MobileNumber, &c.IsVerified, &c.CreatedAt,
			&c.TotalOrders, &c.TotalSpendCents, &c.LastOrderAt, &tagsRaw); err != nil {
			return nil, 0, err
		}
		c.Tags = shared.DecodeStringArray(tagsRaw)
		out = append(out, c)
	}
	return out, total, rows.Err()
}

func (s *Repo) GetCustomerDetail(ctx context.Context, customerID, branchID string) (*adminapi.CustomerRecord, error) {
	const q = `
		SELECT c.id, coalesce(c.email, ''), coalesce(c.first_name, ''), coalesce(c.last_name, ''), c.mobile_number, c.is_verified, c.created_at,
			coalesce(stats.total_orders,0), coalesce(stats.total_spend,0), stats.last_order_at,
			coalesce((SELECT json_agg(ct.tag) FROM customer_tags ct WHERE ct.customer_id = c.id), '[]')
		FROM customers c
		LEFT JOIN LATERAL (
			SELECT count(*) AS total_orders,
				coalesce(sum(total_cents), 0) AS total_spend,
				max(created_at) AS last_order_at
			FROM (SELECT * FROM orders WHERE deleted_at IS NULL) o
			WHERE o.customer_id = c.id AND o.status <> 'cancelled'
			  AND ($2 = '' OR o.branch_id = $2::uuid)
		) stats ON true
		WHERE c.id = $1 AND c.deleted_at IS NULL
		  AND (NOT $3::boolean OR c.business_id IS NOT DISTINCT FROM $4::uuid)`

	var c adminapi.CustomerRecord
	var tagsRaw []byte
	err := s.pool.QueryRow(ctx, q, customerID, branchID, tenant.Scoped(ctx), tenant.BusinessParam(ctx)).Scan(&c.ID, &c.Email, &c.FirstName, &c.LastName, &c.MobileNumber, &c.IsVerified, &c.CreatedAt,
		&c.TotalOrders, &c.TotalSpendCents, &c.LastOrderAt, &tagsRaw)
	if err != nil {
		return nil, err
	}
	c.Tags = shared.DecodeStringArray(tagsRaw)
	return &c, nil
}

// CustomerHasOrderInScope reports whether the customer has ever ordered
// from the given branch — the basis for whether a branch-scoped staff
// member may view this customer at all.
func (s *Repo) CustomerHasOrderInScope(ctx context.Context, customerID, branchID string) (bool, error) {
	var exists bool
	const q = `SELECT EXISTS (SELECT 1 FROM orders WHERE deleted_at IS NULL AND customer_id = $1 AND branch_id = $2::uuid)`
	err := s.pool.QueryRow(ctx, q, customerID, branchID).Scan(&exists)
	return exists, err
}

func (s *Repo) ListCustomerOrders(ctx context.Context, customerID, branchID string) ([]adminapi.CustomerOrderSummary, error) {
	q := `
		SELECT o.id, o.reference, o.bill_no, o.branch_id, coalesce(b.name, ''), o.status,
		       o.fulfilment_type,
		       trim(coalesce(o.recipient_first_name, '') || ' ' || coalesce(o.recipient_last_name, '')),
		       to_char(o.scheduled_date, 'YYYY-MM-DD'), coalesce(o.slot_start::text, ''),
		       (SELECT count(*) FROM order_items i WHERE i.order_id = o.id),
		       o.total_cents, o.created_at
		  FROM (SELECT * FROM orders WHERE deleted_at IS NULL) o
		  LEFT JOIN branches b ON b.id = o.branch_id
		 WHERE o.customer_id = $1
		   AND ($2 = '' OR o.branch_id = $2::uuid)
		   AND ` + customerInBusiness(3) + `
		 ORDER BY o.created_at DESC LIMIT 100`
	rows, err := s.pool.Query(ctx, q, customerID, branchID, tenant.Scoped(ctx), tenant.BusinessParam(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// An empty slice, not nil: the console renders "no orders yet" from an
	// empty list and would have to special-case a null.
	out := []adminapi.CustomerOrderSummary{}
	for rows.Next() {
		var o adminapi.CustomerOrderSummary
		if err := rows.Scan(&o.ID, &o.Reference, &o.BillNo, &o.BranchID, &o.BranchName, &o.Status,
			&o.FulfilmentType, &o.RecipientName, &o.ScheduledDate, &o.SlotStart,
			&o.ItemCount, &o.TotalCents, &o.CreatedAt); err != nil {
			return nil, err
		}
		o.OrderNo = o.BillNo
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Repo) ListCustomerNotes(ctx context.Context, customerID string) ([]adminapi.CustomerNote, error) {
	q := `
		SELECT id, customer_id, author_staff_id, note, created_at
		FROM customer_notes WHERE customer_id = $1 AND ` + customerInBusiness(2) + `
		ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, q, customerID, tenant.Scoped(ctx), tenant.BusinessParam(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []adminapi.CustomerNote
	for rows.Next() {
		var n adminapi.CustomerNote
		if err := rows.Scan(&n.ID, &n.CustomerID, &n.AuthorStaffID, &n.Note, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
