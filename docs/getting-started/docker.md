# Docker Deployment

Docker provides a straightforward way to deploy all four
services of the pgEdge AI DBA Workbench. You can use
pre-built container images for production or build from
source for development.

## Prerequisites

Ensure the following software is installed before
proceeding.

- [Docker Engine](https://docs.docker.com/engine/install/)
  version 24.0 or later is required.
- The
  [Docker Compose v2](https://docs.docker.com/compose/install/)
  plugin must be available as a Docker CLI plugin.
- Access to the GitHub Container Registry is required
  for pulling pre-built production images.

## Quick Start

The quickest way to deploy uses pre-built images from
the GitHub Container Registry.

1. Clone the repository to obtain the configuration
   files.

   In the following example, the `git clone` command
   retrieves the project repository.

   ```bash
   git clone \
     https://github.com/pgEdge/ai-dba-workbench.git
   cd ai-dba-workbench
   ```

2. Generate the required secret files for the server
   and the database.

   In the following example, the `openssl` command
   creates a random secret key.

   ```bash
   mkdir -p docker/secret
   openssl rand -base64 32 \
     > docker/secret/ai-dba.secret
   echo "your-postgres-password" \
     > docker/secret/pg-password
   ```

3. Set the required PostgreSQL password as an
   environment variable.

   ```bash
   export POSTGRES_PASSWORD=your-password
   ```

4. Start all services using the production Compose
   file.

   In the following example, the `docker compose`
   command starts the services in detached mode.

   ```bash
   docker compose \
     -f examples/docker-compose.production.yml up -d
   ```

5. Verify that all services are running.

   In the following example, the `ps` subcommand
   lists running containers and their status.

   ```bash
   docker compose \
     -f examples/docker-compose.production.yml ps
   ```

6. Open a browser and navigate to
   `http://localhost:3000` to access the web client.

7. Create an initial admin user account.

   In the following example, the `exec` subcommand
   runs the user creation command inside the server
   container.

   ```bash
   docker compose \
     -f examples/docker-compose.production.yml exec \
     server /usr/local/bin/ai-dba-server \
     -config /etc/pgedge/ai-dba-server.yaml \
     -add-user -username admin \
     -password "YourPass123!" \
     -full-name "Admin User" \
     -email "admin@example.com"
   ```

## Image Variants

Pre-built images are published to the GitHub Container
Registry after each release. The following table lists
the available images.

| Image | Tags | Description |
|-------|------|-------------|
| `ghcr.io/pgedge/ai-dba-server` | `latest`, `x.y.z` | The MCP server component. |
| `ghcr.io/pgedge/ai-dba-collector` | `latest`, `x.y.z` | The metrics collector. |
| `ghcr.io/pgedge/ai-dba-alerter` | `latest`, `x.y.z` | The alert monitoring service. |
| `ghcr.io/pgedge/ai-dba-client` | `latest`, `x.y.z` | The React web client. |

The `latest` tag always points to the most recent
release. You can pin to a specific version by setting
the `VERSION` environment variable.

In the following example, the `VERSION` variable
selects a specific image tag.

```bash
VERSION=1.2.3 docker compose \
  -f examples/docker-compose.production.yml up -d
```

## Configuration

The `docker/config/` directory contains configuration
files for each service.

- The `ai-dba-server.yaml` file configures the MCP
  server; see
  [Server Configuration](configuration/server.md)
  for details.
- The `ai-dba-collector.yaml` file configures the
  metrics collector; see
  [Collector Configuration](configuration/collector.md)
  for details.
- The `ai-dba-alerter.yaml` file configures the alert
  monitoring service; see
  [Alerter Configuration](configuration/alerter.md)
  for details.
- The `nginx.conf` file configures the reverse proxy
  for the web client.

The production Compose file mounts these configuration
files into the containers at runtime. Edit the files
in `docker/config/` to customize the deployment.

## Environment Variables

The Compose files support the following environment
variables.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `POSTGRES_PASSWORD` | Yes | None | The password for the PostgreSQL database. |
| `POSTGRES_PORT` | No | `5432` | The host port mapped to PostgreSQL. |
| `SERVER_PORT` | No | `8080` | The host port mapped to the server. |
| `CLIENT_PORT` | No | `3000` | The host port mapped to the web client. |
| `VERSION` | No | `latest` | The image tag to pull from the registry. |

## Development Deployment

The root `docker-compose.yml` file builds all images
from source. Use this file for local development and
testing.

In the following example, the `docker compose` command
builds and starts all services from source.

```bash
docker compose up -d
```

This command builds each Dockerfile in the project and
starts the containers. Changes to the source code
require a rebuild of the affected images.

In the following example, the `--build` flag forces a
rebuild of all images.

```bash
docker compose up -d --build
```

## Health Checks

Each service includes health checks that Docker
monitors automatically. Use the following commands
to verify the deployment status.

In the following example, the `ps` subcommand displays
the health status of each container.

```bash
docker compose \
  -f examples/docker-compose.production.yml ps
```

The server exposes a health endpoint for external
monitoring. In the following example, the `curl`
command checks the server health.

```bash
curl http://localhost:8080/health
```

You can follow the logs of a specific service to
diagnose issues. In the following example, the `logs`
subcommand streams the server output.

```bash
docker compose \
  -f examples/docker-compose.production.yml \
  logs -f server
```

## Troubleshooting

This section covers common deployment issues and
their solutions.

### PostgreSQL Fails to Start

The PostgreSQL container may fail to start if the
data directory has incorrect permissions. Check the
container logs for error details.

In the following example, the `logs` subcommand
displays the PostgreSQL container output.

```bash
docker compose \
  -f examples/docker-compose.production.yml \
  logs postgres
```

Ensure that the `POSTGRES_PASSWORD` environment
variable is set before starting the services. The
PostgreSQL container requires this variable on first
initialization.

### Server Cannot Connect to the Database

The server may fail to connect if the database has
not finished initializing. The server container
depends on the PostgreSQL health check, but network
issues can still cause delays.

In the following example, the `restart` subcommand
restarts the server container.

```bash
docker compose \
  -f examples/docker-compose.production.yml \
  restart server
```

Verify that the database credentials in the server
configuration match the PostgreSQL password. Check
the `docker/config/ai-dba-server.yaml` file and the
`docker/secret/pg-password` file for consistency.

### Viewing Logs for All Services

You can view logs for all services simultaneously
to identify the source of a problem.

In the following example, the `logs` subcommand
streams output from all containers.

```bash
docker compose \
  -f examples/docker-compose.production.yml \
  logs -f
```
