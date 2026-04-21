#--------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#--------------------------------------------------------------------------

# Stage 1: Build the collector binary
FROM golang:1.26.2 AS builder

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

# Stage 2: Runtime image
FROM debian:bookworm-slim

RUN mkdir -p /etc/pgedge

COPY --from=builder /build/ai-dba-collector /usr/local/bin/ai-dba-collector

RUN groupadd -r appuser && useradd -r -g appuser -d /nonexistent -s /usr/sbin/nologin appuser
USER appuser

ENTRYPOINT ["/usr/local/bin/ai-dba-collector"]
CMD ["--config", "/etc/pgedge/ai-dba-collector.yaml"]
