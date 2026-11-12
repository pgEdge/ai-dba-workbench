#--------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#--------------------------------------------------------------------------

# Stage 1: Build the server binary
FROM golang:1.26.11 AS builder

WORKDIR /build

# Copy the shared pkg module
COPY pkg/ /build/pkg/

# The server's go.mod requires the collector module for its tests only, so
# the module graph needs the collector's go.mod but no collector source
COPY collector/go.mod collector/go.sum /build/collector/

# Copy server module files and source
COPY server/src/go.mod server/src/go.sum /build/server/src/
COPY server/src/ /build/server/src/

# Download dependencies
WORKDIR /build/server/src
RUN go mod download

# Build the binary (pure Go, no CGO required)
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /build/ai-dba-server ./cmd/mcp-server

# Stage 2: Runtime image. The binary is static (CGO_ENABLED=0), so it does
# not use the C library; Alpine provides a small base that still has a
# shell and BusyBox for debugging. ca-certificates supplies the CA bundle
# for outbound TLS, and tzdata the zone files that time.LoadLocation reads.
# It runs as UID/GID 999, the "appuser" of the earlier Debian images, so
# volumes written by those releases stay writable after an upgrade.
# Alpine's base /etc/group gives GID 999 to "ping", which nothing in this
# image uses, so that group is removed first to free the GID.
FROM alpine:3

# hadolint ignore=DL3018
RUN apk add --no-cache ca-certificates tzdata && \
    delgroup ping && \
    addgroup -S -g 999 appuser && \
    adduser -S -D -H -u 999 -G appuser -h /nonexistent -s /sbin/nologin appuser && \
    mkdir -p /etc/pgedge /data /usr/local/bin/data && \
    chown appuser:appuser /etc/pgedge /data /usr/local/bin/data

COPY --from=builder /build/ai-dba-server /usr/local/bin/ai-dba-server

USER 999:999

EXPOSE 8080
VOLUME /data

ENTRYPOINT ["/usr/local/bin/ai-dba-server"]
CMD ["--config", "/etc/pgedge/ai-dba-server.yaml", "--data-dir", "/data"]
