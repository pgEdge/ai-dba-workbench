# Building the Workbench from Source Code

The AI DBA Workbench collects metrics from PostgreSQL servers, evaluates
alert rules, and displays results in a web interface. You can find the
source code and configuration examples in the
[GitHub repository](https://github.com/pgEdge/ai-dba-workbench). This guide
walks you through building, installing, and starting the Workbench on a
single host.

## Prerequisites

Before building the Workbench, confirm that the following prerequisites
are in place on a supported operating system and platform:

- [Go 1.26](https://go.dev/dl/) or later is required to build the
  server-side components.

- [Node.js 18](https://nodejs.org/en/download) or later is required to
  build the web client.

- [Make](https://www.gnu.org/software/make/) is required to execute the
  Makefile.

- You must have a [PostgreSQL 14](https://www.postgresql.org/download/)
  or later database for the Workbench datastore; you must also have the
  credentials for that database.

- Network access must exist between each monitored PostgreSQL server and
  the system that hosts the Workbench.

!!! hint

    The commands that follow are for a host running Ubuntu; modify the 
    commands as needed for your platform.

## Cloning the Repository

The project uses Makefiles for building and testing; when you clone the
[GitHub repository](https://github.com/pgEdge/ai-dba-workbench), you get
the files you need to build. In the following example, the `git clone`
command clones the repository:

```bash
git clone https://github.com/pgEdge/ai-dba-workbench.git
```

After cloning the repository, move into the top-level directory of the
repo before building the components:

```bash
cd ai-dba-workbench
```

## Building the Binaries

The Makefile builds all of the server-side components and the web client.
In the following example, the `make all` command builds the Workbench:

```bash
make all
```

You can optionally build components individually. In the following
example, the `make build` command builds just the collector component:

```bash
cd collector && make build
```

The `make all` command produces three binaries in the `bin/` directory of
the repository:

- The `bin/ai-dba-collector` binary collects metrics from monitored
  PostgreSQL servers.

- The `bin/ai-dba-server` binary serves the API and MCP endpoints.

- The `bin/ai-dba-alerter` binary evaluates alert rules and sends
  notifications.

## Installing the Workbench

After building the components, copy the binaries and web client files into
the installation directory. In the following example, the `mkdir` and `cp`
commands create the installation directory and copy the binaries into
place:

```bash
sudo mkdir -p /opt/ai-workbench
sudo cp bin/ai-dba-* /opt/ai-workbench/
```

The web client builds separately and produces output in the `client/dist/`
directory. Run the following commands from the `ai-dba-workbench`
repository root. In the following example, the `cd client` command enters
the client directory; the `npm install` command installs the dependencies;
the `npm run build` command then builds the client:

```bash
cd client
npm install && npm run build
```

Then, return to the repository root, and use the `mkdir` command to create the
installation directory; the `cp` command installs the built files from
`client/dist/` to `/opt/ai-workbench/client/`:

```bash
cd ..
sudo mkdir -p /opt/ai-workbench/client
sudo cp -r client/dist/* /opt/ai-workbench/client/
```

The remaining sections run the installed binaries from
`/opt/ai-workbench/`; you'll notice commands use this directory as the
prefix.

## Creating the Datastore Database

Use a PostgreSQL client to create a database for the datastore; the
collector, server, and alerter share this database. Use the
[`psql`](https://www.postgresql.org/docs/18/app-psql.html) client
to connect to the PostgreSQL server:

```bash
sudo -u postgres psql
```

Alter the `postgres` role to make it a login role with a password:

```sql
ALTER ROLE postgres LOGIN PASSWORD '1safepassword';
```

Then, create the datastore database. In the following example, the
`CREATE DATABASE` statement creates the `ai_workbench` database:

```sql
CREATE DATABASE ai_workbench;
```

The collector creates the required schema tables automatically on the
first startup.

!!! hint

    You can use `\q` to exit the `psql` client session and return to the 
    terminal window.

## Creating a Server Secret and a Password File

The Workbench components use the server secret file and password file when
they connect and authenticate with other components and the datastore
database. The Workbench stores both files in the `/etc/pgedge` directory;
the complete paths are:

- The shared secret resides at `/etc/pgedge/server.secret`.

- The datastore password resides at `/etc/pgedge/password.txt`.

In the following example, the `mkdir` command creates the `/etc/pgedge`
directory; the directory may already exist for configuration files, so
`mkdir -p` is the safest form of the command to use:

```bash
sudo mkdir -p /etc/pgedge
```

Then, use the `openssl` command to write a secret to the
`server.secret` file in the `/etc/pgedge` directory:

```bash
sudo openssl rand -base64 32 \
    | sudo tee /etc/pgedge/server.secret \
    > /dev/null
sudo chmod 644 /etc/pgedge/server.secret
```

Then, use the `echo` and `chmod` commands to create the `password.txt`
file in the `/etc/pgedge` directory and set the file permissions:

```bash
sudo sh -c 'echo "1safepassword" > /etc/pgedge/password.txt'
sudo chmod 644 /etc/pgedge/password.txt
```

!!! hint

    When configuring your installation, ensure that the locations of
    the `server.secret` and `password.txt` files match the
    absolute file paths in the YAML configuration files.

## Creating the admin User and SQLite Database

The Workbench uses a SQLite database to store authentication and management
details. Create the database and add an `admin` user account (this account
will be used when connecting to the Workbench client) before editing the
configuration files. 

In the following example, the `mkdir`, `chown`, and `ai-dba-server` commands
create the directory and add a user; the `-data-dir` flag places the `auth.db`
authentication database in `/var/lib/ai-workbench/data`:

```bash
sudo mkdir -p /var/lib/ai-workbench/data
sudo chown -R $USER:$USER /var/lib/ai-workbench/data
/opt/ai-workbench/ai-dba-server \
    -add-user -username admin \
    -data-dir /var/lib/ai-workbench/data
```

The command prompts for a password and optional user details; the password
must include at least one capital letter, one digit, and one special
character:

```bash
/opt/ai-workbench/ai-dba-server -add-user -username admin -data-dir /var/lib/ai-workbench/data
Auth store: /var/lib/ai-workbench/data/auth.db
Enter password:
Confirm password:
Enter full name (optional): admin
Enter email address (optional): admin@pgedge.com
Enter notes for this user (optional):

======================================================================
User created successfully!
======================================================================

Username:  admin
Full Name: admin
Email:    admin@pgedge.com
Status:   Enabled
======================================================================
```

Then, grant superuser status to the admin account. In the following
example, the `-set-superuser` flag promotes the `admin` user to
superuser:

```bash
/opt/ai-workbench/ai-dba-server \
    -set-superuser -username admin \
    -data-dir /var/lib/ai-workbench/data
```

The command confirms the change; for example:

```bash
User 'admin' is now a superuser
```

!!! note

    Without superuser privileges, you are allowed to connect to the Workbench,
    but you will not be able to add servers for monitoring.


## Configuring and Starting the Collector

Copy the sample configuration file for the collector to the system
configuration directory before editing the settings. The `/etc/pgedge`
directory is the system-wide default location that the Workbench searches for
configuration files; specify an alternate location on the command line
when invoking the Workbench. In the following example, the `cp` command
copies the sample collector configuration file to `/etc/pgedge`:

```bash
sudo cp ~/ai-dba-workbench/examples/ai-dba-collector.yaml /etc/pgedge/ai-dba-collector.yaml
```

Use your choice of editor to update the configuration file to describe your
deployment. The following settings are the minimum changes required to create
a local development environment based on this guide:

```yaml
datastore:
  host: localhost
  database: ai_workbench
  username: postgres
  password_file: /etc/pgedge/password.txt
  port: 5432
  sslmode: disable
```

The `sslmode: disable` setting disables SSL for the datastore connection;
this setting is suitable for local development only. For other
deployment environments, use the default `prefer` setting or a stricter mode.

The `SECURITY SETTINGS` section stores the location of the secret file:

```yaml
secret_file: /etc/pgedge/server.secret
```

After updating the settings and saving the file, start the collector. In
the following example, the `ai-dba-collector` command starts the collector
with the configuration file:

```bash
/opt/ai-workbench/ai-dba-collector -config /etc/pgedge/ai-dba-collector.yaml &
```

The collector displays startup messages to confirm successful
initialization; for example:

```bash
/opt/ai-workbench/ai-dba-collector -config /etc/pgedge/ai-dba-collector.yaml
2026/05/19 13:22:00 pgEdge AI DBA Workbench Collector v1.0.0-beta1 starting...
2026/05/19 13:22:00 Configuration loaded from: /etc/pgedge/ai-dba-collector.yaml
2026/05/19 13:22:00 Datastore connection established
2026/05/19 13:22:00 Probe scheduler started
2026/05/19 13:22:00 Collector is running. Press Ctrl+C to stop.
```

The collector runs as a background service; press `Enter` to view your
prompt.

## Configuring and Starting the Server

Copy the server configuration file to the system configuration directory
before editing the settings. In the following example, the `cp` command
copies the sample configuration file to the `/etc/pgedge` directory:

```bash
sudo cp ~/ai-dba-workbench/examples/ai-dba-server.yaml /etc/pgedge/ai-dba-server.yaml
```

The sample configuration file specifies the minimum settings for a local
development environment; for many properties, accept the defaults.
Update the configuration file to describe your deployment.

The `DATABASE CONNECTION` properties provide connection details for the
server's database; update the properties with the connection details and the
password for the `postgres` user:

```yaml
datastore:
  host: localhost
  port: 5432
  database: ai_workbench
  user: postgres
  password: "1safepassword"
  sslmode: disable
```

By default, the server blocks connections to internal and private IP
addresses. To monitor a PostgreSQL instance on the same host or local
network, set the `allow_internal_networks` property to `true` in the
server configuration file:

```yaml
connection_security:
  # Allow connections to RFC 1918 private addresses (10.x.x.x,
  # 172.16.x.x, 192.168.x.x), localhost, link-local, and other
  # internal network ranges.
  # Default: false
  allow_internal_networks: true
```

Update the `PATHS AND DATA DIRECTORIES` section to specify the paths to the
`secret_file` and the `data_dir` you created above:

```yaml
# Path to server secret file (for encryption)
# Default: /etc/pgedge/ai-dba-server.secret or ./ai-dba-server.secret
secret_file: "/etc/pgedge/server.secret"

# Data directory for conversation history and other persistent data
data_dir: "/var/lib/ai-workbench/data"
```

Then, use the `ai-dba-server` command to start the server:

```bash
/opt/ai-workbench/ai-dba-server -config /etc/pgedge/ai-dba-server.yaml &
```

The server displays status messages during startup; for example:

```bash
Auth store: /var/lib/ai-workbench/data/auth.db (1 user(s), 0 token(s))
RBAC: 21 MCP privileges registered
Rate limiting enabled: 10 attempts per 15 minutes per IP
Account lockout enabled: 10 failed attempts before lockout
Server secret: loaded from /etc/pgedge/server.secret
Datastore: connected to postgres@localhost:5432/ai_workbench
Database configured: postgres@localhost:5432/ai_workbench (per-session connections)
Conversation store: PostgreSQL datastore
LLM HTTP client: timeout=2m0s
AI Overview: DISABLED (requires datastore and LLM configuration)
Starting MCP server in HTTP mode on :8080
LLM Proxy: ENABLED (provider: anthropic, model: claude-sonnet-4-5)
Knowledgebase: DISABLED
MCP tool REST bridge: ENABLED
Conversation history: ENABLED
Connection management: ENABLED
Cluster management: ENABLED
Alert management: ENABLED
Blackout management: ENABLED
Probe configuration: ENABLED
Alert rule configuration: ENABLED
Alert override configuration: ENABLED
Probe override configuration: ENABLED
Notification channel management: ENABLED
Channel override configuration: ENABLED
Server info: ENABLED
Timeline events: ENABLED
Performance summary: ENABLED
Metrics query: ENABLED
Latest snapshot: ENABLED
Memory management: ENABLED
RBAC management: ENABLED
```

The server runs as a background process; press `Enter` to view your
prompt.

## Configuring and Starting the Alerter

The alerter connects to the same datastore database as the collector and
server. Configure the alerter with a YAML configuration file or
command-line flags; see the
[alerter configuration](configuration/alerter.md) reference to review the
available options. In the following example, the `cp` command copies the
sample alerter configuration file to `/etc/pgedge`:

```bash
sudo cp ~/ai-dba-workbench/examples/ai-dba-alerter.yaml /etc/pgedge/ai-dba-alerter.yaml
```

Next, update the configuration file to describe your deployment. The following
example shows the minimum settings required for a local development
environment based on the deployment we are performing in this document:

```yaml
datastore:
  host: localhost
  database: ai_workbench
  username: postgres
  password_file: /etc/pgedge/password.txt
  port: 5432
  sslmode: disable
```

The `sslmode: disable` setting disables SSL for the datastore connection;
this setting is suitable for local development only. For other
deployment environments, use the default `prefer` setting or a stricter mode.

The `SECURITY SETTINGS` section stores the location of the secret file:

```yaml
secret_file: /etc/pgedge/server.secret
```

After updating the settings and saving the file, start the alerter:

```bash
/opt/ai-workbench/ai-dba-alerter -config /etc/pgedge/ai-dba-alerter.yaml &
```

The alerter displays status messages during startup; for example:

```bash
pgEdge AI DBA Workbench Alerter v1.0.0-beta1 starting...
Configuration loaded from /etc/pgedge/ai-dba-alerter.yaml
Datastore: connected to postgres@localhost:5432/ai_workbench
[alerter] Initialized embedding provider: nomic-embed-text
[alerter] Initialized reasoning provider: qwen2.5:7b-instruct
Starting alerter engine...
[alerter] Engine starting...
[alerter] All workers started
[alerter] Retention manager started
[alerter] Blackout scheduler started
[alerter] Re-evaluation worker started (interval: 5m0s)
[alerter] Anomaly detector started (interval: 1m0s)
[alerter] Alert cleaner started
[alerter] Threshold evaluator started (interval: 1m0s)
[alerter] Baseline calculator started (interval: 1h0m0s)
[alerter] Connection error evaluator started (interval: 30s)
[alerter] Calculating baselines for 0 connections, 28 rules (lookback: 7 days)
[alerter] Baseline calculation complete
```

The alerter runs as a background process; press `Enter` to view your
prompt.

## Configuring nginx and Opening the Web UI

The server does not include a static file service. You can install and
configure [nginx](https://nginx.org/en/docs/) to serve the client files and
proxy API requests to the server before opening the web UI. Use your choice
of package manager to install nginx. In the following example, the
`apt install` command installs nginx:

```bash
sudo apt install nginx
```

Then, create and open the nginx configuration file; this example uses
`vi`:

```bash
sudo vi /etc/nginx/sites-available/ai-dba-workbench
```

Update the nginx configuration file to set the proxy rules and file root
for your installation:

```nginx
server {
    listen 80;
    server_name your_server_hostname_or_ip;

    root /opt/ai-workbench/client;
    index index.html;

    location /api/ {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location /mcp/ {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 300s;
    }

    location = /health {
        proxy_pass http://localhost:8080;
    }

    location / {
        try_files $uri $uri/ /index.html;
    }
}
```

Then, use the `ln`, `nginx`, and `systemctl` commands to enable the
configuration and restart nginx:

```bash
sudo ln -s /etc/nginx/sites-available/ai-dba-workbench \
    /etc/nginx/sites-enabled/ai-dba-workbench
sudo rm /etc/nginx/sites-enabled/default
sudo nginx -t
sudo systemctl restart nginx
```

Open a browser and navigate to `http://<server-ip>`; provide
authentication details when the Workbench opens.

![Log in to the AI DBA Workbench](../images/workbench_login.png)

After logging in, select the `+` next to the DATABASE SERVERS heading in
the left navigation panel. The Workbench adds a new server definition
entry.

![Adding a server definition](../images/add_server.png)


### Customizing your Configuration

Consult the following guides for additional configuration information:

- The [systemd configuration](configuration/configure_systemd.md) guide
  provides details about setting up systemd service management for users
  who did not use pgEdge packages when installing.

- The [collector](configuration/collector.md) guide covers tuned
  connection pools and SSL.

- The [server](configuration/server.md) guide covers TLS, authentication,
  and LLM integration.

- The [alerter](configuration/alerter.md) guide covers anomaly detection
  and notification channels.

- The [web client](configuration/client.md) guide covers proxy settings
  and build options.
