# Connecting MCP Clients

Any MCP-compatible AI tool can connect to the
Workbench's MCP server endpoint. External clients
gain access to the same tools, resources, and prompts
that power the built-in Ask Ellie assistant. The MCP
server uses HTTP transport with Bearer token
authentication.

## Prerequisites

Ensure the following requirements are met before
configuring a client.

- The Workbench server must be running and accessible
  from the machine where the MCP client operates.
- An API token is required for authentication. Create
  tokens through the admin panel under Security >
  Tokens, or use the REST API. See
  [Users & Authentication](../../admin-guide/authentication.md)
  for details on token management.

## Endpoint

The MCP server exposes a JSON-RPC 2.0 endpoint at
`/mcp/v1` on the server's HTTP address.

In the following example, the endpoint URL uses the
default server address:

```text
http://localhost:8080/mcp/v1
```

Include the token in the `Authorization` header using
the Bearer scheme. The token's scope controls which
connections and MCP tools the client can access.

In the following example, a `curl` command sends a
request to the MCP endpoint:

```bash
curl -X POST http://localhost:8080/mcp/v1 \
  -H "Authorization: Bearer YOUR_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

Replace the URL and token with values that match your
environment.

## Client Configuration

The following sections describe how to configure
popular MCP clients. Each example connects to
`http://localhost:8080/mcp/v1` with a Bearer token.
Replace the URL and token value with your own.

### Claude Code

Claude Code stores MCP server configuration in
`~/.claude.json` for user scope or `.mcp.json` for
project scope.

In the following example, the `claude mcp add`
command registers the Workbench server:

```bash
claude mcp add ai-dba-workbench \
  http://localhost:8080/mcp/v1 \
  -t http \
  -H "Authorization: Bearer YOUR_TOKEN"
```

Alternatively, create a `.mcp.json` file in the
project root for project-scoped configuration.

In the following example, the `.mcp.json` file uses
an environment variable for the token:

```json
{
  "mcpServers": {
    "ai-dba-workbench": {
      "type": "http",
      "url": "http://localhost:8080/mcp/v1",
      "headers": {
        "Authorization": "Bearer ${AI_DBA_WORKBENCH_TOKEN}"
      }
    }
  }
}
```

Set the `AI_DBA_WORKBENCH_TOKEN` environment variable
in your shell before launching Claude Code.

### Cursor

Cursor stores MCP server configuration in
`~/.cursor/mcp.json` for user scope or
`.cursor/mcp.json` for workspace scope.

In the following example, the configuration file
uses the `${env:VAR}` syntax for the token:

```json
{
  "mcpServers": {
    "ai-dba-workbench": {
      "type": "http",
      "url": "http://localhost:8080/mcp/v1",
      "headers": {
        "Authorization": "Bearer ${env:AI_DBA_WORKBENCH_TOKEN}"
      }
    }
  }
}
```

Set the `AI_DBA_WORKBENCH_TOKEN` environment variable
in your shell before launching Cursor.

### VS Code (GitHub Copilot)

VS Code stores MCP server configuration in
`.vscode/mcp.json` at the workspace level. The
top-level key is `servers` rather than `mcpServers`.

In the following example, the configuration file
uses the `${input:name}` syntax for the token:

```json
{
  "servers": {
    "ai-dba-workbench": {
      "type": "http",
      "url": "http://localhost:8080/mcp/v1",
      "headers": {
        "Authorization": "Bearer ${input:ai-dba-workbench-token}"
      }
    }
  }
}
```

VS Code prompts for the `input` variable value when
the MCP client connects. You can also use environment
variables as an alternative to interactive input.

### Windsurf

Windsurf stores MCP server configuration in
`~/.codeium/windsurf/mcp_config.json`.

In the following example, the configuration file
uses `serverUrl` instead of `url`:

```json
{
  "mcpServers": {
    "ai-dba-workbench": {
      "serverUrl": "http://localhost:8080/mcp/v1",
      "headers": {
        "Authorization": "Bearer ${env:AI_DBA_WORKBENCH_TOKEN}"
      }
    }
  }
}
```

Set the `AI_DBA_WORKBENCH_TOKEN` environment variable
in your shell before launching Windsurf. Note that
Windsurf uses `serverUrl` instead of `url` in the
configuration.

## Claude Desktop (Not Supported)

Claude Desktop does not support HTTP transport for
MCP servers. The `claude_desktop_config.json` file
only accepts `stdio` transport for locally installed
MCP servers. Use Claude Code instead for connecting
to the Workbench.

## Verification

Once configured, the MCP client should discover the
Workbench's tools automatically. Verify the connection
by asking your AI assistant to list the available MCP
tools or to run a simple query such as listing
database connections.

If the client does not discover the tools, confirm
that the server is running, the URL is correct, and
the token is valid.

## Available Tools

The full list of tools, resources, and prompts is
documented on the
[MCP Tools](../mcp-tools.md) page.

## Related Documentation

- [Ask Ellie](ask-ellie.md) describes the built-in
  AI assistant that uses these tools internally.
- [AI Overview](overview.md) covers AI-powered
  summaries of database health and status.
