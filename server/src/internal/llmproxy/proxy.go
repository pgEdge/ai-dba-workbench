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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	// Wrap the proxy handler with model-name validation. The library's
	// TransformRequest hook receives a *pgllm.ChatRequest that does NOT
	// carry the per-request model override (the model lives on the proxy
	// wire type proxy.ChatRequest and is consumed by the library before
	// TransformRequest runs), so the validation cannot live in that hook.
	// The middleware peeks the model from the request body and rejects an
	// unsafe value before it can reach a provider's URL path. See
	// validateModelName for the security rationale.
	return validateModelMiddleware(p.Handler()), nil
}

// modelBearingPaths are the POST endpoints whose JSON body carries an
// optional per-request "model" override. These are the only routes that
// can flow an attacker-controlled model into a provider's URL path, so
// the validation middleware inspects exactly these and passes every other
// route (GET discovery endpoints, unknown paths) straight through.
var modelBearingPaths = map[string]bool{
	"/api/v1/llm/chat":             true,
	"/api/v1/llm/chat/stream":      true,
	"/api/v1/llm/embed":            true,
	"/api/v1/llm/embed/multimodal": true,
	"/api/v1/llm/rerank":           true,
}

// modelEnvelope captures just the "model" field from a request body so the
// middleware can validate it without depending on the full proxy wire
// types. Every model-bearing endpoint shares this field name.
type modelEnvelope struct {
	Model string `json:"model"`
}

// validateModelMiddleware rejects requests that carry an unsafe per-request
// model override before they reach the library proxy.
//
// SECURITY: the per-request model is interpolated UNESCAPED into the
// provider URL path (e.g. the Gemini provider builds
// "/v1beta/models/{model}:generateContent"). Without validation an
// authenticated caller could smuggle traversal or path characters
// ("../", "%2f", and similar) into that path and redirect the upstream
// request. This middleware restores the validation that the pre-migration
// HandleChat performed inline; the library's TransformRequest hook cannot
// see the model, so the check is enforced here at the HTTP boundary.
//
// The middleware reads the body (bounded by maxChatBodySize), validates
// the model, then replaces the body with a fresh reader so the proxy can
// parse it normally. An empty model is allowed: it means "use the
// operator-configured default", which is trusted.
//
// SECURITY: the read is bounded by maxChatBodySize+1 bytes so an oversized
// body can be detected rather than silently truncated. A LimitReader at
// exactly maxChatBodySize would truncate a larger body to its first
// maxChatBodySize bytes, and restoring only that prefix would let the
// proxy's own MaxBytesReader (also maxChatBodySize) accept the truncated
// request, processing a request the cap was meant to reject. Reading one
// extra byte lets the middleware distinguish "at the cap" from "over the
// cap" and reject the latter with 413 before the wrapped handler runs.
func validateModelMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !modelBearingPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		// Read up to maxChatBodySize+1 bytes. If the extra byte is
		// present the body exceeds the cap and must be rejected rather
		// than truncated and processed.
		body, err := io.ReadAll(io.LimitReader(r.Body, maxChatBodySize+1))
		if err != nil {
			writeModelError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if int64(len(body)) > maxChatBodySize {
			writeModelError(w, http.StatusRequestEntityTooLarge,
				"request body exceeds the maximum allowed size")
			return
		}
		// Restore the body so the proxy can decode it again. The proxy
		// re-applies its own MaxBytesReader on top of this reader.
		r.Body = io.NopCloser(bytes.NewReader(body))

		var env modelEnvelope
		// A malformed body is left for the proxy to reject with its own
		// error; we only act on a successfully-parsed, unsafe model.
		if jsonErr := json.Unmarshal(body, &env); jsonErr == nil {
			if env.Model != "" && !isValidModelName(env.Model) {
				writeModelError(w, http.StatusBadRequest,
					"invalid model name: must be 1-256 characters and contain only "+
						"alphanumeric characters, hyphens, dots, colons, forward slashes, and underscores")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// writeModelError emits a JSON error response matching the proxy's
// ErrorResponse wire shape ({"error": "..."}) so callers see a consistent
// error envelope regardless of whether the proxy or this middleware
// produced the rejection.
func writeModelError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(proxy.ErrorResponse{Error: msg}); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to encode model-validation error response: %v\n", err)
	}
}

// isValidModelName validates that a model name contains only safe
// characters and is within the allowed length. Allowed characters are
// alphanumeric, hyphens, dots, colons, forward slashes, and underscores.
//
// In addition to the charset check, any model name that contains the
// substring ".." is rejected. A literal "../" sequence passes the
// character-set filter (both '.' and '/' are in the allowed set), but
// the Go HTTP client path-cleans such a value when it is interpolated
// into a provider URL path (e.g. the Gemini provider builds
// "/v1beta/models/{model}:generateContent"), producing a different
// upstream path than intended. Rejecting ".." closes VULN-001 for the
// literal traversal form; the percent-encoded form ("..%2f") is already
// rejected by the charset check because '%' is not in the allowed set.
// Single dots (e.g. "voyage-3.5") and single slashes (e.g.
// "vendor/model") remain valid.
func isValidModelName(model string) bool {
	if model == "" || len(model) > 256 {
		return false
	}
	for _, c := range model {
		if (c < 'a' || c > 'z') &&
			(c < 'A' || c > 'Z') &&
			(c < '0' || c > '9') &&
			c != '-' && c != '.' && c != ':' && c != '/' && c != '_' {
			return false
		}
	}
	// SECURITY: reject any literal ".." substring to prevent path-traversal
	// via the provider URL interpolation (VULN-001). This must come after
	// the charset loop so that an invalid character is caught first and
	// the ".." check is only reached for otherwise-valid names.
	if strings.Contains(model, "..") {
		return false
	}
	return true
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

	c.applyCompactDescriptions(req)

	// Default to the Ellie system prompt when the request omits one, then
	// layer memory and user/RBAC context on top in that exact order.
	base := req.SystemPrompt
	if base == "" {
		base = chat.SystemPrompt
	}
	base = c.applyMemoryContext(ctx, base)
	base = c.applyUserContext(ctx, base)

	req.SystemPrompt = base
	return nil
}

// applyCompactDescriptions populates each tool's CompactDescription from the
// server-side registry and switches the request to compact tool
// descriptions. The web client sends tools without CompactDescription
// populated, so we look them up and set them. It is a no-op when compact
// descriptions are disabled or the registry is empty.
func (c *Config) applyCompactDescriptions(req *pgllm.ChatRequest) {
	if !c.UseCompactDescriptions || len(c.CompactDescriptions) == 0 {
		return
	}
	for i := range req.Tools {
		if cd, ok := c.CompactDescriptions[req.Tools[i].Name]; ok {
			req.Tools[i].CompactDescription = cd
		}
	}
	req.ToolDescriptions = pgllm.ToolDescriptionCompact
}

// applyMemoryContext wraps base with the caller's pinned-memory context when
// a memory store is configured and the caller has pinned memories. It
// returns base unchanged otherwise. Fetch errors are logged and treated as
// "no pinned memories" so the request still proceeds.
func (c *Config) applyMemoryContext(ctx context.Context, base string) string {
	if c.MemoryStore == nil {
		return base
	}
	username := auth.GetUsernameFromContext(ctx)
	if username == "" {
		return base
	}
	pinned, memErr := c.MemoryStore.GetPinned(ctx, username)
	if memErr != nil {
		fmt.Fprintf(os.Stderr, "WARNING: Failed to fetch pinned memories: %v\n", memErr)
		return base
	}
	if len(pinned) == 0 {
		return base
	}
	return chat.BuildSystemPrompt(base, pinned)
}

// applyUserContext wraps base with the caller's user/RBAC context when an
// auth store is configured and the caller resolves to a known user. It
// returns base unchanged otherwise.
func (c *Config) applyUserContext(ctx context.Context, base string) string {
	if c.AuthStore == nil {
		return base
	}
	userID := auth.GetUserIDFromContext(ctx)
	username := auth.GetUsernameFromContext(ctx)
	if userID <= 0 || username == "" {
		return base
	}
	ui := buildUserInfo(c.AuthStore, userID, username)
	if ui == nil {
		return base
	}
	return chat.BuildUserContext(base, ui)
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
