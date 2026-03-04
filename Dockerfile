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

# Start with optimized memory settings for 512MB limit
# max-conns=15: reduce goroutines on small systems
# buf-size=2048: smaller buffers reduce per-connection overhead
ENV FOCUSDB_MAX_CONNS=15
ENV FOCUSDB_BUF_SIZE=200

ENTRYPOINT ["/usr/local/bin/focusd", "-max-conns", "15", "-buf-size", "2048", "-data", "/data"]
CMD []
