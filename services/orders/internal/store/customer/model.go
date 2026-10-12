package customer

// CustomerContact is the name and contact details of a customer record.
type CustomerContact struct {
	FirstName string
	LastName  string
	Email     string
	Phone     *string
}
