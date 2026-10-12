package request

import (
	"context"
	"fmt"
	"strings"

	"bakehouse/services/franchise/franchiseapi"
)

// GetBranchRequest returns a single request by ID.
func (s *Repo) GetBranchRequest(ctx context.Context, id string) (*franchiseapi.BranchRequest, error) {
	return scanBranchRequest(s.pool.QueryRow(ctx, branchRequestSelect+` WHERE br.id = $1`, id))
}

// ListBranchRequests returns requests filtered by the supplied criteria with pagination and statistics.
// Zero-value fields in the filter are not applied.
func (s *Repo) ListBranchRequests(ctx context.Context, f franchiseapi.BranchRequestFilter) ([]franchiseapi.BranchRequest, int, franchiseapi.BranchRequestStats, error) {
	// Build scoping clauses for stats (branch_id).
	scopeClauses := []string{}
	scopeArgs := []interface{}{}
	n := 1

	if f.BranchID != "" {
		scopeClauses = append(scopeClauses, fmt.Sprintf("br.branch_id = $%d", n))
		scopeArgs = append(scopeArgs, f.BranchID)
		n++
	}
	if f.Type != "" {
		scopeClauses = append(scopeClauses, fmt.Sprintf("br.type = $%d", n))
		scopeArgs = append(scopeArgs, f.Type)
		n++
	}
	if f.Search != "" {
		// Match ticket-code prefixes (e.g. TCK-93EI), title, description, or branch name.
		scopeClauses = append(scopeClauses, fmt.Sprintf(
			"(br.title ILIKE $%d OR br.description ILIKE $%d OR COALESCE(b.name,'') ILIKE $%d)",
			n, n, n,
		))
		scopeArgs = append(scopeArgs, "%"+f.Search+"%")
		n++
	}

	statsQ := `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE br.status = 'approved'),
			COUNT(*) FILTER (WHERE br.status = 'rejected'),
			COUNT(*) FILTER (WHERE br.status = 'pending'),
			COUNT(*) FILTER (WHERE br.status = 'completed'),
			COUNT(*) FILTER (WHERE br.status = 'cancelled')
		FROM branch_requests br
		LEFT JOIN branches b ON b.id = br.branch_id`
	if len(scopeClauses) > 0 {
		statsQ += " WHERE " + strings.Join(scopeClauses, " AND ")
	}

	var stats franchiseapi.BranchRequestStats
	if err := s.pool.QueryRow(ctx, statsQ, scopeArgs...).Scan(
		&stats.Total,
		&stats.Approved,
		&stats.Rejected,
		&stats.Pending,
		&stats.Completed,
		&stats.Cancelled,
	); err != nil {
		return nil, 0, stats, fmt.Errorf("branch requests stats: %w", err)
	}

	// Build full filter clauses for listing and pagination.
	filterClauses := append([]string{}, scopeClauses...)
	filterArgs := append([]interface{}{}, scopeArgs...)
	fn := len(filterArgs) + 1

	if f.Status != "" {
		filterClauses = append(filterClauses, fmt.Sprintf("br.status = $%d", fn))
		filterArgs = append(filterArgs, f.Status)
		fn++
	}

	// Calculate total matching records for the current filter.
	var total int
	switch f.Status {
	case "":
		total = int(stats.Total)
	case franchiseapi.RequestStatusApproved:
		total = int(stats.Approved)
	case franchiseapi.RequestStatusRejected:
		total = int(stats.Rejected)
	case franchiseapi.RequestStatusPending:
		total = int(stats.Pending)
	case franchiseapi.RequestStatusCompleted:
		total = int(stats.Completed)
	case franchiseapi.RequestStatusCancelled:
		total = int(stats.Cancelled)
	default:
		countQ := "SELECT COUNT(*) FROM branch_requests br"
		if len(filterClauses) > 0 {
			countQ += " WHERE " + strings.Join(filterClauses, " AND ")
		}
		if err := s.pool.QueryRow(ctx, countQ, filterArgs...).Scan(&total); err != nil {
			return nil, 0, stats, fmt.Errorf("branch requests count: %w", err)
		}
	}

	page := f.Page
	if page < 1 {
		page = 1
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := (page - 1) * limit

	listQ := branchRequestSelect
	if len(filterClauses) > 0 {
		listQ += " WHERE " + strings.Join(filterClauses, " AND ")
	}
	listQ += " ORDER BY br.created_at DESC"
	listQ += fmt.Sprintf(" LIMIT $%d OFFSET $%d", fn, fn+1)
	filterArgs = append(filterArgs, limit, offset)

	rows, err := s.pool.Query(ctx, listQ, filterArgs...)
	if err != nil {
		return nil, 0, stats, err
	}
	defer rows.Close()

	out := make([]franchiseapi.BranchRequest, 0)
	for rows.Next() {
		r, err := scanBranchRequest(rows)
		if err != nil {
			return nil, 0, stats, err
		}
		out = append(out, *r)
	}
	return out, total, stats, rows.Err()
}
