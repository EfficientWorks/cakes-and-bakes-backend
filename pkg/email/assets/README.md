# Email assets

Files here are compiled into the server binary and served by the API.

- `logo-white.png` — the white Baked Bouquet wordmark shown in the purple band
  of every email. Served at `GET /email/logo-white.png` and linked from emails
  as `<PUBLIC_BASE_URL>/email/logo-white.png`.

  Export it from the Figma email frames (white on transparent, ~300px wide so
  it stays sharp at the 150px it is displayed at) and save it here with exactly
  this name. Until it exists, emails show the name set in white type instead of
  a broken image.

  `EMAIL_LOGO_URL` still overrides this if the logo is hosted elsewhere (a CDN).
