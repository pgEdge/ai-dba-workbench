#--------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#--------------------------------------------------------------------------

# Stage 1: Build the server binary
FROM golang:1.24 AS builder

WORKDIR /build

# Copy the shared pkg module
COPY pkg/ /build/pkg/

# Copy server module files and source
COPY server/src/go.mod server/src/go.sum /build/server/src/
COPY server/src/ /build/server/src/

# Download dependencies
WORKDIR /build/server/src
RUN go mod download

# Build the binary (pure Go, no CGO required)
RUN CGO_ENABLED=0 go build -o /build/ai-dba-server ./cmd/mcp-server

# Stage 2: Runtime image
FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

RUN mkdir -p /etc/pgedge /data

COPY --from=builder /build/ai-dba-server /usr/local/bin/ai-dba-server

EXPOSE 8080
VOLUME /data

ENTRYPOINT ["/usr/local/bin/ai-dba-server"]
CMD ["--config", "/etc/pgedge/ai-dba-server.yaml"]
