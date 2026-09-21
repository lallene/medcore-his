# MedCore HIS backend images (LOT 26I-1 / 26I-3).
#
# Named BuildKit targets:
#   api                   — HTTP API (binary: medcore-api)
#   notification-worker   — appointment notification worker
#                           (binary: medcore-notification-worker)
#   migrate               — schema owner only (binary: medcore-migrate)
#
# The final stage is `api`, so `docker build` without --target continues to
# produce the API image (backward compatible with this repository's history).
#
# Production rollout: run migrate successfully once, then start API and worker.
# Do not bake secrets or runtime env into the image. Configure at container start.

FROM golang:1.26-alpine AS builder

WORKDIR /app

ENV GOPROXY=https://proxy.golang.org,direct
ENV GOSUMDB=sum.golang.org
ENV CGO_ENABLED=0

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./

RUN go clean -modcache && go mod download -x

COPY . .

RUN go build -o medcore-api ./cmd/api \
	&& go build -o medcore-notification-worker ./cmd/notification-worker \
	&& go build -o medcore-migrate ./cmd/migrate

# ---- Schema migration (LOT 26I-3 sole schema owner) ------------------------
FROM alpine:latest AS migrate

WORKDIR /app

# ca-certificates for TLS; tzdata for IANA zones (MEDCORE_*_TIMEZONE).
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/medcore-migrate .

# No HTTP port. Exit after schema apply (fail closed).
CMD ["./medcore-migrate"]

# ---- Notification worker runtime -------------------------------------------
FROM alpine:latest AS notification-worker

WORKDIR /app

# ca-certificates for TLS; tzdata for IANA zones (MEDCORE_*_TIMEZONE).
# wget: minimal HEALTHCHECK probe for GET /readyz (LOT 26I-4).
RUN apk add --no-cache ca-certificates tzdata wget

COPY --from=builder /app/medcore-notification-worker .

# Health HTTP (stdlib): GET /healthz (liveness), GET /readyz (readiness).
# Same env drives process listener and Docker HEALTHCHECK (LOT 26I-4 Option B).
ENV NOTIFICATION_WORKER_HEALTH_PORT=8081

# EXPOSE is image metadata for the default port only; runtime may use another
# NOTIFICATION_WORKER_HEALTH_PORT. Publish (-p) still required for host access.
EXPOSE 8081

# Docker has a single health state; probe /readyz (worker started + DB reachable).
# Shell form so ${NOTIFICATION_WORKER_HEALTH_PORT} expands at probe time.
# Kubernetes should use liveness=/healthz and readiness=/readyz separately.
# Conservative timings: do not tie liveness to the default 2s poll interval.
HEALTHCHECK --interval=10s --timeout=2s --start-period=20s --retries=3 \
	CMD wget -qO- http://127.0.0.1:${NOTIFICATION_WORKER_HEALTH_PORT}/readyz || exit 1

# SIGINT/SIGTERM handled by cmd/notification-worker.
CMD ["./medcore-notification-worker"]

# ---- API runtime (final / default stage) -----------------------------------
FROM alpine:latest AS api

WORKDIR /app

# ca-certificates for TLS; tzdata for IANA zones (MEDCORE_*_TIMEZONE).
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/medcore-api .

EXPOSE 8080

CMD ["./medcore-api"]
