package slot

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"bakehouse/services/orders/ordersapi"
)

func (s *Repo) GetScheduleConfig(ctx context.Context, branchID string) (*ordersapi.ScheduleConfig, error) {
	var c ordersapi.ScheduleConfig
	c.BranchID = branchID
	err := s.pool.QueryRow(ctx, `
		SELECT slot_duration_mins, pickup_cutoff_time::text, delivery_cutoff_time::text,
		       max_orders_per_slot, default_lead_hours, advance_days_max
		FROM branch_schedule_config WHERE branch_id = $1`, branchID).
		Scan(&c.SlotDurationMins, &c.PickupCutoffTime, &c.DeliveryCutoffTime, &c.MaxOrdersPerSlot, &c.DefaultLeadHours, &c.AdvanceDaysMax)
	if errors.Is(err, pgx.ErrNoRows) {
		// Sensible defaults so a newly created branch can trade the same
		// day (NFR-14) without a separate scheduling setup step.
		return &ordersapi.ScheduleConfig{
			BranchID: branchID, SlotDurationMins: 60, PickupCutoffTime: "16:00:00", DeliveryCutoffTime: "14:00:00",
			MaxOrdersPerSlot: 10, DefaultLeadHours: 2, AdvanceDaysMax: 30,
		}, nil
	}
	return &c, err
}

func (s *Repo) UpsertScheduleConfig(ctx context.Context, c ordersapi.ScheduleConfig) (*ordersapi.ScheduleConfig, error) {
	const q = `
		INSERT INTO branch_schedule_config (branch_id, slot_duration_mins, pickup_cutoff_time, delivery_cutoff_time, max_orders_per_slot, default_lead_hours, advance_days_max)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (branch_id) DO UPDATE SET
			slot_duration_mins = $2, pickup_cutoff_time = $3, delivery_cutoff_time = $4,
			max_orders_per_slot = $5, default_lead_hours = $6, advance_days_max = $7, updated_at = NOW()`
	_, err := s.pool.Exec(ctx, q, c.BranchID, c.SlotDurationMins, c.PickupCutoffTime, c.DeliveryCutoffTime, c.MaxOrdersPerSlot, c.DefaultLeadHours, c.AdvanceDaysMax)
	if err != nil {
		return nil, err
	}
	return &c, nil
}
