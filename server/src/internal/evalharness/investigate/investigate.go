/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package investigate drives an investigation of a staged incident and
// records what happened.
//
// Investigator is the harness's extension point: ChatLoop reproduces the
// web client's chat loop against a running server, and the server-side
// agent loop planned in issue #770 can be added as a second
// implementation without changing the runner, the scoring or the
// reports.
package investigate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pgEdge/pgedge-go-llm-lib/llm"
)

// Target names the LLM provider and model to evaluate. Empty fields
// mean the server's configured default.
type Target struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// ParseTarget parses "provider:model", "provider" or "" (the server
// default).
func ParseTarget(s string) (Target, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "default" {
		return Target{}, nil
	}
	provider, model, _ := strings.Cut(s, ":")
	switch provider {
	case "anthropic", "openai", "gemini", "ollama":
	default:
		return Target{}, fmt.Errorf("target %q: provider must be anthropic, openai, gemini or ollama", s)
	}
	return Target{Provider: provider, Model: model}, nil
}

// String returns the target in the form ParseTarget reads, or "default".
func (t Target) String() string {
	switch {
	case t.Provider == "":
		return "default"
	case t.Model == "":
		return t.Provider
	default:
		return t.Provider + ":" + t.Model
	}
}

// Request is one investigation.
type Request struct {
	// Prompt is the user message that opens the investigation.
	Prompt string
	// Target selects the model.
	Target Target
	// MaxIterations bounds the number of LLM calls.
	MaxIterations int
}

// Outcome says how an investigation ended.
type Outcome string

// Outcomes. Only OutcomeCompleted produces a report written by the
// model; the others end with a canned message, as the chat panel does.
const (
	OutcomeCompleted      Outcome = "completed"
	OutcomeIterationLimit Outcome = "iteration_limit"
	OutcomeUnknownTool    Outcome = "unknown_tool"
	OutcomeToolFailures   Outcome = "repeated_tool_failures"
	OutcomeNoTools        Outcome = "no_tools"
	OutcomeError          Outcome = "error"
)

// ToolCall records one tool call made during an investigation.
type ToolCall struct {
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input,omitempty"`
	IsError bool            `json:"is_error,omitempty"`
	// Result is the tool's output, truncated for the results file.
	Result string `json:"result,omitempty"`
}

// Transcript is the record of one investigation.
type Transcript struct {
	Outcome    Outcome        `json:"outcome"`
	FinalText  string         `json:"final_text"`
	Iterations int            `json:"iterations"`
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
	Usage      llm.TokenUsage `json:"usage"`
	Duration   time.Duration  `json:"duration_ns"`
	// Error holds the failure when Outcome is OutcomeError.
	Error string `json:"error,omitempty"`
}

// ToolNames returns the distinct tools called, in first-call order.
func (t *Transcript) ToolNames() []string {
	seen := map[string]bool{}
	var names []string
	for _, c := range t.ToolCalls {
		if !seen[c.Name] {
			seen[c.Name] = true
			names = append(names, c.Name)
		}
	}
	return names
}

// Investigator runs one investigation. Implementations return a
// Transcript for every investigation that started, recording failures in
// its Outcome and Error, and return an error only when ctx is done.
type Investigator interface {
	Investigate(ctx context.Context, req Request) (Transcript, error)
}
