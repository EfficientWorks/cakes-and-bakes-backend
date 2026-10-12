# Deployment

## Target: Railway (launch)

Single server. One Postgres plugin (holding `bb_global` + `bb_us`).
One Railway service per Go service. No Terraform. No AWS/GCP.

## Services deployed on Railway

| Railway service | Port | Environment |
|---|---|---|
| `bb-gateway` | 8080 (public) | `PORT`, `ENV=production`, all service URLs |
| `bb-identity` | 8081 (internal) | DB URLs, JWT keys |
| `bb-franchise` | 8082 (internal) | DB URLs |
| `bb-catalog` | 8083 (internal) | DB URL |
| `bb-orders` | 8084 (internal) | DB URL, Google Maps key |
| `bb-scheduling` | 8085 (internal) | DB URL |
| `bb-payments` | 8086 (internal) | DB URL, Stripe keys |
| `bb-admin` | 8087 (internal) | DB URL |
| `bb-realtime` | 8088 (internal) | no DB |

Only `bb-gateway` is public-facing. All other services communicate internally
via Railway's private network (ConnectRPC over HTTP/2).

## Frontends

| Surface | Hosting | Domain |
|---|---|---|
| Customer website | Vercel | `bakedbouquet.com` |
| Admin panels | Vercel | `admin.bakedbouquet.com` |

Vercel auto-deploys on push to `main`. Preview deployments per PR.

## DNS + CDN

Cloudflare proxies all public domains:
- `bakedbouquet.com` → Vercel
- `admin.bakedbouquet.com` → Vercel
- `api.bakedbouquet.com` → Railway gateway

Cloudflare handles SSL, DDoS protection, and edge caching for static assets.

## Flutter app distribution

| App | Platform | Distribution |
|---|---|---|
| POS | Windows | MSIX installer (EV signed) |
| POS | iPad / Android tablet | App Store + Play Store |
| KDS | Windows | MSIX installer |
| KDS | Android TV | APK (sideloaded) |

Built by GitHub Actions on tagged release. EV certificate: order in week 1 (2-4 week lead time).

Auto-update check: Flutter app calls `GET /v1/app/version` on startup. Returns latest
version number and download URL. App prompts if behind.

## Environments

| Env | Gateway URL | DB |
|---|---|---|
| `development` | `http://localhost:8080` | Docker Compose |
| `staging` | `https://api-staging.bakedbouquet.com` | Railway staging project |
| `production` | `https://api.bakedbouquet.com` | Railway production project |

Vercel: preview per PR → staging on `main` → production on tagged release.

## Migration on deploy

Railway runs migrations as part of the deployment start command:

```bash
# Each service's Dockerfile / Railway start command:
goose -dir db/migrations/global postgres $DATABASE_GLOBAL_URL up && \
goose -dir db/migrations/regional postgres $DATABASE_US_URL up && \
./server
```

## Migration to AWS/GCP (when needed)

Scale trigger: monthly orders > ~50k OR team hires a platform engineer.

Steps (zero application code change needed):
1. Export Go service Docker images from Railway → push to ECR/Artifact Registry.
2. Move Postgres to AWS RDS / Cloud SQL (same Postgres 16, same connection string format).
3. Move Redis (when added) to ElastiCache / Upstash.
4. Deploy Go containers to ECS Fargate / Cloud Run.
5. Update environment variable `DATABASE_*_URL` and `INTERNAL_*_URL` values.
6. Flutter apps: only `API_BASE_URL` changes.
7. Vercel stays — no migration needed for frontends.
