#--------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#--------------------------------------------------------------------------

# Stage 1: Build the collector binary
FROM golang:1.26.10 AS builder

WORKDIR /build

# Copy the shared pkg module
COPY pkg/ /build/pkg/

# Copy collector module files and source
COPY collector/go.mod collector/go.sum /build/collector/
COPY collector/src/ /build/collector/src/

# Download dependencies
WORKDIR /build/collector
RUN go mod download

# Build the binary
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /build/ai-dba-collector ./src

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
    mkdir -p /etc/pgedge && \
    chown appuser:appuser /etc/pgedge

COPY --from=builder /build/ai-dba-collector /usr/local/bin/ai-dba-collector

USER 999:999

ENTRYPOINT ["/usr/local/bin/ai-dba-collector"]
CMD ["--config", "/etc/pgedge/ai-dba-collector.yaml"]
