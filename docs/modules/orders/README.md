# Orders module

Service: `services/orders/`. Phase 4.

## What this module owns

- **Order lifecycle** — creation through fulfilment or cancellation.
- **Routing engine** — resolves which branch fulfils each order before payment.
- **Amendment engine** — handles permitted changes within the amendment window.
- **Refund coordination** — triggers Stripe refund via `payments` service.
- **KDS feeds** — today/tomorrow/week views for kitchen display.
- **POS order management** — order list, status transitions for counter staff.
- **Exception queue** — orders that failed to route automatically.
- **Public tracking** — secure-token-based status page for recipients.

## Critical constraints

1. `franchise_id` and `branch_id` are written at creation and never overwritten.
   Reassignment creates a new `order_assignments` record, preserving history.

2. Slot capacity is decremented in the **same Postgres transaction** as order creation.
   If the slot is full, the entire transaction rolls back. No race condition is possible.

3. Amendment window closes the moment status transitions to `in_production`.
   The server enforces this; client permission state is presentation only.

4. Idempotency key is required on `POST /v1/orders`. A duplicate key returns the
   existing order without re-processing.

## Order status lifecycle

```
PLACED       → payment received from Stripe webhook
CONFIRMED    → branch acknowledges (manual in R1; will be automatic in R2)
SCHEDULED    → order is awaiting its production window
IN_PRODUCTION → first KDS item marked in-progress; amendment window closes
READY        → all items marked done on KDS; auto-transition
FULFILLED    → collected by customer or delivered
CANCELLED    → cancelled before or at any point before IN_PRODUCTION
```

## Outbox events (see phase4-orders.md for full list)

Every status transition writes to `orders_outbox` in the same transaction.
In-process relay publishes to `realtime`, which fans out to SSE subscribers.

## See also

- `docs/phases/phase4-orders.md` — full schema, API, routing engine
- `docs/architecture/realtime-sse.md` — how order events reach KDS/POS
- `docs/modules/scheduling/README.md` — slot capacity management
