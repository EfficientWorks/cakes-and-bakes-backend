package franchiseapi

import (
	"strings"
	"time"
)

const (
	StatusPending   = "pending"
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusClosed    = "closed"

	BranchTypeRetail         = "retail"
	BranchTypeCentralKitchen = "central_kitchen"
	// Stored in branches.ownership_type and returned as ownershipType.
	OwnershipCorporate = "corporate_store"
	OwnershipFranchise = "franchise_store"
)

// NormalizeOwnershipType maps what a client may send onto the stored value.
//
// The canonical values are corporate_store and franchise_store. Older consoles
// sent "corporate store" / "franchise store" (with a space), and the storefront
// filter has always used "own" / "franchise", so those are accepted too rather
// than breaking every caller at once. ok is false for anything else.
func NormalizeOwnershipType(v string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(v))
	s = strings.NewReplacer(" ", "_", "-", "_").Replace(s)
	switch s {
	case "corporate_store", "corporate", "own", "company", "company_owned", "corporate_owned":
		return OwnershipCorporate, true
	case "franchise_store", "franchise", "franchised":
		return OwnershipFranchise, true
	}
	return "", false
}

type Contact struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"phone"`
}

type CommercialTerms struct {
	RoyaltyRate  float64 `json:"royaltyRate"`
	FeeBasis     string  `json:"feeBasis"`
	PaymentTerms string  `json:"paymentTerms"`
}

type Franchise struct {
	ID              string
	Name            string
	LegalEntity     string
	PrimaryContact  Contact
	ContractRef     *string
	CommercialTerms CommercialTerms
	Territory       *string
	Status          string
	IsCompanyOwned  bool
	OnboardedAt     *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type OpeningHour struct {
	Day   string  `json:"day"` // monday..sunday
	Open  *string `json:"open"`
	Close *string `json:"close"`
}

type DeliveryZone struct {
	Type string   `json:"type"` // zip_list
	Zips []string `json:"zips"`
}

type Branch struct {
	// LocationCode is the code a till signs in with. Null until the outlet
	// has one; every outlet gets one when it is created.
	LocationCode *string
	// BusinessID is the business this store belongs to (businesses.id). Nil
	// only on stores created before businesses existed.
	BusinessID    *string
	ID            string
	Name          string
	Code          string
	Type          string
	OwnershipType string

	Street    string
	City      string
	State     string
	Postcode  string
	Country   string
	Latitude  *float64
	Longitude *float64

	// The outlet's own public contact details, distinct from the franchise's
	// primary contact: each shop publishes its own number and address, which
	// is what a customer ringing about their order uses.
	Phone *string
	Email *string

	Timezone     string
	OpeningHours []OpeningHour

	FulfilmentTypes  []string
	DeliveryZone     DeliveryZone
	DeliveryFeeCents int64
	MinOrderCents    int64
	// DeliveryPricing is the store's delivery region and charges (see
	// delivery_pricing.go). Empty for stores saved before it existed; read it
	// through Pricing(), which falls back to DeliveryFeeCents on every order.
	DeliveryPricing DeliveryPricing

	TaxName    *string
	TaxRatePct *float64

	// Cash on delivery, per branch rather than per brand.
	//
	// Off unless a branch opts in: taking cash is an operational decision
	// about float, reconciliation and driver safety, and a newly created
	// branch should not silently inherit that exposure. CODMaxOrderCents
	// caps what the branch will let leave unpaid; nil means no cap.
	AcceptsCashOnDelivery bool
	CODMaxOrderCents      *int64

	// CardProviders are the card providers this store takes payment through
	// ("stripe"). Empty: card only on its own terminal.
	CardProviders []string

	// Pickup and delivery hours (see fulfilment_hours.go). Empty: slots come
	// from OpeningHours. SameFulfilmentHours: delivery uses pickup's hours.
	PickupHours         []DayHours
	DeliveryHours       []DayHours
	SameFulfilmentHours bool

	Status            string
	EmergencyClosedOn *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type BranchException struct {
	ID        string
	BranchID  string
	Date      time.Time
	Reason    string
	IsClosed  bool
	OpenTime  *string
	CloseTime *string
	CreatedAt time.Time
}

// BranchWithDistance is a Branch annotated with its distance from a
// resolved customer location, used for pickup selection (SF-05) and the
// "nearest branches" fallback when no zone covers a ZIP (SF-03).
type BranchWithDistance struct {
	Branch
	DistanceMiles *float64
}

// BranchPatch is a partial update to a branch — governance (GOV-02) decides
// which of these fields a given caller is allowed to set; the service layer
// enforces that before this ever reaches the store.
type BranchPatch struct {
	Name *string
	// Identity and address.
	Code     *string
	Type     *string
	Street   *string
	City     *string
	State    *string
	Postcode *string
	Country  *string
	Phone    *string
	Email    *string
	// Timezone, FranchiseID and Status were accepted by the API and then
	// dropped, so editing any of them on the outlet form appeared to save and
	// changed nothing.
	//
	// Moving an outlet between franchises is allowed and forward-looking: an
	// order records the franchise it was taken under, so orders already placed
	// keep theirs rather than being re-parented underneath them.
	Timezone         *string
	OwnershipType    *string
	LocationCode     *string
	Status           *string
	FulfilmentTypes  *[]string
	DeliveryZone     *DeliveryZone
	DeliveryFeeCents *int64
	MinOrderCents    *int64
	OpeningHours     *[]OpeningHour
	TaxName          *string
	TaxRatePct       *float64
	// Cash on delivery. Branch-level, alongside the other fulfilment
	// settings a branch manager owns (ADM-16).
	AcceptsCashOnDelivery *bool
	CODMaxOrderCents      *int64
	CardProviders         *[]string
	PickupHours           *[]DayHours
	DeliveryHours         *[]DayHours
	SameFulfilmentHours   *bool
	// DeliveryPricing replaces the store's delivery region and charges whole.
	DeliveryPricing *DeliveryPricing
}

// GovernanceRule declares which governance level (brand | franchise | branch)
// owns a given editable field. The service layer enforces it on every write.
type GovernanceRule struct {
	FieldKey    string
	Level       string
	Description string
}
