/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package llmproxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/apiconst"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/chat"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/memory"
	"github.com/pgedge/ai-workbench/server/internal/tracing"
)

// Config holds LLM configuration from the server config
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

// Message represents a message in the chat conversation
type Message struct {
	Role         string         `json:"role"`
	Content      any            `json:"content"`
	CacheControl map[string]any `json:"cache_control,omitempty"`
}

// Tool represents an MCP tool definition
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

// InputSchema defines the JSON schema for tool input
type InputSchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Required   []string       `json:"required,omitempty"`
}

// ProvidersResponse represents the response for GET /api/llm/providers
type ProvidersResponse struct {
	Providers    []ProviderInfo `json:"providers"`
	DefaultModel string         `json:"defaultModel"`
}

// ProviderInfo represents information about an LLM provider
type ProviderInfo struct {
	Name      string `json:"name"`
	Display   string `json:"display"`
	IsDefault bool   `json:"isDefault"`
}

// ModelsResponse represents the response for GET /api/llm/models
type ModelsResponse struct {
	Models []ModelInfo `json:"models"`
}

// ModelInfo represents information about an LLM model
type ModelInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ChatRequest represents the request body for POST /api/llm/chat
type ChatRequest struct {
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools"`
	System   string    `json:"system,omitempty"`   // Optional system prompt override
	Provider string    `json:"provider,omitempty"` // Override default provider
	Model    string    `json:"model,omitempty"`    // Override default model
	Debug    bool      `json:"debug,omitempty"`    // Enable debug mode for token usage
}

// ChatResponse represents the response body for POST /api/llm/chat
type ChatResponse struct {
	Content    []any            `json:"content"`
	StopReason string           `json:"stop_reason"`
	TokenUsage *chat.TokenUsage `json:"token_usage,omitempty"` // Optional token usage (when debug enabled)
}

// HandleProviders handles GET /api/v1/llm/providers
func HandleProviders(w http.ResponseWriter, r *http.Request, config *Config) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	providers := []ProviderInfo{}

	// Check which providers are configured
	if config.AnthropicAPIKey != "" {
		providers = append(providers, ProviderInfo{
			Name:      "anthropic",
			Display:   "Anthropic Claude",
			IsDefault: config.Provider == "anthropic",
		})
	}

	if config.OpenAIAPIKey != "" || config.OpenAIBaseURL != "" {
		providers = append(providers, ProviderInfo{
			Name:      "openai",
			Display:   "OpenAI",
			IsDefault: config.Provider == "openai",
		})
	}

	if config.GeminiAPIKey != "" {
		providers = append(providers, ProviderInfo{
			Name:      "gemini",
			Display:   "Google Gemini",
			IsDefault: config.Provider == "gemini",
		})
	}

	if config.OllamaURL != "" {
		providers = append(providers, ProviderInfo{
			Name:      "ollama",
			Display:   "Ollama",
			IsDefault: config.Provider == "ollama",
		})
	}

	response := ProvidersResponse{
		Providers:    providers,
		DefaultModel: config.Model,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Link", fmt.Sprintf("<%s>; rel=\"service-desc\"", apiconst.OpenAPISpecPath))
	if err := json.NewEncoder(w).Encode(response); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to encode LLM providers response: %v\n", err)
	}
}

// validProviders is an allowlist of supported LLM provider identifiers.
// Validate user-supplied provider values against this map before use.
var validProviders = map[string]bool{
	"anthropic": true,
	"openai":    true,
	"gemini":    true,
	"ollama":    true,
}

// HandleModels handles GET /api/v1/llm/models?provider=<provider>
func HandleModels(w http.ResponseWriter, r *http.Request, config *Config) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	provider := r.URL.Query().Get("provider")
	if provider == "" {
		http.Error(w, "Provider parameter is required", http.StatusBadRequest)
		return
	}

	// Validate provider against allowlist before use
	if !validProviders[provider] {
		http.Error(w, "Unsupported provider", http.StatusBadRequest)
		return
	}

	// Create LLM client for the provider (debug mode always false for models listing)
	var client chat.LLMClient
	switch provider {
	case "anthropic":
		if config.AnthropicAPIKey == "" {
			http.Error(w, "Anthropic API key not configured", http.StatusBadRequest)
			return
		}
		headers, err := getProviderHeaders(config.LLMConfig, "anthropic")
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to get Anthropic provider headers: %v\n", err)
			http.Error(w, "Failed to load provider headers", http.StatusInternalServerError)
			return
		}
		client = chat.NewAnthropicClient(config.AnthropicAPIKey, config.Model, config.MaxTokens, config.Temperature, false, config.AnthropicBaseURL, config.UseCompactDescriptions, headers)
	case "openai":
		if config.OpenAIAPIKey == "" && config.OpenAIBaseURL == "" {
			http.Error(w, "OpenAI API key not configured", http.StatusBadRequest)
			return
		}
		headers, err := getProviderHeaders(config.LLMConfig, "openai")
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to get OpenAI provider headers: %v\n", err)
			http.Error(w, "Failed to load provider headers", http.StatusInternalServerError)
			return
		}
		client = chat.NewOpenAIClient(config.OpenAIAPIKey, config.Model, config.MaxTokens, config.Temperature, false, config.OpenAIBaseURL, config.UseCompactDescriptions, headers)
	case "gemini":
		if config.GeminiAPIKey == "" {
			http.Error(w, "Gemini API key not configured", http.StatusBadRequest)
			return
		}
		headers, err := getProviderHeaders(config.LLMConfig, "gemini")
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to get Gemini provider headers: %v\n", err)
			http.Error(w, "Failed to load provider headers", http.StatusInternalServerError)
			return
		}
		client = chat.NewGeminiClient(config.GeminiAPIKey, config.Model, config.MaxTokens, config.Temperature, false, config.GeminiBaseURL, config.UseCompactDescriptions, headers)
	case "ollama":
		if config.OllamaURL == "" {
			http.Error(w, "Ollama URL not configured", http.StatusBadRequest)
			return
		}
		headers, err := getProviderHeaders(config.LLMConfig, "ollama")
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to get Ollama provider headers: %v\n", err)
			http.Error(w, "Failed to load provider headers", http.StatusInternalServerError)
			return
		}
		client = chat.NewOllamaClient(config.OllamaURL, config.Model, false, config.UseCompactDescriptions, headers)
	}

	// List models
	modelNames, err := client.ListModels(r.Context())
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to list models: %v\n", err)
		http.Error(w, "Failed to list models", http.StatusInternalServerError)
		return
	}

	// Convert to model info
	models := make([]ModelInfo, len(modelNames))
	for i, name := range modelNames {
		models[i] = ModelInfo{
			Name: name,
		}
	}

	response := ModelsResponse{
		Models: models,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Link", fmt.Sprintf("<%s>; rel=\"service-desc\"", apiconst.OpenAPISpecPath))
	if err := json.NewEncoder(w).Encode(response); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to encode LLM models response: %v\n", err)
	}
}

// HandleChat handles POST /api/v1/llm/chat
func HandleChat(w http.ResponseWriter, r *http.Request, config *Config) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	startTime := time.Now()

	// Get tracing context from request
	ctx := r.Context()
	tokenHash := auth.GetTokenHashFromContext(ctx)
	sessionID := tokenHash // Use token hash as session ID
	requestID := tracing.GenerateRequestID()

	// Limit request body size to 5MB to accommodate tool definitions and
	// message history, consistent with the DecodeJSONBody pattern used in
	// the API layer.
	const maxChatBodySize = 5 << 20 // 5 MB
	r.Body = http.MaxBytesReader(w, r.Body, maxChatBodySize)

	// Ensure request body is closed
	defer func() {
		if err := r.Body.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "WARNING: Failed to close request body: %v\n", err)
		}
	}()

	// Parse request body
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Log user prompts if tracing is enabled
	if tracing.IsEnabled() {
		// Extract user messages for logging
		for _, msg := range req.Messages {
			if msg.Role == "user" {
				tracing.LogUserPrompt(sessionID, tokenHash, requestID, msg.Content)
			}
		}
	}

	// Use provided provider/model or defaults
	provider := req.Provider
	if provider == "" {
		provider = config.Provider
	}

	// Validate provider against allowlist before use
	if !validProviders[provider] {
		http.Error(w, "Unsupported provider", http.StatusBadRequest)
		return
	}

	model := req.Model
	if model == "" {
		model = config.Model
	}

	// Validate model name: allow only safe characters, max 256 chars
	if !isValidModelName(model) {
		http.Error(w, "Invalid model name: must be 1-256 characters and contain only alphanumeric characters, hyphens, dots, colons, forward slashes, and underscores", http.StatusBadRequest)
		return
	}

	// Create LLM client with debug mode from request
	var client chat.LLMClient
	switch provider {
	case "anthropic":
		if config.AnthropicAPIKey == "" {
			http.Error(w, "Anthropic API key not configured", http.StatusBadRequest)
			return
		}
		headers, err := getProviderHeaders(config.LLMConfig, "anthropic")
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to get Anthropic provider headers: %v\n", err)
			http.Error(w, "Failed to load provider headers", http.StatusInternalServerError)
			return
		}
		client = chat.NewAnthropicClient(config.AnthropicAPIKey, model, config.MaxTokens, config.Temperature, req.Debug, config.AnthropicBaseURL, config.UseCompactDescriptions, headers)
	case "openai":
		if config.OpenAIAPIKey == "" && config.OpenAIBaseURL == "" {
			http.Error(w, "OpenAI API key not configured", http.StatusBadRequest)
			return
		}
		headers, err := getProviderHeaders(config.LLMConfig, "openai")
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to get OpenAI provider headers: %v\n", err)
			http.Error(w, "Failed to load provider headers", http.StatusInternalServerError)
			return
		}
		client = chat.NewOpenAIClient(config.OpenAIAPIKey, model, config.MaxTokens, config.Temperature, req.Debug, config.OpenAIBaseURL, config.UseCompactDescriptions, headers)
	case "gemini":
		if config.GeminiAPIKey == "" {
			http.Error(w, "Gemini API key not configured", http.StatusBadRequest)
			return
		}
		headers, err := getProviderHeaders(config.LLMConfig, "gemini")
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to get Gemini provider headers: %v\n", err)
			http.Error(w, "Failed to load provider headers", http.StatusInternalServerError)
			return
		}
		client = chat.NewGeminiClient(config.GeminiAPIKey, model, config.MaxTokens, config.Temperature, req.Debug, config.GeminiBaseURL, config.UseCompactDescriptions, headers)
	case "ollama":
		if config.OllamaURL == "" {
			http.Error(w, "Ollama URL not configured", http.StatusBadRequest)
			return
		}
		headers, err := getProviderHeaders(config.LLMConfig, "ollama")
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to get Ollama provider headers: %v\n", err)
			http.Error(w, "Failed to load provider headers", http.StatusInternalServerError)
			return
		}
		client = chat.NewOllamaClient(config.OllamaURL, model, req.Debug, config.UseCompactDescriptions, headers)
	}

	// Convert proxy messages to chat messages
	chatMessages := make([]chat.Message, len(req.Messages))
	for i, msg := range req.Messages {
		chatMessages[i] = chat.Message{
			Role:         msg.Role,
			Content:      msg.Content,
			CacheControl: msg.CacheControl,
		}
	}

	// Apply compact descriptions from the server-side registry.
	// The tools arrive from the web client without CompactDescription
	// populated, so we swap the full description for the compact one
	// before sending tools to the LLM.
	if config.UseCompactDescriptions && len(config.CompactDescriptions) > 0 {
		for i := range req.Tools {
			if compact, ok := config.CompactDescriptions[req.Tools[i].Name]; ok {
				req.Tools[i].Description = compact
			}
		}
	}

	// Inject pinned memories into the system prompt when available.
	// This ensures the LLM always has access to important stored context.
	effectiveSystemPrompt := req.System
	if config.MemoryStore != nil {
		username := auth.GetUsernameFromContext(ctx)
		if username != "" {
			pinnedMemories, memErr := config.MemoryStore.GetPinned(ctx, username)
			if memErr != nil {
				fmt.Fprintf(os.Stderr, "WARNING: Failed to fetch pinned memories: %v\n", memErr)
			} else if len(pinnedMemories) > 0 {
				base := effectiveSystemPrompt
				if base == "" {
					base = chat.SystemPrompt
				}
				effectiveSystemPrompt = chat.BuildSystemPrompt(base, pinnedMemories)
			}
		}
	}

	// Inject user context into the system prompt when available.
	// This gives the LLM awareness of who it is talking to.
	if config.AuthStore != nil {
		userID := auth.GetUserIDFromContext(ctx)
		username := auth.GetUsernameFromContext(ctx)
		if userID > 0 && username != "" {
			userInfo := buildUserInfo(config.AuthStore, userID, username)
			if userInfo != nil {
				base := effectiveSystemPrompt
				if base == "" {
					base = chat.SystemPrompt
				}
				effectiveSystemPrompt = chat.BuildUserContext(base, userInfo)
			}
		}
	}

	// Call LLM - pass tools as []any to avoid import cycle
	// The chat client will access tool fields which are structurally identical to mcp.Tool
	llmResponse, err := client.Chat(ctx, chatMessages, req.Tools, effectiveSystemPrompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: LLM chat request failed: %v\n", err)
		if tracing.IsEnabled() {
			tracing.LogError(sessionID, tokenHash, requestID, "llm_chat", err)
		}
		http.Error(w, "LLM request failed", http.StatusInternalServerError)
		return
	}

	// Log LLM response if tracing is enabled
	if tracing.IsEnabled() {
		duration := time.Since(startTime)
		tracing.LogLLMResponse(sessionID, tokenHash, requestID, llmResponse.Content, duration)
	}

	// Return response
	response := ChatResponse{
		Content:    llmResponse.Content,
		StopReason: llmResponse.StopReason,
		TokenUsage: llmResponse.TokenUsage,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Link", fmt.Sprintf("<%s>; rel=\"service-desc\"", apiconst.OpenAPISpecPath))
	if err := json.NewEncoder(w).Encode(response); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to encode LLM chat response: %v\n", err)
	}
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

	// Fetch group names
	groups, err := authStore.GetGroupsForUser(userID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: Failed to fetch groups for user %q: %v\n", username, err)
	} else {
		for _, g := range groups {
			info.Groups = append(info.Groups, g.Name)
		}
	}

	// Fetch admin permissions
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

// isValidModelName validates that a model name contains only safe
// characters and is within the allowed length. Allowed characters are
// alphanumeric, hyphens, dots, colons, forward slashes, and underscores.
func isValidModelName(model string) bool {
	if model == "" || len(model) > 256 {
		return false
	}
	for _, c := range model {
		if !((c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == ':' || c == '/' || c == '_') {
			return false
		}
	}
	return true
}

// getProviderHeaders retrieves custom headers for the given provider from the
// LLMConfig. Returns nil if the config is nil or if header loading fails.
func getProviderHeaders(llmConfig *config.LLMConfig, provider string) (map[string]string, error) {
	if llmConfig == nil {
		return nil, nil
	}
	return llmConfig.GetProviderHeaders(provider)
}
