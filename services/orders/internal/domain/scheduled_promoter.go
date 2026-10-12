package domain

import (
	"context"
	"log/slog"
	"time"

	"bakehouse/services/orders/ordersapi"
)

/*
 * A scheduled order becomes today's order on its day.
 *
 * The POS tabs and the KDS already go by the store's date, so the order
 * shows in today's tab and on the board without this. It changes the status
 * too, so every screen says "confirmed" rather than "scheduled" for an order
 * that is due now, and the status change reaches the POS and KDS live.
 */

const scheduledPromoteEvery = time.Minute

// StartScheduledPromoter runs PromoteDueScheduled every minute until ctx ends.
func (s *Service) StartScheduledPromoter(ctx context.Context) {
	go func() {
		t := time.NewTicker(scheduledPromoteEvery)
		defer t.Stop()
		s.PromoteDueScheduled(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.PromoteDueScheduled(ctx)
			}
		}
	}()
}

// PromoteDueScheduled moves scheduled orders whose day has come to confirmed.
func (s *Service) PromoteDueScheduled(ctx context.Context) {
	ids, err := s.store.DueScheduledOrderIDs(ctx)
	if err != nil {
		slog.Warn("due scheduled orders could not be read", "error", err)
		return
	}
	for _, id := range ids {
		if err := s.setStatus(ctx, "system", nil, id, ordersapi.StatusConfirmed, "Scheduled order is due today"); err != nil {
			slog.Warn("scheduled order could not be moved to today", "order", id, "error", err)
		}
	}
}
