// Package audit writes to the append-only audit_log table shared by every
// module (RBAC-05: every permission-gated action is recorded; NFR-11: every
// financially significant action is attributable and auditable). The table
// itself rejects UPDATE/DELETE via a DB trigger — see db/migrations — so
// this package only ever needs an INSERT.
package audit

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Entry struct {
	ActorID      *string
	ActorType    string // customer | staff | system
	ActorRole    string
	Action       string // e.g. "branch.suspend", "order.amend"
	ResourceType string
	ResourceID   *string
	BranchID     *string
	BeforeValue  interface{}
	AfterValue   interface{}
	Reason       string
	IPAddress    string
}

type Writer struct {
	pool *pgxpool.Pool
}

func NewWriter(pool *pgxpool.Pool) *Writer {
	return &Writer{pool: pool}
}

func (w *Writer) Write(ctx context.Context, e Entry) error {
	before, _ := json.Marshal(e.BeforeValue)
	after, _ := json.Marshal(e.AfterValue)
	const q = `
		INSERT INTO audit_log (actor_id, actor_type, actor_role, action, resource_type, resource_id,
			branch_id, before_value, after_value, reason, ip_address)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`
	_, err := w.pool.Exec(ctx, q, e.ActorID, e.ActorType, e.ActorRole, e.Action, e.ResourceType, e.ResourceID,
		e.BranchID, before, after, e.Reason, e.IPAddress)
	return err
}
