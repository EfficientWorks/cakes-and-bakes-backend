package adminapi

import "time"

// The two things an invoice can bill.
const (
	// InvoiceCustomer bills a person for goods from one of our outlets.
	InvoiceCustomer = "customer"
	// InvoiceStore bills one outlet for what another supplied it — how a
	// central kitchen charges a franchise.
	InvoiceStore = "store"
)

// Where an invoice is in its life. Void is kept rather than deleting, because
// a reference that has been sent to somebody has to keep resolving.
const (
	InvoiceUnpaid = "unpaid"
	InvoicePaid   = "paid"
	InvoiceVoid   = "void"
)

// InvoiceParty is whoever appears in a "Billed to" or "Store" block.
//
// One shape for both, because the document draws them the same way and the
// difference — a person or an outlet — is already carried by the invoice's
// kind. Flattened rather than nested so a renamed customer or a relocated
// outlet does not rewrite an invoice that has already been sent.
type InvoiceParty struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`
	Street   string `json:"street,omitempty"`
	City     string `json:"city,omitempty"`
	State    string `json:"state,omitempty"`
	Postcode string `json:"postcode,omitempty"`
}

// InvoiceItemModifier is one chosen option, as the document prints it.
type InvoiceItemModifier struct {
	Name       string `json:"name"`
	GroupName  string `json:"groupName,omitempty"`
	PriceDelta int64  `json:"priceDelta,omitempty"`
}

// InvoiceItem is one line.
type InvoiceItem struct {
	ID             string                `json:"id,omitempty"`
	ProductID      string                `json:"productId,omitempty"`
	ProductName    string                `json:"productName"`
	VariantName    string                `json:"variantName,omitempty"`
	Modifiers      []InvoiceItemModifier `json:"modifiers,omitempty"`
	Quantity       int                   `json:"quantity"`
	UnitPriceCents int64                 `json:"unitPriceCents"`
	LineTotalCents int64                 `json:"lineTotalCents"`
	Disclaimer     string                `json:"disclaimer"`
}

// Invoice is the document.
type Invoice struct {
	ID        string `json:"id"`
	Reference string `json:"reference"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`

	// The outlet supplying, whose tax applies and whose details head the
	// document. Named "store" throughout the console.
	Store InvoiceParty `json:"store"`
	// Whoever is being billed — a customer or another outlet.
	// BilledTo InvoiceParty `json:"billedTo"`

	OrderID        string `json:"orderId,omitempty"`
	OrderReference string `json:"orderReference,omitempty"`

	IssuedDate string `json:"issuedDate"`
	Message    string `json:"message,omitempty"`

	BankName      string `json:"bankName,omitempty"`
	AccountNumber string `json:"accountNumber,omitempty"`
	SortCode      string `json:"sortCode,omitempty"`

	Items []InvoiceItem `json:"items"`

	SubtotalCents int64    `json:"subtotalCents"`
	DiscountCents int64    `json:"discountCents"`
	DiscountPct   *float64 `json:"discountPct,omitempty"`
	TaxCents      int64    `json:"taxCents"`
	TaxName       string   `json:"taxName"`
	TotalCents    int64    `json:"totalCents"`

	PaidAt    *time.Time `json:"paidAt,omitempty"`
	SentAt    *time.Time `json:"sentAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`

	IsGift bool `json:"isGift,omitempty"`

	BilledTo InvoiceCustomerInput  `json:"billedTo,omitempty"`
	Receiver InvoiceRecipientInput `json:"receiver,omitempty"`

	CusomerFirstName string `json:"customerFirstName,omitempty"`
	CustomerLastName string `json:"customerLastName,omitempty"`
	CustomerEmail    string `json:"customerEmail,omitempty"`
	CustomerPhone    string `json:"customerPhone,omitempty"`
	CustomerAddress  string `json:"customerAddress,omitempty"`
	CustomerCity     string `json:"customerCity,omitempty"`
	CustomerState    string `json:"customerState,omitempty"`

	ReceipiantFirstName string `json:"receipiantFirstName,omitempty"`
	ReceipiantLastName  string `json:"receipiantLastName,omitempty"`
	ReceipiantEmail     string `json:"receipiantEmail,omitempty"`
	ReceipiantPhone     string `json:"receipiantPhone,omitempty"`
	ReceipiantAddress   string `json:"receipiantAddress,omitempty"`
	ReceipiantCity      string `json:"receipiantCity,omitempty"`
	ReceipiantState     string `json:"receipiantState,omitempty"`

	Disclaimer *string `json:"disclaimer"`
}

// InvoiceListFilter narrows the listing. Everything is optional; an empty
// filter is "every invoice this account may see".
type InvoiceListFilter struct {
	// Matches the reference, the billed party's name, or its email.
	Search string
	// "customer" or "store". Empty means both.
	Kind string
	// The supplying outlet.
	BranchID string
	Status   string
	Page     int
	Limit    int
}

// InvoiceItemInput is one line as the console sends it.
//
// The price comes from the console rather than being looked up, for the same
// reason the till's does: the operator is looking at a figure on screen and
// that figure is what is being agreed. Re-deriving it here would only make it
// possible for the preview and the sent document to disagree.
type InvoiceItemInput struct {
	ProductID      string                `json:"productId"`
	ProductName    string                `json:"productName"`
	VariantName    string                `json:"variantName"`
	Modifiers      []InvoiceItemModifier `json:"modifiers"`
	Quantity       int                   `json:"quantity"`
	UnitPriceCents int64                 `json:"unitPriceCents"`
}

type InvoiceCustomerInput struct {
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
	Phone     string `json:"phone,omitempty"`
	Email     string `json:"email,omitempty"`
	Address   string `json:"address,omitempty"`
	City      string `json:"city,omitempty"`
	State     string `json:"state,omitempty"`
}

type InvoiceRecipientInput struct {
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
	Phone     string `json:"phone,omitempty"`
	Email     string `json:"email,omitempty"`
	Address   string `json:"address,omitempty"`
	City      string `json:"city,omitempty"`
	State     string `json:"state,omitempty"`
}

// CreateInvoiceInput is a new invoice.
type CreateInvoiceInput struct {
	Kind     string `json:"kind"`
	BranchID string `json:"branchId"`
	// Required when kind is "customer".
	CustomerID string `json:"customerId,omitempty"`
	// Required when kind is "store" — the outlet being billed.
	BilledBranchID string `json:"billedBranchId,omitempty"`

	// OrderID    string `json:"orderId,omitempty"`
	IssuedDate string `json:"issuedDate"`
	Message    string `json:"message,omitempty"`

	BankName      string `json:"bankName,omitempty"`
	AccountNumber string `json:"accountNumber,omitempty"`
	SortCode      string `json:"sortCode,omitempty"`

	Items []InvoiceItemInput `json:"items"`

	// A percentage or a flat amount, never both. The server works out the
	// other one and stores it, so the document and the arithmetic agree.
	DiscountPct   *float64 `json:"discountPct,omitempty"`
	DiscountCents int64    `json:"discountCents,omitempty"`

	// Send it to the billed party once it is created.
	Send bool `json:"send,omitempty"`

	// InvoiceNumber *string `json:"invoiceNumber,omitempty"`
	IsGift bool `json:"isGift,omitempty"`

	BilledTo InvoiceCustomerInput  `json:"billedTo,omitempty"`
	Receiver InvoiceRecipientInput `json:"receiver,omitempty"`
}
