# Phase 7 — Realtime

Service: `services/realtime/`. See `docs/modules/realtime/README.md`.
Full SSE detail: `docs/architecture/realtime-sse.md`.

## What this phase builds

The SSE hub that fans out events from the outbox to connected clients (KDS Flutter app,
POS Flutter app, storefront slot picker, branch admin order list).

## 7.1 No DB

`realtime` has no database. It is an in-memory event router.
State source of truth is always the owning service's database.

## 7.2 Hub implementation

```go
// services/realtime/internal/hub/hub.go

type Hub struct {
  mu          sync.RWMutex
  subscribers map[string][]chan Event   // key: branch_id
  ring        map[string]*ringBuffer   // last 100 events per branch for replay
}

func (h *Hub) Subscribe(branchID string, lastEventID string) (<-chan Event, CancelFunc)
func (h *Hub) Publish(branchID string, event Event)
func (h *Hub) Replay(branchID string, afterID string) []Event
```

## 7.3 SSE endpoint

```
GET /v1/realtime/events
  Authorization: Bearer <jwt>       -- staff or customer token
  X-Branch-ID: <uuid>               -- must match JWT claims (staff) or order's branch (customer)
  Last-Event-ID: <sequence>         -- client sends on reconnect for replay
```

Response: `Content-Type: text/event-stream`, indefinitely streaming.

```
id: 1001
event: order.routed
data: {"branch_id":"...","order_id":"...","recipient_name":"Sarah","slot":"14:00-14:30",...}

id: 1002
event: catalog.availability_changed
data: {"branch_id":"...","product_id":"...","available":false,"reason":"sold_out"}
```

## 7.4 Event relay

In-process goroutine polls `orders_outbox` and other service outbox tables every 500ms.
Dispatches to hub.Publish for each unprocessed event.
Marks event as published in the outbox after successful dispatch.

## 7.5 Client reconnect

Flutter clients send `Last-Event-ID` header on reconnect.
Hub replays events with `sequence > lastEventID` from the ring buffer (100 events per branch).
Events older than ring buffer capacity are lost — acceptable because the Flutter app
re-fetches current state via REST on reconnect.

## Previous / Next

← [Phase 6 — Admin](phase6-admin.md) · [Phase 8 — POS + KDS](phase8-pos-kds.md) →
