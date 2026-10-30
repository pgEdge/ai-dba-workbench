# Upgrading to 2.0.0

This page describes how to upgrade an installation of release 1.0.0 to
release 2.0.0. Release 2.0.0 contains breaking changes, migrates the
datastore schema and the authentication database forward only, and
requires the components to be upgraded in a fixed order. Read the whole
page before you start, and take the backups described in
[Preparing for the Upgrade](#preparing-for-the-upgrade);
the backups are the only way back to release 1.0.0.

For the general package upgrade behaviour, including the configuration
check that the Debian and RPM packages run, see
[Upgrading the Workbench](upgrading.md).

## Breaking Changes

The changes in this section can stop a component starting, refuse a
request that release 1.0.0 accepted, or change existing behaviour
without warning. Review each one against your installation before you
upgrade; the [changelog](../changelog.md) describes every change in
full.

### Upgrade Order

The collector owns the datastore schema and migrates the schema
automatically when the collector starts. Release 2.0.0 brings the schema
from version 5 to version 31, and the server and the alerter depend on
the new schema:

- The server refuses to start while the collector schema is below
  version 29, and reports that the collector schema is below the
  version the server requires.
- An alerter that starts before the collector has migrated the schema
  raises no anomaly alerts, and logs an error on each cycle until the
  collector catches up.
- One migration adds indexes to the anomaly candidate table and drops
  a duplicate index on the anomaly embeddings table; a retention sweep
  by a running 1.0.0 alerter blocks the migration, and the migration
  blocks writes to the anomaly candidate table while the indexes
  build.

Stop the alerter, the server and the collector before you upgrade any
of them. Start the 2.0.0 collector first, confirm that the collector
has finished migrating the schema, and only then start the server and
the alerter.

### Authentication Database Format

The server migrates the authentication database, `auth.db` in the
server's data directory, on the first start of release 2.0.0. A server
from release 1.0.0 does not check the version of the database, and
running release 1.0.0 against the migrated database is not supported.
To return to release 1.0.0, restore `auth.db` from a backup taken
before the upgrade.

### Server Secret for Command-Line Subcommands

Every server subcommand that opens the authentication database, such
as `-add-user`, `-add-token` and `-list-tokens`, now reads the server
secret file first, and fails if the subcommand cannot read the file.
In release 1.0.0, only the server itself needed the secret; the
`-check-config` option does not read the secret.

The subcommands find the secret file in the same order as the server:

1. The path in the `secret_file` property of the configuration file.
2. `pgedge/ai-dba-server.secret` in the user configuration directory of
   the account that runs the command; the directory is
   `$XDG_CONFIG_HOME` when the variable is set, and `~/.config`
   otherwise.
3. `/etc/pgedge/ai-dba-server.secret`.

Run each subcommand as the account that runs the server, and pass the
server's configuration file with the `-config` option, so that the
subcommand finds the same secret file as the server.

### API Token Scopes

Release 2.0.0 enforces the scope of an API token in places that release
1.0.0 did not check. A token with an explicit scope, most often a token
owned by a superuser, can receive `403 Forbidden` for a request that
release 1.0.0 allowed, and no configuration option restores the old
behaviour. The following changes are the most likely to affect an
existing token:

- The server applies a token's connection scope to connections that
  belong to no group, and to tokens owned by a superuser.
- The server applies a token's MCP scope to the public MCP tools as
  well, so a token whose MCP scope names specific tools cannot call
  `store_memory`, `recall_memories` or `delete_memory` unless the
  scope names those tools too.
- An administrative change made with a token, such as a grant, cannot
  exceed the access the token itself holds.
- The `query_datastore` tool returns data only for the connections the
  caller can read.

Before you upgrade, list the tokens and review the scope of each scoped
token, starting with tokens owned by a superuser and tokens whose MCP
scope names specific tools. In the following example, the commands list
the tokens of a package installation and show the scope of the token
with the ID `3`:

```bash
cd /var/lib/pgedge/ai-dba-server
sudo -u pgedge -H ai-dba-server -list-tokens \
    -config /etc/pgedge/ai-dba-server.yaml \
    -data-dir /var/lib/pgedge/ai-dba-server
sudo -u pgedge -H ai-dba-server -show-token-scope -token-id 3 \
    -config /etc/pgedge/ai-dba-server.yaml \
    -data-dir /var/lib/pgedge/ai-dba-server
```

If a token is refused after the upgrade, clear the token's scope, add
the missing connection or tool to the scope, or change the integration
to stay within the scope. See
[Managing Tokens](managing-users-and-permissions/tokens.md) for the
scope commands.

### Connection Access Grants

Release 2.0.0 decides connection access with one rule, under which
only a grant that names a connection restricts the connection. This
change can widen access on upgrade:

- An `All Connections` grant no longer restricts every connection; in
  release 1.0.0, a single such grant to any group refused every user
  outside the group every connection.
- An `All Connections` grant at `read` no longer caps the members of
  the group, so a shared connection that no grant names stays open at
  `read_write` to every account, subject only to an API token's own
  scope.
- The owner of a connection always holds `read_write` access to the
  connection, whether or not a group restricts the connection.

If you used a read-only `All Connections` grant to keep a group
read-only, the members of the group gain write access when you
upgrade. Before you upgrade, grant the group `read` on each such
connection by name. See the
[permission model](managing-users-and-permissions/permission_model.md)
for the access rules.

### MCP Tool Changes

Release 2.0.0 changes two MCP tools in ways that break an integration
written for release 1.0.0:

- The `query_datastore` tool no longer accepts SQL, and rejects a call
  that passes a `sql` or `query` argument. A caller names a `source`,
  and selects data with arguments such as `columns`, `connection_ids`,
  a time range, `filters` and `shape`. The new `describe_datastore`
  tool lists the sources, and a grant of `query_datastore` also grants
  `describe_datastore`. See
  [Querying the Datastore](../user-guide/mcp-tools.md#querying-the-datastore)
  for the arguments.
- The `query_database` tool is read-only for every caller, including a
  superuser, and refuses a query that holds more than one statement.
  The tool no longer accepts a connection string in the query text; to
  query another database on a stored connection, pass `connection_id`
  with `database_name`.

### Account Lockout Default

In release 1.0.0, a configuration file that omitted the
`http.auth.max_failed_attempts_before_lockout` property disabled account
lockout. Release 2.0.0 applies the default of `10` failed attempts when
the property is omitted. To keep lockout disabled, set the property to
`0` explicitly.

### Notification Redirects

The alerter no longer follows an HTTP redirect when the alerter
delivers a Slack, Mattermost, Telegram or webhook notification; a
delivery that receives a `3xx` response fails. Before you upgrade,
check that the URL of each notification channel is the final URL, and
not a URL that the provider redirects. See
[Notification Channels](notification-channels.md) for the channel
settings.

### Retired Alert Rule

Release 2.0.0 retires the `table_bloat_ratio` alert rule. The upgrade
disables the rule, stops collecting the rule's metric and clears any
active alert the rule raised. Use the `dead_tuple_ratio` rule to
monitor table maintenance instead.

### Changed Defaults

Release 2.0.0 changes the following defaults, which apply to an
installation that leaves the setting unset; to keep a 1.0.0 value, set
the property explicitly:

- The server's default Anthropic model, `llm.model`, changes from
  `claude-sonnet-4-5` to `claude-sonnet-5`.
- The alerter's default reasoning models change from `gpt-4o-mini` to
  `gpt-6-luna` for OpenAI, from `claude-3-5-haiku-20241022` to
  `claude-haiku-4-5` for Anthropic, and from `gemini-2.5-flash` to
  `gemini-3.8-flash` for Gemini.
- The server's datastore connection pool, `database.pool_max_conns`,
  grows from 4 to 20 connections.
- The new `database.statement_timeout` property limits each server
  query on the datastore to 30 seconds by default.
- The alerter's `baselines.lookback_days` property rises from 7 to 15
  days; if you set the property explicitly, raise the value to at least
  15.
- A threshold alert is raised only after the threshold is breached on
  2 consecutive samples, and cleared only after 3 consecutive samples
  within the threshold; set `threshold.trigger_count` and
  `threshold.clear_count` to `1` to restore the 1.0.0 behaviour.
- Schema migration 21 retunes the built-in `cache_hit_ratio_low`
  rule from a warning below 80 percent to information below 50
  percent, and the `dead_tuple_ratio` rule from 20 percent to 50
  percent; the migration leaves a default you changed, and every
  override, unchanged.

### Changes That Do Not Affect an Upgrade from 1.0.0

The changelog for release 2.0.0 includes upgrade steps for two features
that release 1.0.0 did not include, so neither step applies to an
upgrade from release 1.0.0:

- The RBAC audit log is new in release 2.0.0, so you do not need the
  `-rechain-audit-log` command; the command applies only to a database
  written by a pre-release build.
- Federated login is new in release 2.0.0. The trusted proxy rule
  described in
  [Resolving the Trusted Proxy Error](upgrading.md#resolving-the-trusted-proxy-error)
  applies only when you enable federated login after the upgrade.

## Preparing for the Upgrade

Complete the steps in this section before you stop any component. The
backups are the only way to return to release 1.0.0, because neither
the datastore migration nor the authentication database migration can
be reversed.

### Backing Up the Datastore

Back up the datastore database with `pg_dump` in the custom format.
Take connection details from the `database` section of
`ai-dba-server.yaml`; the following example backs up the
`ai_workbench` database on the local host as the `postgres` user:

```bash
pg_dump -h localhost -U postgres -Fc \
    -f ai_workbench-1.0.0.dump ai_workbench
```

The collector writes to the datastore continuously, so a backup taken
while the collector runs does not include the samples collected after
the backup starts. To take a backup that matches the stopped
installation exactly, take the backup after you stop the components in
the upgrade procedure for your installation method.

For a Docker installation, run `pg_dump` in the `postgres` container.
In the following example, the command writes the backup to the current
directory on the host:

```bash
docker compose -f examples/docker-compose.production.yml \
    exec -T postgres pg_dump -U postgres -Fc ai_workbench \
    > ai_workbench-1.0.0.dump
```

### Backing Up the Server Data Directory

Back up the whole server data directory, which holds the `auth.db`
authentication database. Stop the server before you copy the directory,
so that the copy is consistent; the procedures in this page take the
backup at the right point. The location of the data directory depends
on the installation method:

| Installation method | Data directory                   |
|---------------------|----------------------------------|
| Binary              | `/var/lib/ai-workbench/data`     |
| Docker Compose      | The `server-data` volume         |
| Debian or RPM       | `/var/lib/pgedge/ai-dba-server`  |

The binary path is the path that the
[binary installation](../getting-started/binary_install.md) guide uses;
if you chose another path, the server's `-data-dir` option or the
`data_dir` property of the configuration file names the directory.

### Reviewing the Configuration

Compare each component's configuration file with the example
configuration for release 2.0.0, which ships in the `examples`
directory of each release archive and of the source repository. Check
the following points in particular:

- The `secret_file` property, which names the secret file that every
  subcommand opening the authentication database now reads.
- The `http.auth.max_failed_attempts_before_lockout` property, which
  now defaults to `10` when omitted.
- The URL of each notification channel, which must not redirect.

### Checking the Configuration with the New Server

A new release can refuse a configuration that an earlier release
accepted. The Debian and RPM packages check the configuration with the
new server before the packages replace any installed files. For a
binary or Docker installation, run the check yourself with the 2.0.0
server before you stop the 1.0.0 components; the binary and Docker
procedures below include the command. See
[Checking a Configuration by Hand](upgrading.md#checking-a-configuration-by-hand)
for a description of the `-check-config` option.

## Upgrading a Binary Installation

This procedure upgrades an installation set up with the
[binary installation](../getting-started/binary_install.md) guide and
the [systemd units](../getting-started/configuration/configure_systemd.md),
with the binaries in `/opt/ai-workbench`. Replace `user_name` with the
account that runs the services, and replace `linux-amd64` with the
platform of the release archives you use.

1. Download the 2.0.0 release archives from the
   [releases page](https://github.com/pgEdge/ai-dba-workbench/releases),
   and extract each archive into an empty staging directory:

    ```bash
    mkdir -p ~/ai-workbench-2.0.0
    cd ~/ai-workbench-2.0.0
    tar xzf ai-dba-collector-linux-amd64.tar.gz
    tar xzf ai-dba-server-linux-amd64.tar.gz
    tar xzf ai-dba-alerter-linux-amd64.tar.gz
    mkdir client
    tar xzf ai-dba-client.tar.gz -C client
    ```

2. Check the installed configuration with the 2.0.0 server. The command
   prints `Configuration OK` when the 2.0.0 server accepts the
   configuration; otherwise, correct the configuration before you
   continue:

    ```bash
    cd /opt/ai-workbench
    sudo -u user_name -H ~/ai-workbench-2.0.0/ai-dba-server \
        -check-config -config /etc/pgedge/ai-dba-server.yaml
    ```

    The command runs as `user_name` from the service's working
    directory, so the account needs read access to the staging
    directory.

3. Stop the alerter, the server and the collector, in that order:

    ```bash
    sudo systemctl stop pgedge-ai-dba-alerter
    sudo systemctl stop pgedge-ai-dba-server
    sudo systemctl stop pgedge-ai-dba-collector
    ```

4. Back up the datastore as described in
   [Backing Up the Datastore](#backing-up-the-datastore), and copy the
   installed binaries and the server data directory:

    ```bash
    sudo cp -a /opt/ai-workbench /opt/ai-workbench-1.0.0
    sudo cp -a /var/lib/ai-workbench/data \
        /var/lib/ai-workbench/data-1.0.0
    ```

5. Install the 2.0.0 binaries and the 2.0.0 client:

    ```bash
    cd ~/ai-workbench-2.0.0
    sudo cp ai-dba-collector ai-dba-server ai-dba-alerter \
        /opt/ai-workbench/
    sudo chmod +x /opt/ai-workbench/ai-dba-*
    sudo rm -rf /opt/ai-workbench/client
    sudo cp -r client /opt/ai-workbench/client
    ```

6. Start the collector, and wait for the collector to finish migrating
   the schema as described in
   [Confirming the Schema Migration](#confirming-the-schema-migration):

    ```bash
    sudo systemctl start pgedge-ai-dba-collector
    ```

7. Start the server, and then the alerter:

    ```bash
    sudo systemctl start pgedge-ai-dba-server
    sudo systemctl start pgedge-ai-dba-alerter
    ```

8. Verify the upgrade as described in
   [Verifying the Upgrade](#verifying-the-upgrade).

## Upgrading a Docker Installation

This procedure upgrades an installation started from
`examples/docker-compose.production.yml` in a clone of the repository,
as described in [Docker Installation](../getting-started/docker.md).
Run the commands from the root of the clone, with `POSTGRES_PASSWORD`
set as it was when you started the installation; Docker Compose refuses
to read the Compose file while the variable is unset.

An installation that uses the `latest` image tag moves to release
2.0.0 the next time Docker pulls the images. Until you are ready to
upgrade, set `VERSION=1.0.0` whenever you run `docker compose`, so that
a restart or a pull does not upgrade the installation unprepared.

1. Check the installed configuration with the 2.0.0 server image. The
   command prints `Configuration OK` when the 2.0.0 server accepts the
   configuration; otherwise, correct the configuration before you
   continue:

    ```bash
    VERSION=2.0.0 docker compose \
        -f examples/docker-compose.production.yml \
        run --rm --no-deps server \
        -check-config -config /etc/pgedge/ai-dba-server.yaml
    ```

2. Stop the alerter, the server and the collector, in that order,
   leaving the `postgres` and `client` services running:

    ```bash
    docker compose -f examples/docker-compose.production.yml \
        stop alerter server collector
    ```

3. Back up the datastore as described in
   [Backing Up the Datastore](#backing-up-the-datastore).

4. Back up the server data volume. Docker Compose prefixes the volume
   name with the project name, which is `examples` by default; the
   `docker volume ls` command shows the name. The following example
   archives the `examples_server-data` volume into the current
   directory:

    ```bash
    docker run --rm -v examples_server-data:/data:ro \
        -v "$PWD":/backup debian:bookworm-slim \
        tar czf /backup/server-data-1.0.0.tar.gz -C /data .
    ```

5. Update the clone to the 2.0.0 release, so that the Compose file and
   the files under `docker/config` match release 2.0.0. Git refuses to
   check out the release while a tracked file has local changes, and
   the 1.0.0 Docker guide edits `docker/config/ai-dba-server.yaml` to
   set the datastore password. Copy the file aside, discard the local
   changes, and check out the release:

    ```bash
    cp docker/config/ai-dba-server.yaml ~/ai-dba-server.yaml.1.0.0
    git checkout -- docker/config
    git fetch --tags
    git checkout v2.0.0
    ```

6. Reapply your edits to the 2.0.0 copy of
   `docker/config/ai-dba-server.yaml`, rather than copying the 1.0.0
   file over the 2.0.0 file. The 1.0.0 file sets an `http.auth.enabled`
   key that no release reads; the 2.0.0 file replaces the key with
   `http.auth.local.enabled`, which controls local login and defaults
   to `true`. The following command shows the differences between the
   two files:

    ```bash
    diff ~/ai-dba-server.yaml.1.0.0 docker/config/ai-dba-server.yaml
    ```

7. Pull the 2.0.0 images:

    ```bash
    VERSION=2.0.0 docker compose \
        -f examples/docker-compose.production.yml pull
    ```

8. Start the collector, and wait for the collector to finish migrating
   the schema as described in
   [Confirming the Schema Migration](#confirming-the-schema-migration):

    ```bash
    VERSION=2.0.0 docker compose \
        -f examples/docker-compose.production.yml up -d collector
    ```

9. Start the remaining services with the 2.0.0 images:

    ```bash
    VERSION=2.0.0 docker compose \
        -f examples/docker-compose.production.yml up -d
    ```

10. Verify the upgrade as described in
    [Verifying the Upgrade](#verifying-the-upgrade).

## Upgrading an RPM Installation

This procedure upgrades an installation from the RPM packages on a
RHEL-compatible system. The packages restart only the services that
are running during the upgrade, so services you stop before the upgrade
stay stopped until you start them.

1. Stop the alerter, the server and the collector, in that order:

    ```bash
    sudo systemctl stop pgedge-ai-dba-alerter
    sudo systemctl stop pgedge-ai-dba-server
    sudo systemctl stop pgedge-ai-dba-collector
    ```

2. Back up the datastore as described in
   [Backing Up the Datastore](#backing-up-the-datastore), and copy the
   server data directory:

    ```bash
    sudo cp -a /var/lib/pgedge/ai-dba-server \
        /var/lib/pgedge/ai-dba-server-1.0.0
    ```

3. Upgrade the packages:

    ```bash
    sudo dnf upgrade 'pgedge-ai-dba-*'
    ```

    The upgrade installs the new `pgedge-ai-dba-upgrade-check` package,
    which checks the installed server configuration with the 2.0.0
    server before the other packages replace any files. If the check
    fails, the upgrade stops and release 1.0.0 stays installed but
    stopped; start the collector, the server and the alerter again, in
    that order, whilst you fix the configuration as described in
    [Recovering from a Refused Upgrade](upgrading.md#recovering-from-a-refused-upgrade).
    Stop the services again before you rerun the upgrade, so that the
    collector migrates the schema first.

4. Look for configuration files that the upgrade saved with the
   `.rpmnew` suffix. The packages keep a configuration file you edited,
   and save the 2.0.0 default beside the file; compare the two files
   and merge any new settings you need:

    ```bash
    ls /etc/pgedge/*.rpmnew /etc/nginx/conf.d/*.rpmnew
    ```

5. Start the collector, and wait for the collector to finish migrating
   the schema as described in
   [Confirming the Schema Migration](#confirming-the-schema-migration):

    ```bash
    sudo systemctl start pgedge-ai-dba-collector
    ```

6. Start the server, and then the alerter:

    ```bash
    sudo systemctl start pgedge-ai-dba-server
    sudo systemctl start pgedge-ai-dba-alerter
    ```

7. Verify the upgrade as described in
   [Verifying the Upgrade](#verifying-the-upgrade).

## Upgrading a Debian or Ubuntu Installation

This procedure upgrades an installation from the Debian packages on
Debian or Ubuntu. The packages restart only the services that are
running during the upgrade, so services you stop before the upgrade
stay stopped until you start them.

1. Stop the alerter, the server and the collector, in that order:

    ```bash
    sudo systemctl stop pgedge-ai-dba-alerter
    sudo systemctl stop pgedge-ai-dba-server
    sudo systemctl stop pgedge-ai-dba-collector
    ```

2. Back up the datastore as described in
   [Backing Up the Datastore](#backing-up-the-datastore), and copy the
   server data directory:

    ```bash
    sudo cp -a /var/lib/pgedge/ai-dba-server \
        /var/lib/pgedge/ai-dba-server-1.0.0
    ```

3. Upgrade the packages. The upgrade installs the new
   `pgedge-ai-dba-upgrade-check` package, and `apt-get upgrade` keeps
   back an upgrade that needs a new package, so use `apt upgrade`
   instead:

    ```bash
    sudo apt update
    sudo apt upgrade
    ```

    The `pgedge-ai-dba-upgrade-check` package checks the installed
    server configuration with the 2.0.0 server before the other
    packages replace any files. If the check fails, the upgrade stops
    and release 1.0.0 stays installed but stopped; start the collector,
    the server and the alerter again, in that order, whilst you fix the
    configuration. The check package stays unconfigured until the check
    succeeds, and each later `apt` or `dpkg` run retries the check; once
    the configuration is corrected, `apt upgrade` completes the upgrade.
    Stop the services again before you run `apt upgrade`, so that the
    collector migrates the schema first. See
    [Recovering from a Refused Upgrade](upgrading.md#recovering-from-a-refused-upgrade)
    for the recovery steps.

4. Answer any prompt about a configuration file you edited. If you
   keep your version of the file, `dpkg` saves the 2.0.0 default beside
   the file with the `.dpkg-dist` suffix; compare the two files and
   merge any new settings you need:

    ```bash
    ls /etc/pgedge/*.dpkg-dist /etc/nginx/sites-available/*.dpkg-dist
    ```

5. Start the collector, and wait for the collector to finish migrating
   the schema as described in
   [Confirming the Schema Migration](#confirming-the-schema-migration):

    ```bash
    sudo systemctl start pgedge-ai-dba-collector
    ```

6. Start the server, and then the alerter:

    ```bash
    sudo systemctl start pgedge-ai-dba-server
    sudo systemctl start pgedge-ai-dba-alerter
    ```

7. Verify the upgrade as described in
   [Verifying the Upgrade](#verifying-the-upgrade).

## Verifying the Upgrade

The checks in this section confirm that each component runs release
2.0.0 against the migrated schema.

### Confirming the Schema Migration

The collector applies each migration in its own transaction and
records the migration in the `schema_version` table, so the highest
version in the table shows how far the migration has progressed. The
migration can take some time on a large datastore.

Print the schema version that the 2.0.0 collector migrates to; for a
package installation, the binary is `/usr/bin/ai-dba-collector`, and
for a Docker installation, run the binary in the collector image:

```bash
/opt/ai-workbench/ai-dba-collector -print-latest-schema-version
```

In the following example, the command prints the version a Docker
installation migrates to:

```bash
VERSION=2.0.0 docker compose \
    -f examples/docker-compose.production.yml \
    run --rm --no-deps collector -print-latest-schema-version
```

Query the highest version recorded in the datastore; the following
example connects as described in
[Backing Up the Datastore](#backing-up-the-datastore):

```bash
psql -h localhost -U postgres -d ai_workbench \
    -c 'SELECT max(version) FROM schema_version;'
```

For a Docker installation, run the query in the `postgres` container:

```bash
docker compose -f examples/docker-compose.production.yml \
    exec -T postgres psql -U postgres -d ai_workbench \
    -c 'SELECT max(version) FROM schema_version;'
```

When the two numbers match, the migration is complete and you can start
the server and the alerter. If the collector stops before the numbers
match, read the collector log for the migration that failed. Start the
collector with the `-v` option to log each migration as the collector
applies the migration.

### Checking the Components

After you start every component, confirm that each component is
running release 2.0.0:

1. Query the server's health endpoint; the response reports the server
   version. A binary or Docker installation listens on port `8080` by
   default, and a Debian or RPM installation on port `8443`:

    ```bash
    curl -s http://localhost:8080/health
    ```

2. Check that the collector and the alerter are running, and read the
   recent log output of each for errors. For a systemd installation,
   use the following commands; for a Docker installation, use
   `docker compose ps` and `docker compose logs`:

    ```bash
    sudo systemctl status pgedge-ai-dba-collector
    sudo systemctl status pgedge-ai-dba-alerter
    ```

3. Sign in to the web client, and check that the dashboards show new
   data for each monitored connection.

4. Run one server subcommand, such as `-list-tokens`, as the account
   that runs the server, to confirm that the subcommands find the
   server secret.

5. Exercise each integration that uses an API token or an MCP tool,
   and check for `403 Forbidden` responses as described in
   [API Token Scopes](#api-token-scopes).

See [Verifying the Health of Components](verify_health.md) for more
detail on each check.

## Rolling Back to 1.0.0

Release 2.0.0 migrates the datastore schema and the authentication
database forward only, and release 1.0.0 is not supported against
either after the migration. To return to release 1.0.0, restore both
from the backups you took before the upgrade, then reinstall the 1.0.0
software. Any data collected and any change made in the Workbench
after the upgrade is lost.

### Restoring the Datastore

Stop every Workbench component, then drop and recreate the datastore
database and restore the backup. In the following example, the
commands restore the backup into the `ai_workbench` database:

```bash
dropdb -h localhost -U postgres ai_workbench
createdb -h localhost -U postgres ai_workbench
pg_restore -h localhost -U postgres -d ai_workbench \
    ai_workbench-1.0.0.dump
```

For a Docker installation, stop the alerter, the server and the
collector, and run the same commands in the `postgres` container:

```bash
docker compose -f examples/docker-compose.production.yml \
    exec -T postgres dropdb -U postgres ai_workbench
docker compose -f examples/docker-compose.production.yml \
    exec -T postgres createdb -U postgres ai_workbench
docker compose -f examples/docker-compose.production.yml \
    exec -T postgres pg_restore -U postgres -d ai_workbench \
    < ai_workbench-1.0.0.dump
```

### Restoring the Server Data Directory

Replace the server data directory with the copy you took before the
upgrade. The copy holds the 1.0.0 `auth.db` and any `auth.db-wal` and
`auth.db-shm` files beside the database; restore the whole directory
rather than `auth.db` alone. For a package installation, use the
following commands:

```bash
sudo rm -rf /var/lib/pgedge/ai-dba-server
sudo cp -a /var/lib/pgedge/ai-dba-server-1.0.0 \
    /var/lib/pgedge/ai-dba-server
```

For a binary installation, restore `/var/lib/ai-workbench/data` from
`/var/lib/ai-workbench/data-1.0.0` in the same way. For a Docker
installation, restore the volume from the archive:

```bash
docker run --rm -v examples_server-data:/data \
    -v "$PWD":/backup debian:bookworm-slim \
    sh -c 'find /data -mindepth 1 -delete &&
        tar xzf /backup/server-data-1.0.0.tar.gz -C /data'
```

### Reinstalling the 1.0.0 Software

Reinstall release 1.0.0 with the method you used to install release
2.0.0, then start the collector, the server and the alerter in that
order:

- For a binary installation, copy the binaries and the client back
  from `/opt/ai-workbench-1.0.0`.
- For a Docker installation, copy your edited configuration files
  aside, discard the local changes with `git checkout -- docker/config`,
  check out the `v1.0.0` tag, reapply your edits, and start the
  services with `VERSION=1.0.0`.
- For an RPM or Debian installation, downgrade the packages to the
  1.0.0 version as the following examples show.

Name the 1.0.0 version explicitly when you downgrade a package; without
a version, `dnf downgrade` moves to the next lower version, which is
not 1.0.0 once a later release exists. In the following example, the
commands list the available versions and downgrade the RPM packages to
release 1.0.0:

```bash
dnf --showduplicates list 'pgedge-ai-dba-*'
sudo dnf downgrade pgedge-ai-dba-server-1.0.0 \
    pgedge-ai-dba-collector-1.0.0 pgedge-ai-dba-alerter-1.0.0 \
    pgedge-ai-dba-client-1.0.0
```

The `apt install` command needs the full package version that
`apt-cache policy` reports. In the following example, the commands
list the available versions and install release 1.0.0 with the
version `1.0.0-1`; replace the version with the one that
`apt-cache policy` shows:

```bash
apt-cache policy pgedge-ai-dba-server
sudo apt install --allow-downgrades \
    pgedge-ai-dba-server=1.0.0-1 pgedge-ai-dba-collector=1.0.0-1 \
    pgedge-ai-dba-alerter=1.0.0-1 pgedge-ai-dba-client=1.0.0-1
```

The 1.0.0 packages do not require the `pgedge-ai-dba-upgrade-check`
package; after the downgrade, you can remove the package with
`dnf remove` or `apt remove`.
