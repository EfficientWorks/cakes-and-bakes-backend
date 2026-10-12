package audit

import (
	"context"
	"fmt"

	"bakehouse/services/admin/adminapi"
)

func (s *Repo) SearchAuditLog(ctx context.Context, f adminapi.AuditSearchFilter) ([]adminapi.AuditEntry, int, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}

	where := []string{"1=1"}
	args := []interface{}{}
	add := func(cond string, val interface{}) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.ActorID != "" {
		add("actor_id = $%d", f.ActorID)
	}
	if f.ActorType != "" {
		add("actor_type = $%d", f.ActorType)
	}
	if f.Action != "" {
		add("action = $%d", f.Action)
	}
	if f.ResourceType != "" {
		add("resource_type = $%d", f.ResourceType)
	}
	if f.BranchID != "" {
		add("branch_id = $%d", f.BranchID)
	}
	if f.DateFrom != nil {
		add("created_at >= $%d", *f.DateFrom)
	}
	if f.DateTo != nil {
		add("created_at < $%d", *f.DateTo)
	}
	whereClause := where[0]
	for i := 1; i < len(where); i++ {
		whereClause += " AND " + where[i]
	}

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM audit_log WHERE "+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, f.Limit, (f.Page-1)*f.Limit)
	q := fmt.Sprintf(`
		SELECT id, actor_id, actor_type, actor_role, action, resource_type, resource_id,
			branch_id, before_value, after_value, reason, created_at
		FROM audit_log WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, whereClause, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []adminapi.AuditEntry
	for rows.Next() {
		var e adminapi.AuditEntry
		var actorRole, reason *string
		if err := rows.Scan(&e.ID, &e.ActorID, &e.ActorType, &actorRole, &e.Action, &e.ResourceType, &e.ResourceID,
			&e.BranchID, &e.BeforeValue, &e.AfterValue, &reason, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		if actorRole != nil {
			e.ActorRole = *actorRole
		}
		if reason != nil {
			e.Reason = *reason
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
