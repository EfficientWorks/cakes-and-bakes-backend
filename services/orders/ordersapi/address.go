package ordersapi

// AddressResult is one match returned by the address finder.
type AddressResult struct {
	PlaceID     string `json:"placeId"`
	DisplayName string `json:"displayName"`
	Street      string `json:"street"`
	City        string `json:"city"`
	State       string `json:"state"`
	Postcode    string `json:"postcode"`
	Country     string `json:"country"`
	Latitude    string `json:"latitude"`
	Longitude   string `json:"longitude"`
}
