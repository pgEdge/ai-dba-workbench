# Configuring the Collector

The collector supports configuration through a YAML file and command-line
flags. The collector loads configuration in the following order; later sources
override earlier ones:

1. Built-in defaults.
2. Configuration file.
3. Command-line flags.

The collector overrides a configuration file setting only when you pass the
corresponding flag; a flag you omit never overrides the file, even though the
flag has a built-in default. A flag you pass always wins, including when the
value you give it equals that flag's default, such as `-pg-port 5432`.

The following reference pages describe the collector settings in detail:

- [Configuration File Properties](configuration_file.md) describes the
  locations the collector searches for the file, the startup validation rules,
  and the datastore, connection pool, and probe scheduler properties.
- [Command-Line Flags](command_line_flags.md) lists the flags that override
  configuration file settings.
- [Configuration Examples](configuration_examples.md) provides sample
  configuration files for common deployments.

## Reloading or Restarting the Collector

The collector has no signal-based reload for its configuration file, unlike the
server and alerter. Changing any property or command-line flag requires a full
restart to take effect. The collector's shutdown handler only responds to
`SIGINT` and `SIGTERM`, and both signals stop the process rather than
reapplying configuration.

If you have configured systemd, you can restart the service through systemctl
to apply configuration changes:

```bash
sudo systemctl restart pgedge-ai-dba-collector
```

Without systemd, stop and relaunch the process manually with the same
configuration flag used originally:

```bash
pkill -f ai-dba-collector
/opt/ai-workbench/ai-dba-collector -config /etc/pgedge/ai-dba-collector.yaml &
```

Per-server probe configuration is the one exception: the collector stores it in
the datastore rather than the YAML file, polls for changes every 5 minutes, and
applies them automatically without a restart.

## Configuring Probes in the Workbench

The collector stores the probe collection intervals, retention periods, and
enabled states in the datastore rather than in the configuration file. Use the
`Probe Defaults` tab of the Administration console to set the global defaults;
a server inherits the defaults when the collector first runs a probe for that
server. To override the settings for one server, cluster, or group, use the
`PROBE CONFIGURATION` tab of the
[server](../../user-guide/dashboards/server.md#probe-configuration-tab) or
[cluster](../../user-guide/dashboards/cluster.md#probe-configuration-tab)
settings dialog, or of the group edit dialog. The collector picks up changes to
the server-level configuration that a connection uses within 5 minutes without
a restart; cluster, group, and global changes apply only to connections without
a server-level configuration for the probe.
[Managing Probes](../../admin-guide/probes/index.md) describes the override
hierarchy and how to change probe settings with SQL.

## Security Best Practices

The collector uses a secret file and AES-256-GCM encryption to protect stored
passwords; the secret file is required. The `secret_file` option specifies the
path to a file containing the per-installation secret for password encryption.

The collector searches the following locations in order:

1. The per-user config directory at
   `~/.config/pgedge/ai-dba-collector.secret` on Linux (honouring
   `$XDG_CONFIG_HOME`),
   `~/Library/Application Support/pgedge/ai-dba-collector.secret` on macOS,
   and `%AppData%\pgedge\ai-dba-collector.secret` on Windows.
2. `/etc/pgedge/ai-dba-collector.secret` (system-wide).

!!! note

    The collector no longer searches the binary directory or the current
    working directory for the secret file.

The collector uses AES-256-GCM encryption to protect stored passwords and
encrypts each password with a unique cryptographically random salt. It derives
the encryption key from the server secret using PBKDF2 with SHA256 and 100,000
iterations.

The collector must use the same secret as the server, because the server
encrypts the connection passwords that the collector decrypts. The installation
guides point both components at `/etc/pgedge/server.secret` with the
`secret_file` property. When the server already has a secret, set the
collector's `secret_file` property to the server's secret file instead of
generating a new secret; a different secret prevents the collector from
decrypting the stored passwords.

When no secret exists yet, the following example generates one; the `if` test
leaves an existing secret file unchanged:

```bash
if [ ! -e /etc/pgedge/server.secret ]; then
    sudo sh -c 'umask 077 && openssl rand -base64 32 \
        > /etc/pgedge/server.secret'
fi
```

The account that runs each component must be able to read the secret file. On
a packaged installation, the server, the collector, and the alerter run as the
`pgedge` account; the following commands give that account read access to the
secret. Replace `pgedge` with the account that runs the components on other
installations:

```bash
sudo chown root:pgedge /etc/pgedge/server.secret
sudo chmod 640 /etc/pgedge/server.secret
```

Keep this file secure with restricted permissions. Loss of the secret file
requires re-entering all monitored connection passwords. Do not manually
encrypt passwords; use the MCP server API to create and manage connections with
passwords.

The credentials stored against a monitored connection belong to a PostgreSQL
role on the monitored server, which is separate from the datastore role.
[Preparing Monitored Servers](../../admin-guide/monitored-database-privileges.md)
describes the grants that role needs, including the `pg_monitor` predefined
role and the per-database `CONNECT` privilege.

The following commands set restrictive file permissions on the configuration
file and on the password file that the `datastore.password_file` option names;
the commands match the ownership that the packages apply, where the collector
runs as the `pgedge` account. Replace `/etc/pgedge/password.txt` with the path
in your `datastore.password_file` setting, and replace `pgedge` with the
account that runs the collector on other installations:

```bash
sudo chown root:pgedge /etc/pgedge/ai-dba-collector.yaml
sudo chmod 640 /etc/pgedge/ai-dba-collector.yaml
sudo chown pgedge /etc/pgedge/password.txt
sudo chmod 600 /etc/pgedge/password.txt
```

It's safer to use dedicated password files rather than inline passwords.
Generate strong random secrets for the server secret. Never commit
configuration files with real secrets to version control.

For additional security in a production deployment, always use SSL with
certificate verification:

```yaml
datastore:
  sslmode: verify-full
  sslcert: /path/to/client-cert.pem
  sslkey: /path/to/client-key.pem
  sslrootcert: /path/to/ca-cert.pem
```
