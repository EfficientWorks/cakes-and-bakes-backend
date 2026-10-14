package branch

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"bakehouse/services/franchise/franchiseapi"
	"bakehouse/services/franchise/internal/store/shared"
)

func (s *Repo) CreateBranch(ctx context.Context, b franchiseapi.Branch) (*franchiseapi.Branch, error) {
	hours, _ := json.Marshal(b.OpeningHours)
	zone, _ := json.Marshal(b.DeliveryZone)
	pricing, _ := json.Marshal(b.DeliveryPricing)
	var id string
	const q = `
		INSERT INTO branches (name, code, type, ownership_type, street, city, state, postcode, country,
			latitude, longitude, phone, email, timezone, opening_hours, fulfilment_types, delivery_zone,
			delivery_fee_cents, min_order_cents, tax_name, tax_rate_pct,
			accepts_cash_on_delivery, cod_max_order_cents, location_code, delivery_pricing, card_providers,
			pickup_hours, delivery_hours, same_fulfilment_hours, business_id,
			pos_accepts_cash, pos_card_providers, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,'pending')
		RETURNING id`
	if err := s.pool.QueryRow(ctx, q,
		b.Name, b.Code, b.Type, b.OwnershipType, b.Street, b.City, b.State, b.Postcode, b.Country,
		b.Latitude, b.Longitude, b.Phone, b.Email, b.Timezone, hours, b.FulfilmentTypes, zone,
		b.DeliveryFeeCents, b.MinOrderCents, b.TaxName, b.TaxRatePct,
		b.AcceptsCashOnDelivery, b.CODMaxOrderCents,
		coalesceBusinessID(b.LocationCode, b.Code), pricing, cardProvidersOrDefault(b.CardProviders),
		hoursJSON(b.PickupHours), hoursJSON(b.DeliveryHours), b.SameFulfilmentHours, b.BusinessID,
		b.PosAcceptsCash, cardProvidersOrDefault(b.PosCardProviders)).Scan(&id); err != nil {
		return nil, fmt.Errorf("create branch: %w", err)
	}
	if err := s.MapDefaultProductsAndCategories(ctx, id); err != nil {
		return nil, fmt.Errorf("map default products and categories for branch: %w", err)
	}
	return s.GetBranch(ctx, id)
}

// MapDefaultProductsAndCategories creates enabled entries in branch_product_rules
// and branch_category_rules for all active catalog products and categories.
func (s *Repo) MapDefaultProductsAndCategories(ctx context.Context, branchID string) error {
	const mapProducts = `
		INSERT INTO branch_product_rules (branch_id, product_id, branch_enabled, updated_at)
		SELECT $1, id, true, NOW()
		FROM products
		WHERE deleted_at IS NULL
		  AND business_id IS NOT DISTINCT FROM (SELECT business_id FROM branches WHERE id = $1)
		ON CONFLICT (branch_id, product_id)
		DO UPDATE SET branch_enabled = true, updated_at = NOW()`
	if _, err := s.pool.Exec(ctx, mapProducts, branchID); err != nil {
		return fmt.Errorf("map default branch products: %w", err)
	}

	const mapCategories = `
		INSERT INTO branch_category_rules (branch_id, category_id, branch_enabled, updated_at)
		SELECT $1, id, true, NOW()
		FROM product_categories
		WHERE business_id IS NOT DISTINCT FROM (SELECT business_id FROM branches WHERE id = $1)
		ON CONFLICT (branch_id, category_id)
		DO UPDATE SET branch_enabled = true, updated_at = NOW()`
	if _, err := s.pool.Exec(ctx, mapCategories, branchID); err != nil {
		return fmt.Errorf("map default branch categories: %w", err)
	}

	return nil
}

func (s *Repo) UpdateBranch(ctx context.Context, id string, patch franchiseapi.BranchPatch) (*franchiseapi.Branch, error) {
	sets := []string{}
	args := []interface{}{id}
	add := func(col string, val interface{}) {
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}

	if patch.Name != nil {
		add("name", *patch.Name)
	}
	if patch.Code != nil {
		add("code", *patch.Code)
	}
	if patch.Type != nil {
		add("type", *patch.Type)
	}
	if patch.OwnershipType != nil {
		add("ownership_type", *patch.OwnershipType)
	}
	if patch.Street != nil {
		add("street", *patch.Street)
	}
	if patch.City != nil {
		add("city", *patch.City)
	}
	if patch.State != nil {
		add("state", *patch.State)
	}
	if patch.Postcode != nil {
		add("postcode", *patch.Postcode)
	}
	if patch.Country != nil {
		add("country", *patch.Country)
	}
	if patch.Timezone != nil {
		add("timezone", *patch.Timezone)
	}
	if patch.LocationCode != nil {
		add("location_code", shared.NilIfEmpty(*patch.LocationCode))
	}
	if patch.Status != nil {
		add("status", *patch.Status)
	}
	if patch.FulfilmentTypes != nil {
		add("fulfilment_types", *patch.FulfilmentTypes)
	}
	if patch.DeliveryZone != nil {
		zone, _ := json.Marshal(*patch.DeliveryZone)
		add("delivery_zone", zone)
	}
	if patch.DeliveryFeeCents != nil {
		add("delivery_fee_cents", *patch.DeliveryFeeCents)
	}
	if patch.DeliveryPricing != nil {
		pricing, _ := json.Marshal(*patch.DeliveryPricing)
		add("delivery_pricing", pricing)
	}
	if patch.MinOrderCents != nil {
		add("min_order_cents", *patch.MinOrderCents)
	}
	if patch.OpeningHours != nil {
		hours, _ := json.Marshal(*patch.OpeningHours)
		add("opening_hours", hours)
	}
	if patch.Phone != nil {
		// Empty clears it: a manager deleting the number means the outlet has
		// none, not that the field should keep its old value.
		add("phone", shared.NilIfEmpty(*patch.Phone))
	}
	if patch.Email != nil {
		add("email", shared.NilIfEmpty(*patch.Email))
	}
	if patch.TaxName != nil {
		add("tax_name", *patch.TaxName)
	}
	if patch.TaxRatePct != nil {
		add("tax_rate_pct", *patch.TaxRatePct)
	}
	if patch.AcceptsCashOnDelivery != nil {
		add("accepts_cash_on_delivery", *patch.AcceptsCashOnDelivery)
	}
	if patch.CardProviders != nil {
		add("card_providers", *patch.CardProviders)
	}
	if patch.PosAcceptsCash != nil {
		add("pos_accepts_cash", *patch.PosAcceptsCash)
	}
	if patch.PosCardProviders != nil {
		add("pos_card_providers", *patch.PosCardProviders)
	}
	if patch.PickupHours != nil {
		add("pickup_hours", hoursJSON(*patch.PickupHours))
	}
	if patch.DeliveryHours != nil {
		add("delivery_hours", hoursJSON(*patch.DeliveryHours))
	}
	if patch.SameFulfilmentHours != nil {
		add("same_fulfilment_hours", *patch.SameFulfilmentHours)
	}
	if patch.CODMaxOrderCents != nil {
		// A zero here means "no ceiling", which is stored as NULL — an
		// explicit 0 would otherwise read as "COD allowed up to nothing".
		if *patch.CODMaxOrderCents <= 0 {
			add("cod_max_order_cents", nil)
		} else {
			add("cod_max_order_cents", *patch.CODMaxOrderCents)
		}
	}

	if len(sets) == 0 {
		return s.GetBranch(ctx, id)
	}

	q := "UPDATE branches SET " + join(sets, ", ") + ", updated_at = NOW() WHERE id = $1"
	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, shared.ErrNotFound
	}
	return s.GetBranch(ctx, id)
}

func (s *Repo) SetBranchStatus(ctx context.Context, id, status string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE branches SET status = $2, updated_at = NOW() WHERE id = $1`, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.ErrNotFound
	}
	return nil
}

func (s *Repo) SetBranchEmergencyClosed(ctx context.Context, id string, date time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE branches SET emergency_closed_on = $2, updated_at = NOW() WHERE id = $1`, id, date)
	return err
}

// SetBranchCoordinates stores where a branch is, as located from its
// address. Radius delivery measures from here.
func (s *Repo) SetBranchCoordinates(ctx context.Context, id string, lat, lng float64) error {
	_, err := s.pool.Exec(ctx, `UPDATE branches SET latitude = $2, longitude = $3, updated_at = NOW() WHERE id = $1`, id, lat, lng)
	return err
}

func (s *Repo) DeactivateStaffUsersForBranch(ctx context.Context, branchID string) error {
	const q = `
		UPDATE staff_users su
		SET is_active = false, deactivated_at = NOW(), updated_at = NOW()
		WHERE EXISTS (
			SELECT 1
			FROM staff_role_assignments sra
			WHERE sra.staff_user_id = su.id
			  AND sra.scope_type = 'branch'
			  AND sra.branch_id = $1
		)
		AND su.is_active = true`
	_, err := s.pool.Exec(ctx, q, branchID)
	return err
}
