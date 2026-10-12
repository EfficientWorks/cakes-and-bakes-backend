package franchiseapi

import (
	"context"
	"time"
)

/*
 * Central kitchens and the Kitchen Display.
 *
 * A central kitchen is what the console calls a "Corporate Office": a
 * production site with its own Location ID that serves a set of stores. A KDS
 * signs in to either a store or a central kitchen, and everything it shows is
 * scoped by that choice.
 */

// Location types a Business/Location ID can resolve to.
const (
	LocationBranch         = "branch"
	LocationCentralKitchen = "central_kitchen"
)

// Central kitchen statuses.
const (
	CentralKitchenActive   = "active"
	CentralKitchenInactive = "inactive"
)

// CentralKitchen is one production site and the stores it serves.
type CentralKitchen struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	BusinessID string      `json:"businessId"`
	Street     string      `json:"street"`
	City       string      `json:"city"`
	State      string      `json:"state"`
	Postcode   string      `json:"postcode"`
	Country    string      `json:"country"`
	Phone      *string     `json:"phone"`
	Email      *string     `json:"email"`
	Timezone   string      `json:"timezone"`
	Status     string      `json:"status"`
	Manager    Contact     `json:"manager"`
	BranchIDs  []string    `json:"branchIds"`
	Branches   []BranchRef `json:"branches"`
	CreatedAt  time.Time   `json:"createdAt"`
	UpdatedAt  time.Time   `json:"updatedAt"`
}

// BranchRef is the name tag of a store, for a kitchen's assignment chips.
type BranchRef struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Code       string  `json:"code"`
	BusinessID *string `json:"businessId"`
	Status     string  `json:"status"`
	Timezone   string  `json:"timezone"`
}

// CentralKitchenFilter narrows the console listing.
type CentralKitchenFilter struct {
	Search string
	Status string
	Page   int
	Limit  int
}

// CentralKitchenInput is a kitchen being created or edited. Pointers so that
// an edit changes only what it sends.
type CentralKitchenInput struct {
	Name         *string
	BusinessID   *string
	Street       *string
	City         *string
	State        *string
	Postcode     *string
	Country      *string
	Phone        *string
	Email        *string
	Timezone     *string
	Status       *string
	ManagerName  *string
	ManagerPhone *string
	ManagerEmail *string
	// Nil leaves the assignment alone; a non-nil (even empty) list replaces it.
	BranchIDs *[]string
}

// Location is what a Location/Business ID resolves to.
type Location struct {
	Type             string // LocationBranch | LocationCentralKitchen
	ID               string
	BusinessID       string
	Name             string
	Status           string
	Timezone         string
	BranchID         string // set when Type is branch
	CentralKitchenID string // set when Type is central_kitchen
}

/*
 * KDSSettings is one location's Kitchen Display configuration.
 *
 * KDSEnabled deliberately defaults to false. It is the one setting that
 * changes an existing flow: while it is on, a POS payment on an order the
 * kitchen has not finished no longer closes the order — the kitchen handing it
 * over does. Turning the KDS on at a store is a decision, so a store that has
 * never had a KDS keeps closing orders at the till exactly as before.
 */
type KDSSettings struct {
	ID               string  `json:"id"`
	ScopeType        string  `json:"scopeType"`
	BranchID         *string `json:"branchId"`
	CentralKitchenID *string `json:"centralKitchenId"`

	KDSEnabled bool `json:"kdsEnabled"`

	ShowPickup            bool `json:"showPickup"`
	ShowDelivery          bool `json:"showDelivery"`
	ShowTakeaway          bool `json:"showTakeaway"`
	ShowOrdersBasedOnTime bool `json:"showOrdersBasedOnTime"`
	OrderAppearMinutes    int  `json:"orderAppearMinutes"`
	PlaySoundOnNewTicket  bool `json:"playSoundOnNewTicket"`

	TicketLayout        string `json:"ticketLayout"`
	TicketColumns       int    `json:"ticketColumns"`
	HeaderColorsEnabled bool   `json:"headerColorsEnabled"`
	PickupColor         string `json:"pickupColor"`
	DeliveryColor       string `json:"deliveryColor"`
	TakeawayColor       string `json:"takeawayColor"`
	DarkTheme           bool   `json:"darkTheme"`

	CalendarLowFrom    int `json:"calendarLowFrom"`
	CalendarLowTo      int `json:"calendarLowTo"`
	CalendarMediumFrom int `json:"calendarMediumFrom"`
	CalendarMediumTo   int `json:"calendarMediumTo"`
	CalendarHighFrom   int `json:"calendarHighFrom"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// KDSSettingsPatch changes only the fields it carries.
type KDSSettingsPatch struct {
	KDSEnabled            *bool
	ShowPickup            *bool
	ShowDelivery          *bool
	ShowTakeaway          *bool
	ShowOrdersBasedOnTime *bool
	OrderAppearMinutes    *int
	PlaySoundOnNewTicket  *bool
	TicketLayout          *string
	TicketColumns         *int
	HeaderColorsEnabled   *bool
	PickupColor           *string
	DeliveryColor         *string
	TakeawayColor         *string
	DarkTheme             *bool
	CalendarLowFrom       *int
	CalendarLowTo         *int
	CalendarMediumFrom    *int
	CalendarMediumTo      *int
	CalendarHighFrom      *int
}

// KDSPrinter is a printer configured on a KDS location.
type KDSPrinter struct {
	ID               string    `json:"id"`
	ScopeType        string    `json:"scopeType"`
	BranchID         *string   `json:"branchId"`
	CentralKitchenID *string   `json:"centralKitchenId"`
	Name             string    `json:"name"`
	Kind             string    `json:"kind"`
	IPAddress        string    `json:"ipAddress,omitempty"`
	Port             int       `json:"port"`
	DeviceAddress    string    `json:"deviceAddress,omitempty"`
	IsConnected      bool      `json:"isConnected"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// KDSPrinterInput is a KDS printer being created or edited.
type KDSPrinterInput struct {
	Name          *string
	Kind          string
	IPAddress     *string
	Port          *int
	DeviceAddress *string
	IsConnected   *bool
}

// KDSScope names one KDS location.
type KDSScope struct {
	Type             string // LocationBranch | LocationCentralKitchen
	BranchID         string
	CentralKitchenID string
}

// KDSReader is what the orders service needs from this one to serve a KDS:
// which stores a kitchen covers, and how a location's display is configured.
type KDSReader interface {
	GetCentralKitchen(ctx context.Context, id string) (*CentralKitchen, error)
	CentralKitchenBranchIDs(ctx context.Context, centralKitchenID string) ([]string, error)
	KDSSettingsFor(ctx context.Context, scope KDSScope) (*KDSSettings, error)
	ListBranchRefs(ctx context.Context, ids []string) ([]BranchRef, error)
}
