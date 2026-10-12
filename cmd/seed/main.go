// Command seed bootstraps a fresh development database with the minimum
// data needed to exercise the API: a brand-scoped super admin, one
// franchise, and one branch. Without a branch, most of the storefront and
// every branch-scoped admin route has nothing to point at, so the Swagger
// UI at /docs and the Postman collection are both unusable.
//
// It is idempotent — running it twice reports what already exists rather
// than failing or duplicating.
//
// Outside APP_ENV=local it refuses to run unless SEED_ALLOW_NONLOCAL=true and
// SEED_ADMIN_PASSWORD are both set, so bootstrapping a real environment is
// always deliberate and never uses a credential committed to this repository.
package main

import (
	"context"
	"fmt"
	"os"

	"bakehouse/pkg/auth"
	"bakehouse/pkg/config"
	"bakehouse/pkg/db"
	"bakehouse/pkg/obs"
	"bakehouse/pkg/tenant"
	"bakehouse/services/franchise"
	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/identity"
)

const (
	seedEmail     = "admin@example.com"
	seedPassword  = "ChangeMe123!"
	seedFirstName = "Store"
	seedLastName  = "Admin"

	seedBranchName = "London Central"
	seedBranchCode = "LDN-001"
	seedBranchZone = "Europe/London"
)

func main() {
	log := obs.NewLogger()
	cfg := config.Load()
	ctx := context.Background()

	// Outside local, seeding writes real data into a real database, so it has
	// to be asked for twice: once to unlock it, and once to supply a password
	// this repository has never seen. There is deliberately no default
	// credential outside local.
	password := seedPassword
	if cfg.Env != config.EnvLocal {
		if os.Getenv("SEED_ALLOW_NONLOCAL") != "true" {
			fmt.Fprintf(os.Stderr,
				"refusing to seed: APP_ENV is %q. Set SEED_ALLOW_NONLOCAL=true and SEED_ADMIN_PASSWORD to proceed.\n",
				cfg.Env)
			os.Exit(1)
		}
		password = os.Getenv("SEED_ADMIN_PASSWORD")
		if len(password) < 12 {
			fmt.Fprintln(os.Stderr,
				"refusing to seed: SEED_ADMIN_PASSWORD must be set to at least 12 characters outside local.")
			os.Exit(1)
		}
		fmt.Printf("seeding %s environment — this writes to a live database\n", cfg.Env)
	}

	if err := db.Migrate(cfg.DatabaseURL, "db/migrations"); err != nil {
		fail("migrate", err)
	}

	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		fail("connect", err)
	}
	defer pool.Close()

	// Seeding goes through the same module seams the server uses. Nothing
	// here reaches into a service's internal/ tree.
	identitySvc := identity.New(pool, nil, nil, nil, nil, identity.TokenTTLs{})
	franchiseSvc := franchise.New(pool, nil, nil)

	// Outside local the seeded branch is a test fixture sitting
	// in a real database, so they are named as such. Nobody should have to read
	// this file to work out which rows are safe to delete.
	branchName := seedBranchName
	if cfg.Env != config.EnvLocal {
		branchName = "[TEST] " + branchName
	}

	staffID, created, err := identitySvc.SeedSuperAdmin(ctx, seedEmail, password, seedFirstName, seedLastName)
	if err != nil {
		fail("create super admin", err)
	}
	if created {
		fmt.Println("created super admin:")
		fmt.Println("  id      :", staffID)
		fmt.Println("  email   :", seedEmail)
		fmt.Println("  password:", password)
	} else {
		fmt.Printf("super admin already exists: %s (%s)\n", seedEmail, staffID)
	}

	// Every franchise/branch write re-authorises against the caller's
	// claims, so the seeder acts as the super admin it just created rather
	// than bypassing the permission check.
	claims := &auth.StaffClaims{
		StaffID: staffID,
		Email:   seedEmail,
		RoleAssignments: []auth.RoleAssignment{
			{Role: tenant.RoleSuperAdmin, ScopeType: tenant.ScopeBrand},
		},
	}

	branchID, err := seedBranch(ctx, franchiseSvc, claims, branchName)
	if err != nil {
		fail("seed branch", err)
	}

	fmt.Println("branch   :", branchID, "("+seedBranchCode+")")
	log.Info("seed complete")
}

// seedBranch returns the id of the dev branch, creating it only if it does
// not already exist.
func seedBranch(ctx context.Context, svc *franchise.Module, claims *auth.StaffClaims, name string) (string, error) {
	// Looked up across every franchise, not just this one.
	//
	// branches.code is globally unique, so a branch carrying the seed code
	// under some other franchise still blocks the insert. Scoping the search
	// to franchiseID meant the seeder could not see the very row that was
	// about to reject it, and aborted with a raw constraint violation.
	existing, err := svc.ListBranches(ctx, claims)
	if err != nil {
		return "", err
	}
	for _, b := range existing {
		if b.Code == seedBranchCode {
			return b.ID, nil
		}
	}

	b, warnings, err := svc.CreateBranch(ctx, claims, franchiseapi.Branch{
		Name:     name,
		Code:     seedBranchCode,
		Type:     franchiseapi.BranchTypeRetail,
		Street:   "221B Baker Street",
		City:     "London",
		State:    "Greater London",
		Postcode: "NW1 6XE",
		Country:  "GB",
		Timezone: seedBranchZone,
		OpeningHours: []franchiseapi.OpeningHour{
			{Day: "monday", Open: str("09:00"), Close: str("18:00")},
			{Day: "tuesday", Open: str("09:00"), Close: str("18:00")},
			{Day: "wednesday", Open: str("09:00"), Close: str("18:00")},
			{Day: "thursday", Open: str("09:00"), Close: str("18:00")},
			{Day: "friday", Open: str("09:00"), Close: str("20:00")},
			{Day: "saturday", Open: str("10:00"), Close: str("20:00")},
			{Day: "sunday", Open: nil, Close: nil},
		},
		FulfilmentTypes: []string{"pickup", "delivery"},
		DeliveryZone: franchiseapi.DeliveryZone{
			Type: "zip_list",
			Zips: []string{"NW1", "W1", "NW8"},
		},
		DeliveryFeeCents: 499,
		MinOrderCents:    2000,
		Status:           franchiseapi.StatusActive,
	})
	if err != nil {
		return "", err
	}
	for _, w := range warnings {
		fmt.Println("  warning:", w)
	}
	return b.ID, nil
}

func str(s string) *string { return &s }

func fail(what string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", what, err)
	os.Exit(1)
}
