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

USER focusdb
EXPOSE 4444
VOLUME ["/data"]

# EXTREME memory limits for 512MB Render free plan
# GOGC=50: Garbage collection after every 50% heap growth (vs 100% default)
# GOMEMLIMIT=80MiB: Hard limit - stops accepting connections if exceeded
ENV GOGC=50
ENV GOMEMLIMIT=80MiB

# Minimal config:
# max-conns=2: Only 2 concurrent connections
# buf-size=256: Smallest buffers possible (~512 bytes total for buffers)
ENTRYPOINT ["/usr/local/bin/focusd", "-max-conns", "1", "-buf-size", "128", "-data", "/data"]
CMD []
