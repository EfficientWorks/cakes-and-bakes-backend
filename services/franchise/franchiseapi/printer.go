package franchiseapi

import "time"

// Printer kinds. They differ in more than a label: see Printer.
const (
	PrinterWiFi      = "wifi"
	PrinterBluetooth = "bluetooth"
)

/*
 * Printer is one configured printer at an outlet.
 *
 * The two kinds are not interchangeable. An outlet runs several Wi-Fi
 * printers at once — kitchen, hook, receipts — but pairs with exactly one
 * Bluetooth device, so connecting a second Bluetooth printer disconnects the
 * first rather than being refused.
 */
type Printer struct {
	ID       string `json:"id"`
	BranchID string `json:"branchId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	// Set for Wi-Fi printers, empty for Bluetooth.
	IPAddress string `json:"ipAddress,omitempty"`
	// Set for Bluetooth printers, empty for Wi-Fi.
	DeviceAddress string    `json:"deviceAddress,omitempty"`
	IsConnected   bool      `json:"isConnected"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// PrinterFilter narrows the printer listing.
type PrinterFilter struct {
	BranchID string
	Kind     string
	Search   string
}

// PrinterInput is a printer being created or edited.
//
// Pointers on the editable fields so that "not mentioned" and "set to empty"
// stay different requests — renaming a printer should not blank its address.
type PrinterInput struct {
	BranchID      string
	Name          *string
	Kind          string
	IPAddress     *string
	DeviceAddress *string
	IsConnected   *bool
}
