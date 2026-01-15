# Shared Packages

The `pkg` directory contains shared Go packages used across multiple components
of the pgEdge AI DBA Workbench.

## Packages

### embedding

The embedding package provides a unified interface for generating text
embeddings using multiple AI providers including OpenAI, Voyage AI, and Ollama.

See [embedding/README.md](embedding/README.md) for detailed documentation.

### logger

The logger package provides a simple logging interface with verbosity control
for consistent logging across all components.

## Usage

Import packages using the module path:

```go
import (
    "github.com/pgedge/ai-workbench/pkg/embedding"
    "github.com/pgedge/ai-workbench/pkg/logger"
)
```

---

For more information, visit [docs.pgedge.com](https://docs.pgedge.com)

This project is licensed under the [PostgreSQL License](../LICENSE.md).
