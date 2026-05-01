# Quick Start Guide

This guide walks you through setting up the complete
pgEdge AI DBA Workbench using pre-built binaries.
After completing these steps, the system will collect
metrics from PostgreSQL servers, evaluate alert rules,
and display results in a web interface.

## Prerequisites

Before you begin, ensure you have the following:

- [PostgreSQL 14](https://www.postgresql.org/download/)
  or later for the datastore.
- Network access to the PostgreSQL servers you want
  to monitor.
- A Linux x86_64 system for the server-side
  components.
- Database credentials for the datastore.

## Download the Binaries

Download the latest release from the
[GitHub releases page](https://github.com/pgEdge/ai-dba-workbench/releases).
The release archive includes the collector, server,
alerter binaries, and pre-built web client files.

Install the binaries to a deployment directory:

```bash
sudo mkdir -p /opt/ai-workbench
sudo cp ai-dba-collector /opt/ai-workbench/
sudo cp ai-dba-server /opt/ai-workbench/
sudo cp ai-dba-alerter /opt/ai-workbench/
sudo chmod +x /opt/ai-workbench/ai-dba-*
sudo mkdir -p /opt/ai-workbench/client
sudo tar xzf ai-dba-client.tar.gz \
    -C /opt/ai-workbench/client
```

!!! note
    The paths above apply to manual installs from
    GitHub releases. Docker and RPM/DEB packages use
    different locations. See the
    [installation paths table](installation.md#installation-paths-by-method)
    for a complete comparison.

!!! note
    To build the components from source instead,
    see the
    [Developer Guide](../developer-guide/index.md).

## Set Up the Datastore Database

Create a PostgreSQL database for the datastore. The
collector, server, and alerter share this database.

```sql
CREATE DATABASE ai_workbench;
CREATE USER ai_workbench WITH PASSWORD 'your-password';
GRANT ALL PRIVILEGES ON DATABASE ai_workbench
    TO ai_workbench;
```

The collector creates the required schema tables
automatically on first startup.

## Create a Server Secret

The server secret encrypts passwords for monitored
database connections. All components that handle
connection passwords must share the same secret file.
The collector and server discover the secret at
`/etc/pgedge/ai-dba-server.secret` by default.

In the following example, the `openssl` command writes
a secure secret to the system-wide default location:

```bash
sudo openssl rand -base64 32 \
    | sudo tee /etc/pgedge/ai-dba-server.secret \
    > /dev/null
sudo chmod 600 /etc/pgedge/ai-dba-server.secret
```

To store the secret outside the default search paths,
set `secret_file:` in the YAML configuration to an
absolute path of your choice.

## Create a Password File

Store the datastore password in a file with restricted
permissions:

```bash
echo "your-password" > ./db-password.txt
chmod 600 ./db-password.txt
```

## Configure and Start the Collector

Copy the example configuration file and edit the
datastore connection settings:

```bash
cp examples/ai-dba-collector.yaml \
    /etc/pgedge/ai-dba-collector.yaml
```

In the following example, the configuration specifies
minimum settings for a local development environment:

```yaml
datastore:
  host: localhost
  database: ai_workbench
  username: ai_workbench
  password_file: /path/to/db-password.txt
  port: 5432
  sslmode: disable

secret_file: /path/to/ai-dba-server.secret
```

Start the collector:

```bash
/opt/ai-workbench/ai-dba-collector \
    -config /etc/pgedge/ai-dba-collector.yaml
```

The collector displays startup messages to confirm
successful initialization:

```
pgEdge AI DBA Workbench Collector starting...
Configuration loaded from: /etc/pgedge/ai-dba-collector.yaml
Database schema initialized
Datastore connection established
Probe scheduler started with 24 probe(s)
Collector is running. Press Ctrl+C to stop.
```

## Configure and Start the Server

Copy the example configuration file and edit the
settings:

```bash
cp examples/ai-dba-server.yaml \
    /etc/pgedge/ai-dba-server.yaml
```

In the following example, the configuration specifies
minimum settings for a development environment:

```yaml
http:
  address: ":8080"
  auth:
    enabled: true

connection_security:
  allow_internal_networks: true

database:
  host: localhost
  port: 5432
  database: ai_workbench
  user: ai_workbench
  sslmode: disable

secret_file: /path/to/ai-dba-server.secret
```

Create a user account before starting the server:

```bash
/opt/ai-workbench/ai-dba-server \
    -add-user -username admin
```

Start the server:

```bash
/opt/ai-workbench/ai-dba-server \
    -config /etc/pgedge/ai-dba-server.yaml
```

## Configure and Start the Alerter

The alerter connects to the same datastore as the
collector and server. Configure the alerter using a
YAML configuration file or command-line flags.

In the following example, the alerter starts with
database connection flags and debug logging enabled:

```bash
/opt/ai-workbench/ai-dba-alerter -debug \
    -db-host localhost \
    -db-name ai_workbench \
    -db-user ai_workbench \
    -db-password your-password
```

The alerter displays status messages during startup:

```
Datastore: connected to ai_workbench@localhost:5432
Starting alerter engine...
Threshold evaluator started (interval: 1m0s)
All workers started
```

For production deployments, create a YAML configuration
file. See the
[alerter configuration](configuration/alerter.md)
reference for all available options.

## Serve the Web Client

For production deployments, serve the pre-built web
client files from `/opt/ai-workbench/client` using
a web server such as Nginx. Configure the web server
to proxy API requests to the server on port 8080.

## Verify the Setup

After starting all components, follow these steps to
verify the system is working correctly.

### Log In to the Web Client

Open the web client URL in a browser and log in with
the user account you created earlier.

### Add a Monitored Connection

Use the web client to add a PostgreSQL server for
monitoring. Navigate to the administration panel and
create a new connection with the target server
details.

### Check Metrics Collection

After adding a connection, the collector begins
gathering metrics. Verify that data appears in the
web client dashboards within a few minutes.

### Check Alerter Operation

The alerter evaluates threshold rules against
collected metrics. Verify the alerter logs show
rule evaluation progress:

```
Evaluating threshold rules...
Found 24 enabled rules
```

## Stopping the Components

Stop each component gracefully by pressing `Ctrl+C`
in the terminal where the component is running. Each
component waits for in-progress operations to
complete before exiting.

## Next Steps

After verifying the basic setup, explore these topics:

- Review the
  [installation guide](installation.md) for
  production deployment instructions.
- Configure the
  [collector](configuration/collector.md)
  with tuned connection pools and SSL.
- Configure the
  [server](configuration/server.md)
  with TLS, authentication, and LLM integration.
- Configure the
  [alerter](configuration/alerter.md)
  with anomaly detection and notification channels.
- Configure the
  [web client](configuration/client.md)
  proxy settings and build options.
