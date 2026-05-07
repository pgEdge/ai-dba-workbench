#--------------------------------------------------------------------------
#
# pgEdge AI DBA Workbench
#
# Copyright (c) 2025 - 2026, pgEdge, Inc.
# This software is released under The PostgreSQL License
#
#--------------------------------------------------------------------------

# Stage 1: Build the React SPA
FROM node:22-slim AS builder

WORKDIR /build/client

# Copy dependency manifests and install dependencies
COPY client/package.json client/package-lock.json ./
RUN npm ci

# Copy the client source and build the production bundle
COPY client/ ./
RUN npm run build

# Stage 2: Serve with the unprivileged nginx image. This variant of
# the official nginx image listens on port 8080 by default and runs
# the master and worker processes as the non-root "nginx" user (UID
# 101). The base image relocates the PID file and temp paths to
# /tmp, removes the "user" directive, and otherwise mirrors the
# stock alpine image. See https://hub.docker.com/r/nginxinc/nginx-unprivileged
FROM nginxinc/nginx-unprivileged:stable-alpine

COPY --from=builder /build/client/dist /usr/share/nginx/html
COPY docker/nginx.conf /etc/nginx/conf.d/default.conf

# The base image already declares the "nginx" user. Restate USER
# explicitly so static analysers (and humans reading the Dockerfile)
# can verify that the container does not run as root.
USER nginx

EXPOSE 8080

CMD ["nginx", "-g", "daemon off;"]
