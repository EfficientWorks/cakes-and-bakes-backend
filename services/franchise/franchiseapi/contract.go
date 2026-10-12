// Package franchiseapi is the franchise service's published contract: the
// domain types it owns and the interfaces other services consume. It holds
// no logic and imports nothing from the service, so both the service's own
// internals and its consumers can depend on it without a cycle.
package franchiseapi

import "context"

// BranchReader is the read surface other services (catalog, orders) use to
// resolve branch configuration. In a distributed deployment this is the
// boundary an RPC client would sit behind; in-process it is satisfied
// directly by the franchise store.
type BranchReader interface {
	// GetBranch returns one branch by id.
	GetBranch(ctx context.Context, id string) (*Branch, error)
	// ListBranchExceptions returns the branch's closure/short-hours
	// overrides, used by scheduling to build bookable slots.
	ListBranchExceptions(ctx context.Context, branchID string) ([]BranchException, error)
}
