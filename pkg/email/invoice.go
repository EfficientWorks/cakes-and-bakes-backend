package email

import (
	"fmt"
	"html/template"
	"strings"
	"time"
)

/*
 * The invoice email.
 *
 * It carries the whole document, the way the console's invoice view draws it:
 * who is billed and where from, every line with its options and disclaimer,
 * the money summary with the tax named as the outlet names it, and the bank
 * details to pay into. Someone reading it on a phone should be able to pay
 * from it without opening anything else.
 *
 * Data keys (all optional except reference and total):
 *
 *	reference, status ("unpaid"|"paid"|"void"), issuedDate (YYYY-MM-DD),
 *	resend (bool), name (greeting), message,
 *	store:    {name, street, city, state, postcode, email, phone}
 *	billedTo: {name, address, city, state, email, phone}
 *	receiver: {name, address, city, state, email, phone}   (gift orders)
 *	items:    [{name, variant, quantity, unitPrice, total, disclaimer,
 *	            modifiers: [{name, groupName, priceDelta}]}]
 *	disclaimer, subtotal, discount, discountPct, taxName, tax, total,
 *	bankName, accountNumber, sortCode
 *
 * storeName is still read when no store block is given, so older callers keep
 * working.
 */

// invoiceLavender is the pale panel the party blocks sit on (as in the view).
const invoiceLavender = "#F7F5FB"

func renderInvoice(data map[string]interface{}) *RenderedEmail {
	reference := extractString(data, "reference")
	status := strings.ToLower(extractString(data, "status"))
	if status == "" {
		status = "unpaid"
	}
	issued := invoiceDate(extractString(data, "issuedDate"))
	name := strings.TrimSpace(extractString(data, "name"))
	note := extractString(data, "message")
	resend, _ := data["resend"].(bool)

	store := subMap(data, "store")
	storeName := extractItemString(store, "name")
	if storeName == "" {
		storeName = extractString(data, "storeName")
	}
	billed := subMap(data, "billedTo")
	receiver := subMap(data, "receiver")

	subtotal := toCents(data["subtotal"])
	discount := toCents(data["discount"])
	tax := toCents(data["tax"])
	total := toCents(data["total"])
	taxName := strings.TrimSpace(extractString(data, "taxName"))
	if taxName == "" {
		taxName = "Tax"
	}
	discountLabel := "Discount"
	if pct, ok := data["discountPct"].(float64); ok && pct > 0 {
		discountLabel = fmt.Sprintf("Discount (%s%%)", trimFloat(pct))
	}

	// --- subject ------------------------------------------------------------
	// A resend gets its own subject: with the same one, Outlook and Gmail fold
	// it into the first message's thread and it looks like nothing arrived.
	subject := fmt.Sprintf("Invoice %s from Baked Bouquet – %s", reference, Money(total))
	switch {
	case status == "paid":
		subject = fmt.Sprintf("Invoice %s – Paid – Baked Bouquet", reference)
	case status == "void":
		subject = fmt.Sprintf("Invoice %s – Void – Baked Bouquet", reference)
	case resend:
		subject = fmt.Sprintf("Reminder: Invoice %s – %s due – Baked Bouquet", reference, Money(total))
	}

	// --- heading --------------------------------------------------------------
	title := "Your invoice"
	amountLabel := "Amount due"
	switch status {
	case "paid":
		title, amountLabel = "Invoice paid", "Amount paid"
	case "void":
		title, amountLabel = "Invoice void", "Amount (void)"
	}

	greeting := "Hi there,"
	if name != "" {
		greeting = "Hi " + strong(name) + ","
	}
	from := ""
	if storeName != "" {
		from = " from " + strong(storeName)
	}
	intro := greeting + "<br>"
	switch status {
	case "paid":
		intro += "Thank you — this invoice" + from + " has been paid in full. A copy is below for your records."
	case "void":
		intro += "This invoice" + from + " has been cancelled and no payment is due. A copy is below for your records."
	default:
		if resend {
			intro += "A reminder of your invoice" + from + ". The full details are below."
		} else {
			intro += "Here is your invoice" + from + ". The full details are below."
		}
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0"><tr>
  <td valign="top" style="padding:0;">%s</td>
  <td valign="top" align="right" style="padding:0;white-space:nowrap;">%s</td>
</tr></table>`, eyebrow("Invoice #"+template.HTMLEscapeString(reference)), statusPill(status)))
	b.WriteString(h1(title))
	if issued != "" {
		b.WriteString(fmt.Sprintf(`<p style="margin:-10px 0 16px;font-size:13px;color:%s;">Issued %s</p>`, brandMuted, template.HTMLEscapeString(issued)))
	}
	b.WriteString(para(intro))

	b.WriteString(fmt.Sprintf(`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="margin:4px 0 22px;"><tr><td align="center" style="background-color:%s;border-radius:8px;padding:18px 16px;">
  <p style="margin:0 0 6px;font-size:12px;letter-spacing:1px;text-transform:uppercase;color:%s;">%s</p>
  <p style="margin:0;font-size:30px;line-height:1.1;font-weight:800;color:%s;">%s</p>
</td></tr></table>`, brandPink, brandInk, template.HTMLEscapeString(amountLabel), brandInk, template.HTMLEscapeString(Money(total))))

	b.WriteString(noteBlock(note))

	// --- parties --------------------------------------------------------------
	storeLines := []string{
		extractItemString(store, "street"),
		joinNonEmpty(", ", extractItemString(store, "city"), joinNonEmpty(" ", extractItemString(store, "state"), extractItemString(store, "postcode"))),
		extractItemString(store, "email"),
		extractItemString(store, "phone"),
	}
	billedLines := partyLines(billed)
	b.WriteString(fmt.Sprintf(`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="margin:0 0 8px;"><tr><td style="background-color:%s;border-radius:8px;padding:16px;">
  <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0"><tr>
    <td valign="top" width="50%%" style="padding:0 8px 0 0;">%s</td>
    <td valign="top" width="50%%" align="right" style="padding:0 0 0 8px;">%s</td>
  </tr></table>
</td></tr></table>`, invoiceLavender,
		partyBlock("Billed To", extractItemString(billed, "name"), billedLines, "left"),
		partyBlock("Store Location", storeName, storeLines, "right")))

	if rName := extractItemString(receiver, "name"); rName != "" {
		b.WriteString(fmt.Sprintf(`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="margin:8px 0 8px;"><tr><td style="background-color:%s;border-radius:8px;padding:16px;">%s</td></tr></table>`,
			invoiceLavender, partyBlock("Gift Recipient", rName, partyLines(receiver), "left")))
	}

	// --- lines ----------------------------------------------------------------
	b.WriteString(sectionLabel("Product Detail"))
	head := func(label, align, width string) string {
		w := ""
		if width != "" {
			w = ` width="` + width + `"`
		}
		return fmt.Sprintf(`<td align="%s"%s style="padding:0 0 8px;font-size:11px;letter-spacing:0.5px;text-transform:uppercase;color:%s;border-bottom:1px solid %s;">%s</td>`,
			align, w, brandMuted, brandLine, label)
	}
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr>` +
		head("Name", "left", "") + head("Qty", "center", "40") + head("Price", "right", "76") + head("Total", "right", "80") + `</tr>`)

	var itemsText strings.Builder
	for _, item := range invoiceItems(data) {
		itemName := extractItemString(item, "name")
		variant := extractItemString(item, "variant")
		qty := fmt.Sprintf("%v", item["quantity"])
		unit := Money(toCents(item["unitPrice"]))
		lineTotal := Money(toCents(item["total"]))
		disclaimer := extractItemString(item, "disclaimer")

		var sub strings.Builder
		if variant != "" {
			sub.WriteString(fmt.Sprintf(`<div style="font-size:12px;line-height:1.6;color:%s;">%s</div>`, brandMuted, template.HTMLEscapeString(variant)))
		}
		itemsText.WriteString(fmt.Sprintf("  - %s x%s @ %s = %s\n", itemName, qty, unit, lineTotal))
		if variant != "" {
			itemsText.WriteString("      " + variant + "\n")
		}
		for _, m := range invoiceModifiers(item) {
			sub.WriteString(fmt.Sprintf(`<div style="font-size:12px;line-height:1.6;color:%s;">&bull; %s</div>`, brandMuted, template.HTMLEscapeString(m)))
			itemsText.WriteString("      • " + m + "\n")
		}
		if disclaimer != "" {
			sub.WriteString(fmt.Sprintf(`<div style="margin-top:8px;font-size:12px;line-height:1.6;color:%s;"><span style="color:%s;">Disclaimer :</span> %s</div>`,
				brandInk, brandMuted, template.HTMLEscapeString(disclaimer)))
			itemsText.WriteString("      Disclaimer: " + disclaimer + "\n")
		}

		cell := func(v, align string, bold bool) string {
			w := "400"
			if bold {
				w = "700"
			}
			return fmt.Sprintf(`<td valign="top" align="%s" style="padding:12px 0;font-size:13px;font-weight:%s;color:%s;border-bottom:1px solid %s;white-space:nowrap;">%s</td>`,
				align, w, brandInk, brandLine, template.HTMLEscapeString(v))
		}
		b.WriteString(fmt.Sprintf(`<tr><td valign="top" style="padding:12px 8px 12px 0;border-bottom:1px solid %s;"><div style="font-size:13px;font-weight:700;color:%s;">%s</div>%s</td>%s%s%s</tr>`,
			brandLine, brandInk, template.HTMLEscapeString(itemName), sub.String(),
			cell(qty, "center", false), cell(unit, "right", false), cell(lineTotal, "right", true)))
	}
	b.WriteString(`</table>`)

	general := strings.TrimSpace(extractString(data, "disclaimer"))
	if general == "<nil>" {
		general = ""
	}
	if general != "" {
		b.WriteString(fmt.Sprintf(`<p style="margin:12px 0 0;font-size:12px;line-height:1.6;color:%s;"><span style="color:%s;">Disclaimer :</span> %s</p>`,
			brandInk, brandMuted, template.HTMLEscapeString(general)))
	}

	// --- money ----------------------------------------------------------------
	var totals, totalsText strings.Builder
	totals.WriteString(totalsRow("Subtotal", Money(subtotal), false))
	totalsText.WriteString("Subtotal: " + Money(subtotal) + "\n")
	if discount > 0 {
		totals.WriteString(totalsRow(discountLabel, "-"+Money(discount), false))
		totalsText.WriteString(discountLabel + ": -" + Money(discount) + "\n")
	}
	totals.WriteString(totalsRow(taxName, Money(tax), false))
	totalsText.WriteString(taxName + ": " + Money(tax) + "\n")
	totals.WriteString(`<tr><td colspan="2" style="padding:6px 0 0;border-bottom:1px solid ` + brandLine + `;font-size:0;line-height:0;">&nbsp;</td></tr>`)
	totals.WriteString(totalsRow("Total", Money(total), true))
	totalsText.WriteString("Total: " + Money(total))
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin-top:16px;"><tr><td width="40%">&nbsp;</td><td><table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">` +
		totals.String() + `</table></td></tr></table>`)

	// --- how to pay -------------------------------------------------------------
	bankName := extractString(data, "bankName")
	account := extractString(data, "accountNumber")
	sortCode := extractString(data, "sortCode")
	bankText := ""
	if status != "paid" && status != "void" && (bankName != "" || account != "" || sortCode != "") {
		col := func(label, v string) string {
			if v == "" {
				v = "—"
			}
			return fmt.Sprintf(`<td valign="top" width="33%%" style="padding:0 8px 0 0;"><div style="font-size:12px;color:%s;">%s</div><div style="margin-top:2px;font-size:13px;font-weight:700;color:%s;">%s</div></td>`,
				brandMuted, label, brandInk, template.HTMLEscapeString(v))
		}
		b.WriteString(sectionLabel("Payment Details"))
		b.WriteString(fmt.Sprintf(`<table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0" style="border-top:1px solid %s;"><tr><td style="padding-top:12px;">
  <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0"><tr>%s%s%s</tr></table>
</td></tr></table>`, brandLine, col("Bank Name", bankName), col("Account Number", account), col("Sort Code", sortCode)))
		b.WriteString(paraMuted(`<span style="display:block;margin-top:10px;">Please quote ` + strong(reference) + ` as the payment reference.</span>`))
		bankText = fmt.Sprintf("\nPayment details\n  Bank name: %s\n  Account number: %s\n  Sort code: %s\n  Reference: %s\n", bankName, account, sortCode, reference)
	}

	contact := "reply to this email"
	if e := extractItemString(store, "email"); e != "" {
		contact = fmt.Sprintf(`contact us at <a href="mailto:%s" style="color:%s;">%s</a>`, template.HTMLEscapeString(e), brandPurple, template.HTMLEscapeString(e))
	}
	b.WriteString(`<div style="margin-top:22px;">` + rule() + `</div>`)
	b.WriteString(paraMuted(`<span style="display:block;margin-top:14px;">If anything looks wrong, ` + contact + ` quoting <span style="white-space:nowrap;">` + strong(reference) + `</span> and we will put it right.</span>`))

	htmlBody := shell(shellOptions{Title: "Invoice " + reference}, b.String())

	// --- plain text -------------------------------------------------------------
	var t strings.Builder
	t.WriteString(fmt.Sprintf("Baked Bouquet – Invoice %s (%s)\n", reference, strings.ToUpper(status)))
	if issued != "" {
		t.WriteString("Issued " + issued + "\n")
	}
	t.WriteString("\n" + stripTags(intro) + "\n\n")
	t.WriteString(fmt.Sprintf("%s: %s\n\n", amountLabel, Money(total)))
	if note != "" {
		t.WriteString(note + "\n\n")
	}
	t.WriteString("Billed to:\n  " + extractItemString(billed, "name") + "\n")
	for _, l := range billedLines {
		if l != "" {
			t.WriteString("  " + l + "\n")
		}
	}
	t.WriteString("\nStore:\n  " + storeName + "\n")
	for _, l := range storeLines {
		if l != "" {
			t.WriteString("  " + l + "\n")
		}
	}
	if rName := extractItemString(receiver, "name"); rName != "" {
		t.WriteString("\nGift recipient:\n  " + rName + "\n")
	}
	t.WriteString("\nItems:\n" + itemsText.String())
	if general != "" {
		t.WriteString("\nDisclaimer: " + general + "\n")
	}
	t.WriteString("\n" + totalsText.String() + "\n")
	t.WriteString(bankText)
	t.WriteString(fmt.Sprintf("\nIf anything looks wrong, reply quoting %s.", reference))

	return &RenderedEmail{Subject: subject, HTMLBody: htmlBody, TextBody: t.String()}
}

// statusPill is the small coloured status badge beside the reference.
func statusPill(status string) string {
	fg, bg, label := brandRed, "#FDECEF", "Unpaid"
	switch status {
	case "paid":
		fg, bg, label = brandGreen, "#E7F6EC", "Paid"
	case "void":
		fg, bg, label = brandMuted, "#EFEFF4", "Void"
	}
	return fmt.Sprintf(`<span style="display:inline-block;background-color:%s;color:%s;font-size:12px;font-weight:700;padding:4px 10px;border-radius:12px;white-space:nowrap;">&#9679; %s</span>`, bg, fg, label)
}

// partyBlock is one "Billed To" / "Store Location" column.
func partyBlock(label, name string, lines []string, align string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<div style="text-align:%s;">`, align))
	b.WriteString(fmt.Sprintf(`<div style="margin:0 0 8px;font-size:12px;font-weight:700;color:%s;">%s</div>`, brandInk, template.HTMLEscapeString(label)))
	if name != "" {
		b.WriteString(fmt.Sprintf(`<div style="font-size:13px;color:%s;margin-bottom:2px;">%s</div>`, brandInk, template.HTMLEscapeString(name)))
	}
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		b.WriteString(fmt.Sprintf(`<div style="font-size:12px;line-height:1.6;color:%s;word-break:break-word;">%s</div>`, brandMuted, template.HTMLEscapeString(l)))
	}
	b.WriteString(`</div>`)
	return b.String()
}

// partyLines is a person's address and contact lines, in the view's order.
func partyLines(p map[string]interface{}) []string {
	return []string{
		extractItemString(p, "address"),
		joinNonEmpty(", ", extractItemString(p, "city"), extractItemString(p, "state")),
		extractItemString(p, "email"),
		extractItemString(p, "phone"),
	}
}

func subMap(data map[string]interface{}, key string) map[string]interface{} {
	if m, ok := data[key].(map[string]interface{}); ok {
		return m
	}
	if m, ok := data[key].(map[string]string); ok {
		out := make(map[string]interface{}, len(m))
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	return map[string]interface{}{}
}

func invoiceItems(data map[string]interface{}) []map[string]interface{} {
	switch v := data["items"].(type) {
	case []map[string]interface{}:
		return v
	case []interface{}:
		out := make([]map[string]interface{}, 0, len(v))
		for _, e := range v {
			if m, ok := e.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// invoiceModifiers renders each chosen option as "Group : Name (+$x)".
func invoiceModifiers(item map[string]interface{}) []string {
	var raw []map[string]interface{}
	switch v := item["modifiers"].(type) {
	case []map[string]interface{}:
		raw = v
	case []interface{}:
		for _, e := range v {
			if m, ok := e.(map[string]interface{}); ok {
				raw = append(raw, m)
			}
		}
	}
	out := make([]string, 0, len(raw))
	for _, m := range raw {
		name, delta := extractModifierDetails(m)
		if name == "" {
			continue
		}
		s := name
		if g := extractItemString(m, "groupName"); g != "" {
			s = g + " : " + name
		}
		if delta > 0 {
			s += " (+" + Money(delta) + ")"
		}
		out = append(out, s)
	}
	return out
}

// invoiceDate turns YYYY-MM-DD into "26 Sep 2026"; anything else is shown as is.
func invoiceDate(s string) string {
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(s)); err == nil {
		return t.Format("2 Jan 2006")
	}
	return s
}

func joinNonEmpty(sep string, parts ...string) string {
	kept := parts[:0:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, strings.TrimSpace(p))
		}
	}
	return strings.Join(kept, sep)
}

func trimFloat(f float64) string {
	s := fmt.Sprintf("%.2f", f)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return s
}

// stripTags is enough for the few inline tags the intro carries.
func stripTags(s string) string {
	s = strings.ReplaceAll(s, "<br>", "\n")
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return htmlUnescape(b.String())
}

func htmlUnescape(s string) string {
	r := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&#34;", `"`, "&#39;", "'", "&quot;", `"`)
	return r.Replace(s)
}
