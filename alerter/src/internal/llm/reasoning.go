/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	pgllm "github.com/pgEdge/pgedge-go-llm-lib/llm"
	// Register all built-in LLM providers (anthropic, openai, gemini,
	// ollama, voyage) so pgllm.NewClient can construct them by name.
	_ "github.com/pgEdge/pgedge-go-llm-lib/llm/all"
)

// defaultReasoningModels maps a provider name to the reasoning model used
// when the configuration does not specify one. These mirror the defaults
// the previous bespoke providers applied.
var defaultReasoningModels = map[string]string{
	"openai":    "gpt-4o-mini",
	"anthropic": "claude-3-5-haiku-20241022",
	"gemini":    "gemini-2.5-flash",
	"ollama":    "llama3.2",
}

// DefaultReasoningMaxTokens is the output-token budget used for a tier-3
// classification when the configuration leaves llm.max_tokens unset or
// non-positive. It mirrors config.DefaultLLMMaxTokens; the constant is
// duplicated here so this package resolves a usable budget even when a
// caller builds a provider without going through config.NewConfig.
const DefaultReasoningMaxTokens = 4096

// reasoningTemperature keeps classification deterministic.
const reasoningTemperature = 0.1

// ErrNoTextContent reports that a reasoning response carried no usable
// text. The engine treats a Classify error as a tier-3 failure (failing
// safe to "alert") and records the message, so naming the likely cause
// here puts an actionable hint in the candidate's tier-3 error field.
var ErrNoTextContent = errors.New(
	"LLM response contained no text content; the model may have " +
		"exhausted its output token budget on reasoning tokens: " +
		"increase llm.max_tokens or select a model that emits less reasoning")

// libReasoning implements ReasoningProvider by delegating to the
// pgedge-go-llm-lib Client.Chat method. A single implementation serves
// every provider; the provider-specific behavior lives in the library.
type libReasoning struct {
	client pgllm.Client

	// maxTokens is the resolved (always positive) output-token budget for
	// each classification request.
	maxTokens int
}

// Classify sends the classification system prompt and the supplied prompt
// to the underlying LLM and returns the concatenated text of the
// response's text blocks. Non-text content blocks are ignored.
//
// A response with no text block, or whose text is whitespace only, yields
// ErrNoTextContent rather than an empty string. Reasoning models charge
// their thinking blocks against the same output budget as the verdict, so
// an under-sized budget produces exactly this shape of response; returning
// an error stops the engine from parsing an empty verdict and recording a
// blank tier-3 result.
func (r *libReasoning) Classify(ctx context.Context, prompt string) (string, error) {
	resp, err := r.client.Chat(ctx, pgllm.ChatRequest{
		SystemPrompt: classificationSystemPrompt,
		Messages:     []pgllm.Message{pgllm.UserText(prompt)},
		MaxTokens:    pgllm.Int(r.maxTokens),
		Temperature:  pgllm.Float(reasoningTemperature),
	})
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	if resp != nil {
		for _, b := range resp.Content {
			if b.Type == pgllm.BlockText {
				sb.WriteString(b.Text)
			}
		}
	}
	text := sb.String()
	if strings.TrimSpace(text) == "" {
		return "", ErrNoTextContent
	}
	return text, nil
}

// ModelName returns the reasoning model configured on the client.
func (r *libReasoning) ModelName() string {
	return r.client.Model()
}

// newLibReasoning builds a libReasoning for the named provider. When model
// is empty, the per-provider default from defaultReasoningModels is used.
// When maxTokens is zero or negative, DefaultReasoningMaxTokens is used, so
// an unset llm.max_tokens can never produce an unusable budget. apiKey and
// baseURL are passed straight through to the library.
func newLibReasoning(provider, apiKey, model, baseURL string, maxTokens int) (ReasoningProvider, error) {
	if model == "" {
		model = defaultReasoningModels[provider]
	}
	if maxTokens <= 0 {
		maxTokens = DefaultReasoningMaxTokens
	}

	client, err := pgllm.NewClient(provider, pgllm.Options{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: baseURL,
	})
	if err != nil {
		return nil, fmt.Errorf("create %s reasoning client: %w", provider, err)
	}

	return &libReasoning{client: client, maxTokens: maxTokens}, nil
}
