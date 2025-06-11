# Development
FROM golang:1.24.3-alpine@sha256:be1cf73ca9fbe9c5108691405b627cf68b654fb6838a17bc1e95cc48593e70da AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o exam

# Production
FROM alpine:3.21@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c

RUN apk add --no-cache ca-certificates

RUN addgroup -S app && adduser -S -G app app

ENV APP_PORT=8080

WORKDIR /app
COPY --from=builder /app/exam .

USER app

EXPOSE ${APP_PORT}

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:${APP_PORT}/_internal/health || exit 1

ENTRYPOINT ["./exam"]