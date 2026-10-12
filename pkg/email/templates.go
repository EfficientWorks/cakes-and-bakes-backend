package email

import (
	"fmt"
	"html/template"
	"strings"
	"time"
)

// RenderedEmail contains pre-rendered subject, HTML, and text versions of an email.
type RenderedEmail struct {
	Subject  string
	HTMLBody string
	TextBody string
}

// RenderTemplate generates built-in HTML and plaintext templates for known transactional emails.
// RenderTemplate builds one email.
//
// The bodies below are written against the public domains; the wrapper swaps
// them for whatever this deployment actually runs on, so every link in every
// template is corrected in one place rather than in each of the eleven.
func RenderTemplate(templateAlias string, data map[string]interface{}) (*RenderedEmail, error) {
	return RenderBranded(PlatformBrand, templateAlias, data)
}

func renderTemplate(templateAlias string, data map[string]interface{}) (*RenderedEmail, error) {
	switch templateAlias {
	case TemplatePasswordResetOTP:
		otp := extractString(data, "otp")
		expiresInMinutes := extractInt(data, "expiresInMinutes", 10)

		subject := fmt.Sprintf("Reset your {{business}} password - %s", otp)
		body := h1("Reset your password") +
			para("We received a request to reset the password for your {{business}} account. Use the one-time code below:") +
			codeBox(otp, fmt.Sprintf("Valid for %d minutes", expiresInMinutes)) +
			paraMuted("If you did not request a password reset, you can safely ignore this email &mdash; your account remains secure.")
		htmlBody := shell(shellOptions{Title: "Reset your password"}, body)

		textBody := fmt.Sprintf("{{business}} - Password Reset Request\n\nUse the following one-time code to reset your password:\n\n%s\n\nThis code expires in %d minutes.\n\nIf you did not request this, please ignore this email.", otp, expiresInMinutes)

		return &RenderedEmail{Subject: subject, HTMLBody: htmlBody, TextBody: textBody}, nil

	case TemplateAccountVerifyOTP:
		otp := extractString(data, "otp")
		expiresInMinutes := extractInt(data, "expiresInMinutes", 10)

		subject := fmt.Sprintf("Verify your {{business}} account - %s", otp)
		body := h1("Verify your email") +
			para("Enter the code below to verify your email address and activate your account:") +
			codeBox(otp, fmt.Sprintf("Valid for %d minutes", expiresInMinutes)) +
			paraMuted("If you did not sign up for an account, you can safely ignore this email.")
		htmlBody := shell(shellOptions{Title: "Verify your account"}, body)

		textBody := fmt.Sprintf("{{business}} - Verify your email\n\nYour verification code is:\n\n%s\n\nThis code expires in %d minutes.", otp, expiresInMinutes)

		return &RenderedEmail{Subject: subject, HTMLBody: htmlBody, TextBody: textBody}, nil

	case TemplateCustomerWelcome:
		name := strings.TrimSpace(strings.TrimSpace(extractString(data, "firstName")) + " " + strings.TrimSpace(extractString(data, "lastName")))
		greeting := "Welcome"
		if name != "" {
			greeting = "Welcome " + name
		}
		subject := "Welcome to {{business}}! 🎉"
		body := h1(template.HTMLEscapeString(greeting)) +
			para("Your account is verified and ready to go. Browse our beautiful bouquets and place your first order &ndash; We can&rsquo;t wait to create something special for you") +
			button("Start Shopping", "https://shop.invalid/shop")
		htmlBody := shell(shellOptions{Title: "Welcome"}, body)
		textBody := fmt.Sprintf("{{business}} – %s\n\nYour account is now verified and ready to use. Visit https://shop.invalid/shop to start shopping!", greeting)
		return &RenderedEmail{Subject: subject, HTMLBody: htmlBody, TextBody: textBody}, nil

	case TemplateOrderReady:
		reference := extractString(data, "reference")
		recipientName := extractString(data, "recipientName")
		branchName := extractString(data, "branchName")
		branchAddress := extractString(data, "branchAddress")
		subject := fmt.Sprintf("Your Order is Ready for Pickup – %s", reference)
		where := strong(branchName)
		if branchAddress != "" {
			where += `<br>` + template.HTMLEscapeString(branchAddress)
		}
		body := eyebrow("Order #"+template.HTMLEscapeString(reference)) +
			h1Colour("Ready for pickup", brandGreen) +
			para("Hi "+strong(recipientName)+",<br>Great news &ndash; your order is ready and waiting for you.") +
			noticeBox(`<strong style="font-weight:700;">Pickup from :</strong>&nbsp; `+where) +
			paraMuted("Please bring your order reference "+strong(reference)+" when collecting.")
		htmlBody := shell(shellOptions{Title: "Order Ready"}, body)
		textBody := fmt.Sprintf("{{business}} – Order Ready\n\nHi %s, your order %s is ready for pickup at %s.\n\nPlease bring your order reference %s when collecting.",
			recipientName, reference, branchName, reference)
		return &RenderedEmail{Subject: subject, HTMLBody: htmlBody, TextBody: textBody}, nil

	case TemplateOrderDelivered:
		reference := extractString(data, "reference")
		recipientName := extractString(data, "recipientName")
		subject := fmt.Sprintf("Your Order Has Been Delivered – %s", reference)
		body := eyebrow("Order #"+template.HTMLEscapeString(reference)) +
			h1("Delivered!") +
			para("Hi "+strong(recipientName)+",<br>Your order has been delivered. We hope you loved it!") +
			paraMuted("Thank you for choosing {{business}}. We&rsquo;d love to see you again soon.") +
			button("Order Again", "https://shop.invalid/shop")
		htmlBody := shell(shellOptions{Title: "Order Delivered"}, body)
		textBody := fmt.Sprintf("{{business}} – Delivered!\n\nHi %s, your order %s has been delivered. Thank you for choosing {{business}}!",
			recipientName, reference)
		return &RenderedEmail{Subject: subject, HTMLBody: htmlBody, TextBody: textBody}, nil

	case TemplateBranchNewOrder:
		reference := extractString(data, "reference")
		recipientName := extractString(data, "recipientName")
		scheduledDate := extractString(data, "scheduledDate")
		slotStart := extractString(data, "slotStart")
		slotEnd := extractString(data, "slotEnd")
		totalCents := toCents(data["total"])
		if totalCents == 0 && data["total"] != nil {
			totalCents = int64(extractInt(data, "total", 0))
		}
		total := Money(totalCents)
		subject := fmt.Sprintf("New Order Received – %s", reference)
		body := h1("New order received") +
			orderNoBox(reference) +
			para(strong("Customer :")+" "+template.HTMLEscapeString(recipientName)) +
			para(strong("Scheduled :")+" "+template.HTMLEscapeString(scheduleLine(scheduledDate, slotStart, slotEnd))) +
			para(strong("Order Total :")+" "+template.HTMLEscapeString(total)) +
			paraMuted("Log in to the staff portal to view and manage this order.")
		htmlBody := shell(shellOptions{Title: "New Order"}, body)
		textBody := fmt.Sprintf("{{business}} – New Order\n\nReference: %s\nCustomer: %s\nScheduled: %s, %s – %s\nTotal: %s\n\nLog in to the staff portal to manage this order.",
			reference, recipientName, scheduledDate, slotStart, slotEnd, total)
		return &RenderedEmail{Subject: subject, HTMLBody: htmlBody, TextBody: textBody}, nil

	case TemplateOrderConfirmation:
		reference := extractString(data, "reference")
		recipientName := extractString(data, "recipientName")
		purchaserName := strings.TrimSpace(extractString(data, "purchaserName"))
		scheduledDate := extractString(data, "scheduledDate")
		slotStart := extractString(data, "slotStart")
		slotEnd := extractString(data, "slotEnd")
		subtotalCents := toCents(data["subtotal"])
		discountCents := toCents(data["discount"])
		discountCode := extractString(data, "discountCode")
		fulfilmentType := extractString(data, "fulfilmentType")
		deliveryFeeCents := toCents(data["deliveryFee"])
		taxCents := toCents(data["tax"])
		tipCents := toCents(data["tip"])
		serviceChargeCents := toCents(data["serviceCharge"])
		totalCents := toCents(data["total"])
		if totalCents == 0 && data["total"] != nil {
			totalCents = int64(extractInt(data, "total", 0))
		}
		total := Money(totalCents)
		trackingUrl := extractString(data, "trackingUrl")

		// Items: name and price on one line, then the quantity and every
		// chosen option as a bulleted list beneath.
		var itemsHTML strings.Builder
		itemsText := ""
		itemCount := 0
		if items, ok := data["items"].([]map[string]interface{}); ok {
			for _, item := range items {
				name := extractItemString(item, "name")
				variant := extractItemString(item, "variant")
				qty := fmt.Sprintf("%v", item["quantity"])
				lineTotal := Money(toCents(item["total"]))
				if n, ok := item["quantity"].(int); ok {
					itemCount += n
				} else {
					itemCount++
				}

				var bullets strings.Builder
				bullets.WriteString(bullet("Quantity", qty))
				itemsText += fmt.Sprintf("  - %s x%s — %s\n", name, qty, lineTotal)
				if variant != "" {
					bullets.WriteString(bullet("Variation", variant))
					itemsText += fmt.Sprintf("    • Variation: %s\n", variant)
				}
				for _, mod := range extractItemModifiers(item) {
					deltaStr := ""
					if mod.priceDelta > 0 {
						deltaStr = fmt.Sprintf(" (+%s)", Money(mod.priceDelta))
					}
					bullets.WriteString(bullet(mod.label, mod.name+deltaStr))
					itemsText += fmt.Sprintf("    • %s: %s%s\n", mod.label, mod.name, deltaStr)
				}

				itemsHTML.WriteString(fmt.Sprintf(`<tr><td style="padding:12px 0 10px;border-bottom:1px solid %s;">
  <table role="presentation" width="100%%" cellpadding="0" cellspacing="0" border="0"><tr>
    <td style="font-size:13px;font-weight:700;color:%s;">%s</td>
    <td align="right" valign="top" style="font-size:13px;font-weight:700;color:%s;white-space:nowrap;padding-left:12px;">%s</td>
  </tr></table>
  <table role="presentation" cellpadding="0" cellspacing="0" border="0" style="margin-top:6px;">%s</table>
</td></tr>`, brandLine, brandInk, template.HTMLEscapeString(name), brandInk, lineTotal, bullets.String()))
			}
		}

		// The money summary. Lines that are nothing are still shown, the way
		// a receipt is read: delivery and tax are questions a customer asks
		// even when the answer is zero.
		var totals strings.Builder
		var breakdownText strings.Builder
		totals.WriteString(totalsRow("Subtotal", Money(subtotalCents), false))
		breakdownText.WriteString(fmt.Sprintf("Subtotal: %s\n", Money(subtotalCents)))
		if discountCents > 0 {
			discLabel := "Discount"
			if discountCode != "" {
				discLabel = fmt.Sprintf("Discount (%s)", discountCode)
			}
			totals.WriteString(totalsRow(discLabel, "-"+Money(discountCents), false))
			breakdownText.WriteString(fmt.Sprintf("%s: -%s\n", discLabel, Money(discountCents)))
		}
		deliveryLabel := "Delivery"
		if strings.EqualFold(fulfilmentType, "pickup") {
			deliveryLabel = "Pickup"
		}
		totals.WriteString(totalsRow(deliveryLabel, Money(deliveryFeeCents), false))
		breakdownText.WriteString(fmt.Sprintf("%s: %s\n", deliveryLabel, Money(deliveryFeeCents)))
		totals.WriteString(totalsRow("Estimated Tax", Money(taxCents), false))
		breakdownText.WriteString(fmt.Sprintf("Estimated Tax: %s\n", Money(taxCents)))
		if serviceChargeCents > 0 {
			totals.WriteString(totalsRow("Service Charge", Money(serviceChargeCents), false))
			breakdownText.WriteString(fmt.Sprintf("Service Charge: %s\n", Money(serviceChargeCents)))
		}
		totals.WriteString(totalsRow("Tip", Money(tipCents), false))
		breakdownText.WriteString(fmt.Sprintf("Tip: %s\n", Money(tipCents)))
		totals.WriteString(totalsRow("Total", total, true))
		breakdownText.WriteString(fmt.Sprintf("Total: %s", total))

		greeting := "Hi there,"
		if purchaserName != "" {
			greeting = "Hi " + strong(purchaserName) + ","
		}
		intro := greeting + "<br>Your Order for " + strong(recipientName) + " has been received<br>and its being prepared"

		itemsLabel := fmt.Sprintf("%d items", itemCount)
		if itemCount == 1 {
			itemsLabel = "1 item"
		}

		body := h1("Order confirmation") +
			para(intro) +
			orderNoBox(reference) +
			fmt.Sprintf(`<p style="margin:0 0 6px;font-size:13px;color:%s;text-align:center;"><strong style="font-weight:700;">SCHEDULED :</strong> %s</p>`, brandInk, template.HTMLEscapeString(scheduleLine(scheduledDate, slotStart, slotEnd))) +
			sectionLabel("Items") +
			`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">` + itemsHTML.String() + `</table>` +
			`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin-top:14px;">` + totals.String() + `</table>` +
			button("Track shipment", trackingUrl)

		subject := fmt.Sprintf("Order Confirmed – %s", reference)
		htmlBody := shell(shellOptions{Title: "Order confirmation", HeaderRight: pill(itemsLabel + " \U0001F6D2")}, body)

		textBody := fmt.Sprintf("{{business}} – Order Confirmed\n\nHi %s! Your order for %s has been received and is being prepared.\n\nOrder No: %s\nScheduled: %s\n\nItems:\n%s\n%s\n\nTrack your order: %s",
			purchaserName, recipientName, reference, scheduleLine(scheduledDate, slotStart, slotEnd), itemsText, breakdownText.String(), trackingUrl)
		return &RenderedEmail{Subject: subject, HTMLBody: htmlBody, TextBody: textBody}, nil

	case TemplateOrderAmended:
		reference := extractString(data, "reference")
		recipientName := extractString(data, "recipientName")
		reason := extractString(data, "reason")

		subject := fmt.Sprintf("Your Order Has Been Updated – %s", reference)
		body := eyebrow("Order #"+template.HTMLEscapeString(reference)) +
			h1Colour("Order Updated", brandViolet) +
			para("Hi "+strong(recipientName)+",<br>Your order has been updated by our team.") +
			noticeBox(`<strong style="font-weight:700;">Reason :</strong>&nbsp; `+template.HTMLEscapeString(reason)) +
			para("If you have any questions, Please Contact us")
		htmlBody := shell(shellOptions{Title: "Order Updated"}, body)

		textBody := fmt.Sprintf("{{business}} – Order Updated\n\nHi %s,\n\nYour order %s has been updated.\n\nReason: %s\n\nIf you have questions, please contact us quoting your reference %s.",
			recipientName, reference, reason, reference)

		return &RenderedEmail{Subject: subject, HTMLBody: htmlBody, TextBody: textBody}, nil

	case TemplateOrderCancelled:
		reference := extractString(data, "reference")
		recipientName := extractString(data, "recipientName")

		// The reason is deliberately not shown.
		//
		// It is written by staff for staff — "duplicate", "customer no-show",
		// "kitchen error" — and reads as blunt or baffling to the person who
		// placed the order. It stays on the order and in the audit trail,
		// where anyone answering a question about it can see it.

		subject := fmt.Sprintf("Order Cancelled – %s", reference)
		body := eyebrow("Order #"+template.HTMLEscapeString(reference)) +
			h1Colour("Order Cancelled", brandRed) +
			para("Hi "+strong(recipientName)+",<br>Unfortunately your order has been cancelled") +
			para("If you believe this is an error or need help<br>Please Contact us quoting "+strong(reference))
		htmlBody := shell(shellOptions{Title: "Order Cancelled"}, body)

		textBody := fmt.Sprintf("{{business}} – Order Cancelled\n\nHi %s,\n\nYour order %s has been cancelled.\n\nIf you have questions, please contact us quoting your reference %s.",
			recipientName, reference, reference)

		return &RenderedEmail{Subject: subject, HTMLBody: htmlBody, TextBody: textBody}, nil

	case TemplateInvoiceIssued:
		return renderInvoice(data), nil

	case TemplatePaymentLink:
		// The bill number the counter knows the order by ("#74"); the order
		// reference only when there is none.
		reference := extractString(data, "reference")
		label := "Order #" + reference
		if bill := extractString(data, "billNo"); bill != "" {
			label = "Bill No. #" + bill
		}
		name := extractString(data, "customerName")
		url := extractString(data, "url")
		amount := extractMoney(data, "amountCents")
		greeting, plainGreeting := "Hi,", "Hi,"
		if name != "" {
			greeting, plainGreeting = "Hi "+strong(name)+",", "Hi "+name+","
		}

		subject := fmt.Sprintf("Pay for your order – %s", label)
		body := eyebrow(template.HTMLEscapeString(label)) +
			h1("Complete your payment") +
			para(greeting+"<br>Please pay "+strong(amount)+" for your {{business}} order using the secure link below.") +
			button("Pay "+amount, url) +
			paraMuted("This link is for this order only. If you were sent a newer link, use that one instead.")
		htmlBody := shell(shellOptions{Title: "Complete your payment"}, body)

		textBody := fmt.Sprintf("{{business}} – Complete your payment\n\n%s\n\nPlease pay %s for %s here:\n%s\n\nIf you were sent a newer link, use that one instead.",
			plainGreeting, amount, label, url)

		return &RenderedEmail{Subject: subject, HTMLBody: htmlBody, TextBody: textBody}, nil

	case TemplateEnquiryAcknowledged:
		name := extractString(data, "name")
		kind := extractString(data, "type")
		greeting := "Hello"
		if name != "" {
			greeting = "Hello " + name
		}
		what := "enquiry"
		switch kind {
		case "custom_order":
			what = "custom order request"
		case "franchise":
			what = "franchise enquiry"
		}

		subject := "We've got your " + what
		body := h1("Thank you &mdash; we have your "+template.HTMLEscapeString(what)) +
			para(template.HTMLEscapeString(greeting)+",<br>We have received your "+template.HTMLEscapeString(what)+" and someone will be in touch shortly. There is nothing more you need to do.") +
			paraMuted("If you need to add anything, simply reply to this email.") +
			paraMuted("&mdash; The {{business}} team")
		htmlBody := shell(shellOptions{Title: "Thank you"}, body)

		textBody := fmt.Sprintf("%s,\n\nThank you — we have received your %s and someone will be in touch shortly.\n\nIf you need to add anything, just reply to this email.\n\n— The {{business}} team", greeting, what)
		return &RenderedEmail{Subject: subject, HTMLBody: htmlBody, TextBody: textBody}, nil

	case TemplateStaffInvite:
		firstName := extractString(data, "firstName")
		email := extractString(data, "email")
		role := extractString(data, "role")
		loginURL := extractString(data, "loginUrl")
		if loginURL == "" {
			// The console signs in at /signin. /login is a 404, which is what a
			// new member of staff was being sent to.
			loginURL = "https://admin.invalid/signin"
		}

		subject := "You've been invited to {{business}} Staff Portal"
		title := "Welcome to the team"
		if firstName != "" {
			title += ", " + template.HTMLEscapeString(firstName)
		}
		details := strong("Login email :") + " " + template.HTMLEscapeString(email)
		if role != "" {
			details += "<br>" + strong("Role :") + " " + template.HTMLEscapeString(role)
		}
		body := h1(title) +
			para("You have been invited to join the {{business}} staff portal.") +
			noticeBox(details) +
			paraMuted("Use the password you were given to sign in, and change it once you are in.") +
			button("Log In to Staff Portal", loginURL)
		htmlBody := shell(shellOptions{Title: "Staff Invitation"}, body)

		greetingText := "Hello"
		if firstName != "" {
			greetingText = "Hello " + firstName
		}
		textBody := fmt.Sprintf("{{business}} – Staff Invitation\n\n%s,\n\nYou have been invited to join the {{business}} staff portal.\n\nLogin: %s\nRole: %s\n\nLog in at: %s",
			greetingText, email, role, loginURL)
		return &RenderedEmail{Subject: subject, HTMLBody: htmlBody, TextBody: textBody}, nil

	case TemplateEnquiryReceived:
		name := extractString(data, "name")
		enquiryEmail := extractString(data, "email")
		enquiryType := extractString(data, "type")
		phone := extractString(data, "phone")
		message := extractString(data, "message")
		branchName := extractString(data, "branchName")
		enquiryID := extractString(data, "enquiryId")

		subject := fmt.Sprintf("New Enquiry Received – %s", enquiryType)
		if branchName != "" {
			subject = fmt.Sprintf("New Enquiry for %s – %s", branchName, enquiryType)
		}

		row := func(label, value string) string {
			return fmt.Sprintf(`<tr><td valign="top" style="padding:7px 0;font-size:13px;font-weight:700;color:%s;width:110px;">%s</td><td style="padding:7px 0;font-size:13px;color:%s;">%s</td></tr>`, brandInk, template.HTMLEscapeString(label), brandInk, value)
		}
		var rows strings.Builder
		if branchName != "" {
			rows.WriteString(row("Branch", template.HTMLEscapeString(branchName)))
		}
		rows.WriteString(row("Name", template.HTMLEscapeString(name)))
		rows.WriteString(row("Email", fmt.Sprintf(`<a href="mailto:%s" style="color:%s;">%s</a>`, template.HTMLEscapeString(enquiryEmail), brandPurple, template.HTMLEscapeString(enquiryEmail))))
		if phone != "" {
			rows.WriteString(row("Phone", template.HTMLEscapeString(phone)))
		}
		rows.WriteString(row("Type", template.HTMLEscapeString(enquiryType)))
		rows.WriteString(row("ID", `<span style="font-family:ui-monospace,Menlo,Consolas,monospace;color:`+brandMuted+`;">`+template.HTMLEscapeString(enquiryID)+`</span>`))
		messageSection := ""
		if message != "" {
			messageSection = sectionLabel("Message") + noticeBox(template.HTMLEscapeString(message))
		}
		body := h1("You have a new enquiry") +
			`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">` + rows.String() + `</table>` +
			messageSection +
			paraMuted("Reply to the customer directly, or log in to the console to manage this enquiry.")
		htmlBody := shell(shellOptions{Title: "New Enquiry"}, body)

		phoneText := ""
		if phone != "" {
			phoneText = "\nPhone: " + phone
		}
		branchText := ""
		if branchName != "" {
			branchText = "\nBranch: " + branchName
		}
		msgText := ""
		if message != "" {
			msgText = "\n\nMessage:\n" + message
		}
		textBody := fmt.Sprintf("{{business}} – New Enquiry\n\nType: %s%s\nName: %s\nEmail: %s%s\nID: %s%s",
			enquiryType, branchText, name, enquiryEmail, phoneText, enquiryID, msgText)

		return &RenderedEmail{Subject: subject, HTMLBody: htmlBody, TextBody: textBody}, nil

	default:
		return nil, fmt.Errorf("no built-in template for alias: %s", templateAlias)
	}
}

func extractString(data map[string]interface{}, key string) string {
	if data == nil {
		return ""
	}
	if val, ok := data[key]; ok {
		return fmt.Sprintf("%v", val)
	}
	return ""
}

func extractInt(data map[string]interface{}, key string, fallback int) int {
	if data == nil {
		return fallback
	}
	switch v := data[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return fallback
	}
}

// toCents reads a line total out of the loosely-typed template data.
//
// It arrives as whatever the caller put in the map — an int64 from the
// order, a float64 if it has been through JSON — so every numeric shape is
// accepted rather than one being assumed and the rest printing as garbage.
func toCents(v interface{}) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	case int32:
		return int64(n)
	default:
		return 0
	}
}

func extractItemString(item map[string]interface{}, key string) string {
	if item == nil {
		return ""
	}
	val, ok := item[key]
	if !ok || val == nil {
		return ""
	}
	switch s := val.(type) {
	case string:
		if s == "<nil>" {
			return ""
		}
		return strings.TrimSpace(s)
	case *string:
		if s == nil || *s == "<nil>" {
			return ""
		}
		return strings.TrimSpace(*s)
	default:
		str := fmt.Sprintf("%v", val)
		if str == "<nil>" {
			return ""
		}
		return strings.TrimSpace(str)
	}
}

type itemModifier struct {
	label      string
	name       string
	priceDelta int64
}

func extractItemModifiers(item map[string]interface{}) []itemModifier {
	var list []itemModifier
	if item == nil {
		return list
	}

	addMod := func(label string, val interface{}) {
		if val == nil {
			return
		}
		switch v := val.(type) {
		case map[string]interface{}:
			name, delta := extractModifierDetails(v)
			if name != "" {
				list = append(list, itemModifier{label: label, name: name, priceDelta: delta})
			}
		case string:
			if strings.TrimSpace(v) != "" && v != "<nil>" {
				list = append(list, itemModifier{label: label, name: strings.TrimSpace(v)})
			}
		case *string:
			if v != nil && *v != "" && *v != "<nil>" {
				list = append(list, itemModifier{label: label, name: strings.TrimSpace(*v)})
			}
		}
	}

	addList := func(label string, key string) {
		val, ok := item[key]
		if !ok || val == nil {
			return
		}
		switch slice := val.(type) {
		case []map[string]interface{}:
			for _, m := range slice {
				name, delta := extractModifierDetails(m)
				if name != "" {
					list = append(list, itemModifier{label: label, name: name, priceDelta: delta})
				}
			}
		case []interface{}:
			for _, elem := range slice {
				addMod(label, elem)
			}
		}
	}

	addList("Flavour", "flavours")
	addList("Add-on", "addons")
	if pres, ok := item["presentation"]; ok && pres != nil {
		addMod("Presentation", pres)
	}
	if wrap, ok := item["wrap"]; ok && wrap != nil {
		addMod("Wrap", wrap)
	}
	if mods, ok := item["modifiers"]; ok && mods != nil {
		addList("Option", "modifiers")
	}

	return list
}

func extractModifierDetails(m map[string]interface{}) (name string, priceDelta int64) {
	if m == nil {
		return "", 0
	}
	if n, ok := m["name"]; ok && n != nil {
		switch str := n.(type) {
		case string:
			name = str
		case *string:
			if str != nil {
				name = *str
			}
		default:
			name = fmt.Sprintf("%v", n)
		}
	}
	if name == "<nil>" {
		name = ""
	}
	priceDelta = toCents(m["priceDelta"])
	if priceDelta == 0 {
		priceDelta = toCents(m["price"])
	}
	return strings.TrimSpace(name), priceDelta
}

// noteBlock renders the sender's own message, or nothing. An empty panel
// where a note would go reads as something failing to load.
func noteBlock(note string) string {
	if strings.TrimSpace(note) == "" {
		return ""
	}
	return noticeBox(template.HTMLEscapeString(note))
}

// bullet is one "Label : value" line under an item on the confirmation.
func bullet(label, value string) string {
	return fmt.Sprintf(`<tr><td valign="top" style="font-size:12px;line-height:1.6;color:%s;padding-right:6px;">&bull;</td><td style="font-size:12px;line-height:1.6;color:%s;">%s : %s</td></tr>`,
		brandMuted, brandMuted, template.HTMLEscapeString(label), template.HTMLEscapeString(value))
}

// scheduleLine is "26 Aug 2026 @ 9:00 AM - 12:00 PM": the date as a person
// says it and the slot in 12-hour clock, from the YYYY-MM-DD and HH:MM the
// order stores. Anything that does not parse is shown as it came.
func scheduleLine(date, start, end string) string {
	d := date
	if t, err := time.Parse("2006-01-02", date); err == nil {
		d = t.Format("2 Jan 2006")
	}
	clock := func(hm string) string {
		hm = strings.TrimSpace(hm)
		if len(hm) > 5 {
			hm = hm[:5]
		}
		if t, err := time.Parse("15:04", hm); err == nil {
			return t.Format("3:04 PM")
		}
		return hm
	}
	if start == "" && end == "" {
		return d
	}
	if end == "" {
		return d + " @ " + clock(start)
	}
	return d + " @ " + clock(start) + " - " + clock(end)
}

// extractMoney renders a minor-unit amount from the template data. Accepts the
// several numeric shapes a JSON round-trip can produce.
func extractMoney(data map[string]interface{}, key string) string {
	switch v := data[key].(type) {
	case int64:
		return fmt.Sprintf("$%.2f", float64(v)/100)
	case int:
		return fmt.Sprintf("$%.2f", float64(v)/100)
	case float64:
		return fmt.Sprintf("$%.2f", v/100)
	default:
		return "$0.00"
	}
}
