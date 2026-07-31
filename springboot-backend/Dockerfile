FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/fluxd ./cmd/fluxd

FROM alpine:3.21
RUN apk add --no-cache ca-certificates su-exec tzdata \
    && addgroup -S flux \
    && adduser -S -G flux flux \
    && mkdir -p /app/data \
    && chown -R flux:flux /app
WORKDIR /app
COPY --from=build /out/fluxd /app/fluxd
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod 0755 /usr/local/bin/docker-entrypoint.sh
ENV HTTP_ADDR=:6365 \
    DB_PATH=/app/data/gost.db \
    LOG_LEVEL=info
EXPOSE 6365
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=5 \
    CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:6365/health/ready || exit 1
ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["/app/fluxd"]
