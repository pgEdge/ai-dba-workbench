/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package investigate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pgEdge/pgedge-go-llm-lib/llm"
	"github.com/pgEdge/pgedge-go-llm-lib/llm/proxy"

	"github.com/pgedge/ai-workbench/server/internal/evalharness/wbclient"
)

// The paths and limits below mirror the web client's chat loop in
// client/src/hooks/chat/chatAgenticLoop.ts, which this file reproduces.
// Keep the two in step: the point of ChatLoop is to measure what a user
// of the chat panel would get.
const (
	llmChatPath  = "/api/v1/llm/chat"
	toolListPath = "/api/v1/mcp/tools"
	toolCallPath = "/api/v1/mcp/tools/call"

	// MaxRepeatedToolFailures matches MAX_REPEATED_TOOL_FAILURES.
	MaxRepeatedToolFailures = 3

	// maxRecordedResult caps each tool result kept in the transcript.
	maxRecordedResult = 2000
)

// Canned final messages, matching the client's wording.
const (
	IterationLimitMessage = "I was unable to complete the request within the allowed number of " +
		"steps. Please try rephrasing your question."
	UnknownToolMessage = "The model attempted to call tools that are not available to you. " +
		"This usually means your account lacks the necessary MCP " +
		"privileges. Please contact your administrator."
	NoToolsMessage = "You don't have permission to use any of the tools I need to answer " +
		"questions like this. Ask your administrator to grant you the " +
		"relevant MCP privileges and try again."
)

// RepeatedToolFailureMessage matches buildRepeatedToolFailureMessage.
func RepeatedToolFailureMessage(toolName, errorText string) string {
	return fmt.Sprintf("The %q tool failed repeatedly with the same error, "+
		"so I stopped retrying. The error was:\n\n%s\n\n"+
		"This is often a permissions or connection-access problem. "+
		"Ask your administrator to confirm you have access to this "+
		"connection and the relevant MCP privileges, then try again.", toolName, errorText)
}

// ChatLoop investigates by running the chat panel's agentic loop against
// a Workbench server: it sends the conversation to the server's LLM
// proxy and runs each requested tool through the server's MCP tool
// endpoint, exactly as the browser does.
type ChatLoop struct {
	Client *wbclient.Client
	// SystemPrompt is Ellie's chat system prompt (see LoadSystemPrompt).
	SystemPrompt string
	// Now returns the current time; it defaults to time.Now.
	Now func() time.Time
}

// toolListResponse is the body of GET /api/v1/mcp/tools.
type toolListResponse struct {
	Tools []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
	} `json:"tools"`
}

// toolCallRequest and toolCallResponse are the bodies of
// POST /api/v1/mcp/tools/call.
type toolCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type toolCallResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// Investigate implements Investigator.
func (c *ChatLoop) Investigate(ctx context.Context, req Request) (Transcript, error) {
	now := c.Now
	if now == nil {
		now = time.Now
	}
	start := now()
	t := c.run(ctx, req)
	t.Duration = now().Sub(start)
	if err := ctx.Err(); err != nil {
		return t, err
	}
	return t, nil
}

func (c *ChatLoop) run(ctx context.Context, req Request) Transcript {
	var t Transcript

	var list toolListResponse
	if err := c.Client.Do(ctx, http.MethodGet, toolListPath, nil, &list); err != nil {
		return failed(t, fmt.Errorf("list tools: %w", err))
	}
	if len(list.Tools) == 0 {
		t.Outcome, t.FinalText = OutcomeNoTools, NoToolsMessage
		return t
	}
	tools := make([]llm.Tool, 0, len(list.Tools))
	available := make(map[string]bool, len(list.Tools))
	for _, tool := range list.Tools {
		tools = append(tools, llm.Tool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
		available[tool.Name] = true
	}

	messages := []llm.Message{{
		Role:    llm.RoleUser,
		Content: []llm.ContentBlock{{Type: llm.BlockText, Text: req.Prompt}},
	}}
	failures := map[string]int{}

	for t.Iterations < req.MaxIterations {
		if err := ctx.Err(); err != nil {
			return failed(t, err)
		}
		t.Iterations++

		var resp proxy.ChatResponse
		chatReq := proxy.ChatRequest{
			Messages:     messages,
			Tools:        tools,
			SystemPrompt: c.SystemPrompt,
			Provider:     req.Target.Provider,
			Model:        req.Target.Model,
		}
		if err := c.Client.Do(ctx, http.MethodPost, llmChatPath, chatReq, &resp); err != nil {
			return failed(t, fmt.Errorf("LLM request failed: %w", err))
		}
		t.Usage.Add(resp.Usage)

		var toolUses []*llm.ToolUse
		var texts []string
		for _, block := range resp.Content {
			switch {
			case block.Type == llm.BlockToolUse && block.ToolUse != nil:
				toolUses = append(toolUses, block.ToolUse)
			case block.Type == llm.BlockText:
				texts = append(texts, block.Text)
			}
		}

		if len(toolUses) == 0 {
			t.Outcome, t.FinalText = OutcomeCompleted, strings.Join(texts, "\n")
			return t
		}
		if allUnknown(toolUses, available) {
			t.Outcome, t.FinalText = OutcomeUnknownTool, UnknownToolMessage
			return t
		}

		messages = append(messages, llm.Message{Role: llm.RoleAssistant, Content: resp.Content})
		results := make([]llm.ContentBlock, 0, len(toolUses))
		tripped := ""
		for _, use := range toolUses {
			text, isError := c.callTool(ctx, use)
			if ctx.Err() != nil {
				return failed(t, ctx.Err())
			}
			t.ToolCalls = append(t.ToolCalls, ToolCall{
				Name: use.Name, Input: use.Input, IsError: isError, Result: truncate(text, maxRecordedResult),
			})
			results = append(results, llm.ContentBlock{
				Type: llm.BlockToolResult, ToolUseID: use.ID, Text: text, IsError: isError,
			})
			if isError {
				signature := use.Name + " " + text
				failures[signature]++
				if failures[signature] >= MaxRepeatedToolFailures {
					tripped = RepeatedToolFailureMessage(use.Name, text)
				}
			}
		}
		messages = append(messages, llm.Message{Role: llm.RoleUser, Content: results})

		if tripped != "" {
			t.Outcome, t.FinalText = OutcomeToolFailures, tripped
			return t
		}
	}

	t.Outcome, t.FinalText = OutcomeIterationLimit, IterationLimitMessage
	return t
}

// callTool runs one tool call and returns the text fed back to the
// model and whether it is an error, as the client does.
func (c *ChatLoop) callTool(ctx context.Context, use *llm.ToolUse) (string, bool) {
	var out toolCallResponse
	err := c.Client.Do(ctx, http.MethodPost, toolCallPath, toolCallRequest{Name: use.Name, Arguments: use.Input}, &out)
	if err != nil {
		msg := err.Error()
		var he *wbclient.HTTPError
		if errors.As(err, &he) {
			msg = strings.TrimSpace(he.Body)
			if msg == "" {
				msg = fmt.Sprintf("Tool call failed with status %d", he.Status)
			}
		}
		return "Tool execution error: " + msg, true
	}
	text := ""
	if len(out.Content) > 0 {
		text = out.Content[0].Text
	}
	if text == "" {
		if out.IsError {
			text = "Tool execution failed"
		} else {
			text = "No data returned"
		}
	}
	return text, out.IsError
}

func allUnknown(uses []*llm.ToolUse, available map[string]bool) bool {
	for _, u := range uses {
		if available[u.Name] {
			return false
		}
	}
	return true
}

func failed(t Transcript, err error) Transcript {
	t.Outcome, t.Error = OutcomeError, err.Error()
	return t
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "...[truncated]"
}
