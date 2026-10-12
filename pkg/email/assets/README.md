# Email assets

Files here are compiled into the server binary and served by the API.

- `logo-white.png` (optional) — the platform's own white logo, used in the
  header band of emails that belong to no business. Served at
  `GET /email/logo-white.png`. `EMAIL_LOGO_URL` overrides it.

A business's emails carry the business's own logo (its `logoUrl`, set through
the platform API) or, without one, its name set in white type.
