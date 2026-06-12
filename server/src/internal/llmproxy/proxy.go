/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package llmproxy adapts the pgEdge AI DBA Workbench's LLM configuration,
// authentication, and prompt-injection policy onto the library's
// llm/proxy gateway. It is a thin shim: the library owns the HTTP wire
// contract (typed content blocks, system_prompt, usage) and the provider
// transport, whilst this package supplies the Workbench-specific
// behavior via the proxy's Authorize, TransformRequest, and telemetry
// hooks.
package llmproxy

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	pgllm "github.com/pgEdge/pgedge-go-llm-lib/llm"
	_ "github.com/pgEdge/pgedge-go-llm-lib/llm/all" // register all built-in providers
	"github.com/pgEdge/pgedge-go-llm-lib/llm/proxy"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/chat"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/memory"
	"github.com/pgedge/ai-workbench/server/internal/tracing"
)

// Config holds LLM configuration from the server config along with the
// stores and policy flags the shim needs to enforce Workbench behavior
// through the library proxy's hooks.
type Config struct {
	Provider               string
	Model                  string
	AnthropicAPIKey        string
	AnthropicBaseURL       string
	OpenAIAPIKey           string
	OpenAIBaseURL          string
	GeminiAPIKey           string
	GeminiBaseURL          string
	OllamaURL              string
	MaxTokens              int
	Temperature            float64
	UseCompactDescriptions bool
	CompactDescriptions    map[string]string // tool name -> compact description
	MemoryStore            *memory.Store     // Memory store for pinned memory injection (may be nil)
	AuthStore              *auth.AuthStore   // Auth store for user context injection (may be nil)
	LLMConfig              *config.LLMConfig // LLMConfig for accessing custom headers (may be nil)
}

// maxChatBodySize caps the chat/embed/rerank request body at 5MB to
// accommodate tool definitions and message history, consistent with the
// DecodeJSONBody pattern used elsewhere in the API layer.
const maxChatBodySize = 5 << 20 // 5 MB

// NewHandler builds the library LLM proxy from cfg and returns the
// http.Handler that serves the /api/v1/llm routes. The provider map
// includes only the providers cfg actually configures (mirroring the
// historical provider-gating rules), so an unconfigured provider is
// unreachable through the proxy. Authentication, prompt injection, and
// tracing are wired through the proxy's hooks.
func NewHandler(cfg *Config) (http.Handler, error) {
	providers := cfg.buildProviders()

	p := proxy.New(proxy.Config{
		DefaultProvider:  cfg.Provider,
		Providers:        providers,
		PathPrefix:       "/api/v1/llm",
		MaxBodyBytes:     maxChatBodySize,
		Authorize:        cfg.authorize,
		TransformRequest: cfg.transformRequest,
		OnRequest:        cfg.onRequest,
		OnResponse:       cfg.onResponse,
		OnError:          cfg.onError,
	})
	return p.Handler(), nil
}

// buildProviders assembles the per-provider Options map. A provider is
// included only when configured: anthropic and gemini require an API
// key; openai requires a key OR a base URL (to support local
// OpenAI-compatible endpoints); ollama requires a URL. This mirrors the
// gating the old HandleProviders endpoint applied.
func (c *Config) buildProviders() map[string]pgllm.Options {
	providers := make(map[string]pgllm.Options)

	if c.AnthropicAPIKey != "" {
		providers["anthropic"] = c.providerOptions("anthropic", c.AnthropicAPIKey, c.AnthropicBaseURL)
	}
	if c.OpenAIAPIKey != "" || c.OpenAIBaseURL != "" {
		providers["openai"] = c.providerOptions("openai", c.OpenAIAPIKey, c.OpenAIBaseURL)
	}
	if c.GeminiAPIKey != "" {
		providers["gemini"] = c.providerOptions("gemini", c.GeminiAPIKey, c.GeminiBaseURL)
	}
	if c.OllamaURL != "" {
		providers["ollama"] = c.providerOptions("ollama", "", c.OllamaURL)
	}

	return providers
}

// providerOptions constructs the library Options for a single provider,
// carrying the shared model/token/temperature/timeout defaults plus the
// provider's custom headers. Header-loading failures are logged and
// treated as no headers, matching the old getProviderHeaders behavior.
func (c *Config) providerOptions(name, apiKey, baseURL string) pgllm.Options {
	headers, err := getProviderHeaders(c.LLMConfig, name)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to get %s provider headers: %v\n", name, err)
		headers = nil
	}

	opts := pgllm.Options{
		APIKey:        apiKey,
		Model:         c.Model,
		BaseURL:       baseURL,
		CustomHeaders: headers,
	}
	if c.MaxTokens > 0 {
		opts.MaxTokens = pgllm.Int(c.MaxTokens)
	}
	if c.Temperature > 0 {
		opts.Temperature = pgllm.Float(c.Temperature)
	}
	if c.LLMConfig != nil && c.LLMConfig.TimeoutSeconds > 0 {
		opts.RequestTimeout = time.Duration(c.LLMConfig.TimeoutSeconds) * time.Second
	}
	return opts
}

// authorize is the proxy's Authorize hook. The public discovery
// endpoints (providers/models/health) require no credentials, matching
// the old behavior that let the login page list providers. Every other
// endpoint (chat, chat/stream, embed, rerank) requires a valid token; on
// success the enriched auth context is attached to the request so the
// TransformRequest hook can read the authenticated identity.
//
// SECURITY: suffix-matching is safe here because the library registers
// eight exact Go 1.22 method+path routes (GET .../providers, .../models,
// .../health; POST .../chat, .../chat/stream, .../embed, .../rerank) and
// only calls Authorize from within an already-routed handler. By the time
// this function runs the path is exactly one of those values, so
// "/api/v1/llm/chat" cannot reach the public branch.
//
// Coupling note: if a future pgedge-go-llm-lib version adds a new public
// read endpoint it will default to auth-required here (fail-safe, fine);
// but a new route whose path happened to end in "/models" or "/health"
// as a subpath would be made public. Re-validate this allow-list whenever
// the library version is bumped.
func (c *Config) authorize(r *http.Request) error {
	switch {
	case strings.HasSuffix(r.URL.Path, "/providers"),
		strings.HasSuffix(r.URL.Path, "/models"),
		strings.HasSuffix(r.URL.Path, "/health"):
		return nil // public, as today
	}

	ctx, err := auth.AuthenticateRequest(r, c.AuthStore)
	if err != nil {
		return &proxy.AuthError{Err: err, Status: http.StatusUnauthorized}
	}
	// Sanctioned context-attach: the proxy invokes Authorize, then
	// TransformRequest/OnRequest, against this same *http.Request, so
	// the attached identity propagates downstream.
	*r = *r.WithContext(ctx)
	return nil
}

// transformRequest is the proxy's TransformRequest hook. It reproduces
// the old HandleChat injection exactly:
//
//  1. compact tool-description selection (Workbench policy);
//  2. default to the Ellie system prompt when the request omits one;
//  3. wrap with pinned-memory context (when a memory store is present);
//  4. wrap with user/RBAC context (when an auth store is present).
//
// The Ellie persona is ALWAYS applied when the request carries no system
// prompt; this was previously done inside the chat clients and is now
// done here so the persona survives the migration.
func (c *Config) transformRequest(r *http.Request, req *pgllm.ChatRequest) error {
	ctx := r.Context()

	// Apply compact descriptions from the server-side registry. The web
	// client sends tools without CompactDescription populated, so we
	// look them up and set them, then ask the provider to use compact
	// descriptions.
	if c.UseCompactDescriptions && len(c.CompactDescriptions) > 0 {
		for i := range req.Tools {
			if cd, ok := c.CompactDescriptions[req.Tools[i].Name]; ok {
				req.Tools[i].CompactDescription = cd
			}
		}
		req.ToolDescriptions = pgllm.ToolDescriptionCompact
	}

	// Default to the Ellie system prompt when the request omits one.
	base := req.SystemPrompt
	if base == "" {
		base = chat.SystemPrompt
	}

	// Inject pinned memories into the system prompt when available.
	if c.MemoryStore != nil {
		if username := auth.GetUsernameFromContext(ctx); username != "" {
			pinned, memErr := c.MemoryStore.GetPinned(ctx, username)
			if memErr != nil {
				fmt.Fprintf(os.Stderr, "WARNING: Failed to fetch pinned memories: %v\n", memErr)
			} else if len(pinned) > 0 {
				base = chat.BuildSystemPrompt(base, pinned)
			}
		}
	}

	// Inject user/RBAC context into the system prompt when available.
	if c.AuthStore != nil {
		userID := auth.GetUserIDFromContext(ctx)
		username := auth.GetUsernameFromContext(ctx)
		if userID > 0 && username != "" {
			if ui := buildUserInfo(c.AuthStore, userID, username); ui != nil {
				base = chat.BuildUserContext(base, ui)
			}
		}
	}

	req.SystemPrompt = base
	return nil
}

// onRequest logs user prompts when tracing is enabled, reproducing the
// per-user-message LogUserPrompt calls the old HandleChat made.
func (c *Config) onRequest(r *http.Request, info proxy.RequestInfo) {
	if !tracing.IsEnabled() || info.Request == nil {
		return
	}
	tokenHash := auth.GetTokenHashFromContext(r.Context())
	for _, msg := range info.Request.Messages {
		if msg.Role != pgllm.RoleUser {
			continue
		}
		for _, block := range msg.Content {
			if block.Type == pgllm.BlockText {
				tracing.LogUserPrompt(tokenHash, tokenHash, info.RequestID, block.Text)
			}
		}
	}
}

// onResponse logs the assembled LLM response when tracing is enabled,
// reproducing the old HandleChat LogLLMResponse call.
func (c *Config) onResponse(r *http.Request, info proxy.ResponseInfo) {
	if !tracing.IsEnabled() || info.Response == nil {
		return
	}
	tokenHash := auth.GetTokenHashFromContext(r.Context())
	tracing.LogLLMResponse(tokenHash, tokenHash, info.RequestID, info.Response.Content, 0)
}

// onError logs request errors when tracing is enabled, reproducing the
// old HandleChat LogError call.
func (c *Config) onError(r *http.Request, info proxy.ErrorInfo) {
	if !tracing.IsEnabled() || info.Err == nil {
		return
	}
	tokenHash := auth.GetTokenHashFromContext(r.Context())
	tracing.LogError(tokenHash, tokenHash, info.RequestID, "llm_chat", info.Err)
}

// buildUserInfo fetches user data from the auth store and returns a
// UserInfo struct for system prompt injection. Returns nil if the user
// cannot be looked up. Errors are logged but do not fail the request.
func buildUserInfo(authStore *auth.AuthStore, userID int64, username string) *chat.UserInfo {
	user, err := authStore.GetUser(username)
	if err != nil || user == nil {
		fmt.Fprintf(os.Stderr, "WARNING: Failed to fetch user %q for context injection: %v\n", username, err)
		return nil
	}

	info := &chat.UserInfo{
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Notes:       user.Annotation,
		IsSuperuser: user.IsSuperuser,
	}

	// Fetch group names.
	groups, err := authStore.GetGroupsForUser(userID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: Failed to fetch groups for user %q: %v\n", username, err)
	} else {
		for _, g := range groups {
			info.Groups = append(info.Groups, g.Name)
		}
	}

	// Fetch admin permissions.
	perms, err := authStore.GetUserAdminPermissions(userID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: Failed to fetch admin permissions for user %q: %v\n", username, err)
	} else {
		for perm, enabled := range perms {
			if enabled {
				info.AdminPerms = append(info.AdminPerms, perm)
			}
		}
	}

	return info
}

// getProviderHeaders retrieves custom headers for the given provider from
// the LLMConfig. Returns nil if the config is nil or if header loading
// fails.
func getProviderHeaders(llmConfig *config.LLMConfig, provider string) (map[string]string, error) {
	if llmConfig == nil {
		return nil, nil
	}
	return llmConfig.GetProviderHeaders(provider)
}
