# Baked Bouquet — Backend API Overview & Architecture

## Project Summary

Baked Bouquet is a centralised, multi-branch cupcake bouquet ordering platform. The backend serves five surfaces:

| # | Surface | Users |
|---|---|---|
| 1 | Customer storefront | Gift buyers, registered customers, guests |
| 2 | Super admin console | Brand / head office |
| 3 | Branch & franchise admin | Franchise owners, branch managers, counter staff |
| 4 | Kitchen Display System (KDS) | Kitchen production staff |
| 5 | Point of Sale (POS) | Counter staff (Phase 2) |

---

## Module Files Index

| File | Module | Phase |
|---|---|---|
| `01-AUTH.md` | Authentication & customer accounts | R1 |
| `02-TENANCY.md` | Franchise & branch governance | R1 |
| `03-RBAC.md` | Roles, permissions, access control | R1 |
| `04-CATALOG.md` | Products, variants, availability cascade | R1 |
| `05-STOREFRONT.md` | Customer ordering, cart, scheduling, checkout | R1 |
| `06-ORDERS.md` | Order lifecycle, amendments, cancellation | R1 |
| `07-PAYMENTS.md` | Stripe, refunds, settlement | R1 |
| `08-ADMIN.md` | Branch admin, order management, fulfilment | R1 |
| `09-SUPER-ADMIN.md` | Network management, reporting | R1 |
| `10-KDS.md` | Kitchen display, real-time routing | R2 |
| `11-POS.md` | Point of sale (Phase 2) | R2 |
| `12-NOTIFICATIONS.md` | Email, transactional messages | R1 |
| `13-DELIVERY.md` | Delivery zones, Shipday dispatch | R2 |
| `14-REPORTING.md` | Sales reports, comparable reporting | R1 |
| `15-WEBSOCKET.md` | Real-time events (Ably) | R2 |
| `16-NFR.md` | Non-functional & infrastructure requirements | R1 |

---

## Tech Stack (Locked)

| Layer | Choice | Notes |
|---|---|---|
| Runtime | Node.js 20+ | LTS |
| Framework | NestJS (TypeScript) | Modular, decorator-based |
| Database | PostgreSQL 16 | RLS for data isolation |
| Cache / queues | Redis + BullMQ | Sessions, job queues |
| Real-time | Ably | WebSocket fanout to KDS/POS |
| Auth | Auth0 (customers) + PIN (POS/KDS shift) | |
| Payments | Stripe + Stripe Terminal | |
| Email | Postmark | |
| Storage | Cloudflare R2 | Product images, receipts, PDFs |
| Maps | Google Maps API | Geocoding, zone resolution |
| Dispatch | Shipday | Delivery job creation/status |
| Monitoring | Sentry + structured logging | |

---

## Core Architectural Rules

### 1. Every order carries franchise_id and branch_id from creation — never changes
### 2. Data isolation enforced at DB layer (Postgres RLS) — not just application
### 3. All monetary values stored as integers (cents/smallest unit), currency = USD
### 4. All timestamps stored in UTC, rendered in branch timezone
### 5. Audit log is append-only — no role can modify or delete it
### 6. Slot capacity reserved atomically — no overselling under concurrent load
### 7. Availability changes propagate to storefront within 30 seconds
### 8. Order events reach KDS within 2 seconds

---

## Hierarchy

```
BRAND (Baked Bouquet)
  └── FRANCHISE (e.g. Florida Franchisee)
        └── BRANCH (e.g. Edison NJ, Trappe PA)
              ├── Orders
              ├── Staff
              ├── Devices (KDS, POS, Printers)
              └── Catalogue availability rules
```

A company-owned location = franchise whose owner is the brand. One code path for all.

---

## Database Isolation Pattern

Every request sets session variables at the start:

```sql
SET LOCAL app.current_tenant_franchise_id = '...';
SET LOCAL app.current_branch_id = '...';
SET LOCAL app.current_user_role = 'branch_manager';

-- RLS policy on orders:
CREATE POLICY branch_isolation ON orders
  USING (branch_id = current_setting('app.current_branch_id')::uuid
         OR current_setting('app.current_user_role') IN ('super_admin','franchise_owner'));
```

---

## Release Map

| Release | Date | Contents |
|---|---|---|
| R1 | 28 Aug 2026 | Storefront, customer accounts, super admin, branch admin. Orders closed manually. |
| R2 | 9 Sep 2026 | POS + KDS. Real-time routing live. Manual close retired. |
| R3 | 30 Sep 2026 | UAT complete, data migrated, Square + Weebly retired. Full public launch. |

---

## Global Response Format

```typescript
// Success
{
  "success": true,
  "data": { ... },
  "meta": { "page": 1, "total": 50 } // pagination where applicable
}

// Error
{
  "success": false,
  "error": {
    "code": "ORDER_NOT_FOUND",
    "message": "Order with id X was not found",
    "field": "order_id" // optional, for validation errors
  }
}
```

---

## Common Headers

```
Authorization: Bearer <jwt_token>       // customer / staff auth
X-Branch-Id: <uuid>                     // required for branch-scoped requests
X-Device-Id: <uuid>                     // POS / KDS device identifier
Content-Type: application/json
```

---

## Idempotency

All mutating endpoints that involve money or slot reservation accept:

```
Idempotency-Key: <client-generated-uuid>
```

Duplicate requests with the same key within 24h return the original response without side effects.
