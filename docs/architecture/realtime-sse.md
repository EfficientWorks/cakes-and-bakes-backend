# Realtime — SSE

## Design decision

Server-Sent Events (SSE), not WebSockets. SSE is unidirectional (server → client).
Clients send updates back via normal REST API calls. This is exactly what the KDS and
POS need:
- Server pushes new order → KDS screen renders it.
- Kitchen staff taps "done" → REST `PATCH /v1/orders/{id}/items/{itemId}/status`.
- Server pushes updated status → all connected clients for that branch see it.

Go's `http.ResponseWriter` with `text/event-stream` content type handles SSE natively.
No extra library. Works through Cloudflare without configuration.

## Channel structure

One channel per branch. Clients subscribe to their branch channel on startup.

```
/v1/realtime/events?branch_id=<uuid>
```

Auth: Bearer JWT in `Authorization` header. The branch_id in the query param must match
the branch in the JWT claims — enforced by middleware.

## Event envelope

```json
{
  "event": "order.created",
  "branch_id": "uuid",
  "data": { ...event-specific payload... },
  "timestamp": "2026-08-28T14:30:00Z",
  "sequence": 1001
}
```

Event types that trigger a push:

| Event | Consumers | Trigger |
|---|---|---|
| `order.routed` | KDS, branch admin | New order assigned to branch |
| `order.amended` | KDS, branch admin | Order fields changed before production lock |
| `order.cancelled` | KDS, branch admin | Order cancelled |
| `order.status_changed` | KDS, storefront tracking | Status transition |
| `item.status_changed` | KDS | Item marked in-progress or done |
| `catalog.availability_changed` | Storefront | Item enabled/disabled |
| `branch.suspended` | Storefront | Branch suspension |
| `slot.capacity_updated` | Storefront slot picker | Slot fills or opens up |

## In-memory hub (launch)

Single instance, per-branch subscriber map. Correct for a Railway single-server deployment.

```go
type Hub struct {
    mu          sync.RWMutex
    subscribers map[string][]chan Event  // key: branch_id
}
```

On reconnect: clients supply `Last-Event-ID` header; the hub replays the last N events
from an in-memory ring buffer (configurable, default 100 events per branch).

## Scale trigger

Add a Redis pub/sub backplane the moment there is more than one `realtime` instance
behind a load balancer. The hub interface is the same; the backing implementation
switches from in-memory map to Redis channel. This is the only change needed.

## KDS and POS reconnect behaviour

Flutter apps use `web_socket_channel` (for the SSE stream via HTTP) and reconnect
automatically with exponential backoff (500ms → 1s → 2s → 4s, cap 30s).
On reconnect they supply `Last-Event-ID` to replay missed events.
While disconnected, a "reconnecting" banner is shown but the app remains usable
(cached order data from last known state).
