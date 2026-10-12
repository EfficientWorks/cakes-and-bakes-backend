# Module 04 — Catalogue & Availability

**Phase:** R1  
**BRD refs:** CAT-01 to CAT-13, Section 8, Section 2.3  
**Surfaces:** Storefront, branch admin, super admin

---

## Overview

The catalogue has two layers:
1. **Master catalogue** — owned by brand/super admin. Core products, names, images, base pricing.
2. **Branch availability rules** — each branch controls what it actually offers, when, and at what price (within governance bands).

Availability resolves through a 4-layer cascade evaluated in the branch's local timezone.

---

## Product Structure (from BRD Section 2.3)

```
Category: Cupcake Bouquets
  ├── Berry Blossom Cupcake Bouquet
  │     Sizes: 6 ($88), 9 ($88), 12 ($109), 15 ($129)
  │     Flavours: Vanilla, Chocolate, Red Velvet, Strawberry,
  │               Gluten Free Vanilla, Gluten Free Chocolate
  │     Add-ons: It's a Girl ($3), It's a Boy ($3), Birthday ($3),
  │              Congratulations ($3), Butterfly Topper ($3), Macaron 06 Pack ($3)
  │     Presentation: Ceramic Vase (included), Gift Box (+$6), Woven Basket (+$12)
  │     Wrap Colours: pink, lavender, gold, purple, sage
  └── [other bouquets...]

Category: Cupcake Boxes
  └── [boxes...]

Category: Macaron Gift Boxes
  └── [macaron gifts...]
```

Design themes (e.g. "Berry Blossom", "Lavender Spring") = product variations, NOT separate catalogue entries. This is the fix for BRD pain point P4 (choice paralysis).

---

## Database Tables

```sql
-- Master product catalogue
products (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  category_id     UUID NOT NULL REFERENCES product_categories(id),
  name            TEXT NOT NULL,
  slug            TEXT UNIQUE NOT NULL,
  description     TEXT NOT NULL,
  base_price_cents INTEGER NOT NULL,
  lead_time_hours  INTEGER NOT NULL DEFAULT 2,
  production_station TEXT,              -- for KDS routing
  allergen_flags   TEXT[] DEFAULT '{}', -- dairy, nuts, gluten, eggs, soy
  dietary_flags    TEXT[] DEFAULT '{}', -- vegan, vegetarian, gluten_free
  occasion_tags    TEXT[] DEFAULT '{}', -- birthday, anniversary, thank_you, new_baby, sympathy
  is_best_seller   BOOLEAN DEFAULT false,
  is_active        BOOLEAN DEFAULT true,
  sort_order       INTEGER DEFAULT 0,
  price_floor_cents  INTEGER,           -- governance: franchise cannot go below
  price_ceiling_cents INTEGER,          -- governance: franchise cannot exceed
  created_at       TIMESTAMPTZ DEFAULT NOW(),
  updated_at       TIMESTAMPTZ DEFAULT NOW()
)

product_categories (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name        TEXT NOT NULL,            -- Cupcake Bouquets | Cupcake Boxes | Macaron Gift Boxes
  slug        TEXT UNIQUE NOT NULL,
  description TEXT,
  sort_order  INTEGER DEFAULT 0,
  is_active   BOOLEAN DEFAULT true
)

-- Product images (multiple per product — BRD: "Product has more than one image")
product_images (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id  UUID NOT NULL REFERENCES products(id),
  url         TEXT NOT NULL,             -- Cloudflare R2 URL
  alt_text    TEXT,
  type        TEXT DEFAULT 'studio',     -- studio | customer
  sort_order  INTEGER DEFAULT 0,
  is_primary  BOOLEAN DEFAULT false,
  created_at  TIMESTAMPTZ DEFAULT NOW()
)

-- Size variants (6-count, 9-count, 12-count, 15-count)
product_variants (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id       UUID NOT NULL REFERENCES products(id),
  name             TEXT NOT NULL,          -- "6 Cupcakes", "12 Cupcakes"
  label            TEXT,                   -- "Best Deal", "Most Gifted"
  price_delta_cents INTEGER DEFAULT 0,     -- delta from base_price
  serves_min       INTEGER,
  serves_max       INTEGER,
  sort_order       INTEGER DEFAULT 0,
  is_active        BOOLEAN DEFAULT true
)

-- Modifier groups (Flavours, Add-ons, Presentation)
modifier_groups (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id   UUID NOT NULL REFERENCES products(id),
  name         TEXT NOT NULL,             -- "Flavours (Vanilla)", "Add a little extra"
  type         TEXT NOT NULL,             -- flavour | addon | presentation | wrap_colour
  is_required  BOOLEAN DEFAULT false,
  min_select   INTEGER DEFAULT 1,
  max_select   INTEGER DEFAULT 1,
  sort_order   INTEGER DEFAULT 0
)

-- Individual modifier options
modifier_options (
  id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  modifier_group_id UUID NOT NULL REFERENCES modifier_groups(id),
  name             TEXT NOT NULL,          -- "Vanilla", "It's a Girl", "Ceramic Vase"
  price_delta_cents INTEGER DEFAULT 0,
  colour_hex       TEXT,                   -- for wrap colours
  image_url        TEXT,                   -- for topper/addon images
  allergen_flags   TEXT[] DEFAULT '{}',
  is_gluten_free   BOOLEAN DEFAULT false,
  is_active        BOOLEAN DEFAULT true,
  sort_order       INTEGER DEFAULT 0
)

-- Branch-level availability rules (the 4-layer cascade)
branch_product_rules (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id    UUID NOT NULL REFERENCES branches(id),
  product_id   UUID NOT NULL REFERENCES products(id),

  -- Layer 2: branch carries this product?
  branch_enabled  BOOLEAN DEFAULT true,

  -- Layer 3: day-of-week schedule
  available_days  TEXT[] DEFAULT ARRAY['monday','tuesday','wednesday','thursday','friday','saturday'],

  -- Layer 4: date-specific overrides
  date_overrides  JSONB DEFAULT '[]',
  -- [{ "date": "2026-12-25", "available": false, "reason": "Christmas" }]

  -- Price override (within governance band)
  price_override_cents INTEGER,           -- null = use base_price

  -- Seasonal availability (BRD: "Product availability based on season date")
  available_from  DATE,                   -- null = always
  available_until DATE,                   -- null = always

  reason          TEXT,
  updated_by      UUID,
  updated_at      TIMESTAMPTZ DEFAULT NOW(),

  UNIQUE(branch_id, product_id)
)
```

---

## Availability Cascade Logic

```typescript
// Evaluated server-side in branch local timezone
// Most specific layer that expresses an opinion wins

async function isProductAvailable(
  productId: string,
  branchId: string,
  requestedDate: LocalDate
): Promise<{ available: boolean; reason?: string; nextAvailableDate?: Date }> {

  const rule = await getBranchProductRule(productId, branchId);
  const today = toLocalDate(new Date(), branch.timezone);

  // Layer 4 — date-specific override (highest priority)
  const dateOverride = rule.dateOverrides.find(o => o.date === requestedDate.toISO());
  if (dateOverride !== undefined) {
    return { available: dateOverride.available, reason: dateOverride.reason };
  }

  // Layer 3 — day-of-week schedule
  const dayName = requestedDate.weekdayLong.toLowerCase();
  if (!rule.availableDays.includes(dayName)) {
    return { available: false, reason: 'Not available on this day' };
  }

  // Layer 2 — branch carries this product
  if (!rule.branchEnabled) {
    return { available: false, reason: 'Currently unavailable at this location' };
  }

  // Seasonal dates (BRD requirement: "date specified for product to take orders")
  if (rule.availableFrom && requestedDate < LocalDate.parse(rule.availableFrom)) {
    return { available: false, reason: 'Not yet in season', nextAvailableDate: rule.availableFrom };
  }
  if (rule.availableUntil && requestedDate > LocalDate.parse(rule.availableUntil)) {
    return { available: false, reason: 'Season has ended' };
  }

  // Layer 1 — master catalogue
  const product = await getProduct(productId);
  if (!product.isActive) {
    return { available: false, reason: 'Product discontinued' };
  }

  return { available: true };
}
```

---

## Storefront Catalogue Endpoints

### Product Listing

#### `GET /api/v1/storefront/products`
```
Query params:
  branchId    UUID   (required — availability filtered per branch)
  categoryId  UUID   (optional)
  occasion    TEXT   (optional — birthday, anniversary, etc.)
  date        DATE   (optional — filter by availability on specific date)
  page        INT    default 1
  limit       INT    default 20
  sort        TEXT   must_gifted | price_asc | price_desc | newest

Response 200:
{
  "data": [
    {
      "id":          "uuid",
      "name":        "Berry Blossom Cupcake Bouquet",
      "slug":        "berry-blossom-cupcake-bouquet",
      "description": "Raspberry ruffles and blush roses...",
      "basePrice":   8800,             // cents
      "images": [
        { "url": "...", "type": "studio", "isPrimary": true, "altText": "..." },
        { "url": "...", "type": "customer" }
      ],
      "availability": {
        "available":       true,
        "nextAvailableDate": null
      },
      "isBestSeller":  true,
      "occasionTags":  ["birthday", "anniversary"],
      "variants":      [ { "id": "...", "name": "6 Cupcakes", "priceDelta": 0 } ... ],
      "categorySlug":  "cupcake-bouquets"
    }
  ],
  "meta": { "page": 1, "total": 9, "categoryBreakdown": { "cupcake-bouquets": 6, ... } }
}

NOTE: Unavailable items ARE returned but with availability.available=false
and a reason. Items are never silently omitted (CAT-07).
```

### Product Detail

#### `GET /api/v1/storefront/products/:slug`
```
Query: branchId UUID (required), date DATE (optional)

Response 200:
{
  "id":          "uuid",
  "name":        "Berry Blossom Cupcake Bouquet",
  "description": "...",
  "basePrice":   8800,
  "priceNote":   "Price varies based on size & customization",
  "images":      [ ... ],             // all images, sorted by sort_order
  "isBestSeller": true,
  "availableAprilNote": "Available Apr 27",   // from date override reason
  "availability": { "available": true },

  "variants": [
    { "id": "...", "name": "6 Cupcakes",  "label": "Best Deal · Serves 3-4", "priceDelta": 0,    "totalPrice": 8800 },
    { "id": "...", "name": "9 Cupcakes",  "label": "Mini Gifted · Serves 5-7","priceDelta": 0,    "totalPrice": 8800 },
    { "id": "...", "name": "12 Cupcakes", "label": "Table Centerpiece",        "priceDelta": 2100, "totalPrice": 10900 },
    { "id": "...", "name": "15 Cupcakes", "label": "Showstopper · Serves 12+", "priceDelta": 4100, "totalPrice": 12900 }
  ],

  "modifierGroups": [
    {
      "id":         "...",
      "name":       "Flavours",
      "type":       "flavour",
      "isRequired": true,
      "minSelect":  1,
      "maxSelect":  3,
      "options": [
        { "id": "...", "name": "Vanilla",              "priceDelta": 0,   "isGlutenFree": false },
        { "id": "...", "name": "Chocolate",            "priceDelta": 0,   "isGlutenFree": false },
        { "id": "...", "name": "Red Velvet",           "priceDelta": 0,   "isGlutenFree": false },
        { "id": "...", "name": "Strawberry",           "priceDelta": 0,   "isGlutenFree": false },
        { "id": "...", "name": "Gluten Free Vanilla",  "priceDelta": 300, "isGlutenFree": true  },
        { "id": "...", "name": "Gluten Free Chocolate","priceDelta": 300, "isGlutenFree": true  }
      ]
    },
    {
      "id":         "...",
      "name":       "Add a little extra",
      "type":       "addon",
      "isRequired": false,
      "minSelect":  0,
      "maxSelect":  5,
      "options": [
        { "id": "...", "name": "It's a Girl",     "priceDelta": 300, "imageUrl": "..." },
        { "id": "...", "name": "It's a Boy",      "priceDelta": 300, "imageUrl": "..." },
        { "id": "...", "name": "Birthday",        "priceDelta": 300, "imageUrl": "..." },
        { "id": "...", "name": "Congratulations", "priceDelta": 300, "imageUrl": "..." },
        { "id": "...", "name": "Butterfly Topper","priceDelta": 300, "imageUrl": "..." },
        { "id": "...", "name": "Macaron 06 Pack", "priceDelta": 300, "imageUrl": "..." }
      ]
    },
    {
      "id":   "...",
      "name": "Presentation",
      "type": "presentation",
      "isRequired": false,
      "options": [
        { "id": "...", "name": "Ceramic Vase", "label": "Reusable Fluted Vase Included", "priceDelta": 0 },
        { "id": "...", "name": "Gift Box",     "label": "Window Lid, Ribbon",            "priceDelta": 600 },
        { "id": "...", "name": "Woven Basket", "label": "Handled, Keepsake",             "priceDelta": 1200 }
      ]
    },
    {
      "id":   "...",
      "name": "Wrap Colour",
      "type": "wrap_colour",
      "isRequired": false,
      "options": [
        { "id": "...", "name": "Pink",     "colourHex": "#F9C0C0", "priceDelta": 0 },
        { "id": "...", "name": "Lavender", "colourHex": "#C8B4E8", "priceDelta": 0 },
        { "id": "...", "name": "Gold",     "colourHex": "#F5D998", "priceDelta": 0 },
        { "id": "...", "name": "Purple",   "colourHex": "#4B0082", "priceDelta": 0 },
        { "id": "...", "name": "Sage",     "colourHex": "#B2C9B4", "priceDelta": 0 }
      ]
    }
  ],

  "similarProducts": [ { "id": "...", "name": "...", "image": "...", "basePrice": 8800 } ... ],

  "allergens":   ["dairy", "eggs"],
  "dietary":     [],
  "freshness":   "Made fresh daily",
  "ingredients": "...",
  "returns":     "Due to the perishable nature..."
}
```

### Categories

#### `GET /api/v1/storefront/categories`
```
Query: branchId UUID (optional — filters to available categories)

Response 200:
{
  "data": [
    { "id": "...", "name": "Cupcake Bouquets",   "slug": "cupcake-bouquets",   "productCount": 6 },
    { "id": "...", "name": "Cupcake Boxes",       "slug": "cupcake-boxes",       "productCount": 4 },
    { "id": "...", "name": "Macaron Gift Boxes",  "slug": "macaron-gift-boxes",  "productCount": 3 }
  ]
}
```

---

## Admin Catalogue Endpoints

### Master Catalogue (Super Admin)

#### `GET /api/v1/admin/products`
#### `POST /api/v1/admin/products`
#### `GET /api/v1/admin/products/:id`
#### `PATCH /api/v1/admin/products/:id`
#### `DELETE /api/v1/admin/products/:id` — soft delete only

### Product Images

#### `POST /api/v1/admin/products/:id/images`
```
Body: multipart/form-data
Fields: file (image), altText, type (studio|customer), isPrimary

Behaviour:
  - Upload to Cloudflare R2
  - Create product_images record
  - If isPrimary=true, unset isPrimary on all other images for this product

Response 201: { "id": "...", "url": "...", "type": "studio" }
```

#### `DELETE /api/v1/admin/products/:productId/images/:imageId`
#### `PATCH /api/v1/admin/products/:productId/images/:imageId/sort`

### Branch Availability Control

#### `GET /api/v1/admin/branches/:branchId/availability`
```
Query: date DATE (optional — check specific date)

Response 200: availability grid — products as rows
{
  "data": [
    {
      "productId":      "uuid",
      "productName":    "Berry Blossom Cupcake Bouquet",
      "branchEnabled":  true,
      "availableDays":  ["monday","tuesday","wednesday","thursday","friday","saturday"],
      "todayAvailable": true,
      "seasonalDates": { "from": null, "until": null },
      "priceOverride":  null
    }
  ]
}
```

#### `PATCH /api/v1/admin/branches/:branchId/products/:productId/availability`
```
Body (any combination):
{
  "branchEnabled": false,
  "reason":        "Out of stock",
  "availableDays": ["monday","tuesday","wednesday","friday"],
  "priceOverrideCents": 9500,
  "availableFrom":  "2026-09-01",
  "availableUntil": "2026-11-30"
}

Side effects:
  - Storefront reflects within 30 seconds (cache invalidated via Redis pub/sub)
  - Audit event written
  - If branchEnabled=false: unavailable item still shows on storefront as greyed out

Response 200: updated rule object
```

#### `POST /api/v1/admin/branches/:branchId/products/:productId/date-override`
```
Body:
{
  "date":      "2026-12-25",
  "available": false,
  "reason":    "Christmas — closed"
}
Response 200: updated rule object
```

#### `POST /api/v1/admin/branches/:branchId/availability/bulk-disable`
```
Body: { "productIds": ["uuid1","uuid2"], "reason": "Ingredient shortage" }
Disables all listed products at this branch immediately.
Response 200: { "disabled": 2 }
```

### One-tap sold-out

#### `POST /api/v1/admin/branches/:branchId/products/:productId/sold-out`
```
Body: { "reason": "Sold out for today" }

Creates date_override for today with available=false
Reflects on storefront within 30 seconds

Response 200: { "success": true }
```

#### `POST /api/v1/admin/branches/:branchId/products/:productId/restore`
```
Removes today's sold-out override
Response 200: { "success": true }
```

---

## Storefront Dynamic Content (Admin-controlled)

Per BRD requirement: "website contents are dynamic — based on admin"

```sql
site_content (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  key          TEXT UNIQUE NOT NULL,   -- hero_headline, hero_subtitle, banner_text, etc.
  value        TEXT NOT NULL,
  type         TEXT DEFAULT 'text',   -- text | html | json | image_url
  updated_by   UUID,
  updated_at   TIMESTAMPTZ DEFAULT NOW()
)

-- e.g.
-- key: "hero_headline"        value: "Sweet Gifts That Bloom with Joy"
-- key: "hero_subtitle"        value: "Handcrafted to look like flowers..."
-- key: "marquee_text"         value: "NOW AVAILABLE | ELEGANT CUPCAKE BOUQUETS..."
-- key: "bestsellers_heading"  value: "Handpicked Favourites"
-- key: "subscribe_heading"    value: "First Dibs on Seasonal Bouquets—Every Sunday."
```

#### `GET /api/v1/storefront/content/:key`
#### `GET /api/v1/storefront/content?keys=hero_headline,marquee_text`
#### `PATCH /api/v1/admin/content/:key` (super admin only)

---

## Price Calculation

```typescript
function calculateOrderItemPrice(config: {
  basePrice:    number,  // cents
  variant:      ProductVariant,
  flavours:     ModifierOption[],
  addons:       ModifierOption[],
  presentation: ModifierOption | null,
  quantity:     number,
}): PriceBreakdown {
  const base     = config.basePrice + config.variant.priceDelta;
  const flavourExtra = config.flavours
    .filter(f => f.priceDelta > 0)
    .reduce((sum, f) => sum + f.priceDelta, 0);
  const addonTotal   = config.addons.reduce((sum, a) => sum + a.priceDelta, 0);
  const presTotal    = config.presentation?.priceDelta ?? 0;

  const unitTotal = base + flavourExtra + addonTotal + presTotal;

  return {
    base,
    flavourExtra,
    addonTotal,
    presentationExtra: presTotal,
    unitTotal,
    quantity:  config.quantity,
    lineTotal: unitTotal * config.quantity,
    // displayed on Figma as:
    // Base · 6 Cupcakes    $68.00
    // Premium Flavours      $3.00
    // Quantity              x 1
    // Total                $71.00
  };
}
```
