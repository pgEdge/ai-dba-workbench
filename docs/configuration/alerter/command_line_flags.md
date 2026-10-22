# Command-Line Flags

Command-line flags override the alerter's built-in defaults and the settings in
its configuration file. A flag takes effect only when you pass the flag on the
command line; a flag you omit never overrides the configuration file. The
alerter reapplies the flags you passed after each `SIGHUP` reload, so a reload
does not discard them.

The `-db-*` flags set the connection to the Workbench datastore, the PostgreSQL
database from which the alerter reads metrics and in which it records alerts;
each flag overrides the matching option in the `datastore` section of the
configuration file. The alerter never connects to the monitored servers, so no
flag configures them.

The following table lists the available command-line flags:

| Flag | Description | Default |
|------|-------------|---------|
| `-config` | Path to configuration file | Auto-detected |
| `-debug` | Enable debug logging | `false` |
| `-db-host` | Database host | None |
| `-db-port` | Database port | None |
| `-db-name` | Database name | None |
| `-db-user` | Database user | None |
| `-db-password-file` | Path to a file that contains the database password | None |
| `-db-sslmode` | Database SSL mode | None |

In the following example, the alerter starts with debug logging and a custom
configuration file:

```bash
./ai-dba-alerter -debug \
    -config /etc/ai-workbench/alerter.yaml
```

In the following example, the alerter connects to a specific database; the
flags override the matching settings in any configuration file that the alerter
finds in its default locations, and the alerter uses the rest of that file:

```bash
./ai-dba-alerter \
    -db-host db.example.com \
    -db-name ai_workbench \
    -db-user alerter \
    -db-password-file /etc/ai-workbench/password.txt
```
