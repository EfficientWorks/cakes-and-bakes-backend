package domain

import (
	"context"
	"errors"
	"net/mail"
	"regexp"
	"strings"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store"
)

/*
 * Central kitchens ("Corporate Offices") and the Kitchen Display's own
 * configuration: settings and printers, per location.
 *
 * A KDS location is either a store or a central kitchen. Everything here
 * resolves which one a request is about before touching anything, and the
 * rule for who may act on it is the same everywhere (see resolveKDSScope).
 */

// A Location ID is typed on a tablet: letters, digits and dashes, short.
var businessIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{1,31}$`)

// ------------------------------------------------------------ central kitchens

func (s *Service) canManageKitchens(claims *auth.StaffClaims) bool {
	return tenant.HasPermission(claims, tenant.PermBranchManage)
}

func (s *Service) canViewKitchens(claims *auth.StaffClaims) bool {
	return s.canManageKitchens(claims) || tenant.IsBrandScoped(claims)
}

// ListCentralKitchens is the console's Corporate Offices list.
func (s *Service) ListCentralKitchens(ctx context.Context, claims *auth.StaffClaims, f franchiseapi.CentralKitchenFilter) ([]franchiseapi.CentralKitchen, int, error) {
	if !s.canViewKitchens(claims) {
		return nil, 0, apperr.PermissionDenied("You do not have permission to view corporate offices.")
	}
	if st := strings.ToLower(strings.TrimSpace(f.Status)); st != "" && st != "all" &&
		st != franchiseapi.CentralKitchenActive && st != franchiseapi.CentralKitchenInactive {
		return nil, 0, apperr.Validation("status is active, inactive or all.").
			WithFields(apperr.FieldError{Field: "status", Code: "INVALID", Message: "active, inactive or all."})
	}
	out, total, err := s.store.ListCentralKitchens(ctx, f)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}
	return out, total, nil
}

// GetCentralKitchen returns one kitchen. Kitchen staff may read their own.
func (s *Service) GetCentralKitchen(ctx context.Context, claims *auth.StaffClaims, id string) (*franchiseapi.CentralKitchen, error) {
	if !s.canViewKitchens(claims) && !containsStr(tenant.CentralKitchenScopes(claims), id) {
		return nil, apperr.PermissionDenied("You do not have permission to view this corporate office.")
	}
	ck, err := s.store.GetCentralKitchen(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Corporate office not found.")
	}
	return ck, nil
}

// CreateCentralKitchen adds a kitchen with its Location ID, manager, store
// assignment and default KDS settings.
func (s *Service) CreateCentralKitchen(ctx context.Context, claims *auth.StaffClaims, in franchiseapi.CentralKitchenInput) (*franchiseapi.CentralKitchen, error) {
	if !s.canManageKitchens(claims) {
		return nil, apperr.PermissionDenied("You do not have permission to create a corporate office.")
	}
	required := []struct {
		field, label string
		v            *string
	}{
		{"name", "Corporate office name", in.Name},
		{"businessId", "Location ID", in.LocationCode},
		{"street", "Address", in.Street},
		{"country", "Country", in.Country},
		{"city", "City", in.City},
		{"state", "State", in.State},
		{"postcode", "Zipcode", in.Postcode},
	}
	for _, r := range required {
		if strings.TrimSpace(derefStr(r.v)) == "" {
			return nil, apperr.Validation(r.label + " is required.").
				WithFields(apperr.FieldError{Field: r.field, Code: "REQUIRED", Message: "Required."})
		}
	}
	if err := s.validateKitchenInput(ctx, "", in); err != nil {
		return nil, err
	}
	ck, err := s.store.CreateCentralKitchen(ctx, in, claims.StaffID)
	if err != nil {
		return nil, kitchenWriteError(err)
	}
	s.writeAudit(ctx, claims, "central_kitchen.create", "central_kitchen", &ck.ID, nil, nil, ck, "")
	return ck, nil
}

// UpdateCentralKitchen edits the fields sent. A branchIds list replaces the
// store assignment; leaving it out keeps it.
func (s *Service) UpdateCentralKitchen(ctx context.Context, claims *auth.StaffClaims, id string, in franchiseapi.CentralKitchenInput) (*franchiseapi.CentralKitchen, error) {
	if !s.canManageKitchens(claims) {
		return nil, apperr.PermissionDenied("You do not have permission to edit a corporate office.")
	}
	before, err := s.store.GetCentralKitchen(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Corporate office not found.")
	}
	for _, r := range []struct {
		field, label string
		v            *string
	}{
		{"name", "Corporate office name", in.Name}, {"businessId", "Location ID", in.LocationCode},
		{"street", "Address", in.Street}, {"country", "Country", in.Country}, {"city", "City", in.City},
		{"state", "State", in.State}, {"postcode", "Zipcode", in.Postcode},
	} {
		if r.v != nil && strings.TrimSpace(*r.v) == "" {
			return nil, apperr.Validation(r.label + " cannot be blank.").
				WithFields(apperr.FieldError{Field: r.field, Code: "REQUIRED", Message: "Required."})
		}
	}
	if err := s.validateKitchenInput(ctx, id, in); err != nil {
		return nil, err
	}
	after, err := s.store.UpdateCentralKitchen(ctx, id, in)
	if err != nil {
		return nil, kitchenWriteError(err)
	}
	s.writeAudit(ctx, claims, "central_kitchen.update", "central_kitchen", &id, nil, before, after, "")
	return after, nil
}

// DeleteCentralKitchen retires a kitchen. Its KDS can no longer sign in.
func (s *Service) DeleteCentralKitchen(ctx context.Context, claims *auth.StaffClaims, id string) error {
	if !s.canManageKitchens(claims) {
		return apperr.PermissionDenied("You do not have permission to delete a corporate office.")
	}
	if err := s.store.DeleteCentralKitchen(ctx, id); err != nil {
		return notFoundOrInternal(err, "Corporate office not found.")
	}
	s.writeAudit(ctx, claims, "central_kitchen.delete", "central_kitchen", &id, nil, nil, nil, "")
	return nil
}

func (s *Service) validateKitchenInput(ctx context.Context, kitchenID string, in franchiseapi.CentralKitchenInput) error {
	if in.LocationCode != nil {
		code := strings.TrimSpace(*in.LocationCode)
		if !businessIDPattern.MatchString(code) {
			return apperr.Validation("A Location ID is 2–32 letters, digits or dashes.").
				WithFields(apperr.FieldError{Field: "businessId", Code: "INVALID", Message: "Letters, digits and dashes."})
		}
		if err := s.assertBusinessIDFree(ctx, code, "", kitchenID); err != nil {
			return err
		}
	}
	if in.Status != nil {
		st := strings.ToLower(strings.TrimSpace(*in.Status))
		if st != franchiseapi.CentralKitchenActive && st != franchiseapi.CentralKitchenInactive {
			return apperr.Validation("Status is active or inactive.").
				WithFields(apperr.FieldError{Field: "status", Code: "INVALID", Message: "active or inactive."})
		}
		*in.Status = st
	}
	for _, e := range []struct {
		field string
		v     *string
	}{{"email", in.Email}, {"managerEmail", in.ManagerEmail}} {
		if e.v != nil && strings.TrimSpace(*e.v) != "" {
			if _, err := mail.ParseAddress(strings.TrimSpace(*e.v)); err != nil {
				return apperr.Validation("That is not a valid email address.").
					WithFields(apperr.FieldError{Field: e.field, Code: "INVALID", Message: "Not an email address."})
			}
		}
	}
	if in.BranchIDs != nil && len(*in.BranchIDs) > 0 {
		missing, err := s.store.MissingBranchIDs(ctx, *in.BranchIDs)
		if err != nil {
			return apperr.Internal(err)
		}
		if len(missing) > 0 {
			return apperr.Validation("Some assigned stores do not exist: " + strings.Join(missing, ", ")).
				WithFields(apperr.FieldError{Field: "branchIds", Code: "UNKNOWN", Message: "Unknown store."})
		}
	}
	return nil
}

/*
 * assertBusinessIDFree enforces "one Location ID, one place".
 *
 * Stores and central kitchens keep their codes in different tables, but the
 * code is what a POS or KDS types to say where it is — so a clash anywhere is
 * a clash, and the message says which kind of location already has it.
 */
func (s *Service) assertBusinessIDFree(ctx context.Context, code, excludeBranchID, excludeKitchenID string) error {
	owner, err := s.store.BusinessIDOwner(ctx, code, excludeBranchID, excludeKitchenID)
	if err != nil {
		return apperr.Internal(err)
	}
	switch owner {
	case franchiseapi.LocationBranch:
		return apperr.New("LOCATION_ID_TAKEN", apperr.CategoryConflict,
			"The Location ID \""+code+"\" is already used by a store location.").
			WithFields(apperr.FieldError{Field: "businessId", Code: "TAKEN_BY_STORE", Message: "Already used by a store."})
	case franchiseapi.LocationCentralKitchen:
		return apperr.New("LOCATION_ID_TAKEN", apperr.CategoryConflict,
			"The Location ID \""+code+"\" is already used by a corporate office.").
			WithFields(apperr.FieldError{Field: "businessId", Code: "TAKEN_BY_CORPORATE", Message: "Already used by a corporate office."})
	}
	return nil
}

func kitchenWriteError(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return apperr.NotFound("Corporate office not found.")
	}
	if store.IsDuplicateKey(err) {
		return apperr.New("LOCATION_ID_TAKEN", apperr.CategoryConflict,
			"That Location ID is already in use by a store or a corporate office.").
			WithFields(apperr.FieldError{Field: "businessId", Code: "TAKEN", Message: "Already in use."})
	}
	return apperr.Internal(err)
}

// ------------------------------------------------------------ KDS scope

/*
 * resolveKDSScope decides which location a KDS request is about, and whether
 * the caller may act on it.
 *
 *   - centralKitchenId given: brand-wide staff, or staff scoped to that kitchen.
 *   - branchId given: anyone CanAccessBranch lets in (store staff, brand staff).
 *   - neither: the caller's own single kitchen, else their own single store.
 *
 * A caller covering several locations has to say which one; guessing would
 * configure the wrong kitchen.
 */
func (s *Service) resolveKDSScope(ctx context.Context, claims *auth.StaffClaims, branchID, kitchenID string) (franchiseapi.KDSScope, error) {
	branchID, kitchenID = strings.TrimSpace(branchID), strings.TrimSpace(kitchenID)
	if branchID != "" && kitchenID != "" {
		return franchiseapi.KDSScope{}, apperr.Validation("Send either branchId or centralKitchenId, not both.")
	}
	if kitchenID == "" && branchID == "" {
		kitchens := tenant.CentralKitchenScopes(claims)
		switch {
		case len(kitchens) == 1:
			kitchenID = kitchens[0]
		case len(kitchens) == 0:
			branchID = soleBranchOf(claims)
		}
	}
	if kitchenID != "" {
		if !tenant.IsBrandScoped(claims) && !containsStr(tenant.CentralKitchenScopes(claims), kitchenID) {
			return franchiseapi.KDSScope{}, apperr.PermissionDenied("You do not have access to this corporate office.")
		}
		if _, err := s.store.GetCentralKitchen(ctx, kitchenID); err != nil {
			return franchiseapi.KDSScope{}, notFoundOrInternal(err, "Corporate office not found.")
		}
		return franchiseapi.KDSScope{Type: franchiseapi.LocationCentralKitchen, CentralKitchenID: kitchenID}, nil
	}
	if branchID == "" {
		return franchiseapi.KDSScope{}, apperr.Validation("Say which location: branchId or centralKitchenId.").
			WithFields(apperr.FieldError{Field: "branchId", Code: "REQUIRED", Message: "Required."})
	}
	if _, err := s.store.GetBranch(ctx, branchID); err != nil {
		return franchiseapi.KDSScope{}, notFoundOrInternal(err, "Store not found.")
	}
	if !tenant.CanAccessBranch(claims, "", branchID) {
		return franchiseapi.KDSScope{}, apperr.PermissionDenied("You do not have access to this store.")
	}
	return franchiseapi.KDSScope{Type: franchiseapi.LocationBranch, BranchID: branchID}, nil
}

func (s *Service) mayUseScope(claims *auth.StaffClaims, scopeType string, branchID, kitchenID *string) bool {
	if scopeType == franchiseapi.LocationCentralKitchen {
		return kitchenID != nil && (tenant.IsBrandScoped(claims) || containsStr(tenant.CentralKitchenScopes(claims), *kitchenID))
	}
	return branchID != nil && tenant.CanAccessBranch(claims, "", *branchID)
}

// ------------------------------------------------------------ KDS settings

// GetKDSSettings returns the settings of the location the request names.
func (s *Service) GetKDSSettings(ctx context.Context, claims *auth.StaffClaims, branchID, kitchenID string) (*franchiseapi.KDSSettings, error) {
	scope, err := s.resolveKDSScope(ctx, claims, branchID, kitchenID)
	if err != nil {
		return nil, err
	}
	k, err := s.store.KDSSettingsFor(ctx, scope)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return k, nil
}

// UpdateKDSSettings changes a settings row by its id.
func (s *Service) UpdateKDSSettings(ctx context.Context, claims *auth.StaffClaims, id string, p franchiseapi.KDSSettingsPatch) (*franchiseapi.KDSSettings, error) {
	if !tenant.HasPermission(claims, tenant.PermKDSConfigure) {
		return nil, apperr.PermissionDenied("You do not have permission to change KDS settings.")
	}
	current, err := s.store.GetKDSSettings(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "KDS settings not found.")
	}
	if !s.mayUseScope(claims, current.ScopeType, current.BranchID, current.CentralKitchenID) {
		return nil, apperr.NotFound("KDS settings not found.")
	}
	if err := validateKDSSettingsPatch(current, &p); err != nil {
		return nil, err
	}
	out, err := s.store.UpdateKDSSettings(ctx, id, p)
	if err != nil {
		return nil, notFoundOrInternal(err, "KDS settings not found.")
	}
	var branchIDForAudit *string
	if out.BranchID != nil {
		branchIDForAudit = out.BranchID
	}
	s.writeAudit(ctx, claims, "kds.settings.update", "kds_settings", &out.ID, branchIDForAudit, current, out, "")
	return out, nil
}

var (
	kdsLayouts = map[string]bool{"one_rail": true, "two_rail": true, "flex_rail": true, "tile_rail": true}
	kdsColumns = map[int]bool{2: true, 4: true, 6: true}
)

func validateKDSSettingsPatch(cur *franchiseapi.KDSSettings, p *franchiseapi.KDSSettingsPatch) error {
	if p.OrderAppearMinutes != nil && (*p.OrderAppearMinutes < 0 || *p.OrderAppearMinutes > 24*60) {
		return apperr.Validation("Order appear time is between 0 minutes and 24 hours.").
			WithFields(apperr.FieldError{Field: "orderAppearMinutes", Code: "RANGE", Message: "0 to 1440."})
	}
	if p.TicketLayout != nil {
		v := strings.ToLower(strings.TrimSpace(*p.TicketLayout))
		v = strings.ReplaceAll(strings.ReplaceAll(v, "-", "_"), " ", "_")
		if !kdsLayouts[v] {
			return apperr.Validation("Ticket layout is one_rail, two_rail, flex_rail or tile_rail.").
				WithFields(apperr.FieldError{Field: "ticketLayout", Code: "INVALID", Message: "one_rail, two_rail, flex_rail or tile_rail."})
		}
		p.TicketLayout = &v
	}
	if p.TicketColumns != nil && !kdsColumns[*p.TicketColumns] {
		return apperr.Validation("Ticket columns is 2, 4 or 6.").
			WithFields(apperr.FieldError{Field: "ticketColumns", Code: "INVALID", Message: "2, 4 or 6."})
	}
	for _, c := range []struct {
		field string
		v     *string
	}{{"pickupColor", p.PickupColor}, {"deliveryColor", p.DeliveryColor}, {"takeawayColor", p.TakeawayColor}} {
		if c.v != nil && strings.TrimSpace(*c.v) == "" {
			return apperr.Validation("A colour cannot be blank.").
				WithFields(apperr.FieldError{Field: c.field, Code: "REQUIRED", Message: "Required."})
		}
	}

	// Calendar ranges are checked as they will be after the patch, so moving
	// one boundary validates against the others' stored values.
	val := func(p *int, cur int) int {
		if p != nil {
			return *p
		}
		return cur
	}
	lowFrom, lowTo := val(p.CalendarLowFrom, cur.CalendarLowFrom), val(p.CalendarLowTo, cur.CalendarLowTo)
	medFrom, medTo := val(p.CalendarMediumFrom, cur.CalendarMediumFrom), val(p.CalendarMediumTo, cur.CalendarMediumTo)
	highFrom := val(p.CalendarHighFrom, cur.CalendarHighFrom)
	if lowFrom < 1 || lowFrom > lowTo || lowTo >= medFrom || medFrom > medTo || medTo >= highFrom {
		return apperr.Validation("Calendar ranges must run low → medium → high without overlapping (e.g. 1–3, 4–8, 9+).").
			WithFields(apperr.FieldError{Field: "calendarLowFrom", Code: "RANGE", Message: "Ranges overlap or are out of order."})
	}
	return nil
}

// ------------------------------------------------------------ KDS printers

// ListKDSPrinters returns the location's KDS printers.
func (s *Service) ListKDSPrinters(ctx context.Context, claims *auth.StaffClaims, branchID, kitchenID, kind, search string) ([]franchiseapi.KDSPrinter, error) {
	scope, err := s.resolveKDSScope(ctx, claims, branchID, kitchenID)
	if err != nil {
		return nil, err
	}
	if kind != "" && !validKDSPrinterKind(kind) {
		return nil, kdsPrinterKindError()
	}
	out, err := s.store.ListKDSPrinters(ctx, scope, kind, search)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	return out, nil
}

// GetKDSPrinter returns one printer the caller may see.
func (s *Service) GetKDSPrinter(ctx context.Context, claims *auth.StaffClaims, id string) (*franchiseapi.KDSPrinter, error) {
	p, err := s.store.GetKDSPrinter(ctx, id)
	if err != nil {
		return nil, notFoundOrInternal(err, "Printer not found.")
	}
	if !s.mayUseScope(claims, p.ScopeType, p.BranchID, p.CentralKitchenID) {
		return nil, apperr.NotFound("Printer not found.")
	}
	return p, nil
}

// CreateKDSPrinter adds a printer to the location.
func (s *Service) CreateKDSPrinter(ctx context.Context, claims *auth.StaffClaims, branchID, kitchenID string, in franchiseapi.KDSPrinterInput) (*franchiseapi.KDSPrinter, error) {
	if !tenant.HasPermission(claims, tenant.PermKDSConfigure) {
		return nil, apperr.PermissionDenied("You do not have permission to configure KDS printers.")
	}
	scope, err := s.resolveKDSScope(ctx, claims, branchID, kitchenID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Kind) == "" {
		in.Kind = franchiseapi.PrinterWiFi
	}
	if !validKDSPrinterKind(in.Kind) {
		return nil, kdsPrinterKindError()
	}
	if strings.TrimSpace(derefStr(in.Name)) == "" {
		return nil, apperr.Validation("Give the printer a name.").
			WithFields(apperr.FieldError{Field: "name", Code: "REQUIRED", Message: "Required."})
	}
	if in.Kind == franchiseapi.PrinterBluetooth {
		if strings.TrimSpace(derefStr(in.DeviceAddress)) == "" {
			return nil, apperr.Validation("A Bluetooth printer needs its device address.").
				WithFields(apperr.FieldError{Field: "deviceAddress", Code: "REQUIRED", Message: "Required."})
		}
	} else if err := validateIP(derefStr(in.IPAddress)); err != nil {
		return nil, err
	}
	if err := validatePort(in.Port); err != nil {
		return nil, err
	}
	p, err := s.store.CreateKDSPrinter(ctx, scope, in, claims.StaffID)
	if err != nil {
		return nil, kdsPrinterWriteError(err)
	}
	s.writeAudit(ctx, claims, "kds.printer.create", "kds_printer", &p.ID, p.BranchID, nil, p, "")
	return p, nil
}

// UpdateKDSPrinter edits a printer.
func (s *Service) UpdateKDSPrinter(ctx context.Context, claims *auth.StaffClaims, id string, in franchiseapi.KDSPrinterInput) (*franchiseapi.KDSPrinter, error) {
	if !tenant.HasPermission(claims, tenant.PermKDSConfigure) {
		return nil, apperr.PermissionDenied("You do not have permission to configure KDS printers.")
	}
	current, err := s.GetKDSPrinter(ctx, claims, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return nil, apperr.Validation("Give the printer a name.").
			WithFields(apperr.FieldError{Field: "name", Code: "REQUIRED", Message: "Required."})
	}
	if in.IPAddress != nil && current.Kind != franchiseapi.PrinterBluetooth {
		if err := validateIP(*in.IPAddress); err != nil {
			return nil, err
		}
	}
	if err := validatePort(in.Port); err != nil {
		return nil, err
	}
	p, err := s.store.UpdateKDSPrinter(ctx, id, in)
	if err != nil {
		return nil, kdsPrinterWriteError(err)
	}
	s.writeAudit(ctx, claims, "kds.printer.update", "kds_printer", &p.ID, p.BranchID, current, p, "")
	return p, nil
}

// SetKDSPrinterConnected connects or disconnects a printer on this device.
func (s *Service) SetKDSPrinterConnected(ctx context.Context, claims *auth.StaffClaims, id string, connected bool) (*franchiseapi.KDSPrinter, error) {
	if _, err := s.GetKDSPrinter(ctx, claims, id); err != nil {
		return nil, err
	}
	p, err := s.store.UpdateKDSPrinter(ctx, id, franchiseapi.KDSPrinterInput{IsConnected: &connected})
	if err != nil {
		return nil, kdsPrinterWriteError(err)
	}
	return p, nil
}

// DeleteKDSPrinter removes a printer.
func (s *Service) DeleteKDSPrinter(ctx context.Context, claims *auth.StaffClaims, id string) error {
	if !tenant.HasPermission(claims, tenant.PermKDSConfigure) {
		return apperr.PermissionDenied("You do not have permission to configure KDS printers.")
	}
	p, err := s.GetKDSPrinter(ctx, claims, id)
	if err != nil {
		return err
	}
	if err := s.store.DeleteKDSPrinter(ctx, id); err != nil {
		return notFoundOrInternal(err, "Printer not found.")
	}
	s.writeAudit(ctx, claims, "kds.printer.delete", "kds_printer", &p.ID, p.BranchID, p, nil, "")
	return nil
}

func validKDSPrinterKind(k string) bool {
	return k == franchiseapi.PrinterWiFi || k == "lan" || k == franchiseapi.PrinterBluetooth
}

func kdsPrinterKindError() error {
	return apperr.Validation("A printer is wifi, lan or bluetooth.").
		WithFields(apperr.FieldError{Field: "kind", Code: "INVALID", Message: "wifi, lan or bluetooth."})
}

func validatePort(p *int) error {
	if p != nil && (*p < 1 || *p > 65535) {
		return apperr.Validation("A port is between 1 and 65535.").
			WithFields(apperr.FieldError{Field: "port", Code: "RANGE", Message: "1 to 65535."})
	}
	return nil
}

func kdsPrinterWriteError(err error) error {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "kds_printers_scope_name_idx"):
		return apperr.New("PRINTER_NAME_TAKEN", apperr.CategoryConflict,
			"A printer with that name already exists at this location.").
			WithFields(apperr.FieldError{Field: "name", Code: "TAKEN", Message: "Already used."})
	case strings.Contains(msg, "kds_printers_scope_ip_idx"):
		return apperr.New("PRINTER_IP_TAKEN", apperr.CategoryConflict,
			"A printer with that IP address already exists at this location.").
			WithFields(apperr.FieldError{Field: "ipAddress", Code: "TAKEN", Message: "Already used."})
	}
	return notFoundOrInternal(err, "Printer not found.")
}

func containsStr(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
