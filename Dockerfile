# Multi-stage Dockerfile for FocusDB
# Builder: compile a static Go binary optimized for minimal RAM
FROM golang:1.25-alpine AS builder
WORKDIR /src

# Install git and CA certs for fetching modules and TLS-aware binaries
RUN apk add --no-cache git ca-certificates

# Cache go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy only required source files for the server binary.
# This avoids sending tests/docs/dev artifacts into the builder stage.
COPY cmd/focusd ./cmd/focusd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -trimpath \
    -ldflags="-s -w" \
    -buildvcs=false \
    -o /out/focusd ./cmd/focusd

# Final image: minimal runtime image
FROM alpine:3.19
RUN apk add --no-cache ca-certificates && \
    adduser -D -H -s /sbin/nologin focusdb && \
    mkdir -p /data && \
    chown -R focusdb:focusdb /data

COPY --from=builder /out/focusd /usr/local/bin/focusd
RUN chmod +x /usr/local/bin/focusd

USER focusdb
# 4444 = PostgreSQL wire protocol (clients psql/pgAdmin)
# 10000 = HTTP health check (Render scans this via $PORT)
EXPOSE 4444 10000
VOLUME ["/data"]

# GOGC=50: Garbage collection after every 50% heap growth (vs 100% default)
# GOMEMLIMIT=256MiB: soft limit - triggers GC before OOM; keeps 256MB for
# Go heap leaving the remaining ~250MB for OS, Pebble indices, and buffers.
# 80MiB was too tight for LoadAll() peaks; 256MiB fits real workloads.
ENV GOGC=50
ENV GOMEMLIMIT=256MiB

# Minimal config:
# max-conns=1: Only 1 concurrent connection
# buf-size=512: Small read/write buffers per connection
ENTRYPOINT ["/usr/local/bin/focusd", "-max-conns", "1", "-buf-size", "512", "-data", "/data"]
CMD []
