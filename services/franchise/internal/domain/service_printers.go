package domain

import (
	"context"
	"net"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/services/franchise/franchiseapi"
)

/*
 * Printer configuration.
 *
 * Reading is open to anyone who can see the outlet — a till has to know what
 * it can print to. Changing the configuration is not: adding, renaming and
 * removing printers sit behind branch.operational, the same permission that
 * governs the outlet's other operational settings, which keeps a counter from
 * repointing the kitchen printer mid-service.
 *
 * Connecting is deliberately on the reading side of that line. Choosing which
 * paired Bluetooth printer this till is talking to is an act of working the
 * counter, not of configuring the shop.
 */

// ListPrinters returns the outlet's printers.
func (s *Service) ListPrinters(ctx context.Context, claims *auth.StaffClaims, f franchiseapi.PrinterFilter) ([]franchiseapi.Printer, error) {
	branch, err := s.printerBranch(ctx, claims, f.BranchID)
	if err != nil {
		return nil, err
	}
	f.BranchID = branch.ID

	if f.Kind != "" && !validPrinterKind(f.Kind) {
		return nil, printerKindError()
	}

	out, err := s.store.ListPrinters(ctx, f)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

// GetPrinter returns one printer the caller is allowed to see.
func (s *Service) GetPrinter(ctx context.Context, claims *auth.StaffClaims, id string) (*franchiseapi.Printer, error) {
	p, err := s.store.GetPrinter(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Printer not found.")
	}
	// Not found rather than forbidden: whether a printer exists at somebody
	// else's outlet is not this caller's business either way.
	if !tenant.CanAccessBranch(claims, "", p.BranchID) {
		return nil, apperr.NotFound("Printer not found.")
	}
	return p, nil
}

// printOutletAddress joins a branch's address fields into the single line a
// receipt actually prints, skipping any that are blank.
func printOutletAddress(b *franchiseapi.Branch) string {
	parts := []string{b.Street, b.City, b.State, b.Postcode, b.Country}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, strings.TrimSpace(p))
		}
	}
	return strings.Join(out, ", ")
}

// CreatePrinter adds a printer to an outlet.
func (s *Service) CreatePrinter(ctx context.Context, claims *auth.StaffClaims, in franchiseapi.PrinterInput) (*franchiseapi.Printer, error) {
	if !tenant.HasPermission(claims, tenant.PermKitchenPrintUpdates) {
		return nil, apperr.PermissionDenied("You do not have permission to configure printers.")
	}
	branch, err := s.printerBranch(ctx, claims, in.BranchID)
	if err != nil {
		return nil, err
	}
	in.BranchID = branch.ID

	if err := validateNewPrinter(&in); err != nil {
		return nil, err
	}

	p, err := s.store.CreatePrinter(ctx, in, claims.StaffID)
	if err != nil {
		return nil, printerWriteError(err)
	}
	s.writePrinterAudit(ctx, claims, "pos.printer.create", p)
	return p, nil
}

// UpdatePrinter renames a printer or changes where it is reached.
func (s *Service) UpdatePrinter(ctx context.Context, claims *auth.StaffClaims, id string, in franchiseapi.PrinterInput) (*franchiseapi.Printer, error) {
	if !tenant.HasPermission(claims, tenant.PermKitchenPrintUpdates) {
		return nil, apperr.PermissionDenied("You do not have permission to configure printers.")
	}
	current, err := s.GetPrinter(ctx, claims, id)
	if err != nil {
		return nil, err
	}
	if err := validatePrinterEdit(current, &in); err != nil {
		return nil, err
	}

	p, err := s.store.UpdatePrinter(ctx, id, in)
	if err != nil {
		return nil, printerWriteError(err)
	}
	s.writePrinterAudit(ctx, claims, "pos.printer.update", p)
	return p, nil
}

/*
 * SetPrinterConnected is what the radio button and the tick box on the
 * settings screen do.
 *
 * Connecting a Bluetooth printer disconnects whichever one the outlet was
 * paired with, rather than refusing — the till talks to one device at a time,
 * and "disconnect the old one first" is a step the person at the counter has
 * no reason to care about. Wi-Fi printers have no such limit and several run
 * at once, which is why the rule is applied by kind rather than to all of them.
 */
func (s *Service) SetPrinterConnected(ctx context.Context, claims *auth.StaffClaims, id string, connected bool) (*franchiseapi.Printer, error) {
	if _, err := s.GetPrinter(ctx, claims, id); err != nil {
		return nil, err
	}
	p, err := s.store.SetPrinterConnected(ctx, id, connected)
	if err != nil {
		return nil, printerWriteError(err)
	}
	action := "pos.printer.disconnect"
	if connected {
		action = "pos.printer.connect"
	}
	s.writePrinterAudit(ctx, claims, action, p)
	return p, nil
}

// DeletePrinter removes a printer.
func (s *Service) DeletePrinter(ctx context.Context, claims *auth.StaffClaims, id string) error {
	if !tenant.HasPermission(claims, tenant.PermKitchenPrintUpdates) {
		return apperr.PermissionDenied("You do not have permission to configure printers.")
	}
	p, err := s.GetPrinter(ctx, claims, id)
	if err != nil {
		return err
	}
	if err := s.store.DeletePrinter(ctx, id); err != nil {
		return notFoundOrInternal(err, "Printer not found.")
	}
	s.writePrinterAudit(ctx, claims, "pos.printer.delete", p)
	return nil
}

/*
 * printerBranch resolves which outlet a request is about.
 *
 * A till signs in to one shop and should not have to name it on every call,
 * so a branch-scoped token defaults to its own. A token that covers more than
 * one outlet has to say which, because guessing would silently configure the
 * wrong shop's printers.
 */
func (s *Service) printerBranch(ctx context.Context, claims *auth.StaffClaims, branchID string) (*franchiseapi.Branch, error) {
	id := strings.TrimSpace(branchID)
	if id == "" {
		id = soleBranchOf(claims)
	}
	if id == "" {
		return nil, apperr.Validation("Say which outlet.").
			WithFields(apperr.FieldError{Field: "branchId", Code: "REQUIRED", Message: "Required."})
	}

	b, err := s.store.GetBranch(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Branch not found.")
	}
	if !tenant.CanAccessBranch(claims, "", b.ID) {
		return nil, apperr.PermissionDenied("You do not have access to this branch.")
	}
	return b, nil
}

// soleBranchOf returns the branch a token is confined to, or "" when it
// covers none or several.
func soleBranchOf(claims *auth.StaffClaims) string {
	if claims == nil {
		return ""
	}
	found := ""
	for _, ra := range claims.RoleAssignments {
		if ra.ScopeType != tenant.ScopeBranch || ra.BranchID == nil || *ra.BranchID == "" {
			continue
		}
		if found != "" && found != *ra.BranchID {
			return ""
		}
		found = *ra.BranchID
	}
	return found
}

// --- validation -----------------------------------------------------------

func validPrinterKind(k string) bool {
	return k == franchiseapi.PrinterWiFi || k == franchiseapi.PrinterBluetooth
}

func printerKindError() error {
	return apperr.Validation("A printer is either wifi or bluetooth.").
		WithFields(apperr.FieldError{Field: "kind", Code: "INVALID", Message: "wifi or bluetooth."})
}

func validateNewPrinter(in *franchiseapi.PrinterInput) error {
	if !validPrinterKind(in.Kind) {
		return printerKindError()
	}
	if strings.TrimSpace(derefStr(in.Name)) == "" {
		return apperr.Validation("Give the printer a name.").
			WithFields(apperr.FieldError{Field: "name", Code: "REQUIRED", Message: "Required."})
	}
	if in.Kind == franchiseapi.PrinterWiFi {
		return validateIP(derefStr(in.IPAddress))
	}
	if strings.TrimSpace(derefStr(in.DeviceAddress)) == "" {
		return apperr.Validation("A Bluetooth printer needs its device address.").
			WithFields(apperr.FieldError{Field: "deviceAddress", Code: "REQUIRED", Message: "Required."})
	}
	return nil
}

func validatePrinterEdit(current *franchiseapi.Printer, in *franchiseapi.PrinterInput) error {
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return apperr.Validation("Give the printer a name.").
			WithFields(apperr.FieldError{Field: "name", Code: "REQUIRED", Message: "Required."})
	}
	// Checked against the printer's stored kind rather than a kind in the
	// request: a printer does not turn from Wi-Fi into Bluetooth, and letting
	// an edit claim otherwise would let a Wi-Fi printer end up with no
	// address at all.
	if in.IPAddress != nil && current.Kind == franchiseapi.PrinterWiFi {
		return validateIP(*in.IPAddress)
	}
	if in.DeviceAddress != nil && current.Kind == franchiseapi.PrinterBluetooth &&
		strings.TrimSpace(*in.DeviceAddress) == "" {
		return apperr.Validation("A Bluetooth printer needs its device address.").
			WithFields(apperr.FieldError{Field: "deviceAddress", Code: "REQUIRED", Message: "Required."})
	}
	return nil
}

// validateIP rejects an unreachable address at the point somebody types it.
//
// A printer saved with a typo in its IP fails silently at the worst possible
// moment — mid-service, with nothing printing and no error anyone can see.
func validateIP(raw string) error {
	ip := strings.TrimSpace(raw)
	if ip == "" {
		return apperr.Validation("A Wi-Fi printer needs its IP address.").
			WithFields(apperr.FieldError{Field: "ipAddress", Code: "REQUIRED", Message: "Required."})
	}
	if net.ParseIP(ip) == nil {
		return apperr.Validation("That is not a valid IP address.").
			WithFields(apperr.FieldError{Field: "ipAddress", Code: "INVALID", Message: "Not an IP address."})
	}
	return nil
}

// printerWriteError turns the database's uniqueness rules back into the
// sentences that caused them, so a duplicate reads as a duplicate rather than
// as an internal error.
func printerWriteError(err error) error {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "pos_printers_branch_name_idx"):
		return apperr.New("PRINTER_NAME_TAKEN", apperr.CategoryConflict,
			"A printer with that name already exists at this outlet.").
			WithFields(apperr.FieldError{Field: "name", Code: "TAKEN", Message: "Already used."})
	case strings.Contains(msg, "pos_printers_branch_ip_idx"):
		return apperr.New("PRINTER_IP_TAKEN", apperr.CategoryConflict,
			"A printer with that IP address already exists at this outlet.").
			WithFields(apperr.FieldError{Field: "ipAddress", Code: "TAKEN", Message: "Already used."})
	case strings.Contains(msg, "pos_printers_one_connected_bluetooth_idx"):
		// The service disconnects the previous one first, so reaching here
		// means that did not happen — our bug, not the caller's.
		return apperr.Internal(err)
	}
	return notFoundOrInternal(err, "Printer not found.")
}

func (s *Service) writePrinterAudit(ctx context.Context, claims *auth.StaffClaims, action string, p *franchiseapi.Printer) {
	if p == nil {
		return
	}
	after := map[string]any{"name": p.Name, "kind": p.Kind, "isConnected": p.IsConnected}
	s.writeAudit(ctx, claims, action, "pos_printer", &p.ID, &p.BranchID, nil, after, "")
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
