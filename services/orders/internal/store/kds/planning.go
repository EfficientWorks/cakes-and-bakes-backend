package kds

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"bakehouse/services/orders/ordersapi"
)

// KDSSlotCounts counts orders per scheduled date and slot.
func (s *Repo) KDSSlotCounts(ctx context.Context, branchIDs []string, from, to time.Time, fulfilment []string) ([]ordersapi.KDSSlotCount, error) {
	q := `
		SELECT o.scheduled_date, o.slot_start::text, o.slot_end::text, count(*)
		  FROM orders o
		 WHERE ` + kdsPlanningWhere + ` AND o.branch_id::text = ANY($1)
		   AND o.scheduled_date BETWEEN $2 AND $3`
	args := []any{branchIDs, from, to}
	if len(fulfilment) > 0 {
		args = append(args, fulfilment)
		q += ` AND lower(o.fulfilment_type) = ANY($4)`
	}
	q += ` GROUP BY o.scheduled_date, o.slot_start, o.slot_end ORDER BY o.scheduled_date, o.slot_start`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ordersapi.KDSSlotCount{}
	for rows.Next() {
		var c ordersapi.KDSSlotCount
		if err := rows.Scan(&c.Date, &c.SlotStart, &c.SlotEnd, &c.OrderCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// KDSDayCounts counts orders per scheduled date.
func (s *Repo) KDSDayCounts(ctx context.Context, branchIDs []string, from, to time.Time, fulfilment []string) ([]ordersapi.KDSDayCount, error) {
	q := `
		SELECT o.scheduled_date, count(*)
		  FROM orders o
		 WHERE ` + kdsPlanningWhere + ` AND o.branch_id::text = ANY($1)
		   AND o.scheduled_date BETWEEN $2 AND $3`
	args := []any{branchIDs, from, to}
	if len(fulfilment) > 0 {
		args = append(args, fulfilment)
		q += ` AND lower(o.fulfilment_type) = ANY($4)`
	}
	q += ` GROUP BY o.scheduled_date ORDER BY o.scheduled_date`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ordersapi.KDSDayCount{}
	for rows.Next() {
		var c ordersapi.KDSDayCount
		if err := rows.Scan(&c.Date, &c.OrderCount); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// KDSItemAggregates groups the lines of planned orders by category, product
// and variation, optionally for one slot.
func (s *Repo) KDSItemAggregates(ctx context.Context, branchIDs []string, from, to time.Time, slotStart, slotEnd string, fulfilment []string) ([]ordersapi.KDSItemAggregate, error) {
	q := `
		SELECT COALESCE(pc.id::text, ''), COALESCE(pc.name, 'Other'),
		       COALESCE(oi.product_id::text, ''), oi.product_name, COALESCE(oi.variant_name, ''),
		       oi.quantity - oi.returned_quantity, oi.order_id::text,
		       o.scheduled_date::text || ' ' || o.slot_start::text,
		       oi.selected_flavours, oi.selected_addons
		  FROM order_items oi
		  JOIN orders o ON o.id = oi.order_id
		  LEFT JOIN products p ON p.id = oi.product_id
		  LEFT JOIN product_categories pc ON pc.id = p.category_id
		 WHERE ` + kdsPlanningWhere + ` AND o.branch_id::text = ANY($1)
		   AND o.scheduled_date BETWEEN $2 AND $3
		   AND oi.production_status NOT IN ('cancelled', 'cancel')
		   AND oi.kot_status <> 'cancelled'`
	args := []any{branchIDs, from, to}
	if slotStart != "" {
		args = append(args, slotStart)
		q += fmt.Sprintf(` AND o.slot_start = $%d::time`, len(args))
	}
	if slotEnd != "" {
		args = append(args, slotEnd)
		q += fmt.Sprintf(` AND o.slot_end = $%d::time`, len(args))
	}
	if len(fulfilment) > 0 {
		args = append(args, fulfilment)
		q += fmt.Sprintf(` AND lower(o.fulfilment_type) = ANY($%d)`, len(args))
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// One row per configuration: the same product with another variation
	// or another set of add-ons is a separate row, identical ones are summed.
	type key struct{ cat, product, name, variant, config string }
	agg := map[key]*ordersapi.KDSItemAggregate{}
	orders := map[key]map[string]bool{}
	slots := map[key]map[string]bool{}
	var order []key
	for rows.Next() {
		var catID, catName, productID, productName, variant, orderID, slot string
		var qty int
		var flavoursRaw, addonsRaw []byte
		if err := rows.Scan(&catID, &catName, &productID, &productName, &variant, &qty, &orderID, &slot,
			&flavoursRaw, &addonsRaw); err != nil {
			return nil, err
		}
		if qty <= 0 {
			continue
		}
		var mods []ordersapi.SelectedModifier
		_ = json.Unmarshal(flavoursRaw, &mods)
		var addons []ordersapi.SelectedModifier
		_ = json.Unmarshal(addonsRaw, &addons)
		flavourNames, addonNames := modifierNames(mods), modifierNames(addons)
		k := key{catID, productID, productName, variant,
			strings.Join(flavourNames, "") + "" + strings.Join(addonNames, "")}
		a, ok := agg[k]
		if !ok {
			a = &ordersapi.KDSItemAggregate{CategoryID: catID, CategoryName: catName, ProductID: productID,
				ProductName: productName, VariantName: variant, Modifiers: map[string]int{},
				Variations: flavourNames, Addons: addonNames}
			agg[k] = a
			orders[k] = map[string]bool{}
			slots[k] = map[string]bool{}
			order = append(order, k)
		}
		a.TotalQuantity += qty
		orders[k][orderID] = true
		slots[k][slot] = true
		for _, m := range append(mods, addons...) {
			if m.Name != "" {
				a.Modifiers[m.Name] += qty
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ordersapi.KDSItemAggregate, 0, len(order))
	for _, k := range order {
		a := agg[k]
		a.OrderCount = len(orders[k])
		a.SlotCount = len(slots[k])
		out = append(out, *a)
	}
	return out, nil
}

// KDSSlotsNearlyFull counts bookable slots at 75%+ of capacity on a date.
func (s *Repo) KDSSlotsNearlyFull(ctx context.Context, branchIDs []string, date time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM slot_capacity
		 WHERE branch_id::text = ANY($1) AND date = $2
		   AND max_capacity > 0 AND reserved_count::numeric >= max_capacity * 0.75`, branchIDs, date).Scan(&n)
	return n, err
}

// modifierNames is the distinct non-empty names, sorted, so the same
// selection in any order is the same configuration.
func modifierNames(ms []ordersapi.SelectedModifier) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range ms {
		n := strings.TrimSpace(m.Name)
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
