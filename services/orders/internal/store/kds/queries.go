package kds

import "bakehouse/services/orders/ordersapi"

// Statuses an order leaves the board with for good.
var kdsClosedStatuses = []string{ordersapi.StatusRefunded, ordersapi.StatusReturned}

// releasedForKDSSQL is a released later-dated order the board still shows:
// one the kitchen has not finished, or one released today. from and to are
// the board's date range placeholders.
const releasedForKDSSQL = `(kds_released_at IS NOT NULL AND status <> 'cancelled' AND scheduled_date > %[2]s
	AND (kot_status IN ('queue','preparing','ready','packing','ready_for_delivery')
	     OR (kds_released_at AT TIME ZONE COALESCE((SELECT b.timezone FROM branches b WHERE b.id = orders.branch_id), 'America/New_York'))::date >= %[1]s))`

// Orders that count towards planning: everything except what will not be made.
const kdsPlanningWhere = `o.deleted_at IS NULL AND o.status NOT IN ('cancelled', 'refunded', 'returned')`
