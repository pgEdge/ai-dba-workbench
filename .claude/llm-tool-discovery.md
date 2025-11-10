# LLM Tool Discovery Pattern

## Overview

The CLI implements a virtual tool discovery mechanism to optimize token usage
and avoid API rate limits when providing tools to LLMs. Instead of sending all
30+ tools upfront (~10,000 tokens), we send only 6 tools initially (~1,000
tokens) - a 90% reduction in token usage.

## Problem Statement

When integrating with Anthropic's API, sending all available MCP tools in every
request consumes significant tokens:

- **30 tools** with full schemas = ~10,000 input tokens per request
- Anthropic rate limit: 10,000 input tokens per minute
- Result: Could only make 1 request per minute, severely limiting usability

## Solution: Virtual Tool Discovery

We implemented two virtual "meta-tools" that allow LLMs to discover available
tools on-demand:

1. **`list_available_tools`**: Lists all tool names and brief descriptions
2. **`get_tool_schema`**: Returns full schema for a specific tool by name

### Initial Tool Set

When an LLM conversation starts, we provide only 6 tools:

**Discovery Tools (always available):**
- `list_available_tools`
- `get_tool_schema`

**Essential Tools (pre-loaded from catalog):**
- `execute_query` - Most commonly used for database operations
- `set_database_context` - Frequently needed for multi-database work
- `get_database_context` - Companion to set_database_context
- `read_resource` - Common for accessing resource data

### Lazy Loading Behavior

1. LLM starts with 6 tools available
2. If LLM needs a different tool, it calls `list_available_tools` to see what's
   available
3. LLM calls `get_tool_schema` with the tool name to get full schema
4. The tool is added to `availableTools` map for future use in the conversation
5. Subsequent LLM requests include the newly discovered tool

### Virtual Tool Handling

Discovery tools are handled **locally** by the CLI, not by the MCP server:

```go
func handleDiscoveryToolCall(toolName string, args map[string]interface{},
    fullCatalog map[string]Tool, availableTools map[string]Tool)
    (interface{}, bool, error)
```

- Returns `(result, true, nil)` for discovery tools (handled locally)
- Returns `(nil, false, nil)` for regular tools (pass to MCP server)
- This avoids unnecessary network calls and keeps discovery fast

## Implementation Details

### Data Structures

**Full Tool Catalog:**
```go
fullToolCatalog := make(map[string]Tool)
for _, tool := range tools {
    fullToolCatalog[tool.Name] = tool
}
```

**Available Tools (Dynamic):**
```go
availableTools := make(map[string]Tool)
// Add discovery tools
for name, tool := range discoveryTools {
    availableTools[name] = tool
}
// Add essential tools
for name := range essentialToolNames {
    if tool, exists := fullToolCatalog[name]; exists {
        availableTools[name] = tool
    }
}
```

### Tool Building

Tools are built dynamically from `availableTools` on each iteration:

**Anthropic:**
```go
buildAnthropicTools := func() []map[string]interface{} {
    var anthropicTools []map[string]interface{}
    for _, tool := range availableTools {
        anthropicTools = append(anthropicTools, map[string]interface{}{
            "name":         tool.Name,
            "description":  tool.Description,
            "input_schema": tool.InputSchema,
        })
    }
    return anthropicTools
}
```

**Ollama:**
```go
buildOllamaTools := func() []api.Tool {
    var ollamaTools []api.Tool
    for _, tool := range availableTools {
        // Convert to Ollama format...
        ollamaTools = append(ollamaTools, api.Tool{...})
    }
    return ollamaTools
}
```

### Adding Tools Dynamically

When `get_tool_schema` is called:

```go
case "get_tool_schema":
    toolNameArg, ok := args["tool_name"].(string)
    // ... validation ...

    // Look up tool in catalog
    tool, exists := fullCatalog[toolNameArg]

    // Add tool to available tools for future use
    availableTools[toolNameArg] = tool

    // Return full tool schema
    return map[string]interface{}{
        "name":        tool.Name,
        "description": tool.Description,
        "inputSchema": tool.InputSchema,
    }, true, nil
```

## Provider Consistency

**IMPORTANT:** Both Anthropic and Ollama use the **exact same approach** for
consistency:

- Same initial tool set (2 discovery + 4 essential)
- Same discovery mechanism
- Same virtual tool handling
- Same lazy loading behavior

This ensures predictable behavior regardless of which LLM provider is active.

## Token Savings

**Before optimization:**
- Initial request: ~10,000 tokens (all 30 tools)
- Every subsequent request: ~10,000 tokens
- Total for 5-turn conversation: ~50,000 tokens

**After optimization:**
- Initial request: ~1,000 tokens (6 tools)
- If LLM needs more tools: +100 tokens for list, +300 tokens per schema
- Typical 5-turn conversation: ~6,000 tokens
- **Savings: ~88% reduction in token usage**

## Trade-offs

**Benefits:**
- 90% reduction in initial token usage
- Avoids Anthropic rate limits (10,000 tokens/min)
- Scales to any number of tools without impacting initial cost
- Consistent behavior across providers

**Costs:**
- Adds 1-2 extra round-trips if LLM needs non-essential tools
- Slightly more complex implementation
- LLM must "know" to use discovery tools (they do, reliably)

## Files Modified

- **cli/src/llm.go:**
  - `createDiscoveryTools()` - Creates virtual discovery tools (lines 55-82)
  - `handleDiscoveryToolCall()` - Handles discovery tool execution (lines 84-133)
  - `AnthropicClient.Chat()` - Modified to use tool discovery (lines 125-320)
  - `OllamaClient.Chat()` - Modified to use tool discovery (lines 385-608)

## Testing

Tested with both Anthropic and Ollama:

1. LLMs correctly call `list_available_tools` as first action
2. LLMs successfully request schemas for specific tools
3. Tools are properly added to available set
4. Subsequent tool calls work correctly
5. Token usage reduced from ~10,000 to ~1,000 for initial requests

## Future Considerations

- Could implement tool usage analytics to refine "essential tools" list
- Could add caching of tool schemas across conversations
- Could implement tool grouping/categories for better discovery
- Consider making essential tools configurable per use case

## References

- Implemented: 2025-01-10
- Motivated by: Anthropic rate limits (10k tokens/min)
- Pattern inspired by: MCP resource optimization (commit 5ae559e)
