# MedCore HIS backend images (LOT 26I-1).
#
# Named BuildKit targets:
#   api                   — HTTP API (binary: medcore-api)
#   notification-worker   — appointment notification worker
#                           (binary: medcore-notification-worker)
#
# The final stage is `api`, so `docker build` without --target continues to
# produce the API image (backward compatible with this repository's history).
#
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
	&& go build -o medcore-notification-worker ./cmd/notification-worker

# ---- Notification worker runtime -------------------------------------------
FROM alpine:latest AS notification-worker

WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/medcore-notification-worker .

# No HTTP port. SIGINT/SIGTERM handled by cmd/notification-worker.
CMD ["./medcore-notification-worker"]

# ---- API runtime (final / default stage) -----------------------------------
FROM alpine:latest AS api

WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/medcore-api .

EXPOSE 8080

CMD ["./medcore-api"]
