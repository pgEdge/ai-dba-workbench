# Configuration Examples

The following examples show minimal, production, and development configurations
for the Collector.

## Minimal Configuration

This example shows the minimum settings required by the Collector.

```yaml
datastore:
  host: localhost
  database: ai_workbench
  username: collector
  password_file: /etc/ai-workbench/password.txt
```

## Production Configuration

This example shows a production configuration with SSL verification and a
connection pool.

```yaml
datastore:
  host: db.internal.example.com
  database: ai_workbench_prod
  username: ai_workbench
  password_file: /var/secrets/password.txt
  port: 5432
  sslmode: verify-full
  sslcert: /etc/ai-workbench/certs/client.pem
  sslkey: /etc/ai-workbench/certs/client-key.pem
  sslrootcert: /etc/ai-workbench/certs/ca.pem

pool:
  datastore_max_connections: 100
  datastore_max_idle_seconds: 300
  datastore_max_wait_seconds: 60
  max_connections_per_server: 10
  monitored_max_idle_seconds: 300
  monitored_max_wait_seconds: 120

secret_file: /var/secrets/collector.secret
```

## Development Configuration

This example shows a local development configuration with SSL disabled and a
small connection pool.

```yaml
datastore:
  host: localhost
  database: ai_workbench_dev
  username: postgres
  port: 5432
  sslmode: disable

pool:
  datastore_max_connections: 10
  max_connections_per_server: 3

secret_file: ./ai-dba-collector.secret
```
