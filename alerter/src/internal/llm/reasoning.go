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
	"fmt"
	"strings"

	pgllm "github.com/pgEdge/pgedge-go-llm-lib/llm"
	// Register all built-in LLM providers (anthropic, openai, gemini,
	// ollama, voyage) so pgllm.NewClient can construct them by name.
	_ "github.com/pgEdge/pgedge-go-llm-lib/llm/all"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
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

// libReasoning implements ReasoningProvider by delegating to the
// pgedge-go-llm-lib Client.Chat method. A single implementation serves
// every provider; the provider-specific behavior lives in the library.
type libReasoning struct {
	client    pgllm.Client
	maxTokens int
}

// Classify sends the classification system prompt and the supplied prompt
// to the underlying LLM and returns the concatenated text of the
// response's text blocks. Non-text content blocks are ignored.
//
// The output-token budget comes from the operator-configured
// llm.max_tokens; a response that carries no text at all is reported as an
// error rather than an empty classification, because that is what a
// reasoning model produces when it exhausts the budget on its thinking
// block before emitting an answer.
func (r *libReasoning) Classify(ctx context.Context, prompt string) (string, error) {
	resp, err := r.client.Chat(ctx, pgllm.ChatRequest{
		SystemPrompt: classificationSystemPrompt,
		Messages:     []pgllm.Message{pgllm.UserText(prompt)},
		MaxTokens:    pgllm.Int(r.maxTokens),
		Temperature:  pgllm.Float(0.1),
	})
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, b := range resp.Content {
		if b.Type == pgllm.BlockText {
			sb.WriteString(b.Text)
		}
	}
	text := sb.String()
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("LLM returned no text content; the output token "+
			"budget (llm.max_tokens = %d) may be too small for this model",
			r.maxTokens)
	}
	return text, nil
}

// ModelName returns the reasoning model configured on the client.
func (r *libReasoning) ModelName() string {
	return r.client.Model()
}

// newLibReasoning builds a libReasoning for the named provider. When model
// is empty, the per-provider default from defaultReasoningModels is used.
// apiKey and baseURL are passed straight through to the library. A
// non-positive maxTokens falls back to config.DefaultLLMMaxTokens so a
// caller that omits the setting still gets a workable budget.
func newLibReasoning(provider, apiKey, model, baseURL string, maxTokens int) (ReasoningProvider, error) {
	if model == "" {
		model = defaultReasoningModels[provider]
	}
	if maxTokens <= 0 {
		maxTokens = config.DefaultLLMMaxTokens
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
