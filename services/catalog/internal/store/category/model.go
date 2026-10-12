package category

import "bakehouse/services/catalog/catalogapi"

// CategoryRow pairs a category with the scheduling window the admin owns.
// The storefront read path never needs the window, so it stays off
// catalogapi.Category rather than widening a type two services share.
type CategoryRow struct {
	Category  catalogapi.Category
	Schedule  catalogapi.CategoryWrite
	BranchIds []string `json:"storeIds"`
}
