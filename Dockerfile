# syntax=docker/dockerfile:1

# --- build ------------------------------------------------------------------

FROM golang:1.25-alpine AS builder

WORKDIR /src

# Dependencies first: this layer stays cached until go.mod/go.sum change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# -buildvcs=false: .git is excluded by .dockerignore and git is not installed
# here, so version stamping would fail the build rather than skip.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
        -ldflags="-s -w" -o /out/server ./cmd/server

# The seeder ships alongside it so a fresh environment can be bootstrapped from
# the platform console, without a second image or a tunnel to the database. It
# refuses to run outside local unless explicitly unlocked — see cmd/seed.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
        -ldflags="-s -w" -o /out/seed ./cmd/seed

# --- runtime ----------------------------------------------------------------

FROM alpine:3.22

# tzdata is not optional: branches carry an IANA timezone and scheduling,
# catalogue availability and order cut-offs all call time.LoadLocation on it.
# Without the zone database every branch write fails validation.
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 app

WORKDIR /app

COPY --from=builder /out/server ./server
COPY --from=builder /out/seed ./seed

# db.Migrate reads .sql files from disk relative to the working directory at
# startup, so the migrations ship alongside the binary.
COPY db/migrations ./db/migrations

USER app

EXPOSE 8080

CMD ["./server"]
