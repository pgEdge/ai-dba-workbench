# Embedding Package

The embedding package provides a unified interface for generating text
embeddings using multiple AI providers.

## Overview

The package abstracts the differences between embedding providers and offers a
consistent API for generating vector representations of text.

The package wraps `github.com/pgEdge/pgedge-go-llm-lib`, the shared pgEdge LLM
library, and delegates the actual provider calls to its `llm.Client`. Only the
Workbench `Config` mapping and the default models live in this package.

The package includes the following features:

- A unified Provider interface supports multiple embedding backends.
- OpenAI, Voyage AI, Gemini, and Ollama providers are available out of
  the box.
- Configurable logging tracks API calls, performance, and errors.
- Any model name the configured provider accepts can be used.

## Model Names

The package does not check a configured model name against a fixed
list; it passes the name straight to the provider, so any model the
provider itself accepts can be used. New models therefore work as soon
as the provider publishes them, and an OpenAI-protocol-compatible local
model server such as llama.cpp or vLLM can serve embeddings: point
`openai_base_url` at that server and name the model the server
provides.

The default model applies only when the configuration leaves the model
empty. A model the provider does not recognise fails on the first
embedding request rather than when the provider is constructed.

The remaining constraint is the width of the vector. An embedding
wider than 4000 dimensions is rejected when it is stored or used as a
query vector, because 4000 is the width of the `halfvec` columns that
hold embeddings and pgvector's HNSW index limit for that type. The
rejection reports a dimension error rather than silently truncating
the vector.

## Supported Providers

### OpenAI

The OpenAI provider connects to OpenAI's embedding API, or to any
service that implements the same protocol.

The following models are commonly used:

- `text-embedding-3-large`
- `text-embedding-3-small`
- `text-embedding-ada-002`

The default model is `text-embedding-3-small`. Any other model name the
endpoint accepts can be configured instead.

### Voyage AI

The Voyage AI provider connects to Voyage AI's embedding API.

The following models are commonly used:

- `voyage-3`
- `voyage-3-lite`
- `voyage-2`
- `voyage-2-lite`

The default model is `voyage-3-lite`. Any other model name Voyage AI
accepts can be configured instead.

### Gemini

The Gemini provider connects to Google's Generative Language
embedding API.

Google publishes the following embedding models:

- `gemini-embedding-001`
- `gemini-embedding-2`

Any other model name the Gemini API accepts can be configured instead.

The default model is `gemini-embedding-001`, the model the KB Builder
uses. When Gemini supplies knowledgebase embeddings, the configured
model must match the model that produced the knowledgebase; otherwise
the embeddings are incompatible. Model availability varies by Gemini
API key tier; run ListModels to verify which embedding models a given
key can access.

In the following example, the server configuration selects the
Gemini provider and reads the API key from a file on disk:

```yaml
embedding:
  provider: "gemini"
  model: "gemini-embedding-001"
  gemini_api_key_file: "~/.gemini-api-key"
  # gemini_base_url: "https://generativelanguage.googleapis.com"
```

### Ollama

The Ollama provider connects to a local Ollama instance for offline use.

The following models are commonly used:

- `nomic-embed-text`
- `mxbai-embed-large`
- `all-minilm`

The Ollama provider discovers the model at runtime. The default model
is `nomic-embed-text` and the default URL is `http://localhost:11434`.

## Usage

### Creating a Provider

In the following example, the `NewProvider` function creates an OpenAI
provider:

```go
import "github.com/pgedge/ai-workbench/pkg/embedding"

cfg := embedding.Config{
    Provider:     "openai",
    Model:        "text-embedding-3-small",
    OpenAIAPIKey: "your-api-key",
}

provider, err := embedding.NewProvider(cfg)
if err != nil {
    log.Fatal(err)
}
```

### Generating Embeddings

In the following example, the `Embed` method generates a vector for text:

```go
ctx := context.Background()
text := "PostgreSQL is a powerful open-source database."

vector, err := provider.Embed(ctx, text)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Generated %d-dimensional vector\n", len(vector))
```

### Provider Interface

The `Provider` interface defines the following methods:

```go
type Provider interface {
    // Embed generates an embedding vector for the given text.
    Embed(ctx context.Context, text string) ([]float64, error)

    // ModelName returns the name of the model being used.
    ModelName() string

    // ProviderName returns the name of the provider.
    ProviderName() string
}
```

## Configuration

### Config Structure

The `Config` struct holds configuration for embedding providers:

```go
type Config struct {
    Provider string // "voyage", "openai", "gemini", or "ollama"
    Model    string // Model name (provider-specific)

    // Voyage AI-specific
    VoyageAPIKey string

    // OpenAI-specific
    OpenAIAPIKey string

    // Gemini-specific
    GeminiAPIKey string
    GeminiBaseURL string

    // Ollama-specific
    OllamaURL string
}
```

### Environment Variables

The `PGEDGE_LLM_LOG_LEVEL` environment variable controls logging verbosity.

| Value | Description |
|-------|-------------|
| `none` | Disables all LLM logging (default) |
| `info` | Logs basic information including API calls and errors |
| `debug` | Logs detailed information including timing and vector length |
| `trace` | Logs very detailed information including request previews |

In the following example, the environment variable enables debug logging:

```bash
export PGEDGE_LLM_LOG_LEVEL=debug
```

## Logging

The package provides structured logging for debugging and monitoring.

### Log Levels

The following log levels are available:

- `LogLevelNone` disables all logging.
- `LogLevelInfo` logs API calls, errors, and token usage.
- `LogLevelDebug` logs text lengths, vector lengths, timing, and models.
- `LogLevelTrace` logs full request and response details.

### Programmatic Configuration

In the following example, the log level is set programmatically:

```go
import "github.com/pgedge/ai-workbench/pkg/embedding"

embedding.SetLogLevel(embedding.LogLevelDebug)
```

## Error Handling

The providers return descriptive errors for common issues.

The following error conditions are handled:

- Empty text input returns an error before making API calls.
- Missing API keys return an error during provider initialization.
- Network failures include connection details in the error message.
- Rate limit errors are logged with specific details for debugging.
- Ollama connection failures suggest checking if Ollama is running.

---

For more information, visit [docs.pgedge.com](https://docs.pgedge.com)

This project is licensed under the [PostgreSQL License](../../LICENSE.md).
