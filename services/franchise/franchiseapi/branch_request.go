package franchiseapi

import "time"

// Branch request status values. The lifecycle is:
//
//	pending ? approved ? completed  (happy path)
//	pending ? rejected              (super admin declines)
//	pending | approved ? cancelled  (requester or admin withdraws)
const (
	RequestStatusPending   = "pending"
	RequestStatusApproved  = "approved"
	RequestStatusRejected  = "rejected"
	RequestStatusCancelled = "cancelled"
	RequestStatusCompleted = "completed"
)

// Request types — the only accepted values for BranchRequest.Type.
const (
	RequestTypeProduct  = "product"
	RequestTypeCategory = "category"
	RequestTypeModifier = "modifier"
	RequestTypeOther    = "other"
)

// BranchRequest is a request made by branch-scoped staff
// asking for some operation on a branch. Super Admins approve or reject;
// once approved the requester (or a super admin) marks it completed.
type BranchRequest struct {
	ID         string `json:"id"`
	BranchID   string `json:"branchId"`
	Store      string `json:"store"`
	BranchName string `json:"branchName,omitempty"`

	// Requester
	RequestedBy string    `json:"requestedBy"`
	RequestedAt time.Time `json:"requestedAt"`
	Role        string    `json:"role"` // role held at creation time

	// Payload
	Type           string   `json:"type"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	FieldsToChange []string `json:"fieldsToChange,omitempty"`
	ModifierType   *string  `json:"modifierType,omitempty"`
	ModifierName   *string  `json:"modifierName,omitempty"`

	// Lifecycle
	Status string `json:"status"`

	// Approval (populated only when status == approved)
	ApprovedAt *time.Time `json:"approvedAt,omitempty"`
	ApprovedBy *string    `json:"approvedBy,omitempty"`

	// Rejection (populated only when status == rejected)
	RejectedAt    *time.Time `json:"rejectedAt,omitempty"`
	RejectedBy    *string    `json:"rejectedBy,omitempty"`
	RejectionNote *string    `json:"rejectionNote,omitempty"`

	// Completion (populated only when status == completed)
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	CompletedBy *string    `json:"completedBy,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// BranchRequestFilter scopes the list query. Zero values mean "no filter".
type BranchRequestFilter struct {
	Type     string // optional (product, category, modifier, other)
	Status   string // optional
	BranchID string // optional
	Search   string // optional — matches title, description, or branch name (case-insensitive)
	Page     int    // 1-based, default 1
	Limit    int    // items per page, default 20
}

// BranchRequestStats holds request count statistics.
type BranchRequestStats struct {
	Total     int64 `json:"total"`
	Approved  int64 `json:"approved"`
	Rejected  int64 `json:"rejected"`
	Pending   int64 `json:"pending"`
	Completed int64 `json:"completed"`
	Cancelled int64 `json:"cancelled"`
}
