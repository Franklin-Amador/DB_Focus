# syntax=docker/dockerfile:1.7
# ─────────────────────────────────────────────────────────────
# Stage 1 – Build a static Go binary
# ─────────────────────────────────────────────────────────────
FROM golang:1.24-alpine3.21 AS builder

# Multi-arch support: pass --platform=linux/arm64 to docker build
ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

RUN apk add --no-cache ca-certificates tzdata

# Cache module downloads separately from source code
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/go/pkg/mod \
    go mod download

COPY cmd/focusd ./cmd/focusd
COPY internal    ./internal

RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build \
      -trimpath \
      -ldflags="-s -w" \
      -buildvcs=false \
      -o /out/focusd ./cmd/focusd

# ─────────────────────────────────────────────────────────────
# Stage 2 – Minimal runtime image
# ─────────────────────────────────────────────────────────────
FROM alpine:3.21

# ca-certificates: TLS for outbound calls
# tzdata: time-zone data for job scheduler
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -H -u 1001 -s /sbin/nologin focusdb && \
    mkdir -p /data && \
    chown focusdb:focusdb /data

COPY --from=builder --chown=focusdb:focusdb /out/focusd /usr/local/bin/focusd

# OCI standard image labels
LABEL org.opencontainers.image.title="FocusDB" \
      org.opencontainers.image.description="Lightweight SQL engine with PostgreSQL wire protocol" \
      org.opencontainers.image.source="https://github.com/Franklin-Amador/focusdb" \
      org.opencontainers.image.licenses="MIT"

USER focusdb

# 4444 = PostgreSQL wire protocol  |  9011 = GUI / REST API
EXPOSE 4444 9011
VOLUME ["/data"]

# GOGC=50   → collect after 50% heap growth (halves GC pauses)
# GOMEMLIMIT → soft memory cap; triggers GC before OOM
# TZ=UTC    → consistent timestamps regardless of host timezone
ENV GOGC=50 \
    GOMEMLIMIT=256MiB \
    TZ=UTC

# HEALTHCHECK against the GUI HTTP server (present in both local and PaaS runs)
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -q --spider http://localhost:9011/ || exit 1

ENTRYPOINT ["/usr/local/bin/focusd"]
# Defaults — override any flag at `docker run` time, e.g.:
#   docker run focusdb -max-conns 50 -buf-size 8192 -data /data
CMD ["-data", "/data"]
