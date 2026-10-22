# LLM Provider Properties

Use the properties in the `llm` section of the alerter configuration file to
choose the LLM providers that anomaly detection uses, select a model for each
provider, and supply the API keys and endpoints that the providers require.
Tier 2 of [anomaly detection](anomaly_detection.md) uses the embedding provider
to compare candidates with past anomalies; tier 3 uses the reasoning provider,
and the optional decision model, to classify the remaining candidates. The
[Configuration File Properties](configuration_file.md) page describes the
other sections of the file and the locations the alerter searches for it.

Each embedding provider section names its own embedding model, and the alerter
accepts any model name the chosen provider recognizes rather than checking the
name against a fixed list. The defaults in the following tables apply when the
configuration leaves the model empty; an OpenAI-compatible local model server
can supply embeddings under whatever model name that server uses.

The alerter compares each new embedding with the stored embeddings of earlier
candidates without checking which model produced them. After a change to the
embedding provider or `embedding_model`, tier 2 therefore compares embeddings
from different models, and a decision recorded under the previous model can
suppress an unrelated candidate. Expect less reliable tier 2 suppression after
such a change until the stored embeddings come from the new model.

The configured embedding model must not produce vectors with more than 4000
dimensions. The alerter stores anomaly embeddings as `halfvec(4000)` and
zero-pads each embedding to 4000 dimensions; 4000 is pgvector's HNSW index
limit for the `halfvec` type. The alerter does not check the width of a model
at startup: when the model turns out to be wider than 4000 dimensions, the
alerter never truncates the vector, but instead logs an error naming the vector
width and the limit for each anomaly candidate it processes, stores no
embedding for the candidate, and passes the candidate straight to tier 3. Watch
the alerter log after changing the embedding model, because the error first
appears when the first anomaly candidate reaches tier 2, not when the alerter
starts. Storing embeddings as `halfvec` requires pgvector `0.7.0` or newer;
upgrading an existing installation against an older pgvector causes the
embedding-column migration to fail.

The following table describes the provider selection options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `embedding_provider` | string | `ollama` | Embedding provider |
| `reasoning_provider` | string | `ollama` | Classification provider |
| `decision_provider` | string | None | Optional tier 3 decision provider: `typesafe` or `openai`; empty, `none` or `disabled` turns the decision model off |
| `max_tokens` | integer | `4096` | Max output tokens per reasoning call |

## Decision Model (`decision_provider`)

A decision model answers a yes or no question with the probability of yes,
rather than generating text. When `decision_provider` names a provider, tier 3
asks the decision model whether each candidate is a real issue that requires
attention, and applies `anomaly.tier3.decision_suppress_threshold` to the
probability it returns. The decision model receives the same context as the
reasoning model, without the reasoning model's instructions for a JSON
response.

The following providers serve decision models:

- The `typesafe` provider uses the TypeSafe Jev models, configured in the
  `llm.typesafe` section; the default model is `jev-latest`.
- The `openai` provider uses the OpenAI Decisions API with the model in
  `llm.openai.decision_model`; the default model is `gpt-6-luna`.

The alerter sends the decision provider the tier 3 classification context for
each candidate. The context holds fixed instructions about the task and about
weighing past user feedback, together with the following details:

- The context includes the metric name, value, and z-score, the connection ID,
  the database name, and the detection time.
- The context includes the baseline mean, standard deviation, and period.
- The context includes the similarity, decision, and metric name of up to five
  similar past anomalies.
- The context includes up to ten past acknowledgments of the same metric and
  connection, each with its note, its false positive flag, the username of the
  user who acknowledged the alert, the time of the acknowledgment, and the
  original alert's value, z-score, and severity.
- The context includes the number, names, and roles of cluster peer
  connections.
- The context includes the title, severity, server name, and trigger time of
  each active or acknowledged alert on those peers.

The decision provider receives nothing beyond what the reasoning provider
already receives. A decision provider such as TypeSafe may, however, be a third
party that has not received the data before, so review the provider's data
handling terms before enabling it.

The decision model applies to tier 3 classification only. The re-evaluation of
acknowledged anomaly alerts always uses the reasoning provider, whatever
`decision_provider` holds.

When the decision model fails for a candidate, tier 3 falls back to the
reasoning model for that candidate. A failure includes an error, a timeout, a
refusal to answer, a missing answer, and a probability outside the range 0 to
1. When no reasoning provider is configured, tier 3 raises the candidate as an
alert without classification, the same fail-safe that applies when the
reasoning model fails. The candidate's tier 3 error records the decision model
failure in either case.

A decision provider can serve tier 3 on its own, without a reasoning provider;
the alerter then has no fallback and does not re-evaluate acknowledged alerts
with an LLM. When the alerter cannot create the decision provider at startup,
for example because the TypeSafe API key is missing, the alerter logs a warning
and runs tier 3 on the reasoning provider alone.

For a decision model verdict, the candidate's tier 3 result is a one-line
summary rather than a model response, as in the following example:

```text
Decision model typesafe/jev-latest: alert (P(real issue)=0.87, suppress threshold 0.50)
```

Because the decision model gives no explanation, the server generates one on
demand when a user asks why such an alert was raised. The first request asks
the LLM configured in the `llm` section of the server configuration, the same
LLM the server uses for AI analysis, to explain the verdict from the alert's
stored data. The server stores the explanation on the alert, so later requests
return it without another LLM call. When the server has no LLM configured, or
the call fails, no explanation is available and a later request tries again;
see the
[server configuration reference](../server/configuration_file.md#llm-proxy-properties)
for the server's `llm` settings. A reasoning model verdict needs no server LLM,
because the server returns the reasoning stored with the tier 3 response.

The `tier3_source` column of the `anomaly_candidates` table records whether a
`decision` or a `reasoning` model produced the tier 3 outcome, and the
`tier3_model` column records the provider and model in the form
`provider/model`. When every model fails, the columns name the last model
tried.

In the following example, the `llm` section selects the TypeSafe decision model
and keeps the Anthropic reasoning model as the fallback:

```yaml
llm:
  reasoning_provider: anthropic
  decision_provider: typesafe
  anthropic:
    api_key_file: /etc/ai-workbench/anthropic-api-key.txt
  typesafe:
    api_key_file: /etc/ai-workbench/typesafe-api-key.txt
    decision_model: jev-latest
anomaly:
  tier3:
    decision_suppress_threshold: 0.5
```

The alerter creates the decision provider at startup only, so changing
`decision_provider` or the `llm.typesafe` settings requires a restart. Turning
the decision provider off also requires a restart; after a reload that sets
`decision_provider` to `none`, tier 3 keeps using the provider built at
startup. A configuration reload does apply a new `decision_suppress_threshold`;
while the reloaded configuration has no decision provider, the alerter skips
the threshold check and uses `0.5` in place of a value outside the valid range.

## Choosing a Decision Model

A spike in October 2026 compared the decision providers with the stored tier 3
decisions on 150 real candidates from the previous 30 days, sampled evenly
between candidates stored as alerts and candidates stored as suppressed. The
stored decisions came from the reasoning model, so the figures measure
agreement with that model rather than accuracy against decisions that users
labeled; no such labels existed. The reasoning model, Claude Haiku 4.5, agreed
with the stored decisions on 91% of the candidates when run again. The
[spike results](https://github.com/pgEdge/ai-dba-workbench/issues/626#issuecomment-6187170285)
on issue #626 give the full data.

The following table compares the models at the default threshold of 0.5:

| Model | Agreement with stored decisions | Median latency | Cost per 1,000 decisions |
|-------|---------------------------------|----------------|--------------------------|
| TypeSafe `jev-latest` | 89% | About 200 ms | About US$0.04 |
| OpenAI `gpt-6-luna` decisions | 79% | About 250 ms | Not confirmed |
| Claude Haiku 4.5 reasoning | 91% | About 3 s | About US$1.93 |

The Jev model agreed with Claude Haiku 4.5 on 88% of the candidates, and never
suppressed a candidate stored as an alert; every disagreement was an alert
where the stored decision was to suppress. The OpenAI decision model suppressed
some candidates that were stored as alerts. The costs use the published prices
as of October 2026; Jev charges US$0.042 per million input tokens with no
charge for output, and Claude Haiku 4.5 charges US$1 per million input tokens
and US$5 per million output tokens. The spike could not confirm the OpenAI
decision price from official pricing.

On this evidence, `typesafe` is the recommended decision provider; `openai` is
supported, but agreed less often and suppressed some candidates that were
stored as alerts. Raising `decision_suppress_threshold` above 0.5 reduced
agreement in the spike. Because each provider calibrates its probabilities
differently, test any change to the threshold against your own candidates for
the provider in use.

Re-evaluation stays on the reasoning model because of the spike. In a synthetic
test, the Jev model never cleared an alert whose acknowledgment note explained
the anomaly, and no real acknowledgments were available to validate either
decision model.

## Output Token Budget (`max_tokens`)

The `max_tokens` option caps the number of output tokens a reasoning model may
generate for a single call. The budget applies to the following reasoning work:

- The tier 3 anomaly classification applies the budget to each candidate that
  the alerter examines.
- The re-evaluation of acknowledged alerts applies the budget to each alert
  that the alerter revisits.

The default value is `4096` tokens; a value less than or equal to zero falls
back to the default. Earlier releases capped both calls at 500 tokens and
offered no way to change the limit.

A reasoning model counts its internal thinking tokens against this same budget.
A budget that suits a conventional model can therefore leave no room for the
verdict itself. The model then returns a response that contains no text, which
the alerter treats as a tier 3 failure. A failed tier 3 classification fails
safe and raises the alert; the alerter records the cause in the candidate's
tier 3 error field. Raise `max_tokens` when these failures appear;
alternatively, select a model that emits less reasoning.

In the following example, the `llm` section raises the budget for a reasoning
model that a local llama.cpp server hosts:

```yaml
llm:
  reasoning_provider: openai
  max_tokens: 8192
  openai:
    base_url: http://localhost:8080/v1
    reasoning_model: qwen3-8b
```

## Reasoning Effort, Prompt Caching, and Extra Fields

Each of the `llm.openai`, `llm.anthropic`, `llm.gemini`, and `llm.ollama`
sections accepts three further options. The alerter applies the options of the
section that `llm.reasoning_provider` names to the tier 3 classification and
the re-evaluation of acknowledged alerts. The decision model does not use the
options.

- `reasoning_effort` sets how much a reasoning model thinks before it answers,
  and accepts `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, or `max`.
  OpenAI, Anthropic, and Gemini translate the level into their own request
  fields; Ollama ignores the option. When a model rejects a level, the alerter
  retries the request without the setting.
- `prompt_caching` accepts `auto` or `off`; an unset value means `off`. With
  `auto`, Anthropic caches the classification system prompt, which stays the
  same on every call. OpenAI and Gemini cache prompts automatically, so the
  option makes no difference for those providers; Ollama ignores the option.
- `extra_body` adds fields to the JSON body of each request the `openai`
  provider sends, for a setting the alerter does not model, such as the options
  of an OpenAI-compatible gateway. A field in `extra_body` replaces any field
  of the same name that the alerter builds. The other providers ignore the
  option.

The alerter refuses to start, and refuses a reload, when any section sets an
unknown `reasoning_effort` or `prompt_caching` value, sets `stream` in
`extra_body`, or puts a value in `extra_body` that cannot be sent as JSON. The
alerter checks every section, including the sections of providers that are not
in use. The alerter never logs the values in `extra_body`, but you should still
keep a credential in an API key file rather than in this option.

A model counts its thinking tokens against `max_tokens`, so a higher effort may
need a larger output budget.

In the following example, the `llm` section asks an OpenAI reasoning model for
low effort and passes an option to an OpenAI-compatible gateway:

```yaml
llm:
  reasoning_provider: openai
  openai:
    base_url: https://gateway.example.com/v1
    reasoning_model: openai/gpt-6-luna
    reasoning_effort: low
    extra_body:
      user: ai-dba-alerter
```

## Ollama Configuration

The `llm.ollama` section configures the local Ollama provider.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `base_url` | string | `http://localhost:11434` | Ollama server URL |
| `embedding_model` | string | `nomic-embed-text` | Embedding model |
| `reasoning_model` | string | `qwen2.5:7b-instruct` | Reasoning model |
| `reasoning_effort` | string | None | Reasoning effort level |
| `prompt_caching` | string | None | Prompt caching mode |
| `extra_body` | mapping | None | Extra request body fields |

## OpenAI Configuration

The `llm.openai` section configures the OpenAI provider. The following table
describes the OpenAI options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api_key_file` | string | None | Path to API key file |
| `base_url` | string | `https://api.openai.com/v1` | OpenAI base URL |
| `embedding_model` | string | `text-embedding-3-small` | Embedding model |
| `reasoning_model` | string | `gpt-6-luna` | Reasoning model |
| `decision_model` | string | `gpt-6-luna` | Decisions API model when `decision_provider` is `openai` |
| `reasoning_effort` | string | None | Reasoning effort level |
| `prompt_caching` | string | None | Prompt caching mode |
| `extra_body` | mapping | None | Extra request body fields |

The decision model shares the `api_key_file` and `base_url` options with the
OpenAI embedding and reasoning models. A `base_url` that points at a local
inference server therefore also receives the decision requests, and the server
must implement the OpenAI Decisions API for them to succeed.

The `openai` provider works with any server that implements the
OpenAI-compatible API. Set `base_url` to point at a local inference server, and
set `embedding_model` to the model that server provides. The API key is
optional when using a custom base URL.

The following local inference servers are compatible:

- Docker Model Runner uses `http://localhost:12434/engines/llama.cpp/v1` as the
  default endpoint.
- llama.cpp uses `http://localhost:8080/v1` as the default endpoint.
- LM Studio uses `http://localhost:1234/v1` as the default endpoint.
- EXO uses `http://localhost:52415/v1` as the default endpoint.

In the following example, the `llm.openai` section configures a local llama.cpp
server:

```yaml
llm:
  reasoning_provider: openai
  openai:
    base_url: http://localhost:8080/v1
    reasoning_model: my-local-model
```

A local server that hosts a reasoning model usually needs a larger output
budget than the default. For details, see
[Output Token Budget](#output-token-budget-max_tokens).

## Anthropic Configuration

The `llm.anthropic` section configures the Anthropic provider. The following
table describes the Anthropic options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api_key_file` | string | None | Path to API key file |
| `base_url` | string | `https://api.anthropic.com/v1` | Anthropic base URL |
| `reasoning_model` | string | `claude-haiku-4-5` | Reasoning model |
| `reasoning_effort` | string | None | Reasoning effort level |
| `prompt_caching` | string | None | Prompt caching mode |
| `extra_body` | mapping | None | Extra request body fields |

## Gemini Configuration

The `llm.gemini` section configures the Google Gemini provider. Model
availability varies by Gemini API key tier; run ListModels to verify which
models a given key can access. The following table describes the Gemini
options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api_key_file` | string | None | Path to API key file |
| `base_url` | string | `https://generativelanguage.googleapis.com` | Gemini base URL |
| `reasoning_model` | string | `gemini-3.8-flash` | Reasoning model |
| `embedding_model` | string | `gemini-embedding-001` | Embedding model |
| `reasoning_effort` | string | None | Reasoning effort level |
| `prompt_caching` | string | None | Prompt caching mode |
| `extra_body` | mapping | None | Extra request body fields |

## Voyage Configuration

The `llm.voyage` section configures the Voyage provider for embeddings.

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api_key_file` | string | None | Path to API key file |
| `base_url` | string | `https://api.voyageai.com/v1/embeddings` | Voyage base URL |
| `embedding_model` | string | `voyage-3-lite` | Embedding model |

## TypeSafe Configuration

The `llm.typesafe` section configures the TypeSafe provider, which serves the
Jev decision models. The alerter uses TypeSafe only as a `decision_provider`;
TypeSafe cannot serve as an embedding or reasoning provider.

The following table describes the TypeSafe options:

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `api_key_file` | string | None | Path to API key file |
| `base_url` | string | `https://api.typesafe.ai/v1` | TypeSafe base URL |
| `decision_model` | string | `jev-latest` | Decision model |

The TypeSafe provider always requires an API key, even when `base_url` points
elsewhere, such as at a proxy. The alerter reads the key only from
`api_key_file`, and does not fall back to the `TYPESAFE_API_KEY` environment
variable.
