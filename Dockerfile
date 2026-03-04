# Multi-stage Dockerfile for FocusDB
# Builder: compile a static Go binary optimized for minimal RAM
FROM golang:1.25-alpine AS builder
WORKDIR /src

# Install git and CA certs for fetching modules and TLS-aware binaries
RUN apk add --no-cache git ca-certificates

# Cache go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build with aggressive optimizations for memory efficiency
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -trimpath \
    -ldflags="-s -w" \
    -gcflags="all=-l=4" \
    -o /out/focusd ./cmd/focusd

# Final image: minimal runtime image
FROM alpine:3.19
RUN apk add --no-cache ca-certificates && \
    adduser -D -H -s /sbin/nologin focusdb && \
    mkdir -p /data && \
    chown -R focusdb:focusdb /data

COPY --from=builder /out/focusd /usr/local/bin/focusd
RUN chmod +x /usr/local/bin/focusd

# Health check for Render deployment
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD /usr/local/bin/focusd -help > /dev/null 2>&1 || exit 1

USER focusdb
EXPOSE 4444
VOLUME ["/data"]

# Aggressive memory limits for ultra-low footprint (~50-100MB target)
# GOGC=20: GC runs more frequently to keep heap small
# GOMEMLIMIT: Hard limit to prevent OOM
ENV GOGC=20
ENV GOMEMLIMIT=100MiB

# max-conns=5: Only 5 concurrent connections (minimal goroutines)
# buf-size=512: Tiny buffers (512 bytes per connection = ~2.5KB total for buffers)
ENTRYPOINT ["/usr/local/bin/focusd", "-max-conns", "5", "-buf-size", "512", "-data", "/data"]
CMD []
