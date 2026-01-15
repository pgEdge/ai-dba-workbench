# Embedding Package

The embedding package provides a unified interface for generating text
embeddings using multiple AI providers.

## Overview

The package abstracts the differences between embedding providers and offers a
consistent API for generating vector representations of text.

The package includes the following features:

- A unified Provider interface supports multiple embedding backends.
- OpenAI, Voyage AI, and Ollama providers are available out of the box.
- Configurable logging tracks API calls, performance, and errors.
- Automatic model dimension detection simplifies configuration.

## Supported Providers

### OpenAI

The OpenAI provider connects to OpenAI's embedding API.

The following models are supported:

| Model | Dimensions |
|-------|------------|
| `text-embedding-3-large` | 3072 |
| `text-embedding-3-small` | 1536 |
| `text-embedding-ada-002` | 1536 |

The default model is `text-embedding-3-small`.

### Voyage AI

The Voyage AI provider connects to Voyage AI's embedding API.

The following models are supported:

| Model | Dimensions |
|-------|------------|
| `voyage-3` | 1024 |
| `voyage-3-lite` | 512 |
| `voyage-2` | 1024 |
| `voyage-2-lite` | 1024 |

The default model is `voyage-3-lite`.

### Ollama

The Ollama provider connects to a local Ollama instance for offline use.

The following models are commonly used:

| Model | Dimensions |
|-------|------------|
| `nomic-embed-text` | 768 |
| `mxbai-embed-large` | 1024 |
| `all-minilm` | 384 |

The default model is `nomic-embed-text` and the default URL is
`http://localhost:11434`.

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

    // Dimensions returns the number of dimensions in the embedding vector.
    Dimensions() int

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
    Provider string // "voyage", "ollama", or "openai"
    Model    string // Model name (provider-specific)

    // Voyage AI-specific
    VoyageAPIKey string

    // OpenAI-specific
    OpenAIAPIKey string

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
| `debug` | Logs detailed information including timing and dimensions |
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
- `LogLevelDebug` logs text lengths, dimensions, timing, and models.
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
