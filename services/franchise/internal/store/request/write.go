package request

import (
	"context"
	"time"

	"bakehouse/pkg/timex"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store/shared"
)

// CreateBranchRequest inserts a new request and returns the persisted row.
func (s *Repo) CreateBranchRequest(ctx context.Context, req franchiseapi.BranchRequest) (*franchiseapi.BranchRequest, error) {
	fieldsToChange := req.FieldsToChange
	if fieldsToChange == nil {
		fieldsToChange = []string{}
	}
	const q = `
		WITH ins AS (
			INSERT INTO branch_requests (
				branch_id,
				requested_by, requested_at, role,
				type, title, description, fields_to_change, status,
				modifier_type, modifier_name
			)
			VALUES ($1,$2,NOW(),$3,$4,$5,$6,$7,'pending',$8,$9)
			RETURNING
				id, branch_id,
				requested_by, requested_at, role,
				type, title, description, fields_to_change, status,
				approved_at, approved_by,
				rejected_at, rejected_by, rejection_note,
				completed_at, completed_by,
				created_at, updated_at,
				modifier_type, modifier_name
		)
		SELECT
			ins.id, ins.branch_id,
			ins.requested_by, ins.requested_at, ins.role,
			ins.type, ins.title, ins.description, ins.fields_to_change, ins.status,
			ins.approved_at, ins.approved_by,
			ins.rejected_at, ins.rejected_by, ins.rejection_note,
			ins.completed_at, ins.completed_by,
			ins.created_at, ins.updated_at,
			ins.modifier_type, ins.modifier_name,
			COALESCE(b.name, '') AS branch_name
		FROM ins
		LEFT JOIN branches b ON b.id = ins.branch_id`
	row := s.pool.QueryRow(ctx, q,
		req.BranchID, req.RequestedBy, req.Role,
		req.Type, req.Title, req.Description, fieldsToChange,
		req.ModifierType, req.ModifierName,
	)
	return scanBranchRequest(row)
}

// ApproveRequest sets status=approved and stamps approved_at / approved_by.
// Returns shared.ErrNotFound if the request does not exist.
func (s *Repo) ApproveRequest(ctx context.Context, id, actorID string) (*franchiseapi.BranchRequest, error) {
	now := timex.EventTimeOr(ctx, time.Now()).UTC()
	const q = `
		WITH upd AS (
			UPDATE branch_requests
			SET status = 'approved',
			    approved_at = $2,
			    approved_by = $3,
			    updated_at  = NOW()
			WHERE id = $1
			RETURNING
				id, branch_id,
				requested_by, requested_at, role,
				type, title, description, fields_to_change, status,
				approved_at, approved_by,
				rejected_at, rejected_by, rejection_note,
				completed_at, completed_by,
				created_at, updated_at,
				modifier_type, modifier_name
		)
		SELECT
			upd.id, upd.branch_id,
			upd.requested_by, upd.requested_at, upd.role,
			upd.type, upd.title, upd.description, upd.fields_to_change, upd.status,
			upd.approved_at, upd.approved_by,
			upd.rejected_at, upd.rejected_by, upd.rejection_note,
			upd.completed_at, upd.completed_by,
			upd.created_at, upd.updated_at,
			upd.modifier_type, upd.modifier_name,
			COALESCE(b.name, '') AS branch_name
		FROM upd
		LEFT JOIN branches b ON b.id = upd.branch_id`
	row := s.pool.QueryRow(ctx, q, id, now, actorID)
	return scanBranchRequest(row)
}

// RejectRequest sets status=rejected and stamps rejected_at / rejected_by / rejection_note.
func (s *Repo) RejectRequest(ctx context.Context, id, actorID, note string) (*franchiseapi.BranchRequest, error) {
	now := timex.EventTimeOr(ctx, time.Now()).UTC()
	const q = `
		WITH upd AS (
			UPDATE branch_requests
			SET status         = 'rejected',
			    rejected_at    = $2,
			    rejected_by    = $3,
			    rejection_note = $4,
			    updated_at     = NOW()
			WHERE id = $1
			RETURNING
				id, branch_id,
				requested_by, requested_at, role,
				type, title, description, fields_to_change, status,
				approved_at, approved_by,
				rejected_at, rejected_by, rejection_note,
				completed_at, completed_by,
				created_at, updated_at,
				modifier_type, modifier_name
		)
		SELECT
			upd.id, upd.branch_id,
			upd.requested_by, upd.requested_at, upd.role,
			upd.type, upd.title, upd.description, upd.fields_to_change, upd.status,
			upd.approved_at, upd.approved_by,
			upd.rejected_at, upd.rejected_by, upd.rejection_note,
			upd.completed_at, upd.completed_by,
			upd.created_at, upd.updated_at,
			upd.modifier_type, upd.modifier_name,
			COALESCE(b.name, '') AS branch_name
		FROM upd
		LEFT JOIN branches b ON b.id = upd.branch_id`
	row := s.pool.QueryRow(ctx, q, id, now, actorID, shared.NilIfEmpty(note))
	return scanBranchRequest(row)
}

// CancelRequest sets status=cancelled.
func (s *Repo) CancelRequest(ctx context.Context, id string) (*franchiseapi.BranchRequest, error) {
	const q = `
		WITH upd AS (
			UPDATE branch_requests
			SET status     = 'cancelled',
			    updated_at = NOW()
			WHERE id = $1
			RETURNING
				id, branch_id,
				requested_by, requested_at, role,
				type, title, description, fields_to_change, status,
				approved_at, approved_by,
				rejected_at, rejected_by, rejection_note,
				completed_at, completed_by,
				created_at, updated_at,
				modifier_type, modifier_name
		)
		SELECT
			upd.id, upd.branch_id,
			upd.requested_by, upd.requested_at, upd.role,
			upd.type, upd.title, upd.description, upd.fields_to_change, upd.status,
			upd.approved_at, upd.approved_by,
			upd.rejected_at, upd.rejected_by, upd.rejection_note,
			upd.completed_at, upd.completed_by,
			upd.created_at, upd.updated_at,
			upd.modifier_type, upd.modifier_name,
			COALESCE(b.name, '') AS branch_name
		FROM upd
		LEFT JOIN branches b ON b.id = upd.branch_id`
	row := s.pool.QueryRow(ctx, q, id)
	return scanBranchRequest(row)
}

// CompleteRequest sets status=completed and stamps completed_at / completed_by.
func (s *Repo) CompleteRequest(ctx context.Context, id, actorID string) (*franchiseapi.BranchRequest, error) {
	now := timex.EventTimeOr(ctx, time.Now()).UTC()
	const q = `
		WITH upd AS (
			UPDATE branch_requests
			SET status       = 'completed',
			    completed_at = $2,
			    completed_by = $3,
			    updated_at   = NOW()
			WHERE id = $1
			RETURNING
				id, branch_id,
				requested_by, requested_at, role,
				type, title, description, fields_to_change, status,
				approved_at, approved_by,
				rejected_at, rejected_by, rejection_note,
				completed_at, completed_by,
				created_at, updated_at,
				modifier_type, modifier_name
		)
		SELECT
			upd.id, upd.branch_id,
			upd.requested_by, upd.requested_at, upd.role,
			upd.type, upd.title, upd.description, upd.fields_to_change, upd.status,
			upd.approved_at, upd.approved_by,
			upd.rejected_at, upd.rejected_by, upd.rejection_note,
			upd.completed_at, upd.completed_by,
			upd.created_at, upd.updated_at,
			upd.modifier_type, upd.modifier_name,
			COALESCE(b.name, '') AS branch_name
		FROM upd
		LEFT JOIN branches b ON b.id = upd.branch_id`
	row := s.pool.QueryRow(ctx, q, id, now, actorID)
	return scanBranchRequest(row)
}
