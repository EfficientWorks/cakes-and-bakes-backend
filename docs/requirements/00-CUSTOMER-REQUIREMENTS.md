# Baked Bouquet — Complete Customer-Facing Requirements

**Source:** BRD v5.0, Figma designs, meeting transcript, existing site analysis  
**Scope:** Everything a customer sees and does on bakedbouquet.com  
**Phase:** R1 (28 Aug 2026 milestone)

---

## What the Customer Experience Must Be

Customers are **gift buyers** — buying for someone else, often for a dated occasion, frequently on mobile, under time pressure. Their anxiety is not price — it is **whether it will arrive, on the right day, with the right name on it.** Every design and API decision must reduce that anxiety.

---

## 1. Homepage

### What the customer sees (from Figma images 1-3)
- Hero banner: "Sweet Gifts That Bloom with Joy" — 10K+ Satisfied Customers
- Marquee ticker: seasonal availability announcements
- Category cards: Bouquets, Cupcakes, Macarons
- Occasion collection: Birthday Bouquets, Anniversary Specials, Custom Bouquets, Thank You Gifts
- Best sellers carousel
- "How you want to receive your bouquet" — Store Pickup / Local Delivery
- Customer reviews (4.9/5 — 1,847 reviews)
- Social media feed
- Newsletter subscribe: "First Dibs on Seasonal Bouquets—Every Sunday"
- Footer: Quick Links, Products, Support, Locations

### Dynamic content requirements (admin-controlled)
All text on homepage is editable by super admin without a deployment:
- Hero headline and subtitle
- Marquee announcement text
- Featured occasion collections
- Best sellers selection
- Newsletter heading

### APIs needed
- `GET /api/v1/storefront/content?keys=hero_headline,marquee_text,...`
- `GET /api/v1/storefront/products?sort=must_gifted&limit=4` (best sellers)
- `GET /api/v1/storefront/categories`
- `POST /api/v1/storefront/newsletter/subscribe`

---

## 2. Delivery Eligibility Check

### Customer experience
- ZIP entry field on homepage above the fold
- Four states: delivery available ✓, pickup only, invalid ZIP, loading
- If delivery available: show delivery fee + free delivery threshold
- If not eligible: show 3 nearest branches with distance + pickup offer + email capture
- Same-day countdown: "Order within 2h 14m for delivery today"

### Figma reference (Image 10)
- Pickup modal: choose outlet (Edison NJ, North Wales PA, Trappe PA with distances), calendar, time slots
- Delivery modal: enter address, system validates, shows calendar and time slots on success

### APIs needed
- `POST /api/v1/storefront/check-delivery` — { postcode } → eligible/not
- `GET /api/v1/storefront/branches` — list branches with distance from ZIP
- `POST /api/v1/storefront/zone-coverage-notify` — email capture for future coverage

---

## 3. Shop Now / Product Listing

### Customer experience (Figma Image 4)
- Page heading: "Made to Gift, Baked Today"
- Tab navigation: ALL | CUPCAKE BOUQUETS | CUPCAKE BOXES | MACARON GIFT BOXES
- Product cards: image, name, price range, "Shop Now" button
- Sort: Most Gifted | Newest | Price
- Filter sidebar:
  - Occasion: Birthday, Anniversary, Thank You, New Baby, Sympathy
  - Dietary: Egg-free, Gluten Free, Vegan
  - Starting Price: slider up to $140
- Product count shown: "9 Products"

### Key rule
Design themes (Berry Blossom, Lavender Spring, etc.) = variations of a product, NOT separate catalogue entries. 6 products in a category, not 20.

### APIs needed
- `GET /api/v1/storefront/products?branchId=&categoryId=&occasion=&minPrice=&maxPrice=&sort=`
- `GET /api/v1/storefront/categories`

---

## 4. Product Detail Page

### Customer experience (Figma Images 5-8)
Page is a progressive configuration flow — one decision type shown at a time:

**Step 1 — Size (Required)**
```
Size (6 cupcakes)
○ 6 Cupcakes  — Best Deal · Serves 3-4        $68.00  ← selected
○ 9 Cupcakes  — Mini Gifted · Serves 5-7      $88.00
○ 12 Cupcakes — Table Centerpiece             $109.00
○ 15 Cupcakes — Showstopper · Serves 12+      $129.00
```

**Step 2 — Flavours (Required, multi-select)**
```
Flavours (Vanilla)
[✓ Vanilla] [Chocolate] [Red Velvet]
[Strawberry] [Gluten Free Vanilla] [Gluten Free Chocolate]
```

**Step 3 — Add a little extra (Optional)**
```
Add a little extra                              ● Optional
[It's a Girl $3] [It's a Boy $3] [Birthday $3] [Congratulations $3]
[Butterfly Topper $3] [Macaron 06 Pack $3]
```

**Step 4 — Presentation (Optional)**
```
Presentation (Ceramic Vase)                     ● Optional
○ Ceramic Vase — Reusable Fluted Vase Included
○ Gift Box — Window Lid, Ribbon                 $6.00
○ Woven Basket — Handled, Keepsake              $12.00

Wrap Colour: [Pink] [Lavender] [Gold] [Purple] [Sage]
```

**Price summary (live update)**
```
Base · 6 Cupcakes    $68.00
Premium Flavours      $3.00
Quantity              x 1
                   ─────────
Total               $71.00

[–] [1] [+]    [Add To Cart — $71.00]
```

**Gift toggle**
```
☑ Make This As A Gift
  Recipient's name: [Who is it for?]
  Gift message:     [Write your message here — we'll letter it onto the card]
  
  Quick suggestions:
  [Happy birthday — eat the flowers.] [Congratulations. Thoroughly deserved.]
  [Thinking of you today.] [Thank you, genuinely.]
```

**Fulfillment (Required)**
```
How will you get it                            ● Required
[PICKUP] [DELIVERY ← selected]

Delivery:                           [CHANGE ADDRESS ✕]
Delivery Address         Delivery Date
80 Lafayette Ave Edison, Apr 24, 2026
New Jersey 08837         02:00 PM – 03:00 PM
```

**Accordions at bottom**
- Product Description
- Ingredients & Allergens
- Freshness & Storage
- Returns

**Similar Products**
- "You May Also Love — These Freshly Baked Favorites"

### Rules
- Configuration state preserved if customer goes back (SF-11)
- Required steps block advancement with inline errors (SF-12)
- Price updates live as configuration changes (SF-13)
- Gift message max 120 characters (SF-14)
- Recipient name in gift mode: mandatory field labelled "Who is it for?" (SF-15)
- "This is for me" shortcut pre-fills from logged-in user profile (SF-16)

### APIs needed
- `GET /api/v1/storefront/products/:slug?branchId=&date=`
- `GET /api/v1/storefront/slots?branchId=&fulfilmentType=&startDate=&days=14`
- `GET /api/v1/storefront/gift-messages?occasion=birthday`
- `POST /api/v1/storefront/cart/items`

---

## 5. Cart

### Customer experience (Figma Image 11)
Cart auto-opens on add-to-cart (slides from right on desktop, bottom on mobile).

**Left panel: Cart items**
```
Your Cart                              03 Items

[img] Summer Sunset Cupcake Bouquet              $264.00
      06 Cupcakes · Vanilla            Edit | Remove
      [–] 1 [+]

[img] Golden Glow Cupcake Bouquet                $89.00
      06 Cupcakes · Vanilla            Edit | Remove
      [–] 1 [+]

[img] Royal Bloom Cupcake Box                    $53.00
      06 Cupcakes · Vanilla            Edit | Remove
      [–] 1 [+]

Add More items to Make it Extra Special
[It's a Girl $3] [It's a Boy $3] [Birthday $3] [Congratulations $3]
[Butterfly Topper $2.50] [Macaron 06 Pack $21] [Macaron 12 Pack $38]

[CONTINUE SHOPPING]
```

**Right panel: Order summary**
```
[PICKUP]   [DELIVERY ←]

Delivery:                    [CHANGE ADDRESS ✕]
Delivery Address    Delivery Date
80 Lafayette Ave    Apr 24, 2026
Edison, NJ 08837    02:00 PM - 03:00 PM

Gift Cards or Coupons:
[Gift card or promo code]  [Apply]

Add a Tip:
[None ← ] [15% $51.90] [18% $62.90] [25% $86.90]

Amount Summary
Subtotal:              $346.00
Delivery Fee:          Free
Estimated taxes
(Edison, NJ) ⓘ        $22.92
Tip (15%) ⓘ           $51.90
                   ──────────
Estimated Order Total  $420.82

🔒 Secure checkout · Freshly Baked Guarantee

[PROCEED TO CHECKOUT]
```

### Rules
- Cart scoped to one branch (SF-19)
- Upsell add-ons shown in cart (CX-06)
- Free delivery threshold shown inline

### APIs needed
- `GET /api/v1/storefront/cart`
- `PATCH /api/v1/storefront/cart/items/:id`
- `DELETE /api/v1/storefront/cart/items/:id`
- `POST /api/v1/storefront/cart/discount`
- `POST /api/v1/storefront/cart/tip`

---

## 6. Checkout Page

### Customer experience (Figma Image 12)

**Left panel: Check Out form**
```
Check Out

Who is this for?
We'll print the recipient's name and gift message on the card or letter to the right person.

[  ] This is for me   (toggle)

Receiver Information
First Name [          ]  Last Name [          ]
[US +1 ▼] Phone Number    Email for Receipt

Gift Message
[Write your message here]

─────────────────────────────

Purchaser Information *
First Name [          ]  Last Name [          ]
[US +1 ▼] Phone Number    Email for Receipt
[ ] Keep me updated with new products and offers.

─────────────────────────────

Payment Methods
(All transactions are encrypted)
[Card]  [Google Pay]
  ○ Pay by card
  Card Number [                    ]
  Expiration Date [      ]  Security Code [   ]
  Name on Card [          ]
```

**Right panel: Order Summary**
```
[Delivery]          [CHANGE ADDRESS ✕]
Delivery Address    Delivery Date
...                 Apr 24, 2026 · 02:00 PM - 03:00 PM

Order Summary
[img] Berry Blossom Cupcake Bouquet   $264.00
      Qty: 31

Delivery Instruction
Add notes to the Delivering Partner
[Tip: Call Arrival, Leave at Door, Gate Code...]

Gift Cards or Coupons:
[Gift card or promo code]    [Apply]

Add a Tip: [None] [15% $51.90] [18% $62.90] [25% $86.90]

Amount Summary
Subtotal:           $346.00
Delivery Fee:       Free
Estimated taxes:    $22.92
Tip (15%):          $51.90
                ──────────
Estimated Order Total: $420.82

🔒 Secure checkout · Freshly Baked Guarantee

[PROCEED TO CHECKOUT]
```

### Critical rules
- **Recipient section FIRST** — prominently labelled "Who is this for?" (SF-22)
- Purchaser section below recipient
- All costs visible before payment section (SF-23)
- Marketing opt-in unchecked by default (SF-24)
- Guest checkout supported (SF-29)
- No price introduced after card entry

### APIs needed
- `POST /api/v1/storefront/checkout/validate`
- `POST /api/v1/storefront/payments/intent`
- `POST /api/v1/storefront/orders` (with Idempotency-Key)

---

## 7. Order Confirmation Page

### Customer experience (Figma Image 13)
```
✓ Order Confirmed

Thanks for your purchase! Your order is in the kitchen,
and the receipt has been sent to tim@example.com.
You'll get a text when your bouquet leaves the studio.

[Track Order ●]  [Continue Shopping]

[Items listed]                            [Order Details panel]
Summer Sunset Cupcake Bouquet  $53.00     Order ID:       #12345677
Golden Glow Cupcake Bouquet    $53.00     Recipient Name: John Kelwin
Royal Bloom Cupcake Box        $53.00     Delivery Date:  May 23, 2026 · 11:00 PM – 1:00 PM
Eterna Bloom Cupcake Bouquet   $53.00     Delivery Address: 412 Garden St, Hoboken NJ 07030

                                          Order Summary:
                                          Subtotal:  $240.00
                                          Delivery:  Free
                                          Tax:       $20.00
                                          Total:     $360.00
```

### Rules
- Recipient name shown prominently in Order Details (CX-01)
- Order ID and tracking link generated
- Confirmation email sent automatically (SF-31)
- All amounts in USD

---

## 8. Order Tracking Page (Public)

Accessible via secure link without login — safe to share with recipient (SF-32).

```
https://bakedbouquet.com/track/EZYODR123

Order #EZYODR123
For: John K.
Status: Your bouquet is being prepared 🎂

Timeline:
✓ Order placed — Aug 22, 2026 2:00 PM
✓ Order confirmed — Aug 22, 2026 2:01 PM  
● Being prepared — Aug 24, 2026 9:00 AM
○ Ready for delivery
○ Delivered
```

---

## 9. Customer Account

### Registration flow (Figma Images 16-18)

**Step 1 — Credentials**
Email, Password, Confirm Password → [Next]

**Step 2 — Personal details**
First Name, Last Name, Mobile Number → [Next]

**Step 3 — Address**
Country, State, City, Post Code, Address → [Submit]

**Login:** Email + Password with Forgot Password → OTP verification

**Forgot password:** Email → OTP sent → Enter OTP → Reset password

### Profile page (Figma Image 19 - left)
```
My Account
Guru Santhosh
  Profile ←
  Order History
  Address
  Log Out

Profile:
First Name [Guru]     Last Name [Santhosh]
Mobile Number [123456789]  E-mail ID [santhosh@gmail.com]
[Change Password]                        [SAVE & UPDATE]

[Delete Account]
```

### Order History page (Figma Image 19 - right)
```
My Account
Guru Santhosh

Order History:
EZYODR123              ● Order Placed    [Cancel Order]
Placed at 2nd Aug 2026, 12:30 PM
Total Items: 3                            £29.00   ← NOTE: must be $ not £

EZYODR125              ● Delivered       [View Details]
Delivered at 2nd Aug 2026, 12:30 PM
Total Items: 8                            £295.00  ← must be $ not £
```

> ⚠️ **Currency defect to correct:** Figma shows £ (GBP). All amounts must display in $ (USD). This is a design defect — not an implementation choice.

---

## 10. Custom Orders Form

### Customer experience (Figma Image 14)
```
Custom Orders

Need a Custom Order?
Looking to make a big impression?...

CONTACT US:
Edison:      732-903-4600
North Wales: 732-903-4600
Trappe:      732-903-4600

Complete the form below:
Name *              Email *
Mobile Number *     Event Location *
Event Date *        Event Time *
Event Type *▼       Guest Count *▼
Select a Store Location *▼
Additional Information

[Submit]
```

---

## 11. Franchise Page

### Customer experience (Figma Image 15)
```
Baked Bouquet
Bring Edible Art to Life in Your City

Since 2014, Baked Bouquet® has changed the way people gift...

With our Edison, NJ location thriving and new stores opening:
[North Wales, PA] [Trappe, PA] [Cinnaminson, NJ] [Miami, FL] [Tampa, FL]

Why Own a Baked Bouquet?
+ Proven, One-of-a-Kind Concept
+ Custom Gifting Business with Demand Year-Round
+ Streamlined Online Ordering System
+ Work-Life Balance Built In
```

Franchise form fields: Name, Email, Phone, Location, Investment timeline, Message.

---

## 12. All Supporting Pages

| Page | URL | Editable by |
|---|---|---|
| Returns Policy | /returns | Super admin (CMS) |
| FAQ | /faq | Super admin (CMS) |
| Privacy Policy | /privacy | Super admin (CMS) |
| Terms & Conditions | /terms | Super admin (CMS) |
| Contact Us form | /contact | - |
| Custom Orders form | /custom-orders | - |
| Franchise inquiry | /franchise | - |
| Per-branch landing | /locations/:slug | Super admin + branch |

---

## 13. Footer Content

From Figma Image 9:
```
Quick Links: Home · Shop Now · Custom Order · Franchise
Our Products: Bouquet · Cup Cakes · Macarons · Mother's Day Special
Support: Contact · Terms of Service · Privacy Policy · Return Policy
Our Locations: ● Edison, New Jersey · ● Jersey City, New Jersey · ● Philadelphia, Pennsylvania

© 2026 Baked Bouquet. All rights reserved. Designed and Developed by Efficient Works.
[Refund Policy] [Privacy Policy] [Terms and Conditions]
```

---

## 14. Complete API List — Customer Facing

### Authentication
- `POST /api/v1/auth/register` (step 1)
- `POST /api/v1/auth/register/profile` (step 2)
- `POST /api/v1/auth/register/address` (step 3)
- `POST /api/v1/auth/verify-otp`
- `POST /api/v1/auth/resend-otp`
- `POST /api/v1/auth/login`
- `POST /api/v1/auth/refresh`
- `POST /api/v1/auth/logout`
- `POST /api/v1/auth/forgot-password`
- `POST /api/v1/auth/verify-reset-otp`
- `POST /api/v1/auth/reset-password`

### Account
- `GET /PATCH /api/v1/account/profile`
- `POST /api/v1/account/change-password`
- `DELETE /api/v1/account`
- `GET /POST /PATCH /DELETE /api/v1/account/addresses`
- `POST /api/v1/account/addresses/:id/set-default`
- `GET /POST /PATCH /DELETE /api/v1/account/recipients`
- `GET /api/v1/account/orders`
- `GET /api/v1/account/orders/:orderId`
- `POST /api/v1/account/orders/:orderId/cancel`
- `POST /api/v1/account/orders/:orderId/reorder`
- `PATCH /api/v1/account/marketing`
- `POST /api/v1/account/claim-order`

### Storefront Discovery
- `GET /api/v1/storefront/categories`
- `GET /api/v1/storefront/products`
- `GET /api/v1/storefront/products/:slug`
- `POST /api/v1/storefront/check-delivery`
- `GET /api/v1/storefront/branches`
- `POST /api/v1/storefront/zone-coverage-notify`
- `GET /api/v1/storefront/content`
- `POST /api/v1/storefront/newsletter/subscribe`

### Scheduling
- `GET /api/v1/storefront/slots`

### Cart
- `GET /api/v1/storefront/cart`
- `POST /api/v1/storefront/cart/items`
- `PATCH /api/v1/storefront/cart/items/:id`
- `DELETE /api/v1/storefront/cart/items/:id`
- `POST /api/v1/storefront/cart/discount`
- `DELETE /api/v1/storefront/cart/discount`
- `POST /api/v1/storefront/cart/tip`

### Checkout & Orders
- `POST /api/v1/storefront/checkout/validate`
- `POST /api/v1/storefront/payments/intent`
- `POST /api/v1/storefront/orders`
- `GET /api/v1/orders/track/:reference`

### Gift & Content
- `GET /api/v1/storefront/gift-messages`

### Forms
- `POST /api/v1/storefront/forms/custom-order`
- `POST /api/v1/storefront/forms/franchise`
- `POST /api/v1/storefront/forms/contact`

---

## 15. Key Behaviours Summary

| # | Behaviour | Source |
|---|---|---|
| 1 | ZIP check on homepage — 4 states, never a dead end | BRD SF-01, SF-02, SF-03 |
| 2 | Cart auto-opens on every add-to-cart | BRD SF-17, Figma |
| 3 | Recipient section first and visually dominant in checkout | BRD SF-22 |
| 4 | All costs shown before card entry — nothing added after | BRD SF-23 |
| 5 | Products show as unavailable on storefront — never silently removed | BRD CAT-07 |
| 6 | Availability change reflects within 30 seconds | BRD CAT-06 |
| 7 | Slots shown are only slots the branch can actually fulfil | BRD SF-20 |
| 8 | Product has multiple images (studio + customer photos) | BRD stated, Figma |
| 9 | Seasonal/date-based product availability enforced | BRD requirement |
| 10 | All amounts in USD (not GBP — Figma defect to fix) | BRD |
| 11 | Gift message on every ticket — order cannot be fulfilled without it | BRD CX-01 |
| 12 | Guest checkout always available — account never required | BRD SF-29 |
| 13 | Customer accounts are brand-level — one login across all branches | BRD ACC-17 |
| 14 | Progressive product configuration — one decision per step | BRD SF-10 |
| 15 | Slot capacity reserved atomically — no overselling | BRD SCH-04, NFR-05 |
