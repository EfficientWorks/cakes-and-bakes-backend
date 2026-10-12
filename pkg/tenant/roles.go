// Package tenant resolves franchise/branch scope and role-based permissions
// from staff JWT claims (BRD Section 5 governance, Section 6 RBAC). Scope is
// always read from the verified token, never from the URL (RBAC-06, RBAC-08).
package tenant

// Staff roles (BRD Section 4.1, 6.1).
const (
	RoleSuperAdmin      = "super_admin"
	RoleFinance         = "finance"
	RoleFranchiseOwner  = "franchise_owner"
	RoleFranchiseViewer = "franchise_viewer"
	RoleBranchManager   = "branch_manager"
	RoleCounterStaff    = "counter_staff"
	RoleKitchen         = "kitchen"
	// RoleCentralKitchenStaff ("corporate staff" in the console) works the
	// Kitchen Display. Scoped to a store it sees that store's orders; scoped
	// to a central kitchen it sees every store the kitchen produces for. It
	// never signs in to the POS.
	RoleCentralKitchenStaff = "central_kitchen_staff"
)

// Scope types a RoleAssignment can carry.
const (
	ScopeBrand     = "brand"
	ScopeFranchise = "franchise"
	ScopeBranch    = "branch"
	// ScopeCentralKitchen ties a role to a central kitchen (corporate office)
	// rather than a store. Only RoleCentralKitchenStaff may carry it.
	ScopeCentralKitchen = "central_kitchen"
)

// Permissions referenced across handlers. Kept as constants so a typo fails
// to compile rather than silently granting nothing.
const (
	PermOrdersView          = "orders.view"
	PermOrdersAmend         = "orders.amend"
	PermOrdersCancel        = "orders.cancel"
	PermOrdersRefundLimited = "orders.refund_limited"
	PermOrdersRefundFull    = "orders.refund_full"
	PermOrdersCreateManual  = "orders.create_manual"
	PermOrdersExport        = "orders.export"
	PermOrdersReassign      = "orders.reassign"

	PermCatalogueView         = "catalogue.view"
	PermCatalogueEditMaster   = "catalogue.edit_master"
	PermCatalogueAvailability = "catalogue.availability"
	PermCatalogueSoldOut      = "catalogue.sold_out"

	PermReportsOwnBranch  = "reports.own_branch"
	PermReportsNetwork    = "reports.network"
	PermReportsComparable = "reports.comparable"
	PermReportsExport     = "reports.export"

	PermStaffView   = "staff.view"
	PermStaffManage = "staff.manage"

	PermCustomersView      = "customers.view"
	PermCustomersExportPII = "customers.export_pii"

	PermFranchiseManage      = "franchise.manage"
	PermFranchiseSuspend     = "franchise.suspend"
	PermBranchManage         = "branch.manage"
	PermBranchEmergencyClose = "branch.emergency_close"
	// PermBranchOperational governs branch-level fields: opening hours,
	// exception dates (SCH-06, SCH-07).
	PermBranchOperational = "branch.operational"
	// PermBranchCommercial governs franchise-level fields on a branch:
	// delivery zone, delivery fee, minimum order, tax jurisdiction
	// (governance_rules: branch.delivery_zone = franchise level).
	PermBranchCommercial = "branch.commercial"

	// PermCashManage governs the till's cash drawer: the opening float,
	// pay-ins, pay-outs and closing the day. Counter staff hold it because in
	// a small shop the person on the counter is the person who counts the
	// drawer — withholding it would mean nobody present could close the day.
	PermCashManage = "cash.manage"

	PermKitchenViewOrders   = "kitchen.view_orders"
	PermKitchenPrintUpdates = "kitchen.print_updates"
	PermKitchenMarkStatus   = "kitchen.mark_status"

	// PermKDSAccess is signing in to and working the Kitchen Display.
	PermKDSAccess = "kds.access"
	// PermKDSConfigure is changing a location's KDS settings and printers.
	PermKDSConfigure = "kds.configure"

	// PermBranchRequestManage lets staff create, list, and view branch requests.
	// Approve/reject is a separate, narrower permission (PermBranchRequestApprove).
	PermBranchRequestManage = "branch_request.manage"
	// PermBranchRequestApprove lets a super admin approve or reject a request.
	PermBranchRequestApprove = "branch_request.approve"
)

// permAll is the sentinel for "every authenticated staff role".
var permAll = []string{
	RoleSuperAdmin, RoleFinance, RoleFranchiseOwner, RoleFranchiseViewer,
	RoleBranchManager, RoleCounterStaff, RoleKitchen, RoleCentralKitchenStaff,
}

// matrix mirrors BRD Section 6.1 exactly.
var matrix = map[string][]string{
	PermOrdersView:          {RoleSuperAdmin, RoleFinance, RoleFranchiseOwner, RoleFranchiseViewer, RoleBranchManager, RoleCounterStaff},
	PermOrdersAmend:         {RoleSuperAdmin, RoleFranchiseOwner, RoleBranchManager, RoleCounterStaff},
	PermOrdersCancel:        {RoleSuperAdmin, RoleFranchiseOwner, RoleBranchManager, RoleCounterStaff},
	PermOrdersRefundLimited: {RoleSuperAdmin, RoleFranchiseOwner, RoleBranchManager, RoleCounterStaff},
	PermOrdersRefundFull:    {RoleSuperAdmin, RoleFinance, RoleFranchiseOwner, RoleBranchManager},
	PermOrdersCreateManual:  {RoleSuperAdmin, RoleFranchiseOwner, RoleBranchManager, RoleCounterStaff},
	PermOrdersExport:        {RoleSuperAdmin, RoleFinance, RoleFranchiseOwner, RoleBranchManager},
	PermOrdersReassign:      {RoleSuperAdmin, RoleFranchiseOwner},

	PermCatalogueView:         permAll,
	PermCatalogueEditMaster:   {RoleSuperAdmin},
	PermCatalogueAvailability: {RoleSuperAdmin, RoleFranchiseOwner, RoleBranchManager},
	PermCatalogueSoldOut:      {RoleSuperAdmin, RoleFranchiseOwner, RoleBranchManager, RoleCounterStaff},

	PermReportsOwnBranch:  permAll,
	PermReportsNetwork:    {RoleSuperAdmin, RoleFinance},
	PermReportsComparable: {RoleSuperAdmin, RoleFinance},
	PermReportsExport:     {RoleSuperAdmin, RoleFinance, RoleFranchiseOwner, RoleBranchManager},

	PermStaffView:   {RoleSuperAdmin, RoleFranchiseOwner, RoleFranchiseViewer, RoleBranchManager},
	PermStaffManage: {RoleSuperAdmin, RoleFranchiseOwner, RoleBranchManager},

	PermCustomersView:      {RoleSuperAdmin, RoleFranchiseOwner, RoleFranchiseViewer, RoleBranchManager, RoleCounterStaff},
	PermCustomersExportPII: {RoleSuperAdmin, RoleFranchiseOwner},

	PermFranchiseManage:      {RoleSuperAdmin},
	PermFranchiseSuspend:     {RoleSuperAdmin},
	PermBranchManage:         {RoleSuperAdmin, RoleFranchiseOwner},
	PermBranchEmergencyClose: {RoleSuperAdmin, RoleFranchiseOwner, RoleBranchManager},
	PermBranchOperational:    {RoleSuperAdmin, RoleFranchiseOwner, RoleBranchManager},
	PermBranchCommercial:     {RoleSuperAdmin, RoleFranchiseOwner},

	PermCashManage: {RoleSuperAdmin, RoleFranchiseOwner, RoleBranchManager, RoleCounterStaff},

	PermKitchenViewOrders:   {RoleSuperAdmin, RoleFranchiseOwner, RoleBranchManager, RoleCounterStaff, RoleKitchen, RoleCentralKitchenStaff},
	PermKitchenPrintUpdates: {RoleSuperAdmin, RoleFranchiseOwner, RoleBranchManager, RoleCounterStaff, RoleKitchen, RoleCentralKitchenStaff},
	PermKitchenMarkStatus:   permAll,

	PermKDSAccess:    {RoleSuperAdmin, RoleCentralKitchenStaff},
	PermKDSConfigure: {RoleSuperAdmin, RoleFranchiseOwner, RoleBranchManager, RoleCentralKitchenStaff},

	PermBranchRequestManage:  {RoleSuperAdmin, RoleFranchiseOwner, RoleBranchManager},
	PermBranchRequestApprove: {RoleSuperAdmin},
}

func allowedRoles(permission string) []string {
	return matrix[permission]
}
