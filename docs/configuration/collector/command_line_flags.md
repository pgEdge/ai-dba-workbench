# Command-Line Flags

Command-line flags override the collector's built-in defaults and the settings
in its configuration file. A flag takes effect only when you pass the flag on
the command line; a flag you omit never overrides the configuration file, even
though the flag has a built-in default.

The `-pg-*` flags set the connection to the Workbench datastore, the PostgreSQL
database where the collector stores the metrics it collects; each flag
overrides the matching option in the `datastore` section of the configuration
file. The flags do not configure the monitored servers, which you add in the
Workbench, as described in [Using the Workbench](../../user-guide/index.md).

The following table lists the available command-line flags:

| Flag | Default | Description |
|------|---------|-------------|
| `-config` | Auto-detected | Path to configuration file |
| `-v` | `false` | Enable verbose logging |
| `-print-latest-schema-version` | `false` | Print the newest datastore schema version that the collector knows and exit without connecting to the datastore |
| `-pg-host` | `localhost` | PostgreSQL hostname |
| `-pg-hostaddr` | none | PostgreSQL IP address |
| `-pg-database` | `ai_workbench` | Database name |
| `-pg-username` | `postgres` | Database username |
| `-pg-password-file` | none | Path to password file |
| `-pg-port` | `5432` | Database port |
| `-pg-sslmode` | `prefer` | SSL mode |
| `-pg-sslcert` | none | Client SSL certificate |
| `-pg-sslkey` | none | Client SSL key |
| `-pg-sslrootcert` | none | Root SSL certificate |
| `-use-coldfront-for-metrics` | `false` | Store metrics tables in pgEdge ColdFront |

In the following example, the command uses flags to override configuration file
settings:

```bash
./ai-dba-collector \
    -config /path/to/config.yaml \
    -pg-host localhost \
    -pg-database ai_workbench \
    -pg-username collector \
    -pg-password-file /path/to/password.txt \
    -pg-port 5432 \
    -pg-sslmode prefer
```
