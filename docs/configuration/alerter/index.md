# Configuring the Alerter

The pgEdge AI DBA Workbench Alerter supports configuration through YAML files
and command-line flags. The alerter applies configuration settings in the
following order; later sources override earlier ones:

1. Default values built into the application.
2. Configuration file settings (YAML format).
3. Command-line flag overrides.

The alerter overrides a configuration file setting only when you pass the
corresponding flag; a flag you omit never overrides the file, even though the
flag has a built-in default. A flag you pass always wins, including when the
value you give the flag equals the flag's default.

The following reference pages describe the alerter settings in detail:

- [Configuration File Properties](configuration_file.md) describes the
  datastore, connection pool, threshold, baseline, correlation, and
  notification properties.
- [Anomaly Detection Properties](anomaly_detection.md) describes the three
  anomaly detection tiers and their settings.
- [LLM Provider Properties](llm_providers.md) describes the decision and
  reasoning models and the settings for each LLM provider.
- [Command-Line Flags](command_line_flags.md) lists the flags that override
  configuration file settings.

## Reloading or Restarting the Alerter

The alerter responds to Unix signals for operational control:
    
- `SIGINT` and `SIGTERM` trigger a graceful shutdown.
- `SIGHUP` reloads the configuration file without restarting the process.

In the following example, the `kill` command reloads the configuration:

```bash
kill -HUP $(pidof ai-dba-alerter)
```

A reload does not recreate the LLM providers, which the alerter builds once at
startup. Changing a provider (including `llm.decision_provider`), a model, a
base URL, an API key file, or a `reasoning_effort`, `prompt_caching`, or
`extra_body` setting therefore requires a restart; the reloaded values of
settings such as `anomaly.tier3.decision_suppress_threshold` apply at once.

A reload also leaves the datastore connection pool and the notification
manager as the alerter built them at startup. Changes to the `datastore`,
`pool`, and `notifications` sections therefore require a restart; for
example, enabling notifications in a reloaded file does not start notification
delivery.

A reload does not start background workers that were not running at startup.
When `anomaly.enabled` is `false` at startup, setting `anomaly.enabled` to
`true` in a reloaded file does not start the anomaly detector or the
re-evaluation worker; when `anomaly.reevaluation.enabled` is `false` at
startup, setting the option to `true` does not start the re-evaluation worker.
Restart the alerter to apply either change. Setting either option to `false` in
a reloaded file takes effect on the next detection or re-evaluation pass.

## Security Best Practices

Store API keys for LLM providers in files with restricted permissions. The
alerter reads API keys from the paths specified in the `api_key_file` options
of the `openai`, `anthropic`, `gemini`, `voyage`, and `typesafe` sections. When
the alerter cannot read a configured key file, the alerter logs a warning and
continues with the next configured file, keeping each key that it reads
successfully. A provider that requires a key it did not receive then fails to
initialize.

In the following example, the commands create an API key file with secure
permissions:

```bash
echo "sk-your-api-key-here" | sudo tee /etc/ai-workbench/openai-api-key.txt
sudo chmod 600 /etc/ai-workbench/openai-api-key.txt
sudo chown pgedge:pgedge /etc/ai-workbench/openai-api-key.txt
```

On a packaged installation, the alerter runs as the `pgedge` account, so the
`chown` command gives that account ownership of the key file; replace `pgedge`
with the account that runs the alerter on other installations.

The corresponding configuration then references the key file:

```yaml
llm:
  embedding_provider: openai
  reasoning_provider: openai
  openai:
    api_key_file: /etc/ai-workbench/openai-api-key.txt
    embedding_model: text-embedding-3-small
    reasoning_model: gpt-4o
```

For additional security in a production deployment, always use SSL with
certificate verification. In the following example, the `datastore` section
configures a secure connection with certificate verification:

```yaml
datastore:
  host: db.example.com
  database: ai_workbench
  username: ai_workbench
  password_file: /etc/ai-workbench/password.txt
  port: 5432
  sslmode: verify-full
  sslcert: /etc/ai-workbench/client-cert.pem
  sslkey: /etc/ai-workbench/client-key.pem
  sslrootcert: /etc/ai-workbench/ca-cert.pem
```
