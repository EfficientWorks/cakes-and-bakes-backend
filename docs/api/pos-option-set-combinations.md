# POS: products sold by option-set combinations

For the POS app team. Backend is live from this release; nothing existing changed shape — the POS/KDS contract check passes.

## What changed

Some products are now sold by **combination**: the customer picks one option from each option set (e.g. Size, Sponge, Flavour of cream), and every combination has its own price and can be switched off by the admin.

Example: **Blue Vanilla Cake**

| Combination | Price | Sold? |
|---|---|---|
| 8 Inch \| Vanilla Eggless \| Vanilla Fresh Cream | £29.99 | yes |
| 8 Inch \| Red Velvet with Egg \| Chocolate Fresh Cream | — | no (switched off, not sent) |
| 10 Inch \| Vanilla Eggless \| Vanilla Fresh Cream | £35.00 | yes |

There is no stock: a combination is either on sale (present in the list) or not.

## API changes (additive only)

### `GET /v1/pos/products` and `GET /v1/pos/products/{slug}`

Each product may now carry:

```json
"combinations": [
  {
    "id": "6f1c…",
    "optionIds": ["71b0f905-…", "7e8cb872-…", "a1d2…"],
    "label": "8 Inch | Vanilla Eggless | Vanilla Fresh Cream",
    "priceCents": 2999
  }
]
```

- **Omitted** for products not sold by combination — those work exactly as before.
- Only combinations that are **on sale** are listed. Anything not in the list must not be offered.
- `optionIds` are ids from the product's `modifierGroups` whose `kind` is `"variation"` — one per variation group, in no particular order.
- `priceCents` is the full price of that combination before add-ons. It is already resolved for the store (store prices applied).
- `minPriceCents` / `maxPriceCents` on the product are the cheapest / dearest combination, so a grid tile can show "From £29.99".
- On the list endpoint, `modifierGroups` and `combinations` are now included for these products so the grid has what it needs.

### `POST /v1/pos/orders` (and the other order-line endpoints)

No new fields. Send the line as today:

- `selectedFlavours`: the chosen variation options (`[{ "id": "…" }]`, names optional).
- `selectedAddons`: any add-ons, as today.
- **`unitPriceCents`: send `0` for a new combination line.** The server then prices it from the catalogue: combination price + add-ons, and **rejects a combination that is not on sale** with `PRODUCT_UNAVAILABLE` (`combination_unavailable`).
  If you send a non-zero `unitPriceCents` the server keeps your number (current POS behaviour), so if you do, it must be `combination.priceCents + add-on prices`.
- In the saved line, the variation options come back with `priceDelta: 0` — the combination price sits in `unitPriceCents`. Don't add option deltas on top.

## What the POS screen should do

1. **Product grid**: for a product with `combinations`, show "From £{minPriceCents}".
2. **Tap product → options sheet**: one row of buttons per variation group (group name as the heading, e.g. "Size", "Sponge", "Flavour of cream"), then add-on groups as today.
3. **Only allow real combinations**: a button is enabled only if some combination contains it together with what is already chosen in the other groups. Changing an earlier choice clears later choices that no longer fit.
4. **Price**: show "From £X" until one option from every variation group is chosen, then the matched combination's `priceCents` (+ add-ons) × quantity.
5. **Add to order** is disabled until a full combination is chosen.
6. **Order line / receipt**: product name, then the combination label (or the chosen option names joined with " | "), then add-ons.

## KDS

No change needed. The kitchen ticket already prints the line's chosen options, so it shows e.g. "Blue Vanilla Cake — 8 Inch, Vanilla Eggless, Vanilla Fresh Cream".

## Test data (local)

`admin@example.com` / `ChangeMe123!` on a local backend; the product `combo-cake-mwddyu1q` has four combinations, one switched off and one with a typed price.
