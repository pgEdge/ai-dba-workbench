# Configuration File Properties

The server reads its settings from a YAML configuration file; command-line
flags and environment variables override the matching settings in the file.

The server searches for its configuration file in the following order:

1. The absolute path that the `-config` flag specifies.
2. The per-user `config` directory at `~/.config/pgedge/ai-dba-server.yaml` on
   Linux (honouring `$XDG_CONFIG_HOME`),
   `~/Library/Application Support/pgedge/ai-dba-server.yaml` on macOS, and
   `%AppData%\pgedge\ai-dba-server.yaml` on Windows.
3. The system-wide file at `/etc/pgedge/ai-dba-server.yaml`.

If `-config` is set and the file is missing, the server exits with an error. If
`-config` is not set and none of the default locations contain a configuration
file, the server uses built-in defaults silently. The server does not search
the binary directory or the current working directory.

The server searches the same locations for a secret file named
`ai-dba-server.secret` when the configuration file sets no `secret_file`
property. Unlike the configuration file, the secret file is required: if the
server finds no secret file, the server exits with an error at startup.

The following sections describe the properties in each section of the
configuration file:

- [HTTP Server Properties](#http-server-properties)
    - [Using the auth Properties](#using-the-auth-properties)
    - [Using the cors_origin Property](#using-the-cors_origin-property)
- [Connection Security Properties](#connection-security-properties)
- [Database Properties](#database-properties)
    - [Using the statement_timeout Property](#using-the-statement_timeout-property)
    - [Using the pool_max_conns Property](#using-the-pool_max_conns-property)
- [Embedding Properties](#embedding-properties)
- [LLM Proxy Properties](#llm-proxy-properties)
    - [Using the `temperature` Property](#using-the-temperature-property)
    - [Using the `max_tokens` Property](#using-the-max_tokens-property)
    - [Using the `timeout_seconds` Property](#using-the-timeout_seconds-property)
    - [Using the `compact_tool_descriptions` Property](#using-the-compact_tool_descriptions-property)
    - [Using the `reasoning_effort` Property](#using-the-reasoning_effort-property)
    - [Using the `prompt_caching` Property](#using-the-prompt_caching-property)
    - [Using the `extra_body` Property](#using-the-extra_body-property)
    - [Using the `openai_base_url` Property](#using-the-openai_base_url-property)
    - [Using the Custom Header Properties](#using-the-custom-header-properties)
- [Knowledgebase Properties](#knowledgebase-properties)
- [Memory Properties](#memory-properties)
    - [Using the Memory Tool Properties](#using-the-memory-tool-properties)
- [Built-in Features Properties](#built-in-features-properties)
    - [Using the Datastore Query Tool Properties](#using-the-datastore-query-tool-properties)
- [Paths and Directories](#paths-and-directories)
    - [Using the data_dir Property](#using-the-data_dir-property)
    - [Using the trace_file Property](#using-the-trace_file-property)

For sample configuration files, see
[Configuration Examples](configuration_examples.md).

## HTTP Server Properties

The HTTP server properties configure the listener address, TLS certificates,
and authentication rate limiting.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `address` | string | `:8080` | Listen address |
| `tls.enabled` | bool | `false` | Enable TLS |
| `tls.cert_file` | string | | Certificate file |
| `tls.key_file` | string | | Private key file |
| `tls.chain_file` | string | | Certificate chain |
| `trusted_proxies` | list | `[]` | Trusted proxy CIDRs; required behind a reverse proxy when OIDC login is enabled |
| `cors_origin` | string | `""` | Allowed CORS origin |
| `hsts_enabled` | bool | `false` | Send the Strict-Transport-Security header |
| `auth.max_failed_attempts_before_lockout` | int | `10` | Lock after N failures (`0` disables); restart required |
| `auth.max_user_token_days` | int | `0` | Max token lifetime |
| `auth.rate_limit_window_minutes` | int | `15` | Rate limit window |
| `auth.rate_limit_max_attempts` | int | `10` | Max attempts per window |
| `auth.audit_retention_days` | int | `90` | Days to keep RBAC audit events (`0` keeps forever) |
| `auth.local.enabled` | bool | `true` | Enable username and password login |
| `auth.oidc.enabled` | bool | `false` | Enable federated login |
| `auth.oidc.issuer` | string | | Identity provider issuer URL |
| `auth.oidc.client_id` | string | | OAuth 2.0 client identifier |
| `auth.oidc.client_secret` | string | | Inline client secret |
| `auth.oidc.client_secret_file` | string | | File holding the client secret |
| `auth.oidc.redirect_url` | string | | Callback URL registered at the provider |
| `auth.oidc.scopes` | list | `[openid, email, profile]` | Scopes requested at the provider |
| `auth.oidc.username_claim` | string | `email` | Claim used as the username |
| `auth.oidc.display_name_claim` | string | `name` | Claim used as the display name |
| `auth.oidc.groups_claim` | string | `groups` | Claim carrying group membership |
| `auth.oidc.button_label` | string | `Sign in with your identity provider` | Federated login button label |
| `auth.oidc.provision_users` | bool | `false` | Create an account on first login |
| `auth.oidc.allowed_email_domains` | list | `[]` | Permitted verified email domains |
| `auth.oidc.superuser_group` | string | | Provider group granting superuser |
| `auth.oidc.group_map` | map | `{}` | Provider group to Workbench group map |
| `auth.oidc.allow_unprefixed_state_cookie` | bool | `false` | Accept an unprefixed login state cookie behind a proxy |
| `auth.oidc.allow_api_tokens` | bool | `false` | Let federated users own API tokens |
| `auth.oidc.auth_params` | map | `{}` | Extra authorisation request parameters |

### Using the auth Properties

The `auth` section governs both login methods. Local username and password
login is on unless `auth.local.enabled` is set to `false`, and federated login
is off unless `auth.oidc.enabled` is set to `true`. The server refuses to start
when neither method is available.

While federated login is disabled, the server ignores every `auth.oidc.*`
option except `auth_params`, which the server still validates, and
`allow_api_tokens`, which still applies to accounts linked to a provider
earlier. The
[Configuring Single Sign-On](single_sign_on.md) page describes each option in full, along with registering the Workbench at an
identity provider and mapping provider groups onto Workbench groups.

The server also refuses to start when federated login is enabled with an
`https` `auth.oidc.redirect_url`, `tls.enabled` set to `false`, and an empty
`trusted_proxies` list. In that deployment, the login state cookie cannot use
the `__Host-` name prefix, so a sibling subdomain that can set cookies can log
users in as an attacker. Set `trusted_proxies` to the reverse proxy's address,
or set `auth.oidc.allow_unprefixed_state_cookie` to `true` to accept the risk.

The server likewise refuses to start, whether or not federated login is
enabled, when `auth.oidc.auth_params` names a parameter the Workbench sets
itself, such as `state` or `redirect_uri`; the Single Sign-On guide lists those
parameters, and gives the Google example that needs `access_type: offline`.

!!! warning

    While `auth.oidc.allow_api_tokens` is `false`, the default, an account that
    signs in through the identity provider cannot create API tokens, and any
    token such an account already owns is refused on use. Switching the option
    on lets those tokens keep working after the person is removed at the
    identity provider, until each token expires or is revoked, or the Workbench
    account is disabled or deleted, because the provider is never consulted
    when a token is used. Use a local service account for automation instead.

### Using the cors_origin Property

The `cors_origin` option sets the value for the `Access-Control-Allow-Origin`
response header. The empty default suits same-origin deployments, where the web
client and the server share an origin. The server omits CORS headers entirely
in that case.

Set `cors_origin` to an explicit origin when the web client runs on a different
origin than the server. For example, use `https://workbench.example.com` as the
value. The server returns `Access-Control-Allow-Credentials: true` so browsers
can attach session cookies to cross-origin requests.

The server rejects a `cors_origin` value of `"*"` at startup when
authentication is enabled. Browsers disallow combining
`Access-Control-Allow-Origin: *` with `Access-Control-Allow-Credentials: true`
per the Fetch spec. Use an explicit origin, or leave the option empty for
same-origin deployments.

In the following example, the `http` section allows cross-origin requests from
a dedicated workbench host:

```yaml
http:
  address: ":8443"
  cors_origin: "https://workbench.example.com"
  auth:
    local:
      enabled: true
```

## Connection Security Properties

The connection security section controls SSRF protection for user-created
database connections.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `allow_internal_networks` | bool | `false` | Allow RFC 1918 addresses |
| `allowed_hosts` | list | `[]` | Always-permitted hosts |
| `blocked_hosts` | list | `[]` | Always-blocked hosts |

## Database Properties

The database properties configure the PostgreSQL connection and connection pool
for the server's datastore.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `host` | string | `localhost` | Database host |
| `port` | int | `5432` | Database port |
| `database` | string | `postgres` | Database name |
| `user` | string | | Database user (required) |
| `password` | string | | Database password |
| `password_file` | string | | DB password file (if `password` empty) |
| `sslmode` | string | `prefer` | SSL mode |
| `pool_max_conns` | int | `20` | Max pool connections |
| `pool_min_conns` | int | `0` | Min pool connections |
| `pool_max_conn_idle_time` | string | `30m` | Max idle time |
| `statement_timeout` | string | `30s` | Server-side statement timeout |

### Using the statement_timeout Property

The `statement_timeout` option bounds how long a single statement may run on a
datastore pool connection before PostgreSQL cancels the statement. The server
already cancels its own queries when the request that asked for them has given
up, but that cancellation is a request sent over the same connection. A lost
cancel, or a backend that is not at a point where it can be interrupted,
leaves the query running and holding one of the few pool connections; the
server-side timeout is what actually frees the connection.

The default of `30s` matches the longest deadline the server grants a
datastore query, so the timeout rejects nothing that would otherwise have
completed. The value accepts any Go duration string, such as `45s` or
`1m30s`. A value of `0` disables the timeout; disabling the timeout is not
recommended, because a single runaway query can then starve the pool.

### Using the pool_max_conns Property

The `pool_max_conns` option sets the size of the datastore connection pool
that every API request shares. A single dashboard page load issues more than
twenty metrics requests at once, so a small pool makes a page wait on itself
and lets a few slow requests hold up the rest of the API. The default of `20`
leaves room for a second user. Together with the collector's default of 25 and
the alerter's default of 10, the server's pool uses 55 of the 97 connections
that the PostgreSQL default `max_connections` of 100 makes available to
ordinary users. Raise the datastore's `max_connections` before raising any of
the three pools past that total.

The server sizes the datastore pool at startup, so a change takes effect on
restart rather than on a `SIGHUP` reload. When the option is set explicitly,
the same value also caps each per-session pool that the server opens to a
monitored database, and a per-session pool opened after a `SIGHUP` reload uses
the reloaded value. When the option is unset, including when the database is
configured only through the `-db-*` command-line flags, those pools use the
connection library's default.

## Embedding Properties

The `provider` option accepts `voyage`, `openai`, `gemini`, or `ollama`. The
`model` option accepts any model name the chosen provider recognizes; the
server passes the name through rather than checking a fixed list, so a new
provider model works as soon as the provider publishes the model. Leaving
`model` empty selects the default model for the provider. Google publishes
`gemini-embedding-001` (the default, 3072 dimensions) and `gemini-embedding-2`
(also 3072 dimensions); model availability varies by Gemini API key tier, so
run ListModels to verify which embedding models a given key can access.

Because the server does not restrict the model name, an OpenAI-compatible
local model server such as llama.cpp or vLLM can supply embeddings. Set
`provider` to `openai`, point `openai_base_url` at that server, and set
`model` to the name the server provides.

The configured `model` must not produce vectors with more than 4000
dimensions. The server stores chat memory embeddings as `halfvec(4000)` and
zero-pads each embedding to 4000 dimensions; 4000 is pgvector's HNSW index
limit for the `halfvec` type. The server rejects a model that exceeds 4000
dimensions with a clear error rather than truncating the vector. Storing
embeddings as `halfvec` requires pgvector `0.7.0` or newer; upgrading an
existing installation against an older pgvector causes the embedding-column
migration to fail.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `false` | Enable embeddings |
| `provider` | string | `ollama` | Provider name |
| `model` | string | varies | Model name |
| `ollama_url` | string | `http://localhost:11434` | Ollama URL |
| `voyage_api_key_file` | string | | Voyage API key path |
| `openai_api_key_file` | string | | OpenAI API key path |
| `gemini_api_key_file` | string | | Gemini API key path |
| `voyage_base_url` | string | `https://api.voyageai.com/v1/embeddings` | Voyage base URL |
| `openai_base_url` | string | `https://api.openai.com/v1` | OpenAI base URL |
| `gemini_base_url` | string | `https://generativelanguage.googleapis.com` | Gemini base URL |

## LLM Proxy Properties

The LLM proxy is optional. When no valid API key is configured for the chosen
provider, the server disables all AI features at startup. The web client
detects the server's capabilities through the `/api/v1/capabilities` endpoint
and hides all AI-dependent UI elements. The hidden elements include the AI
Overview, Ask Ellie chat, alert analysis, chart analysis, and server analysis.
The system continues to function as a monitoring tool without AI features.

For Ollama, a valid URL is sufficient because Ollama does not require an API
key. For Anthropic and Gemini, the corresponding API key file must contain a
valid key. For OpenAI, the API key file must contain a valid key unless
`openai_base_url` points to an OpenAI-compatible server that needs no key; see
[Using the `openai_base_url` Property](#using-the-openai_base_url-property).

The settings in this section govern every AI feature the server drives,
including the Ask Ellie chat proxy, the AI Overview, and the server-info AI
database analysis. The following table describes the LLM proxy options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `provider` | string | `anthropic` | LLM provider |
| `model` | string | `claude-sonnet-5` | Model name |
| `anthropic_api_key_file` | string | | Anthropic key path |
| `openai_api_key_file` | string | | OpenAI key path |
| `gemini_api_key_file` | string | | Gemini key path |
| `ollama_url` | string | `http://localhost:11434` | Ollama URL |
| `max_tokens` | int | `4096` | Max output tokens per response |
| `temperature` | float | `0.7` | Sampling temperature; `0` is honored |
| `max_iterations` | int | `50` | Max tool-call iterations |
| `compact_tool_descriptions` | string | `auto` | Tool description mode |
| `timeout_seconds` | int | `120` | LLM HTTP request timeout |
| `anthropic_base_url` | string | `https://api.anthropic.com/v1` | Anthropic base URL |
| `openai_base_url` | string | `https://api.openai.com/v1` | OpenAI base URL |
| `gemini_base_url` | string | `https://generativelanguage.googleapis.com` | Gemini base URL |
| `reasoning_effort` | string | | Reasoning effort level |
| `prompt_caching` | string | | Prompt caching mode |
| `extra_body` | mapping | | Extra request body fields (OpenAI only) |
| `custom_headers` | mapping | | Headers sent to every provider |
| `custom_headers_files` | mapping | | Header names mapped to secret files |
| `anthropic_custom_headers` | mapping | | Headers sent to Anthropic only |
| `openai_custom_headers` | mapping | | Headers sent to OpenAI only |
| `gemini_custom_headers` | mapping | | Headers sent to Gemini only |
| `ollama_custom_headers` | mapping | | Headers sent to Ollama only |

### Using the `temperature` Property

Some models accept only their own default temperature; these models include
`claude-sonnet-5`, the default model, and OpenAI's GPT-6 models. When a model
rejects the configured `temperature`, the server resends the request without
the setting and omits the setting from later requests. The model's own default
then applies, so the `temperature` option has no effect, and the server does
not log the change.

### Using the `max_tokens` Property

The `max_tokens` option caps the number of output tokens a model may generate
for a single response. The budget applies to the following AI features:

- The Ask Ellie chat proxy sends the budget with every chat request that the
  proxy forwards to a provider.
- The AI Overview applies the budget to the estate summary and to each scoped
  summary.
- The server-info AI database analysis applies the budget to the per-database
  descriptions.

The default value is `4096` tokens; a value less than or equal to zero falls
back to the default. Earlier releases capped the AI Overview and the
server-info analysis at 512 tokens and ignored the configured value on both
paths.

A reasoning model counts its internal thinking tokens against this same
budget. A budget that suits a conventional model can therefore leave no room
for the answer itself. The model then returns a response that contains no
text, and the affected panel has nothing to render. Raise `max_tokens` when a
summary or an analysis comes back empty; alternatively, select a model that
emits less reasoning.

The server reports an empty response instead of discarding the response
silently. A scoped AI Overview request fails with an error, and the estate
summary records the failure in the server log. The
`GET /api/v1/server-info/{id}/ai-analysis` endpoint returns `502` with a
message that names the likely cause.

In the following example, the `llm` section raises the budget for a reasoning
model that a local llama.cpp server hosts:

```yaml
llm:
  provider: "openai"
  model: "qwen3-8b"
  openai_base_url: "http://localhost:8080/v1"
  max_tokens: 8192
```

### Using the `timeout_seconds` Property

The `timeout_seconds` option sets the HTTP client timeout, in seconds, for
every request the server makes to the configured LLM provider. The timeout
applies uniformly to Anthropic, OpenAI, Gemini, and Ollama, including Ask Ellie
chat requests. The default value is `120` seconds; values less than or equal to
zero fall back to the default.

Increase this value when running local models on low-resource hardware or when
full-context prompts regularly exceed the default; large prompts that combine
schema, metrics, and connection data can take longer than 120 seconds on slow
Ollama hosts. Raising the timeout to `300` prevents `context deadline exceeded`
errors in these environments.

In the following example, the `llm` section extends the timeout for a slow
local Ollama server:

```yaml
llm:
  provider: "ollama"
  model: "llama3:latest"
  ollama_url: "http://localhost:11434"
  timeout_seconds: 300
```

### Using the `compact_tool_descriptions` Property

The `compact_tool_descriptions` option controls whether the server sends
compact or verbose tool descriptions to the LLM. The option accepts three
values:

- `auto` (default) uses compact descriptions for localhost endpoints and
  verbose descriptions for remote APIs. Compact descriptions reduce prompt
  token count by approximately fifty-four percent.
- `true` always sends compact tool descriptions.
- `false` always sends verbose tool descriptions.

This setting affects the chat proxy (Ask Ellie) only. MCP protocol clients
always receive full tool descriptions regardless of this setting.

In the following example, the `llm` section configures verbose tool
descriptions:

```yaml
llm:
  provider: "anthropic"
  model: "claude-sonnet-5"
  anthropic_api_key_file: "~/.anthropic-api-key"
  compact_tool_descriptions: "false"
```

### Using the `reasoning_effort` Property

The `reasoning_effort` option sets how much a reasoning model thinks before it
answers. The option accepts `none`, `minimal`, `low`, `medium`, `high`,
`xhigh`, or `max`; the server refuses to start with any other value. When the
option is unset, the server leaves the setting out of each request and the
model uses its own default.

The setting applies to Ask Ellie, the AI Overview, and the server-info AI
database analysis. Each provider translates the level into its own request
field:

- OpenAI sends the level as `reasoning_effort`.
- Anthropic enables adaptive thinking at the given effort, treating `minimal`
  as `low`; `none` turns thinking off.
- Gemini sends the level as the thinking level, treating `none` as `minimal`,
  and `xhigh` and `max` as `high`.
- Ollama ignores the setting.

Which levels a model accepts varies by model. When a model rejects a level, the
server retries the request without the setting. A model counts its thinking
tokens against `max_tokens`, so a higher effort may need a larger output
budget.

### Using the `prompt_caching` Property

The `prompt_caching` option accepts `auto` or `off`; an unset value means
`off`. With `auto`, Anthropic caches the tool list, the system prompt, and the
conversation so far, so each step of an Ask Ellie tool loop reads the unchanged
part of the prompt from the cache instead of paying the full input rate for the
part again. OpenAI and Gemini cache prompts automatically, so the option makes
no difference for those providers; Ollama ignores the option.

Anthropic charges more than the normal input rate to write a prompt to the
cache, so leave the option off when requests rarely repeat a prompt.

### Using the `extra_body` Property

The `extra_body` option adds fields to the JSON body of every chat request the
server sends through the `openai` provider. Use the option to pass a setting
that the server does not model, such as the options of an OpenAI-compatible
gateway. A field in `extra_body` replaces any field of the same name that the
server builds. The Anthropic, Gemini, and Ollama providers ignore the option.

The server refuses to start when `extra_body` sets `stream`, which the server
chooses for each request, or contains a value that cannot be sent as JSON. The
server never logs the values in `extra_body`, but you should still store a
credential in an API key file rather than in this option.

In the following example, the `llm` section asks a reasoning model for low
effort and passes a caching option to an OpenAI-compatible gateway:

```yaml
llm:
  provider: "openai"
  model: "anthropic/claude-sonnet-5"
  openai_base_url: "https://gateway.example.com/v1"
  reasoning_effort: "low"
  extra_body:
    providerOptions:
      gateway:
        caching: "auto"
```

The server reads the `reasoning_effort`, `prompt_caching`, and `extra_body`
options at startup only; restart the server to apply a change.

### Using the `openai_base_url` Property

The `openai` provider works with any server that implements the
OpenAI-compatible API. Set `openai_base_url` to point at a local inference
server. The API key is optional when using a custom base URL.

The following local inference servers are compatible:

- Docker Model Runner uses `http://localhost:12434/engines/llama.cpp/v1` as the
  default endpoint.
- llama.cpp uses `http://localhost:8080/v1` as the default endpoint.
- LM Studio uses `http://localhost:1234/v1` as the default endpoint.
- EXO uses `http://localhost:52415/v1` as the default endpoint.

In the following example, the `llm` section configures a local llama.cpp
server:

```yaml
llm:
  provider: "openai"
  model: "my-local-model"
  openai_base_url: "http://localhost:8080/v1"
```

A local server that hosts a reasoning model usually needs a larger output
budget than the default. For details, see
[Using the `max_tokens` Property](#using-the-max_tokens-property).

### Using the Custom Header Properties

The custom header options add HTTP headers to every request the server sends to
the LLM provider. Use them when requests pass through an LLM gateway or proxy
that needs an authentication token or a routing header. The headers apply to
every AI feature the server drives, including the Ask Ellie chat proxy and the
AI Overview.

The server builds the headers for a request from the following sources, in
order; a later source replaces a header of the same name from an earlier one:

1. The `custom_headers` map supplies headers for every provider.
2. The `custom_headers_files` map names a header and the path to a file that
   holds its value.
3. The `LLM_CUSTOM_HEADERS` environment variable supplies headers for every
   provider.
4. The `anthropic_custom_headers`, `openai_custom_headers`,
   `gemini_custom_headers`, or `ollama_custom_headers` map supplies headers for
   the matching provider only.
5. The `LLM_ANTHROPIC_CUSTOM_HEADERS`, `LLM_OPENAI_CUSTOM_HEADERS`,
   `LLM_GEMINI_CUSTOM_HEADERS`, or `LLM_OLLAMA_CUSTOM_HEADERS` environment
   variable supplies headers for the matching provider only.

Each environment variable holds a comma-separated list of `NAME=value` pairs,
such as `X-Team=dba,X-Route=primary`. The first `=` in a pair separates the
name from the value; the server skips an entry with no `=`. A header with an
empty value in one of the first three sources is ignored.

Use `custom_headers_files` for a secret value, such as a gateway token, so that
the secret stays out of the configuration file. The server expands a leading
`~` in the path, removes the trailing newline from the value, and logs a
warning when the group or other users can access the file. The Ask Ellie chat
proxy reads each file once, when the server starts, so restart the server after
replacing a file; the other AI features read the file each time they call the
provider. When a file is missing, unreadable, or empty, the server logs an
error and sends the affected requests without any custom headers.

In the following example, the `llm` section sends a gateway token from a file
with every request, and adds a routing header to Anthropic requests:

```yaml
llm:
  provider: "anthropic"
  model: "claude-sonnet-5"
  anthropic_api_key_file: "~/.anthropic-api-key"
  anthropic_base_url: "https://llm-gateway.example.com/v1"
  custom_headers:
    X-Team: "dba"
  custom_headers_files:
    X-Gateway-Token: "/etc/ai-workbench/gateway-token"
  anthropic_custom_headers:
    X-Route: "anthropic-primary"
```

## Knowledgebase Properties

The `pgedge-ai-kb` packages install one knowledgebase database per embedding
provider and model. On Linux these files live in
`/usr/share/pgedge/pgedge-ai-kb/`, and each file is named
`kb-<provider>-<model>.db`. The `<provider>` and `<model>` parts match the
configured `embedding_provider` and `embedding_model` values. For the default
ollama provider with the `nomic-embed-text` model, the file is
`kb-ollama-nomic-embed-text.db`. The full default path is therefore
`/usr/share/pgedge/pgedge-ai-kb/kb-ollama-nomic-embed-text.db`. The exact
location can vary by deployment method and operating system.

The built-in `database_path` default still points at the legacy
`/usr/share/pgedge/postgres-mcp-kb/kb.db` location. Set `database_path`
explicitly when you use the current `pgedge-ai-kb` packages, so the server
reads the installed file.

The `embedding_provider` option accepts `voyage`, `openai`, `gemini`, or
`ollama`, and `embedding_model` accepts any model name the chosen provider
recognizes. Google publishes `gemini-embedding-001` (the default, 3072
dimensions) and `gemini-embedding-2` (also 3072 dimensions); model
availability varies by Gemini API key tier, so run ListModels to verify which
embedding models a given key can access.

When Gemini provides the knowledgebase embeddings, the configured
`embedding_model` must match the model that the KB Builder used to produce the
knowledgebase. The KB Builder uses `gemini-embedding-001`; selecting a
different model produces incompatible embeddings and breaks similarity search.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `false` | Enable knowledgebase |
| `database_path` | string | `/usr/share/pgedge/postgres-mcp-kb/kb.db` | SQLite database path |
| `embedding_provider` | string | `ollama` | Embedding provider |
| `embedding_model` | string | `nomic-embed-text` | Embedding model |
| `embedding_ollama_url` | string | `http://localhost:11434` | Ollama URL |
| `embedding_voyage_api_key_file` | string | | Voyage API key path |
| `embedding_openai_api_key_file` | string | | OpenAI key path |
| `embedding_gemini_api_key_file` | string | | Gemini key path |
| `embedding_voyage_base_url` | string | `https://api.voyageai.com/v1/embeddings` | Voyage base URL |
| `embedding_openai_base_url` | string | `https://api.openai.com/v1` | OpenAI base URL |
| `embedding_gemini_base_url` | string | `https://generativelanguage.googleapis.com` | Gemini base URL |

## Memory Properties

The memory section controls the chat memory feature for Ask Ellie. Chat memory
allows Ellie to store and recall information across conversations.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | bool | `true` | Enable chat memory |

The memory feature requires a configured PostgreSQL datastore connection. The
server stores memories in the datastore alongside other persistent data. When
memory is disabled, the three memory tools (`store_memory`, `recall_memories`,
`delete_memory`) are unavailable even when enabled in the `builtins.tools`
section.

Creating system-scoped memories requires the `store_system_memory` admin
permission. Administrators assign this permission to groups through the admin
panel. Users without the permission can only create user-scoped memories.

In the following example, the `memory` section disables chat memory:

```yaml
memory:
  enabled: false
```

The `PGEDGE_MEMORY_ENABLED` environment variable can also control this setting;
see [Environment Variables](environment_variables.md) for details.

## Built-in Features Properties

The builtins section enables or disables individual tools and resources.

Two tools are always enabled and cannot be turned off through the
`builtins.tools` section. The `test_query` tool validates SQL without
executing the query, and the server asks Ellie to check every statement
through that tool before presenting the statement to a user. The
`read_resource` tool backs resource listing, so resources become unreachable
without the tool.

In the following example, the `builtins` section lists each configurable tool
and resource:

```yaml
builtins:
  tools:
    query_database: true
    get_schema_info: true
    similarity_search: true
    execute_explain: true
    count_rows: true
    list_probes: true
    describe_probe: true
    query_metrics: true
    list_connections: true
    get_alert_history: true
    get_alert_rules: true
    get_metric_baselines: true
    get_blackouts: true
    get_timeline_events: true
    query_datastore: true
    describe_datastore: true
    generate_embedding: true
    search_knowledgebase: true
    store_memory: true
    recall_memories: true
    delete_memory: true
  resources:
    system_info: true
    connection_info: true
```

### Using the Datastore Query Tool Properties

The `query_datastore` tool runs structured queries against the datastore, and
the `describe_datastore` tool lists the sources that `query_datastore` can
read and describes their columns. Both tools default to `true`. The
`query_datastore` tool does not accept SQL; the tool limits
connection-specific rows to the connections the caller can access, while
estate-wide configuration rows remain readable. The
[MCP Tools and Resources](../../user-guide/ai/mcp-tools.md#querying-the-datastore)
page describes the tool's parameters.

In the following example, the configuration disables both datastore query
tools:

```yaml
builtins:
  tools:
    query_datastore: false
    describe_datastore: false
```

### Using the Memory Tool Properties

The memory tools (`store_memory`, `recall_memories`, `delete_memory`) allow
Ellie to store and recall information across conversations. The server stores
memories in the PostgreSQL datastore. When embedding generation is enabled, the
`recall_memories` tool uses semantic similarity for searching. The tool falls
back to text matching when embeddings are unavailable.

Set any memory tool to `false` in the `builtins.tools` section to disable the
tool.

In the following example, the configuration disables the `store_memory` and
`delete_memory` tools while keeping `recall_memories` enabled:

```yaml
builtins:
  tools:
    store_memory: false
    delete_memory: false
    recall_memories: true
```

## Paths and Directories

These properties configure filesystem paths for secrets, custom prompts,
persistent data, and tracing.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `secret_file` | string | `/etc/pgedge/ai-dba-server.secret` | Encryption secret path |
| `custom_definitions_path` | string | | Custom prompts file path |
| `data_dir` | string | `./data/` | Directory location for persistent storage of auth and chat data |
| `trace_file` | string | | MCP trace file path |

### Using the data_dir Property

The `data_dir` property specifies the location of persistent data files used
for data storage:

- `auth.db` stores authentication data in a SQLite database.
- `conversations.db` stores chat history in a SQLite database.

The default location of the data files is `./data/` relative to the server
binary.

In the following example, the `-data-dir` flag specifies a custom location:

```bash
./ai-dba-server -data-dir /var/lib/ai-workbench/data
```

The `data` directory can also be set in the configuration file:

```yaml
data_dir: "/var/lib/ai-workbench/data"
```

### Using the trace_file Property

You can use the `trace_file` property to instruct the server to log all MCP
requests and responses to a trace file for debugging purposes.

In the following example, the `-trace-file` flag enables tracing:

```bash
./ai-dba-server \
    -trace-file /var/log/ai-workbench/mcp-trace.log
```

The trace file can also be set in the configuration file:

```yaml
trace_file: "/var/log/ai-workbench/mcp-trace.log"
```
