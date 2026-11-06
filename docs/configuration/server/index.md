# Configuring the Server

The MCP server supports configuration through a YAML file, command-line flags,
and environment variables.

The server applies configuration values in the following order from highest to
lowest priority:

1. Command-line flags (highest priority).
2. Environment variables.
3. Configuration file.
4. Built-in defaults (lowest priority).

The server does not require a restart after every configuration change. Sending
a SIGHUP signal reloads database connection, LLM proxy, and knowledgebase
settings, and the federated login policy, at runtime without interrupting
active connections. The other authentication settings and the HTTP server
settings are exceptions; changes to either require a full restart to take
effect, since these govern the listener and session state the server
establishes at startup.

The following reference pages describe the server settings in detail:

- [Configuration File Properties](configuration_file.md) describes every
  property in the server's YAML configuration file.
- [Command-Line Options](command_line_options.md) lists the flags that
  configure the server and manage users, groups, privileges, and tokens.
- [Environment Variables](environment_variables.md) describes the variables
  that override configuration file settings.
- [Configuring Single Sign-On](single_sign_on.md) describes how to configure
  federated login through an OpenID Connect identity provider.
- [Configuration Examples](configuration_examples.md) provides sample
  configuration files for common deployments.

## Reloading or Restarting the Server

If you have configured the server to use systemd, you can restart the service
with `systemctl` to apply changes that require a full restart, such as
authentication or HTTP server settings:

```bash
sudo systemctl restart pgedge-ai-dba-server
```

Without systemd, stop and relaunch the process manually:

```bash
pkill -f ai-dba-server
/opt/ai-workbench/ai-dba-server -config /etc/pgedge/ai-dba-server.yaml &
```

!!! hint

    Docker Compose deployments use a dedicated `restart` subcommand instead;
    see
    [Deploying the Workbench with Docker](../../getting-started/docker.md#server-cannot-connect-to-the-database)
    for details.

If you have made configuration changes that require only a reload, you can use
a runtime configuration reload via `SIGHUP` to update the configuration:

```bash
kill -SIGHUP $(pgrep ai-dba-server)
```

A `SIGHUP` signal reloads the following settings:

- Database connection settings.
- LLM proxy settings.
- Knowledgebase settings.
- The federated login policy under `http.auth.oidc`, as described in
  [Configuring Single Sign-On](single_sign_on.md).

A database setting given on the command line at startup, such as `-db-host`,
keeps priority over the configuration file on every reload, as it does at
startup.

The other authentication settings and the HTTP server settings require a full
restart. A reload keeps the value the server started with and logs a warning
for each such setting whose change the server detects. These settings include
`http.auth.max_failed_attempts_before_lockout`, which the authentication store
reads once at startup, so account lockout stays as configured at the last
restart until the server restarts again.
