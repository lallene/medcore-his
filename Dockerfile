FROM golang:1.26-alpine AS builder

WORKDIR /app

ENV GOPROXY=https://proxy.golang.org,direct
ENV GOSUMDB=sum.golang.org
ENV CGO_ENABLED=0

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./

RUN go clean -modcache && go mod download -x

COPY . .

RUN go build -o medcore-api ./cmd/api

FROM alpine:latest

WORKDIR /app

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/medcore-api .

EXPOSE 8080

CMD ["./medcore-api"]