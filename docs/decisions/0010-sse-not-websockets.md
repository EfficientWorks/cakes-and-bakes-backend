# ADR-0010 — SSE not WebSockets for realtime push

## Status

Accepted

## Context

KDS needs to receive new orders and status updates in real time (<2 seconds).
POS orders screen needs to see incoming online orders.
Storefront slot picker needs to see when a slot fills up.

Two options: WebSockets (bidirectional) or SSE (unidirectional, server → client).

## Decision

Use SSE (Server-Sent Events).

## Reasons

- The communication is inherently unidirectional. The server pushes events; clients send
  updates via normal REST calls (`POST /v1/orders/{id}/items/{itemId}/done`). There is no
  case where the client needs to push a stream to the server.
- SSE works natively with Go's `http.ResponseWriter` and `text/event-stream`. No library.
- SSE reconnects automatically on the client side. Flutter's HTTP client handles it.
- SSE works through Cloudflare without any special WebSocket upgrade configuration.
- SSE supports `Last-Event-ID` for replay on reconnect — the ring buffer in `realtime`
  handles this.
- WebSockets would require sticky sessions behind a load balancer (single Railway instance
  at launch makes this moot, but avoiding the pattern is cleaner).

## Consequences

- Clients send all mutations via REST, not via the SSE connection.
- KDS and POS Flutter apps reconnect with `Last-Event-ID` on network disruption.
- If scaling beyond one `realtime` instance, a Redis pub/sub backplane is needed regardless
  of SSE vs WebSocket — this is not a differentiating factor.
