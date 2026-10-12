package identityapi

import "time"

type Customer struct {
	ID               string
	BranchID         *string
	CustomerType     string
	Email            string
	PasswordHash     string
	FirstName        string
	LastName         string
	MobileNumber     *string
	Address          *string
	Street           *string
	City             *string
	State            *string
	Postcode         *string
	IsVerified       bool
	MarketingOptIn   bool
	MarketingOptInAt *time.Time
	DateOfBirth      *time.Time
	AnniversaryDate  *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CustomerAddress struct {
	ID         string
	CustomerID string
	Label      *string
	Address    string
	Street     string
	City       string
	State      string
	Postcode   string
	Country    string
	IsDefault  bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type OccasionDate struct {
	Occasion string `json:"occasion"`
	Month    int    `json:"month"`
	Day      int    `json:"day"`
}

type CustomerRecipient struct {
	ID            string
	CustomerID    string
	FirstName     string
	LastName      string
	Phone         string
	Relationship  *string
	OccasionDates []OccasionDate
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type StaffUser struct {
	ID    string
	Email string
	// The business the account belongs to. Nil only on accounts created
	// before businesses existed.
	BusinessID   *string
	PasswordHash string
	FirstName    string
	LastName     string
	IsActive     bool
	Phone        *string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type StaffRoleAssignment struct {
	ID          string
	StaffUserID string
	Role        string
	ScopeType   string
	BranchID    *string
	// Set only when ScopeType is central_kitchen (KDS staff at a kitchen).
	CentralKitchenID    *string
	MaxRefundCents      *int64
	MaxDiscountPct      *float64
	OrderVoidWindowMins *int
	CreatedAt           time.Time
}

// StaffRoleSummary is the compact role-assignment shape returned by the
// staff directory listing (SA-16) — a subset of StaffRoleAssignment's
// fields, matching what json_agg produces in ListStaffNetwork's query.
type StaffRoleSummary struct {
	ID        string  `json:"id"`
	Role      string  `json:"role"`
	ScopeType string  `json:"scopeType"`
	BranchID  *string `json:"branchId"`
	// Present only on a central_kitchen scope.
	CentralKitchenID *string `json:"centralKitchenId,omitempty"`
}

// StaffDirectoryEntry is one row of the network-wide staff directory
// (SA-16): the staff account plus every role assignment they hold and their
// most recent login, derived from refresh_tokens rather than tracked
// separately.
type StaffDirectoryEntry struct {
	StaffUser
	Roles       []StaffRoleSummary
	LastLoginAt *time.Time
	HasPasscode bool
	// Encrypted till passcode; opened only for staff managers.
	PasscodeSealed *string
}

// StaffDirectoryFilter narrows the staff directory listing. A staff member
// matches the role/branch filters if ANY of their role
// assignments satisfies them — the listing still returns their complete
// role set, not just the matching assignment.
type StaffDirectoryFilter struct {
	Role     string
	BranchID string
	// Staff assigned to this central kitchen.
	CentralKitchenID string
	IsActive         *bool
	Search           string
}

// Refresh-token subjects. The two realms are fully isolated: a customer
// account can never become a staff account (domain-model.md).
const (
	SubjectCustomer = "customer"
	SubjectStaff    = "staff"
)

// StaffPasscodeCandidate is one person who might be signing in at a till.
//
// Carries the hash because a passcode cannot be looked up — it has to be
// compared against each candidate in turn.
type StaffPasscodeCandidate struct {
	ID           string
	Email        string
	FirstName    string
	LastName     string
	PasscodeHash *string
	// Active is false for a deactivated account. Login still checks its
	// passcode so it can say "deactivated" rather than "incorrect".
	Active bool
}

// POSOutlet is the shop a till just signed in to.
type POSOutlet struct {
	BranchID   string
	BusinessID string
	// Shown on the passcode screen, so an operator can see which shop the till
	// is about to sign in to before typing anything. A till set up with the
	// wrong code should be obvious then, not after the first order.
	Name       string
	TaxName    string
	TaxRatePct *float64
	// "branch" or "central_kitchen": what the Business/Location ID named.
	Type string
	// Set when Type is central_kitchen; BranchID is then empty.
	CentralKitchenID string
}

// KDSLocation is the store or central kitchen a KDS signed in to.
type KDSLocation struct {
	Type             string
	BranchID         string
	CentralKitchenID string
	BusinessID       string
	Name             string
	Timezone         string
	// The stores this KDS shows: the store itself, or every store the
	// central kitchen is assigned.
	Branches []KDSBranchRef
}

// KDSBranchRef names one store a KDS covers.
type KDSBranchRef struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	BusinessID *string `json:"businessId"`
}
