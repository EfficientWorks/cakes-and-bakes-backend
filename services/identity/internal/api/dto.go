package api

import "bakehouse/services/identity/identityapi"

type customerDTOType struct {
	ID             string  `json:"id"`
	Email          string  `json:"email"`
	FirstName      string  `json:"firstName"`
	LastName       string  `json:"lastName"`
	MobileNumber   *string `json:"mobileNumber"`
	IsVerified     bool    `json:"isVerified"`
	MarketingOptIn bool    `json:"marketingOptIn"`
	CreatedAt      string  `json:"createdAt"`
}

func customerDTO(c *identityapi.Customer) customerDTOType {
	return customerDTOType{
		ID: c.ID, Email: c.Email, FirstName: c.FirstName, LastName: c.LastName,
		MobileNumber: c.MobileNumber, IsVerified: c.IsVerified, MarketingOptIn: c.MarketingOptIn,
		CreatedAt: c.CreatedAt.Format(rfc3339),
	}
}

type staffRoleSummaryDTOType struct {
	ID        string  `json:"id"`
	Role      string  `json:"role"`
	ScopeType string  `json:"scopeType"`
	BranchID  *string `json:"branchId"`
	// KDS: present only on a central_kitchen scope.
	CentralKitchenID *string `json:"centralKitchenId,omitempty"`
}

type staffDirectoryDTOType struct {
	ID          string                    `json:"id"`
	Email       string                    `json:"email"`
	FirstName   string                    `json:"firstName"`
	LastName    string                    `json:"lastName"`
	IsActive    bool                      `json:"isActive"`
	CreatedAt   string                    `json:"createdAt"`
	LastLoginAt *string                   `json:"lastLoginAt"`
	Roles       []staffRoleSummaryDTOType `json:"roles"`
	HasPasscode bool                      `json:"hasPasscode"`
	// The till passcode, only for staff managers and only when it was set
	// after codes started being kept recoverable.
	Passcode *string `json:"passcode,omitempty"`
}

func staffDirectoryDTO(e *identityapi.StaffDirectoryEntry) staffDirectoryDTOType {
	roles := make([]staffRoleSummaryDTOType, 0, len(e.Roles))
	for _, r := range e.Roles {
		roles = append(roles, staffRoleSummaryDTOType{
			ID: r.ID, Role: r.Role, ScopeType: r.ScopeType, BranchID: r.BranchID,
			CentralKitchenID: r.CentralKitchenID,
		})
	}
	var lastLogin *string
	if e.LastLoginAt != nil {
		f := e.LastLoginAt.Format(rfc3339)
		lastLogin = &f
	}
	return staffDirectoryDTOType{
		ID: e.ID, Email: e.Email, FirstName: e.FirstName, LastName: e.LastName,
		IsActive: e.IsActive, CreatedAt: e.CreatedAt.Format(rfc3339), LastLoginAt: lastLogin, Roles: roles,
		HasPasscode: e.HasPasscode,
	}
}

type addressDTOType struct {
	ID        string  `json:"id"`
	Label     *string `json:"label"`
	Street    string  `json:"street"`
	City      string  `json:"city"`
	State     string  `json:"state"`
	Postcode  string  `json:"postcode"`
	Country   string  `json:"country"`
	IsDefault bool    `json:"isDefault"`
}

func addressDTO(a *identityapi.CustomerAddress) addressDTOType {
	return addressDTOType{
		ID: a.ID, Label: a.Label, Street: a.Street, City: a.City, State: a.State,
		Postcode: a.Postcode, Country: a.Country, IsDefault: a.IsDefault,
	}
}

func addressFromRequest(req addressRequest) identityapi.CustomerAddress {
	var label *string
	if req.Label != "" {
		label = &req.Label
	}
	return identityapi.CustomerAddress{Label: label, Street: req.Street, City: req.City, State: req.State, Postcode: req.Postcode, Country: req.Country}
}

type recipientDTOType struct {
	ID            string                     `json:"id"`
	FirstName     string                     `json:"firstName"`
	LastName      string                     `json:"lastName"`
	Phone         string                     `json:"phone"`
	Relationship  *string                    `json:"relationship"`
	OccasionDates []identityapi.OccasionDate `json:"occasionDates"`
}

func recipientDTO(r *identityapi.CustomerRecipient) recipientDTOType {
	return recipientDTOType{
		ID: r.ID, FirstName: r.FirstName, LastName: r.LastName, Phone: r.Phone,
		Relationship: r.Relationship, OccasionDates: r.OccasionDates,
	}
}

func recipientFromRequest(req recipientRequest) identityapi.CustomerRecipient {
	var rel *string
	if req.Relationship != "" {
		rel = &req.Relationship
	}
	occasions := req.OccasionDates
	if occasions == nil {
		occasions = []identityapi.OccasionDate{}
	}
	return identityapi.CustomerRecipient{FirstName: req.FirstName, LastName: req.LastName, Phone: req.Phone, Relationship: rel, OccasionDates: occasions}
}

type roleAssignmentDTOType struct {
	Role      string  `json:"role"`
	ScopeType string  `json:"scopeType"`
	BranchID  *string `json:"branchId,omitempty"`
	// KDS: present only on a central_kitchen scope.
	CentralKitchenID *string `json:"centralKitchenId,omitempty"`
}

type staffDTOType struct {
	ID    string                  `json:"id"`
	Email string                  `json:"email"`
	Name  string                  `json:"name"`
	Roles []roleAssignmentDTOType `json:"roles"`
}

func staffDTO(u *identityapi.StaffUser, roles []identityapi.StaffRoleAssignment) staffDTOType {
	out := staffDTOType{ID: u.ID, Email: u.Email, Name: u.FirstName + " " + u.LastName}
	for _, ra := range roles {
		out.Roles = append(out.Roles, roleAssignmentDTOType{
			Role: ra.Role, ScopeType: ra.ScopeType, BranchID: ra.BranchID,
			CentralKitchenID: ra.CentralKitchenID,
		})
	}
	return out
}

const rfc3339 = "2006-01-02T15:04:05Z07:00"
