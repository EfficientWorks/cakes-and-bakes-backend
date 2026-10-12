package domain

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/email"
	"bakehouse/pkg/tenant"
	"bakehouse/services/admin/adminapi"
)

/*
 * Invoices.
 *
 * Raising one is a commercial act, so it is gated on the same permission that
 * governs the rest of a branch's money rather than on "can see reports": a
 * viewer who may read the takings should not be able to bill somebody for
 * them.
 */

// ListInvoices returns one page, scoped to what the caller may see.
func (s *Service) ListInvoices(ctx context.Context, claims *auth.StaffClaims, f adminapi.InvoiceListFilter) ([]adminapi.Invoice, int, error) {
	if !tenant.HasPermission(claims, tenant.PermOrdersView) {
		return nil, 0, apperr.PermissionDenied("You do not have permission to view invoices.")
	}

	/*
	 * Scope is applied over whatever was asked for, never instead of it.
	 *
	 * A branch-scoped account asking for another outlet's invoices gets its
	 * own — the filter narrows, it cannot widen. Brand-scoped accounts pass
	 * through untouched, which is what lets head office see the network.
	 */
	if !tenant.HasPermission(claims, tenant.PermReportsNetwork) {
		own := ""
		if claims != nil {
			own = branchOf(claims)
		}
		// No branch to narrow to must not mean "no filter at all".
		if own == "" {
			return nil, 0, apperr.PermissionDenied("You do not have a branch to list invoices for.")
		}
		f.BranchID = own
	}

	out, total, err := s.store.ListInvoices(ctx, f)
	if err != nil {
		return nil, 0, apperr.Internal(err)
	}
	return out, total, nil
}

// GetInvoice returns one invoice with its lines.
func (s *Service) GetInvoice(ctx context.Context, claims *auth.StaffClaims, id string) (*adminapi.Invoice, error) {
	if !tenant.HasPermission(claims, tenant.PermOrdersView) {
		return nil, apperr.PermissionDenied("You do not have permission to view invoices.")
	}
	inv, err := s.store.GetInvoice(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound("Invoice not found.")
	}
	if err != nil {
		return nil, apperr.Internal(err)
	}
	if !tenant.CanAccessBranch(claims, "", inv.Store.ID) && !tenant.HasPermission(claims, tenant.PermReportsNetwork) {
		return nil, apperr.NotFound("Invoice not found.")
	}
	return inv, nil
}

/*
 * CreateInvoice prices the document and stores it.
 *
 * The arithmetic happens here, not in the console. The console draws a live
 * preview and the server has to agree with it — but "agree" means the server
 * decides and the preview follows, because only one of the two is what the
 * customer will be asked to pay.
 */
func (s *Service) CreateInvoice(ctx context.Context, claims *auth.StaffClaims, in adminapi.CreateInvoiceInput) (*adminapi.Invoice, error) {
	if !tenant.HasPermission(claims, tenant.PermOrdersCreateManual) {
		return nil, apperr.PermissionDenied("You do not have permission to raise invoices.")
	}

	in.Kind = strings.TrimSpace(in.Kind)
	// switch in.Kind {
	// case adminapi.InvoiceCustomer:
	// 	if strings.TrimSpace(in.CustomerID) == "" {
	// 		return nil, apperr.Validation("Choose who this invoice is for.").
	// 			WithFields(apperr.FieldError{Field: "customerId", Code: "REQUIRED", Message: "Required."})
	// 	}
	// case adminapi.InvoiceStore:
	// 	if strings.TrimSpace(in.BilledBranchID) == "" {
	// 		return nil, apperr.Validation("Choose which store this invoice is for.").
	// 			WithFields(apperr.FieldError{Field: "billedBranchId", Code: "REQUIRED", Message: "Required."})
	// 	}
	// default:
	// 	return nil, apperr.Validation("An invoice is for a customer or a store.").
	// 		WithFields(apperr.FieldError{Field: "kind", Code: "INVALID", Message: "customer or store."})
	// }

	if strings.TrimSpace(in.BranchID) == "" {
		return nil, apperr.Validation("Choose the store supplying this.").
			WithFields(apperr.FieldError{Field: "branchId", Code: "REQUIRED", Message: "Required."})
	}
	if !tenant.CanAccessBranch(claims, "", in.BranchID) {
		return nil, apperr.PermissionDenied("You do not have access to that store.")
	}
	if strings.TrimSpace(in.IssuedDate) == "" {
		in.IssuedDate = time.Now().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", in.IssuedDate); err != nil {
		return nil, apperr.Validation("An issue date is YYYY-MM-DD.").
			WithFields(apperr.FieldError{Field: "issuedDate", Code: "INVALID", Message: "YYYY-MM-DD."})
	}
	if len(in.Items) == 0 {
		return nil, apperr.Validation("Add at least one line to the invoice.").
			WithFields(apperr.FieldError{Field: "items", Code: "REQUIRED", Message: "Required."})
	}
	for i, item := range in.Items {
		// if strings.TrimSpace(item.ProductName) == "" {
		// 	return nil, apperr.Validation("Every line needs a name.").
		// 		WithFields(apperr.FieldError{Field: itemField(i, "productName"), Code: "REQUIRED", Message: "Required."})
		// }
		if item.Quantity <= 0 {
			return nil, apperr.Validation("A quantity is at least one.").
				WithFields(apperr.FieldError{Field: itemField(i, "quantity"), Code: "MIN", Message: "At least 1."})
		}
		if item.UnitPriceCents < 0 {
			return nil, apperr.Validation("A price cannot be negative.").
				WithFields(apperr.FieldError{Field: itemField(i, "unitPriceCents"), Code: "MIN", Message: "Zero or more."})
		}
	}
	if in.DiscountPct != nil && (*in.DiscountPct < 0 || *in.DiscountPct > 100) {
		return nil, apperr.Validation("A discount is between 0 and 100 percent.").
			WithFields(apperr.FieldError{Field: "discountPct", Code: "RANGE", Message: "0 to 100."})
	}

	// The console may identify who is billed by id rather than typing the
	// details in. Without this, billedTo.email stays blank and a "send"
	// silently does nothing — which is how invoices were never arriving.
	if err := s.fillBilledTo(ctx, &in); err != nil {
		return nil, err
	}
	if in.Send && strings.TrimSpace(in.BilledTo.Email) == "" {
		return nil, apperr.Validation("Add an email address for whoever this invoice bills, or save it without sending.").
			WithFields(apperr.FieldError{Field: "billedTo.email", Code: "REQUIRED", Message: "Required to send."})
	}

	totals, err := s.priceInvoice(ctx, in)
	if err != nil {
		return nil, err
	}

	id, err := s.store.CreateInvoice(ctx, in, *totals, staffIDOf(claims))
	if err != nil {
		return nil, apperr.Internal(err)
	}

	created, err := s.store.GetInvoice(ctx, id)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	if in.Send {
		// A failure to send is reported on the invoice (sentAt stays null)
		// and logged, not by failing the call — the document exists either
		// way, and telling somebody their invoice was not created when it
		// was is the worse outcome. The console can retry via /send, which
		// does return the error.
		if err := s.sendInvoice(ctx, created); err != nil {
			slog.Default().Error("invoice created but email failed", "invoiceId", created.ID, "reference", created.Reference, "to", created.BilledTo.Email, "error", err)
		} else if refreshed, err := s.store.GetInvoice(ctx, id); err == nil {
			created = refreshed // carries sentAt
		}
	}
	return created, nil
}

// fillBilledTo completes the billed party from customerId / billedBranchId
// when the console did not send the details itself. Anything the operator
// typed wins; only blanks are filled.
func (s *Service) fillBilledTo(ctx context.Context, in *adminapi.CreateInvoiceInput) error {
	var p adminapi.InvoiceCustomerInput
	var err error
	switch {
	case strings.TrimSpace(in.CustomerID) != "":
		p, err = s.store.CustomerContact(ctx, strings.TrimSpace(in.CustomerID))
	case strings.TrimSpace(in.BilledBranchID) != "":
		p, err = s.store.BranchContact(ctx, strings.TrimSpace(in.BilledBranchID))
	default:
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.Validation("Whoever this invoice bills could not be found.").
			WithFields(apperr.FieldError{Field: "customerId", Code: "INVALID", Message: "Unknown."})
	}
	if err != nil {
		return apperr.Internal(err)
	}
	b := &in.BilledTo
	fill := func(dst *string, v string) {
		if strings.TrimSpace(*dst) == "" {
			*dst = v
		}
	}
	fill(&b.FirstName, p.FirstName)
	fill(&b.LastName, p.LastName)
	fill(&b.Email, p.Email)
	fill(&b.Phone, p.Phone)
	fill(&b.Address, p.Address)
	fill(&b.City, p.City)
	fill(&b.State, p.State)
	return nil
}

/*
 * priceInvoice works out what is owed.
 *
 * Tax comes from the supplying outlet, because that is the jurisdiction the
 * sale happens in — not from wherever the customer lives. Applied after the
 * discount, so a discounted order is taxed on what is actually paid.
 */
func (s *Service) priceInvoice(ctx context.Context, in adminapi.CreateInvoiceInput) (*adminapi.Invoice, error) {
	var subtotal int64
	for _, item := range in.Items {
		subtotal += item.UnitPriceCents * int64(item.Quantity)
	}

	discount := in.DiscountCents
	if in.DiscountPct != nil && *in.DiscountPct > 0 {
		// Rounded to the nearest cent: truncating always favours the shop, and
		// over a run of invoices that is a real amount owed to somebody.
		discount = int64(math.Round(float64(subtotal) * (*in.DiscountPct) / 100))
	}
	if discount > subtotal {
		discount = subtotal
	}

	taxable := subtotal - discount
	ratePct, taxName, err := s.store.BranchTax(ctx, in.BranchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.Validation("That store does not exist.").
			WithFields(apperr.FieldError{Field: "branchId", Code: "INVALID", Message: "Unknown store."})
	}
	if err != nil {
		return nil, apperr.Internal(err)
	}

	var tax int64
	if ratePct > 0 && taxable > 0 {
		tax = int64(math.Round(float64(taxable) * ratePct / 100))
	}

	return &adminapi.Invoice{
		SubtotalCents: subtotal,
		DiscountCents: discount,
		DiscountPct:   in.DiscountPct,
		TaxCents:      tax,
		TaxName:       taxName,
		TotalCents:    taxable + tax,
	}, nil
}

/*
 * SetInvoiceStatus settles an invoice by hand.
 *
 * "Close invoice" on the detail screen. Money arriving by bank transfer is not
 * something this system can observe, so somebody has to say so — and that is a
 * deliberate act with a permission behind it rather than a toggle.
 */
func (s *Service) SetInvoiceStatus(ctx context.Context, claims *auth.StaffClaims, id, status string) (*adminapi.Invoice, error) {
	if !tenant.HasPermission(claims, tenant.PermOrdersCreateManual) {
		return nil, apperr.PermissionDenied("You do not have permission to settle invoices.")
	}
	switch status {
	case adminapi.InvoicePaid, adminapi.InvoiceUnpaid, adminapi.InvoiceVoid:
	default:
		return nil, apperr.Validation("An invoice is unpaid, paid or void.").
			WithFields(apperr.FieldError{Field: "status", Code: "INVALID", Message: "unpaid, paid or void."})
	}

	if _, err := s.GetInvoice(ctx, claims, id); err != nil {
		return nil, err
	}
	if err := s.store.SetInvoiceStatus(ctx, id, status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.NotFound("Invoice not found.")
		}
		return nil, apperr.Internal(err)
	}
	return s.GetInvoice(ctx, claims, id)
}

// SendInvoice emails the document to whoever it bills.
func (s *Service) SendInvoice(ctx context.Context, claims *auth.StaffClaims, id string) (*adminapi.Invoice, error) {
	inv, err := s.GetInvoice(ctx, claims, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(inv.BilledTo.Email) == "" {
		return nil, apperr.Validation("There is no email address on file for whoever this bills.")
	}
	if err := s.sendInvoice(ctx, inv); err != nil {
		slog.Default().Error("invoice email failed", "invoiceId", inv.ID, "reference", inv.Reference, "to", inv.BilledTo.Email, "error", err)
		// An explicit "send" must say when nothing went out.
		return nil, apperr.Wrap(err, "EMAIL_SEND_FAILED", apperr.CategoryUpstream, "The invoice email could not be sent: "+err.Error())
	}
	return s.GetInvoice(ctx, claims, id)
}

// sendInvoice posts the document and records that it went. It returns the
// reason when nothing was sent, so no caller can mistake silence for success.
func (s *Service) sendInvoice(ctx context.Context, inv *adminapi.Invoice) error {
	if s.emailer == nil {
		return errors.New("no emailer configured on the admin service")
	}
	to := strings.TrimSpace(inv.BilledTo.Email)
	if to == "" {
		return errors.New("invoice has no billed-to email address")
	}
	full := func(first, last string) string {
		return strings.TrimSpace(strings.TrimSpace(first) + " " + strings.TrimSpace(last))
	}
	lines := make([]map[string]any, 0, len(inv.Items))
	for _, item := range inv.Items {
		mods := make([]map[string]any, 0, len(item.Modifiers))
		for _, m := range item.Modifiers {
			mods = append(mods, map[string]any{"name": m.Name, "groupName": m.GroupName, "priceDelta": m.PriceDelta})
		}
		lines = append(lines, map[string]any{
			"name":       item.ProductName,
			"variant":    item.VariantName,
			"modifiers":  mods,
			"quantity":   item.Quantity,
			"unitPrice":  item.UnitPriceCents,
			"total":      item.LineTotalCents,
			"disclaimer": item.Disclaimer,
		})
	}
	data := map[string]interface{}{
		"reference":  inv.Reference,
		"status":     inv.Status,
		"resend":     inv.SentAt != nil, // sent before: a reminder, with its own subject
		"name":       strings.TrimSpace(inv.BilledTo.FirstName),
		"issuedDate": inv.IssuedDate,
		"message":    inv.Message,
		"store": map[string]interface{}{
			"name":     inv.Store.Name,
			"street":   inv.Store.Street,
			"city":     inv.Store.City,
			"state":    inv.Store.State,
			"postcode": inv.Store.Postcode,
			"email":    inv.Store.Email,
			"phone":    inv.Store.Phone,
		},
		"billedTo": map[string]interface{}{
			"name":    full(inv.BilledTo.FirstName, inv.BilledTo.LastName),
			"address": inv.BilledTo.Address,
			"city":    inv.BilledTo.City,
			"state":   inv.BilledTo.State,
			"email":   inv.BilledTo.Email,
			"phone":   inv.BilledTo.Phone,
		},
		"items":         lines,
		"subtotal":      inv.SubtotalCents,
		"discount":      inv.DiscountCents,
		"taxName":       inv.TaxName,
		"tax":           inv.TaxCents,
		"total":         inv.TotalCents,
		"bankName":      inv.BankName,
		"accountNumber": inv.AccountNumber,
		"sortCode":      inv.SortCode,
	}
	if inv.DiscountPct != nil {
		data["discountPct"] = *inv.DiscountPct
	}
	if inv.Disclaimer != nil {
		data["disclaimer"] = *inv.Disclaimer
	}
	if inv.IsGift {
		data["receiver"] = map[string]interface{}{
			"name":    full(inv.Receiver.FirstName, inv.Receiver.LastName),
			"address": inv.Receiver.Address,
			"city":    inv.Receiver.City,
			"state":   inv.Receiver.State,
			"email":   inv.Receiver.Email,
			"phone":   inv.Receiver.Phone,
		}
	}
	if err := s.emailer.Send(ctx, email.Email{
		To:       to,
		Template: email.TemplateInvoiceIssued,
		Data:     data,
	}); err != nil {
		return err
	}
	slog.Default().Info("invoice emailed", "invoiceId", inv.ID, "reference", inv.Reference, "to", to, "reminder", inv.SentAt != nil)
	if err := s.store.MarkInvoiceSent(ctx, inv.ID); err != nil {
		// The email went; only the bookkeeping failed. Log, don't fail.
		slog.Default().Warn("invoice emailed but sent_at not recorded", "invoiceId", inv.ID, "error", err)
	}
	return nil
}

func itemField(i int, name string) string {
	return "items[" + itoa(i) + "]." + name
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [8]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}

func staffIDOf(claims *auth.StaffClaims) string {
	if claims == nil {
		return ""
	}
	return claims.StaffID
}

// branchOf reads the first branch assignment a token carries. Staff can hold
// several; the first one is what the listing is limited to.
func branchOf(claims *auth.StaffClaims) string {
	for _, ra := range claims.RoleAssignments {
		if ra.ScopeType == tenant.ScopeBranch && ra.BranchID != nil {
			return *ra.BranchID
		}
	}
	return ""
}
