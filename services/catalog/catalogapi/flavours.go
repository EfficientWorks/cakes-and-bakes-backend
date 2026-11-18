package catalogapi

import "time"

/*
 * A flavour is a home-page card ("Discover our flavours"): a name, a short
 * description, a photo and a tint, with products mapped to it. The website
 * shows the active flavours that have an active product; "Shop now" lists
 * products with ?flavour=<slug>.
 */

const (
	FlavourActive   = "active"
	FlavourInactive = "inactive"
)

type Flavour struct {
	ID               string
	Name             string
	Slug             string
	Description      *string
	ImageURL         *string
	ImageAltText     *string
	BackgroundColour *string // "#RRGGBB"
	SortOrder        int
	Status           string
	// ProductIDs are the mapped products (admin reads only).
	ProductIDs []string
	// ProductCount counts mapped products that are not deleted; on the
	// storefront list, only active ones.
	ProductCount int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
