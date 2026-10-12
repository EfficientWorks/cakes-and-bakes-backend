package identityapi

import "context"

// BusinessOwners creates the first login of a newly registered business: a
// super admin over that business. The platform onboarding API uses it.
type BusinessOwners interface {
	CreateBusinessOwner(ctx context.Context, businessID, email, password, firstName, lastName string) (*StaffUser, error)
}
