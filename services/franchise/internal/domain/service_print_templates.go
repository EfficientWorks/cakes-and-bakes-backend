package domain

import (
	"context"
	"encoding/json"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/tenant"
	"bakehouse/services/franchise/franchiseapi"
)

/*
 * Print template configuration — three tables (receipt/kot/cancel), one
 * GET/PUT pair. Reading is open, same as printers: a till has to know how to
 * lay out what it prints. Writing sits behind branch.operational.
 */

// printTemplateFieldRegistry whitelists exactly which field names each
// receipt type accepts, and what table/column/kind each maps to. This is
// the only thing standing between a PUT request's "field" string and a raw
// SQL column name — nothing outside this map ever reaches a query.
type fieldSpec struct {
	table  string
	column string
	kind   string // "bool" | "text" | "json"
}

var printTemplateFields = map[string]map[string]fieldSpec{
	franchiseapi.ReceiptTypeReceipt: {
		"showCustomerName":       {"print_receipt_templates", "show_customer_name", "bool"},
		"showOrderNotes":         {"print_receipt_templates", "show_order_notes", "bool"},
		"showProductComments":    {"print_receipt_templates", "show_product_comments", "bool"},
		"showAddonPrice":         {"print_receipt_templates", "show_addon_price", "bool"},
		"includeCustomerReceipt": {"print_receipt_templates", "include_customer_receipt", "bool"},
		"columnAlignment":        {"print_receipt_templates", "column_alignment", "json"},
		"dateTimeFormat":         {"print_receipt_templates", "date_time_format", "text"}, // "12h" | "24h"
		"businessName":           {"print_receipt_templates", "business_name", "text"},
		"mobileNo":               {"print_receipt_templates", "mobile_no", "text"},
		"address":                {"print_receipt_templates", "address", "text"},
		"gstVatNumber":           {"print_receipt_templates", "gst_vat_number", "text"},
		"businessLogo":           {"print_receipt_templates", "business_logo", "bool"},
		"showTime":               {"print_receipt_templates", "show_time", "bool"},
		"textWeight":             {"print_receipt_templates", "text_weight", "text"},
		"status":                 {"print_receipt_templates", "is_active", "bool"},
	},
	franchiseapi.ReceiptTypeKOT: {
		"showTime":   {"print_kot_templates", "show_time", "bool"},
		"textWeight": {"print_kot_templates", "text_weight", "text"},
		"status":     {"print_kot_templates", "is_active", "bool"},
	},
	franchiseapi.ReceiptTypeCancel: {
		"showTime":   {"print_cancel_templates", "show_time", "bool"},
		"textWeight": {"print_cancel_templates", "text_weight", "text"},
		"status":     {"print_cancel_templates", "is_active", "bool"},
	},
}

// GetPrintTemplate returns one outlet's config for one receipt kind, reading
// from whichever of the three tables that kind lives in.
func (s *Service) GetPrintTemplate(ctx context.Context, claims *auth.StaffClaims, branchID, receiptType string) (interface{}, error) {
	if _, ok := printTemplateFields[receiptType]; !ok {
		return nil, receiptTypeError()
	}
	branch, err := s.printerBranch(ctx, claims, branchID)
	if err != nil {
		return nil, err
	}

	var out interface{}
	switch receiptType {
	case franchiseapi.ReceiptTypeReceipt:
		out, err = s.store.GetPrintReceiptTemplate(ctx, branch.ID)
	case franchiseapi.ReceiptTypeKOT:
		out, err = s.store.GetPrintKOTTemplate(ctx, branch.ID)
	case franchiseapi.ReceiptTypeCancel:
		out, err = s.store.GetPrintCancelTemplate(ctx, branch.ID)
	}
	if err != nil {
		return nil, notFoundOrInternal(err, "Print template not found.")
	}
	return out, nil
}

// UpdatePrintTemplateField sets one field on one outlet's config for one
// receipt kind, then returns the full row so the caller can refresh its
// screen from a single response.
func (s *Service) UpdatePrintTemplateField(ctx context.Context, claims *auth.StaffClaims, branchID, receiptType string, upd franchiseapi.PrintTemplateFieldUpdate) (interface{}, error) {
	if !tenant.HasPermission(claims, tenant.PermKitchenPrintUpdates) {
		return nil, apperr.PermissionDenied("You do not have permission to configure print templates.")
	}
	fields, ok := printTemplateFields[receiptType]
	if !ok {
		return nil, receiptTypeError()
	}
	spec, ok := fields[upd.Field]
	if !ok {
		return nil, apperr.Validation("Unknown field for this receipt type.").
			WithFields(apperr.FieldError{Field: "field", Code: "INVALID", Message: "Not a field on this template."})
	}
	value, err := coerceFieldValue(spec, upd.Value)
	if err != nil {
		return nil, err
	}

	branch, err := s.printerBranch(ctx, claims, branchID)
	if err != nil {
		return nil, err
	}
	if err := s.store.UpdatePrintTemplateField(ctx, spec.table, spec.column, branch.ID, value); err != nil {
		return nil, notFoundOrInternal(err, "Print template not found.")
	}

	s.writeAudit(ctx, claims, "pos.print_template.update", "pos_print_template", nil, &branch.ID, nil,
		map[string]interface{}{"receiptType": receiptType, "field": upd.Field, "value": upd.Value}, "")

	return s.GetPrintTemplate(ctx, claims, branch.ID, receiptType)
}

// coerceFieldValue rejects a value whose JSON shape doesn't match what the
// column expects — e.g. a string where showTime needs a bool — before it
// ever reaches the database, and re-marshals a "json" field's array so the
// store layer can hand it to pgx as bytes.
func coerceFieldValue(spec fieldSpec, raw interface{}) (interface{}, error) {
	switch spec.kind {
	case "bool":
		v, ok := raw.(bool)
		if !ok {
			return nil, apperr.Validation("This field needs a true/false value.").
				WithFields(apperr.FieldError{Field: "value", Code: "TYPE", Message: "Must be a boolean."})
		}
		return v, nil
	case "text":
		v, ok := raw.(string)
		if !ok {
			return nil, apperr.Validation("This field needs a text value.").
				WithFields(apperr.FieldError{Field: "value", Code: "TYPE", Message: "Must be a string."})
		}
		return v, nil
	case "json":
		b, err := json.Marshal(raw)
		if err != nil {
			return nil, apperr.Validation("That value isn't valid JSON.").
				WithFields(apperr.FieldError{Field: "value", Code: "TYPE", Message: "Must be a column list."})
		}
		return b, nil
	}
	return nil, apperr.Internal(nil)
}

func receiptTypeError() error {
	return apperr.Validation(`Receipt type must be "receipt", "kot", or "cancel".`).
		WithFields(apperr.FieldError{Field: "receiptType", Code: "INVALID", Message: `Must be "receipt", "kot", or "cancel".`})
}
