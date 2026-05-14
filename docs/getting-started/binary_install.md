# Quick Start Guide

This guide covers setting up the complete pgEdge AI DBA Workbench using
pre-built binaries. After completing these steps, the Workbench collects
metrics from PostgreSQL servers, evaluates alert rules, and displays results
in a web interface.

Before beginning, confirm the following prerequisites are in place:

- [PostgreSQL 14](https://www.postgresql.org/download/) or later is installed
  for the datastore.
- You have network access between each PostgreSQL server to be monitored and the system hosting the Workbench.
- A Linux x86_64 system is available for the server-side components.
- You have access to the database credentials for the datastore database.

## Installing the Binaries

Download the latest release from the
[GitHub releases page](https://github.com/pgEdge/ai-dba-workbench/releases).
The release archive includes the collector, server, and alerter binaries, and
pre-built web client files.  After downloading the files, extract the archives
and copy the files into a deployment directory; in our example, we're using
the `opt/ai-workbench` directory.

In the following example, we extract the archives before using the `cp` and 
`tar` commands install the binaries and client files to the 
`/opt/ai-workbench` directory:

```bash
tar xzf ai-dba-collector-linux-arm64.tar.gz
tar xzf ai-dba-server-linux-arm64.tar.gz
tar xzf ai-dba-alerter-linux-arm64.tar.gz
sudo mkdir -p /opt/ai-workbench
sudo cp ai-dba-collector /opt/ai-workbench/
sudo cp ai-dba-server /opt/ai-workbench/
sudo cp ai-dba-alerter /opt/ai-workbench/
sudo chmod +x /opt/ai-workbench/ai-dba-*
sudo mkdir -p /opt/ai-workbench/client
sudo tar xzf ai-dba-client.tar.gz -C /opt/ai-workbench/client
```

!!! note
    See the [installation paths table](installation_overview.md)
    for a comparison of installation paths used in different deployment
    methods.


## Creating the Datastore Database

Next, use a Postgres client to create a PostgreSQL database for the datastore. The collector, server, and
alerter share this database.

```bash
psql -U postgres -h localhost
```

In the following example, the `CREATE DATABASE` and `GRANT` statements create
the datastore database and user:

```sql
CREATE DATABASE ai_workbench;
CREATE USER ai_workbench WITH PASSWORD 'your-password';
GRANT ALL PRIVILEGES ON DATABASE ai_workbench TO ai_workbench;
```

The collector creates the required schema tables automatically on first
startup.

## Create a Server Secret

The server secret encrypts passwords for monitored database connections. All
components that handle connection passwords must share the same secret file.
The server discovers the secret at `/etc/pgedge/ai-dba-server.secret` by
default. The collector's auto-discovered default uses a different filename
(`ai-dba-collector.secret`); the collector reads the server's secret only
when `secret_file:` in `ai-dba-collector.yaml` points to it explicitly.

In the following example, the `mkdir` command creates the system-wide
configuration directory; the same directory holds the YAML configuration
files used in later steps:

```bash
sudo mkdir -p /etc/pgedge
```

Then, use the `openssl` command to write a secure secret to
the system-wide default location:

```bash
sudo openssl rand -base64 32 \
    | sudo tee /etc/pgedge/ai-dba-server.secret \
    > /dev/null
sudo chmod 600 /etc/pgedge/ai-dba-server.secret
```

!!! hint

    If you store the secret outside the default search paths, set the 
    `secret_file:` property in the YAML configuration files to the absolute
    path of the alternate location.


## Create a Password File

Store the datastore password in a file with restricted permissions.

In the following example, the `echo` and `chmod` commands create the password
file and set its permissions:

```bash
echo "your-password" > ./db-password.txt
chmod 600 ./db-password.txt
```

## Configure and Start the Collector

In the following example, the `cp` command copies the example configuration
file to the system configuration directory:

```bash
cp examples/ai-dba-collector.yaml \
    /etc/pgedge/ai-dba-collector.yaml
```

In the following example, the configuration specifies minimum settings for a
local development environment:

```yaml
datastore:
  host: localhost
  database: ai_workbench
  username: ai_workbench
  password_file: /path/to/db-password.txt
  port: 5432
  sslmode: disable

secret_file: /etc/pgedge/ai-dba-server.secret
```

In the following example, the `ai-dba-collector` command starts the collector
with the configuration file:

```bash
/opt/ai-workbench/ai-dba-collector \
    -config /etc/pgedge/ai-dba-collector.yaml
```

The collector displays startup messages to confirm successful initialization:

```
pgEdge AI DBA Workbench Collector starting...
Configuration loaded from: /etc/pgedge/ai-dba-collector.yaml
Database schema initialized
Datastore connection established
Probe scheduler started with 24 probe(s)
Collector is running. Press Ctrl+C to stop.
```

## Configure and Start the Server

In the following example, the `cp` command copies the example configuration
file to the system configuration directory:

```bash
cp examples/ai-dba-server.yaml \
    /etc/pgedge/ai-dba-server.yaml
```

In the following example, the configuration specifies minimum settings for a
development environment:

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

secret_file: /etc/pgedge/ai-dba-server.secret
```

In the following example, the `ai-dba-server` command creates a user account
before the server starts:

```bash
/opt/ai-workbench/ai-dba-server \
    -add-user -username admin
```

In the following example, the `ai-dba-server` command starts the server with
the configuration file:

```bash
/opt/ai-workbench/ai-dba-server \
    -config /etc/pgedge/ai-dba-server.yaml
```

## Configure and Start the Alerter

The alerter connects to the same datastore as the collector and server.
Configure the alerter using a YAML configuration file or command-line flags.

In the following example, the `ai-dba-alerter` command starts the alerter
with database connection flags and debug logging enabled:

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

For production deployments, create a YAML configuration file; see the
[alerter configuration](configuration/alerter.md) reference for all available
options.

## Serve the Web Client

For production deployments, serve the pre-built web client files from
`/opt/ai-workbench/client` using a web server such as nginx. Configure the
web server to proxy API requests to the server on port 8080.

## Verify the Setup

After starting all components, verify the system is working correctly by
completing the following steps.

### Log In to the Web Client

Open the web client URL in a browser and log in with the user account created
in the Configure and Start the Server section.

### Add a Monitored Connection

The web client provides an administration panel for adding monitored
connections. Navigate to the administration panel and create a new connection
with the target server details.

### Check Metrics Collection

After adding a connection, the collector begins gathering metrics. Verify that
data appears in the web client dashboards within a few minutes.

### Check Alerter Operation

The alerter evaluates threshold rules against collected metrics. Verify that
the alerter logs show rule evaluation progress:

```
Evaluating threshold rules...
Found 24 enabled rules
```

## Stopping the Components

Stop each component gracefully by pressing `Ctrl+C` in the terminal where the
component is running. Each component waits for in-progress operations to
complete before exiting.

## Next Steps

After verifying the basic setup, the following guides cover additional
configuration topics:

- Review the [installation guide](installation_overview.md) for production
  deployment instructions.
- Configure the [collector](configuration/collector.md) with tuned connection
  pools and SSL.
- Configure the [server](configuration/server.md) with TLS, authentication,
  and LLM integration.
- Configure the [alerter](configuration/alerter.md) with anomaly detection
  and notification channels.
- Configure the [web client](configuration/client.md) proxy settings and
  build options.
