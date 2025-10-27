FROM golang:1.24-alpine AS builder
WORKDIR /build
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -a \
    -installsuffix cgo \
    -ldflags="-s -w -extldflags '-static'" \
    -trimpath \
    -o bot \
    ./cmd/bot

FROM gcr.io/distroless/static-debian11:nonroot
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
WORKDIR /app
COPY --from=builder /build/bot ./bot
COPY --from=builder /build/scripts/migrations ./scripts/migrations
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD ["/app/bot", "--health-check"] || exit 1
ENTRYPOINT ["/app/bot"]

