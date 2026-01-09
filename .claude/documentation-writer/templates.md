# Documentation Templates

This document provides templates for common documentation types.

## README Template

Use this template for sub-project README files:

```markdown
# Project Name

[![Build Status](badge-url)](action-url)
[![Test Status](badge-url)](action-url)

Brief one-paragraph description of the project and its purpose.

## Table of Contents

- [Features](#features)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
- [Development](#development)
- [Documentation](#documentation)

## Features

The project provides the following capabilities:

- First feature as a complete sentence.
- Second feature as a complete sentence.
- Third feature as a complete sentence.

## Prerequisites

Before installing, ensure you have:

- [Dependency 1](https://example.com) version X.X or higher
- [Dependency 2](https://example.com) version X.X or higher

## Installation

Clone the repository:

` ` `bash
git clone https://github.com/pgEdge/ai-dba-workbench.git
cd ai-dba-workbench/project-name
` ` `

Build the project:

` ` `bash
make build
` ` `

## Configuration

Create a configuration file or set environment variables:

` ` `bash
export DATABASE_URL="postgres://user:pass@localhost/dbname"
` ` `

For detailed configuration options, see
[Configuration](docs/project-name/configuration.md).

## Usage

Start the service:

` ` `bash
./project-name start
` ` `

For detailed usage instructions, see
[Usage Guide](docs/project-name/usage.md).

## Development

Run tests:

` ` `bash
make test
` ` `

Run linter:

` ` `bash
make lint
` ` `

## Documentation

For complete documentation, see [docs/project-name/](docs/project-name/).

---

To report an issue with the software, visit:
[GitHub Issues](https://github.com/pgEdge/ai-dba-workbench/issues)

We welcome your project contributions; for more information, see
[docs/developers.md](docs/developers.md).

For more information, visit [docs.pgedge.com](https://docs.pgedge.com)

This project is licensed under the [PostgreSQL License](LICENCE.md).
```

## API Documentation Template

Use this template for documenting API endpoints or MCP tools:

```markdown
# API Name

Brief description of what this API does and when to use it.

## Endpoint / Tool

` ` `
METHOD /path/to/endpoint
` ` `

Or for MCP tools:

` ` `
Tool: tool_name
` ` `

## Description

Detailed explanation of the API's purpose and behavior.

## Authentication

Describe authentication requirements:

- Required token type
- Required permissions

## Parameters

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `param1` | string | Yes | Description of param1 |
| `param2` | integer | No | Description of param2 (default: 10) |

## Request Example

` ` `json
{
    "param1": "value",
    "param2": 20
}
` ` `

## Response

### Success Response

**Status:** 200 OK

` ` `json
{
    "id": 123,
    "status": "created"
}
` ` `

### Error Responses

**Status:** 400 Bad Request

` ` `json
{
    "error": "Invalid parameter: param1"
}
` ` `

**Status:** 401 Unauthorized

` ` `json
{
    "error": "Authentication required"
}
` ` `

## Example Usage

In the following example, the client creates a new resource:

` ` `bash
curl -X POST https://api.example.com/resource \
    -H "Authorization: Bearer token" \
    -d '{"param1": "value"}'
` ` `

## Related

- [Related API 1](related-api-1.md)
- [Related API 2](related-api-2.md)
```

## Feature Documentation Template

Use this template for documenting features:

```markdown
# Feature Name

Brief description of the feature and its purpose.

## Overview

The feature provides the following capabilities:

- Capability one as a complete sentence.
- Capability two as a complete sentence.
- Capability three as a complete sentence.

## How It Works

Explain the feature's operation:

1. First step in the process.
2. Second step in the process.
3. Third step in the process.

## Configuration

Describe how to configure the feature:

` ` `yaml
feature:
  enabled: true
  option1: value1
` ` `

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `enabled` | boolean | false | Enables the feature |
| `option1` | string | - | Description of option1 |

## Usage Examples

### Basic Usage

In the following example, the user performs a basic operation:

` ` `bash
command --option value
` ` `

### Advanced Usage

In the following example, the user configures advanced options:

` ` `bash
command --option1 value1 --option2 value2
` ` `

## Troubleshooting

### Common Issue 1

**Symptom:** Description of what the user sees.

**Cause:** Explanation of why this happens.

**Solution:** Steps to resolve the issue.

### Common Issue 2

**Symptom:** Description of what the user sees.

**Cause:** Explanation of why this happens.

**Solution:** Steps to resolve the issue.

## Related Documentation

- [Related Feature](related-feature.md)
- [Configuration Guide](configuration.md)
```

## Changelog Entry Template

Use this format for changelog entries:

```markdown
## [X.Y.Z] - YYYY-MM-DD

### Added

- Add new feature that does X. (#issue-number)
- Add support for Y in the Z component. (#issue-number)

### Changed

- Update authentication to use token-based auth. (#issue-number)
- Improve query performance for large datasets. (#issue-number)

### Fixed

- Fix issue where connections would timeout prematurely. (#issue-number)
- Fix incorrect error message when user not found. (#issue-number)

### Deprecated

- Deprecate old_function in favor of new_function.

### Removed

- Remove support for deprecated API v1.

### Security

- Fix SQL injection vulnerability in query handler. (#issue-number)
```

## Index Page Template

Use this template for documentation index pages:

```markdown
# Component Name Documentation

Welcome to the Component Name documentation.

## Getting Started

New users should start with the [Quick Start Guide](quickstart.md).

## Documentation

- [Installation](installation.md) - How to install and configure.
- [Configuration](configuration.md) - Configuration options reference.
- [Usage](usage.md) - How to use the component.
- [API Reference](api-reference.md) - Complete API documentation.

## Guides

- [Guide Topic 1](guides/topic1.md) - Detailed guide on topic 1.
- [Guide Topic 2](guides/topic2.md) - Detailed guide on topic 2.

## Reference

- [CLI Reference](reference/cli.md) - Command line options.
- [Configuration Reference](reference/config.md) - All config options.

## Troubleshooting

See [Troubleshooting](troubleshooting.md) for common issues and solutions.
```
