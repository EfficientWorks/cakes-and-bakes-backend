package catalogapi

// SelectedModifier is a resolved, priced modifier choice — the shape the
// ordering module snapshots onto a cart/order line so history survives a
// later catalogue edit.
type SelectedModifier struct {
	ID         string
	Name       string
	PriceDelta int64
}

// PricedItem is a fully validated, priced cart-line: every selected id is
// confirmed to exist, be active, and belong to the right product/group.
type PricedItem struct {
	ProductID       string
	ProductName     string
	ProductImageURL *string
	VariantID       *string
	VariantName     *string
	UnitPriceCents  int64 // base (branch-overridden) + variant delta + all modifier deltas
	PricingMode     string
	Available       bool
	AvailableReason string

	SelectedFlavours     []SelectedModifier
	SelectedAddons       []SelectedModifier
	SelectedPresentation *SelectedModifier
	SelectedWrapColour   *SelectedModifier

	// Cake is the product's message-on-cake and photo settings, which a cart
	// line's cakeMessage and photoUrl are validated against.
	Cake CakeOptions
}
