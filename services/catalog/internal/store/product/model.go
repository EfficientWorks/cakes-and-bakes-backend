package product

// ListProducts returns lightweight product summaries (no modifier groups —
// those load on the detail page) for the storefront grid and admin list.
// categoryID and occasion filter when non-empty; includeInactive is used by
// the admin listing only.
// AdminProductQuery is one page of the console's product list.
//
// Search and category are applied in SQL rather than in the browser because
// the list is paged server-side: filtering after the page has been cut would
// hand back a short page and a total that disagrees with it.
type AdminProductQuery struct {
	CategoryID    string
	Search        string
	ShowOnPos     *bool
	ShowOnWebsite *bool
	// BranchIDs, when non-nil, keeps only products mapped to at least one of
	// these branches: an enabled branch_product_rules row, in a category that
	// branch has enabled too. A branch login passes its own branch; a
	// franchise owner the franchise's branches. nil is the master catalogue.
	BranchIDs []string
	Limit     int
	Offset    int
}
