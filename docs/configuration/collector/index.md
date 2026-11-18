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
- [Storing Metrics in pgEdge ColdFront](coldfront-metrics.md) describes how
  to store the metrics tables in Apache Iceberg on object storage.

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

Without systemd, stop the collector process by its process ID, wait for it to
exit, and then relaunch it. The following example finds the process by its full
command line, because Linux shortens the process name `ai-dba-collector` to 15
characters. Relaunch the collector with every flag that the original process
used; the example assumes that it used only `-config`:

```bash
PID=$(pgrep -f '/opt/ai-workbench/ai-dba-collector -config /etc/pgedge/ai-dba-collector.yaml')
kill "$PID"
while kill -0 "$PID" 2> /dev/null; do sleep 1; done
/opt/ai-workbench/ai-dba-collector -config /etc/pgedge/ai-dba-collector.yaml &
```

To check the flags that the running collector uses before you stop it, run
`ps -o args= -p "$PID"`.

Per-server probe configuration is the one exception: the collector stores it in
the datastore rather than the YAML file, polls for changes every 5 minutes, and
applies them automatically without a restart.

## Configuring Probes in the Workbench

The collector stores the probe collection intervals, retention periods, and
enabled states in the datastore rather than in the configuration file. Use the
`Probe Defaults` tab of the Administration console to set the global defaults,
which every server without an override follows. To override the settings for
one server, cluster, or group, use the `PROBE CONFIGURATION` tab of the
[server](../../user-guide/dashboards/server.md#probe-configuration-tab) or
[cluster](../../user-guide/dashboards/cluster.md#probe-configuration-tab)
settings dialog, or of the group edit dialog. The collector picks up changes at
every level within 5 minutes without a restart, including for servers that it
already monitors.
A collector that stores metrics in pgEdge ColdFront runs no garbage collection
and ignores retention changes.
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

Then set the `secret_file` property of both the server and the collector to
`/etc/pgedge/server.secret`, so that both components use the same secret.

The account that runs each component must be able to read the secret file. On
a packaged installation, the server, the collector, and the alerter run as the
`pgedge` account in the `pgedge` group; the following commands give that group
read access to the secret. Replace `/etc/pgedge/server.secret` with the path in
your `secret_file` setting; on other installations, replace the `pgedge` group
in `root:pgedge` with a group that grants the components' account read access:

```bash
sudo chown root:pgedge /etc/pgedge/server.secret
sudo chmod 640 /etc/pgedge/server.secret
```

When the server and the collector run as different accounts that share no
group, grant both accounts read access to the secret file, either through a
group that both accounts belong to or through an access control list, and keep
the file unreadable by every other account. When the alerter reads the same
secret file to send notifications, grant its account read access in the same
way.

Keep this file secure with restricted permissions. Loss of the secret file
requires re-entering all monitored connection passwords. The server also keys
the audit log from the secret, so a replacement secret leaves the existing
audit events unverifiable and stops the audit retention purge.
[Recovering a Log That No Longer Verifies](../../admin-guide/audit-log.md#recovering-a-log-that-no-longer-verifies)
describes how to re-anchor the log; re-anchoring accepts the affected events as
history, but does not restore the proof that the server wrote them. Do not manually
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
runs as the `pgedge` account in the `pgedge` group. Use the path in your
`datastore.password_file` setting in place of `/etc/pgedge/password.txt`. On
other installations, replace the `pgedge` group in `root:pgedge` with a group
that grants the collector's account read access, and replace the `pgedge`
account in the password file's `chown` command with the account that runs the
collector:

```bash
sudo chown root:pgedge /etc/pgedge/ai-dba-collector.yaml
sudo chmod 640 /etc/pgedge/ai-dba-collector.yaml
sudo chown pgedge /etc/pgedge/password.txt
sudo chmod 600 /etc/pgedge/password.txt
```

The example configuration files for the collector, the alerter, and the server
all name `/etc/pgedge/password.txt` as the datastore password file, and the
packages run all three services as the `pgedge` account. When the services run
under different accounts and share one password file, grant each account read
access through a shared group or an ACL; alternatively, give each service its
own password file and point each configuration at that file.

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
