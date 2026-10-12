package franchiseapi

import "time"

const (
	ReceiptTypeReceipt = "receipt"
	ReceiptTypeKOT     = "kot"
	ReceiptTypeCancel  = "cancel"
)

type PrintColumn struct {
	ColumnName string `json:"column_name"`
	Order      string `json:"order"`
}

// PrintReceiptTemplate is the print_receipt_templates row for one outlet.
type PrintReceiptTemplate struct {
	ID                     string        `json:"id"`
	BranchID               string        `json:"branchId"`
	IsActive               bool          `json:"status"`
	ShowCustomerName       bool          `json:"showCustomerName"`
	ShowOrderNotes         bool          `json:"showOrderNotes"`
	ShowProductComments    bool          `json:"showProductComments"`
	ShowAddonPrice         bool          `json:"showAddonPrice"`
	IncludeCustomerReceipt bool          `json:"includeCustomerReceipt"`
	ColumnAlignment        []PrintColumn `json:"columnAlignment"`
	DateTimeFormat         string        `json:"dateTimeFormat"` // "12h" | "24h"
	BusinessName           string        `json:"businessName"`
	MobileNo               string        `json:"mobileNo"`
	Address                string        `json:"address"`
	GSTVATNumber           string        `json:"gstVatNumber"`
	BusinessLogo           bool          `json:"businessLogo"`
	ShowTime               bool          `json:"showTime"`
	TextWeight             string        `json:"textWeight"`
	CreatedAt              time.Time     `json:"createdAt"`
	UpdatedAt              time.Time     `json:"updatedAt"`
}

// PrintSimpleTemplate is the shape shared by print_kot_templates and
// print_cancel_templates — identical columns, two different tables.
// ReceiptType says which one a given instance came from.
type PrintSimpleTemplate struct {
	ID          string    `json:"id"`
	BranchID    string    `json:"branchId"`
	ReceiptType string    `json:"receiptType"` // "kot" | "cancel"
	IsActive    bool      `json:"status"`
	ShowTime    bool      `json:"showTime"`
	TextWeight  string    `json:"textWeight"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// PrintTemplateFieldUpdate is one field/value pair from
// PUT /pos/print-templates/{receiptType}. Value is left as interface{} since
// its real type (bool/string/[]PrintColumn) depends on which field it names
// — printTemplateFieldRegistry in the domain package is what checks that.
type PrintTemplateFieldUpdate struct {
	Field string      `json:"field"`
	Value interface{} `json:"value"`
}
