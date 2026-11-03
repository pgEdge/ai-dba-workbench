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

# Create the runtime image's configuration directory here, as the
# distroless runtime image has no shell or mkdir to create it itself
RUN mkdir -p /rootfs/etc/pgedge

# Stage 2: Runtime image. The binary is static (CGO_ENABLED=0), so it runs
# on distroless/static, which carries only CA certificates, tzdata and
# /etc/passwd: no shell, package manager or libc, and so none of the
# Debian packages whose unfixed CVEs a bookworm-slim base accumulates.
# It runs as UID/GID 999, the "appuser" of the earlier Debian images, so
# volumes written by those releases stay writable after an upgrade. The
# distroless /etc/passwd has no entry for UID 999, so ENV USER supplies the
# name that Go's os/user falls back to.
FROM gcr.io/distroless/static-debian13

ENV USER=appuser

COPY --from=builder /build/ai-dba-collector /usr/local/bin/ai-dba-collector
COPY --from=builder --chown=999:999 /rootfs/etc/pgedge /etc/pgedge

USER 999:999

ENTRYPOINT ["/usr/local/bin/ai-dba-collector"]
CMD ["--config", "/etc/pgedge/ai-dba-collector.yaml"]
