# Architecture Decision Records

| ADR | Decision | Status |
|---|---|---|
| [0001](0001-go-architecture-from-emzo-restaurant.md) | Reuse emzo-restaurant Go architecture pattern | Accepted |
| [0002](0002-franchise-not-outlet-model.md) | 3-level Brand → Franchise → Branch, not 2-level Business → Outlet | Accepted |
| [0003](0003-stripe-first-payments.md) | Stripe online from day 1 (not manual-entry-first like emzo-restaurant) | Accepted |
| [0004](0004-no-redis-at-launch.md) | No Redis — in-memory SSE hub, no rate-limit backplane | Accepted |
| [0005](0005-routing-engine-in-orders.md) | Branch routing engine lives in orders service (not gateway) | Accepted |
| [0006](0006-scheduling-as-orders-module.md) | Scheduling is an internal module of orders, not a separate service | Accepted |
| [0007](0007-railway-deployment.md) | Railway for launch, cloud migration path documented | Accepted |
| [0008](0008-postmark-email.md) | Postmark for transactional email | Accepted |
| [0009](0009-google-maps-for-zones.md) | Google Maps for geocoding and distance ranking | Accepted |
| [0010](0010-sse-not-websockets.md) | SSE not WebSockets for realtime push | Accepted |
