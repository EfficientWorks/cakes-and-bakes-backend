package branch

import (
	"context"

	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store/shared"
)

func (s *Repo) ListBranchExceptions(ctx context.Context, branchID string) ([]franchiseapi.BranchException, error) {
	const q = `
		SELECT id, branch_id, date, reason, is_closed, open_time, close_time, created_at
		FROM branch_exceptions WHERE branch_id = $1 ORDER BY date`
	rows, err := s.pool.Query(ctx, q, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []franchiseapi.BranchException
	for rows.Next() {
		var e franchiseapi.BranchException
		if err := rows.Scan(&e.ID, &e.BranchID, &e.Date, &e.Reason, &e.IsClosed, &e.OpenTime, &e.CloseTime, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Repo) CreateBranchException(ctx context.Context, e franchiseapi.BranchException) (*franchiseapi.BranchException, error) {
	const q = `
		INSERT INTO branch_exceptions (branch_id, date, reason, is_closed, open_time, close_time)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (branch_id, date) DO UPDATE SET reason = $3, is_closed = $4, open_time = $5, close_time = $6
		RETURNING id, branch_id, date, reason, is_closed, open_time, close_time, created_at`
	row := s.pool.QueryRow(ctx, q, e.BranchID, e.Date, e.Reason, e.IsClosed, e.OpenTime, e.CloseTime)
	var out franchiseapi.BranchException
	err := row.Scan(&out.ID, &out.BranchID, &out.Date, &out.Reason, &out.IsClosed, &out.OpenTime, &out.CloseTime, &out.CreatedAt)
	return &out, err
}

func (s *Repo) DeleteBranchException(ctx context.Context, branchID, exceptionID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM branch_exceptions WHERE id = $1 AND branch_id = $2`, exceptionID, branchID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}
