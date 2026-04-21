#--------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#--------------------------------------------------------------------------

# Stage 1: Build the alerter binary
FROM golang:1.26 AS builder

WORKDIR /build

# Copy the shared pkg module
COPY pkg/ /build/pkg/

# Copy alerter module files and source
COPY alerter/src/go.mod alerter/src/go.sum /build/alerter/src/
COPY alerter/src/ /build/alerter/src/

# Download dependencies
WORKDIR /build/alerter/src
RUN go mod download

# Build the binary
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /build/ai-dba-alerter ./cmd/ai-dba-alerter

# Stage 2: Runtime image
FROM debian:bookworm-slim

RUN mkdir -p /etc/pgedge

COPY --from=builder /build/ai-dba-alerter /usr/local/bin/ai-dba-alerter

RUN groupadd -r appuser && useradd -r -g appuser -d /nonexistent -s /usr/sbin/nologin appuser
USER appuser

ENTRYPOINT ["/usr/local/bin/ai-dba-alerter"]
CMD ["--config", "/etc/pgedge/ai-dba-alerter.yaml"]
