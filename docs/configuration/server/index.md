# Configuring the Server

The MCP server supports configuration through a YAML file, command-line flags,
and environment variables.

The server applies configuration values in the following order from highest to
lowest priority:

1. Command-line flags (highest priority).
2. Environment variables.
3. Configuration file.
4. Built-in defaults (lowest priority).

Sending a SIGHUP signal reloads a small set of settings, such as the federated
login policy, without interrupting active connections. Most settings, including
the datastore connection, LLM, knowledgebase, authentication, and HTTP server
settings, take effect only when the server restarts;
[Reloading or Restarting the Server](#reloading-or-restarting-the-server)
lists which settings a reload applies.

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

Without systemd, stop the process, wait for it to exit, and then relaunch it
manually:

```bash
pkill -x ai-dba-server
while pgrep -x ai-dba-server > /dev/null; do sleep 1; done
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

A `SIGHUP` signal reloads the federated login policy under `http.auth.oidc`,
as described in [Configuring Single Sign-On](single_sign_on.md).

The server reads the following settings only at startup, so a change to them
needs a restart:

- The datastore connection settings under `database`, such as the host and the
  credentials; the server opens its datastore connection pool at startup and
  keeps it across a reload.
- The LLM settings under `llm`, including the provider, the model, the API
  keys, and the AI Overview rate limits under `llm.rate_limit`.
- The knowledgebase settings.

A database setting given on the command line at startup, such as `-db-host`,
keeps priority over the configuration file on every reload, as it does at
startup.

The other authentication settings and the HTTP server settings require a full
restart. A reload keeps the value the server started with and logs a warning
for each such setting whose change the server detects. These settings include
`http.auth.max_failed_attempts_before_lockout`, which the authentication store
reads once at startup, so account lockout stays as configured at the last
restart until the server restarts again.
