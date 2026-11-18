# Home page: "Discover our flavours"

Backend for the home page section with the eyebrow **Discover our flavours**, the heading
**A Flavour for Every Sweet Moment**, and a 2 × 2 grid of cards (Chocolate, Vanilla, Fruit,
Indian-Inspired). Each card has a photo, a name, a short description, a tinted background and a
**Shop now →** link.

Staff create the flavours in admin and map products to them. The website shows only the
flavours that have products on sale.

## Website

### 1. Load the cards

```
GET /v1/storefront/flavours
X-Business: <business slug>
```

```json
{
  "success": true,
  "statusCode": 200,
  "data": [
    {
      "id": "5f0c…",
      "name": "Chocolate",
      "slug": "chocolate",
      "description": "Rich sponges, drips and dark glazes for serious chocolate lovers.",
      "imageUrl": "https://…/v1/media/…",
      "imageAltText": "Chocolate drip cake with gold truffles",
      "backgroundColour": "#F8E8E8",
      "sortOrder": 1,
      "status": "active",
      "productCount": 6
    }
  ]
}
```

- The list is already filtered: active flavours that have at least one active product. It is
  already in display order (`sortOrder`, then name).
- **Hide the whole section when `data` is empty.**
- These fields can be `null`:
  - `description`: show only the name.
  - `imageUrl`: use a placeholder photo.
  - `imageAltText`: fall back to the name.
  - `backgroundColour`: cycle the design's tints in order: pink `#F8E8E8`, blue `#E4E8F1`,
    rose `#F6DDE1`, cream `#F3EBDD`.
- **Layout:** the design shows four cards, two per row with the photo on the left. More cards
  continue the grid; with one card it spans the row.

### 2. "Shop now →" link

Link each card to the shop with the flavour's slug:

```
/shop?flavour=<slug>
```

The products endpoint now accepts the same filter. It can be repeated or comma-separated, and
matches any of the given flavours:

```
GET /v1/storefront/products?flavour=chocolate&branchId=…
```

It combines with the other filters (category, colour, sponge, tag…) like they combine with each
other. To support it, the Shop page passes `flavour` from its URL to this request, the same way it
passes `tag`.

### 3. Heading text (optional)

The eyebrow and heading can be edited without a deploy through the existing site content:

| Key | Default to use when missing |
| --- | --- |
| `flavours_eyebrow` | Discover our flavours |
| `flavours_heading` | A Flavour for Every Sweet Moment |

To read them, add both keys to the website's `CONTENT_KEYS` (`types/catalog.ts`), which
`GET /v1/storefront/content` returns. Staff set them with `PATCH /v1/admin/content/{key}`.

## Admin

Requires the master catalogue permission (super admin), like seasonal tags.

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/v1/admin/flavours?status=active\|inactive&search=` | List, in card order, with `productCount` |
| GET | `/v1/admin/flavours/{flavourId}` | One flavour, with `productIds` |
| POST | `/v1/admin/flavours` | Create |
| PUT / PATCH | `/v1/admin/flavours/{flavourId}` | Update (send every field) |
| DELETE | `/v1/admin/flavours/{flavourId}` | Delete; its products are kept, only unmapped |

Request body for POST and PUT:

```json
{
  "name": "Chocolate",
  "description": "Rich sponges, drips and dark glazes for serious chocolate lovers.",
  "imageUrl": "https://…/v1/media/…",
  "imageAltText": "Chocolate drip cake with gold truffles",
  "backgroundColour": "#F8E8E8",
  "sortOrder": 1,
  "status": "active",
  "productIds": ["…", "…"]
}
```

- **Products are mapped from the flavour**, so the product form does not change. On PUT,
  `productIds` replaces the list; leave it out to keep the current products, or send `[]` to
  unmap them all.
- **Photo:** upload it first with the existing `POST /v1/admin/images/upload-url` flow, as for
  category images, then send the resulting URL as `imageUrl`.

Validation errors come back in the usual shape, with `fields[].field`:

| Field | Rule |
| --- | --- |
| `name` | Required; up to 60 characters; must contain a letter or number |
| `description` | Up to 200 characters |
| `backgroundColour` | `#RRGGBB` |
| `sortOrder` | 0 or more |
| `status` | `active` or `inactive` (default `active`) |
| `productIds` | Every id must be one of this business's products (`NOT_FOUND`) |

A name already used by another flavour returns **409 `FLAVOUR_NAME_TAKEN`**.

## Database

Migration `0102_flavours.sql` adds two tables: `flavours` (per business) and
`product_flavours` (which products each flavour lists). It runs when the server starts.
