# Catalog module

Service: `services/catalog/`. Phase 3.

## What this module owns

- **Master catalogue** — 3 categories, 8 core products, variants (size/design), flavours,
  add-ons, occasion tags. Brand-level: super admin edits, all branches inherit.
- **Availability cascade** — 4-layer resolution per branch per product.
- **Seasonal schedules** — products available only within date ranges.
- **Occasion landing pages** — occasion-tagged products for gift discovery.
- **ConnectRPC handler** — `orders` service validates items at order creation.

## The 3 categories (seeded at launch)

```
bouquets          → 6, 10, 15, 20 cupcake count variants
boxes             → 4, 6, 12 count + combo box variants
macaron-gift-boxes → 6-pack, 12-pack, gift sets
```

Design theme = a **variant**, never a separate product.
This is the direct fix for the "20 apparent products when there are really 8" problem.

## Availability cascade (4 layers)

```
Layer 4 — Date override:   is this item forced on/off for TODAY at THIS branch?
Layer 3 — Weekly schedule: is this item available on this day of week at this branch?
Layer 2 — Branch setting:  does this branch carry this item at all?
Layer 1 — Master catalogue: does this product exist in the brand catalogue?
```

Most specific layer with an opinion wins. Resolved in one SQL query.
Changes propagate to storefront within 30 seconds via SSE `catalog.availability_changed` event.

## Product pricing mode

`products.pricing_mode` (migration 0060) decides what an order line is charged. It is
accepted on `POST /admin/products` and `PATCH /admin/products/{id}` as `pricingMode` and
returned on every product shape.

| `pricingMode` | Order line unit price | Storefront `minPriceCents` / `maxPriceCents` |
|---|---|---|
| `product` (default) | base price (branch override applied) + every selected modifier delta | base + cheapest pick per variation group … base + dearest pick |
| `variation` | sum of the selected **variation** option prices (+ add-ons); the base price is ignored | cheapest pick per variation group summed … dearest pick summed |

Example: variation groups (10, 30) and (5, 10) → 15–40; (10, 30) alone → 10–30. Add-on
groups never count towards the range. A `variation` product with no variation groups
falls back to its base price rather than charging nothing.

Both pricing paths go through `GetPricedItem`, so website checkout and POS order
create/update charge the same number.

`GET /storefront/products` and `GET /pos/products` list cards stay light for `product`
items; for `variation` items they carry `modifierGroups` (with that branch's option
price overrides applied when a branch is given) so the client can show a price at all.

## Product images

Multiple images per product. `sort_order = 0` = primary image.
`season_start`/`season_end` on images: show Mother's Day imagery in May, standard imagery
the rest of the year.

## See also

- `docs/phases/phase3-catalog.md` — full schema, SQL, API
- `docs/architecture/realtime-sse.md` — availability change propagation
