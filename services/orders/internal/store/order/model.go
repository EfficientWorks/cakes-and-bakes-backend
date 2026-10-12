package order

// CustomerDetails holds resolved customer info for order DTOs.
type CustomerDetails struct {
	FirstName       *string
	LastName        *string
	Email           *string
	MobileNumber    *string
	DOB             *string
	AnniversaryDate *string
	Address         *string
	Street          *string
	City            *string
	State           *string
	Postcode        *string
}
