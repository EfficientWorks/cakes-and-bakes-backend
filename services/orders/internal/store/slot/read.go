package slot

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/orders/ordersapi"
)

// GetOrCreateSlot returns the slot_capacity row for (branch, date, start,
// fulfilmentType), creating it with maxCapacity from schedule config if it
// does not exist yet. Slots are created lazily rather than pre-generated
// for the whole calendar.
func (s *Repo) GetOrCreateSlot(ctx context.Context, branchID string, date time.Time, slotStart, slotEnd, fulfilmentType string, maxCapacity int) (*ordersapi.SlotCapacity, error) {
	const q = `
		INSERT INTO slot_capacity (branch_id, date, slot_start, slot_end, fulfilment_type, max_capacity)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (branch_id, date, slot_start, fulfilment_type) DO UPDATE SET slot_end = EXCLUDED.slot_end
		RETURNING id, branch_id, date, slot_start::text, slot_end::text, fulfilment_type, max_capacity, reserved_count`
	var sc ordersapi.SlotCapacity
	err := s.pool.QueryRow(ctx, q, branchID, date, slotStart, slotEnd, fulfilmentType, maxCapacity).
		Scan(&sc.ID, &sc.BranchID, &sc.Date, &sc.SlotStart, &sc.SlotEnd, &sc.FulfilmentType, &sc.MaxCapacity, &sc.ReservedCount)
	return &sc, err
}

// GetOrCreateSlots is GetOrCreateSlot for many slots in one round trip.
//
// The storefront asks for up to two weeks of slots at once. Doing that one
// upsert per slot meant hundreds of sequential queries per page load — slow
// enough over a remote database that the storefront gave up and the request
// died with "context canceled". Same upsert, same no-op update on conflict
// (so an existing slot keeps its reserved_count), one statement.
//
// The result is keyed by "YYYY-MM-DD|HH:MM" because RETURNING order is not
// guaranteed.
func (s *Repo) GetOrCreateSlots(ctx context.Context, branchID, fulfilmentType string, maxCapacity int, keys []SlotKey) (map[string]ordersapi.SlotCapacity, error) {
	out := make(map[string]ordersapi.SlotCapacity, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	dates := make([]string, len(keys))
	starts := make([]string, len(keys))
	ends := make([]string, len(keys))
	for i, k := range keys {
		dates[i] = k.Date.Format("2006-01-02")
		starts[i] = k.SlotStart
		ends[i] = k.SlotEnd
	}
	const q = `
		INSERT INTO slot_capacity (branch_id, date, slot_start, slot_end, fulfilment_type, max_capacity)
		SELECT $1::uuid, t.d::date, t.s::time, t.e::time, $2, $3
		  FROM unnest($4::text[], $5::text[], $6::text[]) AS t(d, s, e)
		ON CONFLICT (branch_id, date, slot_start, fulfilment_type) DO UPDATE SET slot_end = EXCLUDED.slot_end
		RETURNING id, branch_id, date, slot_start::text, slot_end::text, fulfilment_type, max_capacity, reserved_count`
	rows, err := s.pool.Query(ctx, q, branchID, fulfilmentType, maxCapacity, dates, starts, ends)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sc ordersapi.SlotCapacity
		if err := rows.Scan(&sc.ID, &sc.BranchID, &sc.Date, &sc.SlotStart, &sc.SlotEnd, &sc.FulfilmentType, &sc.MaxCapacity, &sc.ReservedCount); err != nil {
			return nil, err
		}
		out[SlotMapKey(sc.Date, sc.SlotStart)] = sc
	}
	return out, rows.Err()
}

// SlotKey names one candidate slot for GetOrCreateSlots.
type SlotKey struct {
	Date      time.Time
	SlotStart string // "15:04"
	SlotEnd   string // "15:04"
}

// SlotMapKey is the key GetOrCreateSlots returns slots under. slotStart may
// carry seconds ("09:00:00"); only HH:MM is used.
func SlotMapKey(date time.Time, slotStart string) string {
	if len(slotStart) > 5 {
		slotStart = slotStart[:5]
	}
	return date.Format("2006-01-02") + "|" + slotStart
}

func (s *Repo) ListSlots(ctx context.Context, branchID string, date time.Time, fulfilmentType string) ([]ordersapi.SlotCapacity, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, branch_id, date, slot_start::text, slot_end::text, fulfilment_type, max_capacity, reserved_count
		FROM slot_capacity WHERE branch_id = $1 AND date = $2 AND fulfilment_type = $3 ORDER BY slot_start`,
		branchID, date, fulfilmentType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ordersapi.SlotCapacity
	for rows.Next() {
		var sc ordersapi.SlotCapacity
		if err := rows.Scan(&sc.ID, &sc.BranchID, &sc.Date, &sc.SlotStart, &sc.SlotEnd, &sc.FulfilmentType, &sc.MaxCapacity, &sc.ReservedCount); err != nil {
			return nil, err
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}

func (s *Repo) GetSlotByID(ctx context.Context, id string) (*ordersapi.SlotCapacity, error) {
	var sc ordersapi.SlotCapacity
	err := s.pool.QueryRow(ctx, `
		SELECT id, branch_id, date, slot_start::text, slot_end::text, fulfilment_type, max_capacity, reserved_count
		FROM slot_capacity WHERE id = $1`, id).
		Scan(&sc.ID, &sc.BranchID, &sc.Date, &sc.SlotStart, &sc.SlotEnd, &sc.FulfilmentType, &sc.MaxCapacity, &sc.ReservedCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ordersapi.ErrNotFound
	}
	return &sc, err
}
