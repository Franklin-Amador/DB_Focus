# Multi-stage Dockerfile for FocusDB
# Builder: compile a static Go binary
FROM golang:1.25-alpine AS builder
WORKDIR /src

# Install git and CA certs for fetching modules and TLS-aware binaries
RUN apk add --no-cache git ca-certificates

# Cache go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -ldflags "-s -w" -o /out/focusd ./cmd/focusd

# Final image: small runtime image
FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /out/focusd /usr/local/bin/focusd

EXPOSE 4444
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/focusd"]
