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
- [Git](https://git-scm.com/book/en/v2/Getting-Started-Installing-Git)
  is required to clone the repository in the Quick
  Start steps.
- [OpenSSL](https://www.openssl.org/source/) is used
  to generate the shared secret file; OpenSSL is
  pre-installed on most Linux and macOS systems.

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
Registry for each release and for each push to the
`main` branch. The following table lists the available
images and their tags.

| Image | Tags | Description |
|-------|------|-------------|
| `ghcr.io/pgedge/ai-dba-server` | `latest`, `x.y.z`, `x.y`, `edge` | The MCP server component. |
| `ghcr.io/pgedge/ai-dba-collector` | `latest`, `x.y.z`, `x.y`, `edge` | The metrics collector. |
| `ghcr.io/pgedge/ai-dba-alerter` | `latest`, `x.y.z`, `x.y`, `edge` | The alert monitoring service. |
| `ghcr.io/pgedge/ai-dba-client` | `latest`, `x.y.z`, `x.y`, `edge` | The React web client. |

Each image also receives a `sha-<hash>` tag that
provides an immutable reference to a specific commit.
The publishing workflow produces the following tag
types:

- The `latest` tag points to the most recent stable
  release and updates only on version tag pushes.
- The `edge` tag tracks the `main` branch and may
  contain unstable changes.
- The `x.y` tag pins to a minor version and receives
  automatic patch updates.
- The `x.y.z` tag pins to an exact release and never
  changes.

You can select a tag by setting the `VERSION`
environment variable. In the following examples, the
`VERSION` variable controls which image tag the
`docker compose` command pulls.

```bash
# Pin to an exact release
VERSION=1.2.3 docker compose \
  -f examples/docker-compose.production.yml up -d

# Pin to a minor version (receives patch updates)
VERSION=1.2 docker compose \
  -f examples/docker-compose.production.yml up -d

# Use the latest main-branch build
VERSION=edge docker compose \
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

The `postgres`, `server`, and `client` services include
health checks that Docker monitors automatically. Use
the following commands to verify the deployment status.

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

### Port Already in Use

The PostgreSQL container binds to port 5432 on the host
by default. The container will fail to start if another
PostgreSQL instance or other service already occupies
that port. The error message includes the text
"port is already allocated."

Set the `POSTGRES_PORT` environment variable to use a
different host port. In the following example, the
datastore binds to port 5433 instead of 5432.

```bash
export POSTGRES_PORT=5433
docker compose \
  -f examples/docker-compose.production.yml up -d
```

The container port inside Docker remains 5432; only the
host-side mapping changes. The collector, server, and
alerter services connect to the container by service
name, so they are unaffected by this change.

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
