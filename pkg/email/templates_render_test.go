package email

import (
	"os"
	"strings"
	"testing"
)

// Every template renders on the shared shell, escapes what it is given, and
// says what it is meant to say. With EMAIL_PREVIEW_DIR set, the rendered HTML
// is also written there so the look can be checked in a browser:
//
//	EMAIL_PREVIEW_DIR=/tmp/emails go test ./pkg/email/
func TestTemplatesRenderOnBrandShell(t *testing.T) {
	items := []map[string]interface{}{
		{"name": "Golden Glow Cupcake Bouquet", "quantity": 1, "total": 9100,
			"flavours": []map[string]interface{}{{"name": "6 cupcakes"}},
			"addons":   []map[string]interface{}{{"name": "Gluten - Free Vanilla"}, {"name": `"It's A Girl" Micro Balloon`}}},
		{"name": "Lavender Spring Cupcake Bouquet", "quantity": 1, "total": 20000,
			"flavours": []map[string]interface{}{{"name": "6 cupcakes"}}},
	}
	cases := []struct {
		alias string
		data  map[string]interface{}
		want  []string
	}{
		{TemplateOrderConfirmation, map[string]interface{}{
			"reference": "BB6VPXNXFF", "recipientName": "Naveen Kumar", "purchaserName": "Santhosh",
			"scheduledDate": "2026-08-26", "slotStart": "09:00", "slotEnd": "12:00",
			"items": items, "subtotal": 29100, "deliveryFee": 0, "tax": 0, "tip": 0, "total": 29100,
			"fulfilmentType": "delivery", "trackingUrl": ShopLink("/orders/BB6VPXNXFF"),
		}, []string{"Order confirmation", "Santhosh", "Naveen Kumar", "BB6VPXNXFF", "26 Aug 2026 @ 9:00 AM - 12:00 PM",
			"Quantity : 1", "Flavour : 6 cupcakes", "Add-on : Gluten - Free Vanilla", "£291.00", "Track shipment", "2 items"}},
		{TemplateCustomerWelcome, map[string]interface{}{"firstName": "Guru", "lastName": "Santhosh"},
			[]string{"Welcome Guru Santhosh", "Start Shopping"}},
		{TemplateOrderCancelled, map[string]interface{}{"reference": "BB6VPXNXFF", "recipientName": "Naveen Kumar"},
			[]string{"Order #BB6VPXNXFF", "Order Cancelled", "Naveen Kumar", "Please Contact us"}},
		{TemplateOrderAmended, map[string]interface{}{"reference": "BB6VPXNXFF", "recipientName": "Naveen Kumar", "reason": "Order delivery date has changed"},
			[]string{"Order Updated", "Reason :", "Order delivery date has changed"}},
		{TemplateAccountVerifyOTP, map[string]interface{}{"otp": "482913", "expiresInMinutes": 10}, []string{"482913", "Valid for 10 minutes"}},
		{TemplatePasswordResetOTP, map[string]interface{}{"otp": "482913"}, []string{"482913"}},
		{TemplateOrderReady, map[string]interface{}{"reference": "BB1", "recipientName": "A", "branchName": "Soho", "branchAddress": "1 High St"}, []string{"Ready for pickup", "Soho", "1 High St"}},
		{TemplateOrderDelivered, map[string]interface{}{"reference": "BB1", "recipientName": "A"}, []string{"Delivered!"}},
		{TemplateBranchNewOrder, map[string]interface{}{"reference": "BB1", "recipientName": "A", "scheduledDate": "2026-08-26", "slotStart": "09:00", "slotEnd": "12:00", "total": 1000}, []string{"New order received", "£10.00"}},
		{TemplateInvoiceIssued, map[string]interface{}{"reference": "INV-1", "name": "A", "storeName": "Soho", "issuedDate": "1 Sep 2026", "total": int64(12345), "message": "Thanks"}, []string{"£123.45", "Thanks"}},
		{TemplateInvoiceIssued, map[string]interface{}{
			"reference": "INV-1041", "status": "unpaid", "name": "Jenifer", "issuedDate": "2026-09-26",
			"store":    map[string]interface{}{"name": "new store 2", "street": "1/23 EVP Prabhu Avanue", "city": "Chennai", "state": "Tamil Nadu", "postcode": "60005", "email": "store@x.com", "phone": "+1 (207) 381-4063"},
			"billedTo": map[string]interface{}{"name": "Jenifer A", "city": "Chennai", "state": "Tamilnadu", "email": "jen@x.com", "phone": "0967"},
			"items": []map[string]any{{"name": "Berry Blossom Combination Box", "variant": "6 Cupcakes", "quantity": 1, "unitPrice": int64(7570), "total": int64(7570), "disclaimer": "Keep chilled",
				"modifiers": []map[string]any{{"name": "Vanilla", "groupName": "Add-on", "priceDelta": int64(200)}}}},
			"disclaimer": "Unboxing video required", "subtotal": int64(15140), "taxName": "GST", "tax": int64(1514), "total": int64(16654),
			"bankName": "test", "accountNumber": "123456788", "sortCode": "123445",
		}, []string{"Invoice #INV-1041", "Unpaid", "Issued 26 Sep 2026", "Billed To", "Jenifer A", "Store Location", "1/23 EVP Prabhu Avanue", "Chennai, Tamil Nadu 60005",
			"Berry Blossom Combination Box", "6 Cupcakes", "Add-on : Vanilla (+£2.00)", "Keep chilled", "Unboxing video required", "GST", "£15.14", "£166.54", "Bank Name", "123456788", "123445"}},
		{TemplateStaffInvite, map[string]interface{}{"firstName": "Ana", "email": "ana@x.com", "role": "Manager"}, []string{"Welcome to the team, Ana", "ana@x.com", "Manager"}},
		{TemplateEnquiryAcknowledged, map[string]interface{}{"name": "A", "type": "custom_order"}, []string{"custom order request"}},
		{TemplateEnquiryReceived, map[string]interface{}{"name": "A <b>", "email": "a@x.com", "type": "contact", "message": "Hello", "enquiryId": "e1"}, []string{"A &lt;b&gt;", "Hello"}},
	}
	previewDir := os.Getenv("EMAIL_PREVIEW_DIR")
	if previewDir != "" {
		_ = os.MkdirAll(previewDir, 0o755)
	}
	for _, c := range cases {
		out, err := RenderBranded(Brand{Name: "Cakes & Bakes", Currency: "GBP", WebsiteURL: "https://cakesandbakes.co.uk"}, c.alias, c.data)
		if err != nil {
			t.Fatalf("%s: %v", c.alias, err)
		}
		if !strings.Contains(out.HTMLBody, brandPurple) || !strings.Contains(out.HTMLBody, "Cakes &amp; Bakes. All rights reserved") {
			t.Errorf("%s: not on the brand shell", c.alias)
		}
		for _, w := range c.want {
			if !strings.Contains(out.HTMLBody, w) {
				t.Errorf("%s: html missing %q", c.alias, w)
			}
		}
		if strings.Contains(out.HTMLBody, "https://shop.invalid") || strings.Contains(out.HTMLBody+out.Subject+out.TextBody, "Baked Bouquet") || strings.Contains(out.HTMLBody, brandLogo) {
			t.Errorf("%s: placeholder domain leaked", c.alias)
		}
		if previewDir != "" {
			_ = os.WriteFile(previewDir+"/"+c.alias+".html", []byte(out.HTMLBody), 0o644)
		}
	}
}
