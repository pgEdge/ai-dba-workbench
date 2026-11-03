#--------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#--------------------------------------------------------------------------

# Stage 1: Build the server binary
FROM golang:1.26.10 AS builder

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

# Create the runtime image's writable directories here, as the distroless
# runtime image has no shell or mkdir to create them itself
RUN mkdir -p /rootfs/etc/pgedge /rootfs/data /rootfs/usr/local/bin/data

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

COPY --from=builder /build/ai-dba-server /usr/local/bin/ai-dba-server
COPY --from=builder --chown=999:999 /rootfs/etc/pgedge /etc/pgedge
COPY --from=builder --chown=999:999 /rootfs/data /data
COPY --from=builder --chown=999:999 /rootfs/usr/local/bin/data /usr/local/bin/data

USER 999:999

EXPOSE 8080
VOLUME /data

ENTRYPOINT ["/usr/local/bin/ai-dba-server"]
CMD ["--config", "/etc/pgedge/ai-dba-server.yaml", "--data-dir", "/data"]
