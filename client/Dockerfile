#--------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#--------------------------------------------------------------------------

# Stage 1: Build the React SPA. We pin to the Debian 13 (trixie)
# variant rather than the default `node:22-slim` (Debian 12 /
# bookworm) because bookworm currently ships gnutls28 versions
# affected by CVE-2026-33845 and CVE-2026-33846; trixie carries
# the patched package. A residual CVE remains in this image
# (CVE-2026-33671 in `picomatch` < 4.0.4, which is bundled inside
# npm itself); npm has not yet shipped a release that pulls in
# the fixed picomatch, and the issue is not reachable from our
# build pipeline. Revisit when npm cuts a new release or when we
# can move to Node 24 LTS.
FROM node:22-trixie-slim AS builder

WORKDIR /build/client

# Copy dependency manifests and install dependencies
COPY client/package.json client/package-lock.json ./
RUN npm ci

# Copy the client source and build the production bundle
COPY client/ ./
RUN npm run build

# Stage 2: Serve with the unprivileged nginx image. The "stable-
# alpine-slim" variant is identical to "stable-alpine" but drops
# optional packages (notably `curl`) that pull in CVEs
# (e.g. CVE-2026-3805) without being needed at runtime. It still
# listens on port 8080 by default and runs the master and worker
# processes as the non-root "nginx" user (UID 101). The base
# image relocates the PID file and temp paths to /tmp, removes
# the "user" directive, and otherwise mirrors the stock alpine
# image. See https://hub.docker.com/r/nginxinc/nginx-unprivileged
FROM nginxinc/nginx-unprivileged:stable-alpine-slim

COPY --from=builder /build/client/dist /usr/share/nginx/html
COPY docker/nginx.conf /etc/nginx/conf.d/default.conf

# The base image already declares the "nginx" user. Restate USER
# explicitly so static analysers (and humans reading the Dockerfile)
# can verify that the container does not run as root.
USER nginx

EXPOSE 8080

CMD ["nginx", "-g", "daemon off;"]
