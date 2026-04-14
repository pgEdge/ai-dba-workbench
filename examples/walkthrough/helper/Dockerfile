#--------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#--------------------------------------------------------------------------

FROM golang:1.24-alpine AS builder

WORKDIR /build
COPY go.mod main.go ./
RUN CGO_ENABLED=0 go build -o /helper .

FROM alpine:3.21
RUN apk add --no-cache docker-cli
COPY --from=builder /helper /usr/local/bin/helper
EXPOSE 8090
CMD ["/usr/local/bin/helper"]
