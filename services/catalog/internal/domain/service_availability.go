package domain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"bakehouse/pkg/apperr"
	"bakehouse/pkg/auth"
	"bakehouse/pkg/strx"
	"bakehouse/pkg/tenant"
	"bakehouse/services/catalog/catalogapi"
	"bakehouse/services/franchise/franchiseapi"
)

// --- Branch availability control (CAT-05 to CAT-10) ---------------------------

// AvailabilityGridRow is one line of the admin availability grid — products
// as rows, day-of-week columns, plus today's resolved status.
type AvailabilityGridRow struct {
	ProductID      string
	ProductName    string
	BranchEnabled  bool
	AvailableDays  []string
	TodayAvailable bool
	SeasonalFrom   *string
	SeasonalUntil  *string
	PriceOverride  *int64
}

func (s *Service) GetAvailabilityGrid(ctx context.Context, claims *auth.StaffClaims, branchID string, date time.Time) ([]AvailabilityGridRow, error) {
	branch, err := s.checkBranchCatalogueAccess(ctx, claims, branchID, tenant.PermCatalogueView)
	if err != nil {
		return nil, err
	}
	products, err := s.store.ListProducts(ctx, "", "", false)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	rules, err := s.store.ListBranchProductRules(ctx, branchID)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	localDate := toLocalDate(date, branch.Timezone)

	rows := make([]AvailabilityGridRow, 0, len(products))
	for _, p := range products {
		rule, ok := rules[p.ID]
		if !ok {
			rule = catalogapi.BranchProductRule{BranchEnabled: false, AvailableDays: catalogapi.DefaultAvailableDays}
		}
		avail := resolveAvailability(p, rule, localDate)
		var from, until *string
		if rule.AvailableFrom != nil {
			v := rule.AvailableFrom.Format("2006-01-02")
			from = &v
		}
		if rule.AvailableUntil != nil {
			v := rule.AvailableUntil.Format("2006-01-02")
			until = &v
		}
		rows = append(rows, AvailabilityGridRow{
			ProductID: p.ID, ProductName: p.Name, BranchEnabled: rule.BranchEnabled,
			AvailableDays: rule.AvailableDays, TodayAvailable: avail.Available,
			SeasonalFrom: from, SeasonalUntil: until, PriceOverride: rule.PriceOverrideCents,
		})
	}
	return rows, nil
}

// AvailabilityPatch is the set of fields an admin may change in one call
// (PATCH .../availability).
type AvailabilityPatch struct {
	BranchEnabled      *bool
	Reason             string
	AvailableDays      []string
	PriceOverrideCents *int64
	AvailableFrom      *time.Time
	AvailableUntil     *time.Time
}

func (s *Service) UpdateAvailability(ctx context.Context, claims *auth.StaffClaims, branchID, productID string, patch AvailabilityPatch) (*catalogapi.BranchProductRule, error) {
	if _, err := s.checkBranchCatalogueAccess(ctx, claims, branchID, tenant.PermCatalogueAvailability); err != nil {
		return nil, err
	}

	if patch.PriceOverrideCents != nil {
		product, err := s.store.GetProductByID(ctx, productID)
		if err != nil {
			return nil, notFoundOrInternal(err, "Product not found.")
		}
		if err := checkPriceBand(product, *patch.PriceOverrideCents); err != nil {
			return nil, err
		}
	}

	before, _ := s.store.GetBranchProductRule(ctx, branchID, productID)
	out, err := s.store.UpsertBranchProductRulePatch(ctx, branchID, productID, func(r *catalogapi.BranchProductRule) {
		if patch.BranchEnabled != nil {
			r.BranchEnabled = *patch.BranchEnabled
		}
		if patch.AvailableDays != nil {
			r.AvailableDays = patch.AvailableDays
		}
		if patch.PriceOverrideCents != nil {
			r.PriceOverrideCents = patch.PriceOverrideCents
		}
		if patch.AvailableFrom != nil {
			r.AvailableFrom = patch.AvailableFrom
		}
		if patch.AvailableUntil != nil {
			r.AvailableUntil = patch.AvailableUntil
		}
	}, claims.StaffID, patch.Reason)
	if err != nil {
		return nil, apperr.Internal(err)
	}

	s.writeAudit(ctx, claims, "catalogue.availability.update", "branch_product_rule", &out.ID, &branchID, before, out)
	return out, nil
}

// checkPriceBand enforces GOV-10: a franchise price override outside the
// brand-defined floor/ceiling is rejected with an explanatory message.
func checkPriceBand(p *catalogapi.Product, priceCents int64) error {
	if p.PriceFloorCents != nil && priceCents < *p.PriceFloorCents {
		return apperr.New("PRICE_BELOW_FLOOR", apperr.CategoryBusinessRule,
			fmt.Sprintf("Price cannot be set below the brand floor of $%.2f for this product.", float64(*p.PriceFloorCents)/100))
	}
	if p.PriceCeilingCents != nil && priceCents > *p.PriceCeilingCents {
		return apperr.New("PRICE_ABOVE_CEILING", apperr.CategoryBusinessRule,
			fmt.Sprintf("Price cannot be set above the brand ceiling of $%.2f for this product.", float64(*p.PriceCeilingCents)/100))
	}
	return nil
}

func (s *Service) AddDateOverride(ctx context.Context, claims *auth.StaffClaims, branchID, productID string, override catalogapi.DateOverride) (*catalogapi.BranchProductRule, error) {
	if _, err := s.checkBranchCatalogueAccess(ctx, claims, branchID, tenant.PermCatalogueAvailability); err != nil {
		return nil, err
	}
	before, _ := s.store.GetBranchProductRule(ctx, branchID, productID)
	out, err := s.store.UpsertBranchProductRulePatch(ctx, branchID, productID, func(r *catalogapi.BranchProductRule) {
		replaced := false
		for i, o := range r.DateOverrides {
			if o.Date == override.Date {
				r.DateOverrides[i] = override
				replaced = true
				break
			}
		}
		if !replaced {
			r.DateOverrides = append(r.DateOverrides, override)
		}
	}, claims.StaffID, override.Reason)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "catalogue.date_override.create", "branch_product_rule", &out.ID, &branchID, before, out)
	return out, nil
}

func (s *Service) BulkDisable(ctx context.Context, claims *auth.StaffClaims, branchID string, productIDs []string, reason string) (int, error) {
	if _, err := s.checkBranchCatalogueAccess(ctx, claims, branchID, tenant.PermCatalogueAvailability); err != nil {
		return 0, err
	}
	if strings.TrimSpace(reason) == "" {
		return 0, apperr.Validation("A reason is required to bulk-disable items.")
	}
	count, err := s.store.BulkDisable(ctx, branchID, productIDs, reason, claims.StaffID)
	if err != nil {
		return 0, apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "catalogue.bulk_disable", "branch", &branchID, &branchID, nil, map[string]interface{}{"productIds": productIDs, "reason": reason})
	return count, nil
}

// SetSoldOut is the one-tap sold-out control (CAT-05, ADM-14): a date
// override for today, no navigation into settings required.
func (s *Service) SetSoldOut(ctx context.Context, claims *auth.StaffClaims, branchID, productID, reason string) error {
	branch, err := s.checkBranchCatalogueAccess(ctx, claims, branchID, tenant.PermCatalogueSoldOut)
	if err != nil {
		return err
	}
	today := toLocalDate(time.Now(), branch.Timezone).Format("2006-01-02")
	_, err = s.AddDateOverride(ctx, claims, branchID, productID, catalogapi.DateOverride{Date: today, Available: false, Reason: strx.OrDefault(reason, "Sold out for today")})
	return err
}

func (s *Service) RestoreToday(ctx context.Context, claims *auth.StaffClaims, branchID, productID string) error {
	branch, err := s.checkBranchCatalogueAccess(ctx, claims, branchID, tenant.PermCatalogueSoldOut)
	if err != nil {
		return err
	}
	today := toLocalDate(time.Now(), branch.Timezone).Format("2006-01-02")
	before, _ := s.store.GetBranchProductRule(ctx, branchID, productID)
	out, err := s.store.UpsertBranchProductRulePatch(ctx, branchID, productID, func(r *catalogapi.BranchProductRule) {
		filtered := r.DateOverrides[:0]
		for _, o := range r.DateOverrides {
			if o.Date != today {
				filtered = append(filtered, o)
			}
		}
		r.DateOverrides = filtered
	}, claims.StaffID, "")
	if err != nil {
		return apperr.Internal(err)
	}
	s.writeAudit(ctx, claims, "catalogue.restore", "branch_product_rule", &out.ID, &branchID, before, out)
	return nil
}

// checkBranchCatalogueAccess resolves the branch (for timezone + franchise)
// and enforces both the permission and the branch scope in one place.
func (s *Service) checkBranchCatalogueAccess(ctx context.Context, claims *auth.StaffClaims, branchID, permission string) (*franchiseapi.Branch, error) {
	if !tenant.HasPermission(claims, permission) {
		return nil, apperr.PermissionDenied("You do not have permission to manage this branch's catalogue.")
	}
	branch, err := s.branches.GetBranch(ctx, branchID)
	if err != nil {
		return nil, apperr.NotFound("Branch not found.")
	}
	if !tenant.CanAccessBranch(claims, "", branch.ID) {
		return nil, apperr.PermissionDenied("You do not have access to this branch.")
	}
	return branch, nil
}
