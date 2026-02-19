/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package chat

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

	pkgchat "github.com/pgedge/ai-workbench/pkg/chat"
)

// loginHTTPClient is used for login requests with a reasonable timeout.
var loginHTTPClient = &http.Client{Timeout: 30 * time.Second}

// connectToMCP establishes connection to the MCP server
func (c *Client) connectToMCP(ctx context.Context) error {
	if c.config.MCP.Mode == "http" {
		// HTTP mode
		var token string

		if c.config.MCP.AuthMode == "none" {
			// No authentication - connect without a token
			// Used when server has auth disabled
			token = ""
		} else if c.config.MCP.AuthMode == "user" {
			// User authentication mode
			username := c.config.MCP.Username
			password := c.config.MCP.Password

			// Prompt for username if not provided
			if username == "" {
				var err error
				username, err = c.ui.PromptForUsername(ctx)
				if err != nil {
					// User interrupted (Ctrl+C) or other input error
					return fmt.Errorf("authentication canceled")
				}
				if username == "" {
					return fmt.Errorf("username is required for user authentication")
				}
			}

			// Prompt for password if not provided
			if password == "" {
				var err error
				password, err = c.ui.PromptForPassword(ctx)
				if err != nil {
					// User interrupted (Ctrl+C) or other input error
					return fmt.Errorf("authentication canceled")
				}
				if password == "" {
					return fmt.Errorf("password is required for user authentication")
				}
			}

			// Authenticate and get session token
			sessionToken, err := c.authenticateUser(ctx, username, password)
			if err != nil {
				return fmt.Errorf("authentication failed: %w", err)
			}
			token = sessionToken
		} else {
			// Token authentication mode (default for non-"none", non-"user")
			token = c.config.MCP.Token
			if token == "" {
				// Prompt for token
				token = c.ui.PromptForToken()
				if token == "" {
					return fmt.Errorf("authentication token is required for HTTP mode")
				}
			}
		}

		url := c.config.MCP.URL
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			if c.config.MCP.TLS {
				url = "https://" + url
			} else {
				url = "http://" + url
			}
		}

		// Ensure URL ends with /mcp/v1
		if !strings.HasSuffix(url, "/mcp/v1") {
			if strings.HasSuffix(url, "/") {
				url += "mcp/v1"
			} else {
				url += "/mcp/v1"
			}
		}

		c.mcp = NewHTTPClient(url, token)
		// Initialize conversations client for HTTP mode with authentication
		c.conversations = NewConversationsClient(url, token)
	} else {
		// Stdio mode
		mcpClient, err := NewStdioClient(c.config.MCP.ServerPath, c.config.MCP.ServerConfigPath)
		if err != nil {
			return err
		}
		c.mcp = mcpClient
	}

	return nil
}

// authenticateUser authenticates with username/password via the REST API
// and returns a session token
func (c *Client) authenticateUser(ctx context.Context, username, password string) (string, error) {
	// Construct the base URL for the REST API
	baseURL := c.config.MCP.URL
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		if c.config.MCP.TLS {
			baseURL = "https://" + baseURL
		} else {
			baseURL = "http://" + baseURL
		}
	}

	// Remove any trailing path components to get the server root
	// The MCP URL may end with /mcp/v1 but the auth endpoint is at /api/v1/auth/login
	if idx := strings.Index(baseURL, "/mcp"); idx != -1 {
		baseURL = baseURL[:idx]
	}
	baseURL = strings.TrimRight(baseURL, "/")

	// Build the login request
	loginReq := struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{
		Username: username,
		Password: password,
	}

	reqBody, err := json.Marshal(loginReq)
	if err != nil {
		return "", fmt.Errorf("failed to marshal login request: %w", err)
	}

	// POST to the REST API login endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/v1/auth/login", bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := loginHTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read login response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Try to extract error message from JSON response
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return "", fmt.Errorf("%s", errResp.Error)
		}
		return "", fmt.Errorf("authentication failed (HTTP %d)", resp.StatusCode)
	}

	// Parse the success response
	var authResult struct {
		Success   bool   `json:"success"`
		ExpiresAt string `json:"expires_at"`
		Message   string `json:"message"`
	}

	if err := json.Unmarshal(body, &authResult); err != nil {
		return "", fmt.Errorf("failed to parse authentication response: %w", err)
	}

	if !authResult.Success {
		return "", fmt.Errorf("authentication failed: %s", authResult.Message)
	}

	// Extract session token from httpOnly cookie (secure approach)
	var sessionToken string
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "session_token" {
			sessionToken = cookie.Value
			break
		}
	}

	if sessionToken == "" {
		return "", fmt.Errorf("authentication succeeded but no session cookie received")
	}

	return sessionToken, nil
}

// initializeLLM creates the LLM client with model validation and auto-selection
func (c *Client) initializeLLM() error {
	provider := c.config.LLM.Provider

	// Create a temporary client to query available models
	var tempClient LLMClient
	switch provider {
	case "anthropic":
		tempClient = NewAnthropicClient(
			c.config.LLM.AnthropicAPIKey, "", 0, 0, false, c.config.LLM.AnthropicBaseURL, false)
	case "openai":
		tempClient = NewOpenAIClient(
			c.config.LLM.OpenAIAPIKey, "", 0, 0, false, c.config.LLM.OpenAIBaseURL, false)
	case "ollama":
		tempClient = NewOllamaClient(
			c.config.LLM.OllamaURL, "", false, false)
	default:
		return fmt.Errorf("unsupported LLM provider: %s", provider)
	}

	// Get available models from the provider
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	availableModels, err := tempClient.ListModels(ctx)
	if err != nil {
		// If we can't list models, log warning but continue with defaults
		if c.config.UI.Debug {
			fmt.Fprintf(os.Stderr, "Warning: Failed to list models from %s: %v\n", provider, err)
		}
		availableModels = nil
	}

	// Select the best model to use
	selection := c.selectModel(provider, availableModels)
	c.config.LLM.Model = selection.Model

	// Log if we used a family match (newer version of saved model)
	if selection.UsedFamilyMatch && c.config.UI.Debug {
		savedModel := c.preferences.GetModelForProvider(provider)
		fmt.Fprintf(os.Stderr, "[DEBUG] Model updated: %s -> %s (newer version available)\n",
			savedModel, selection.Model)
	}

	// Only save preferences if:
	// 1. There was no saved preference (first time using this provider)
	// 2. We used a family match (update to newer version)
	// Do NOT save if we fell back to default/first available - that would corrupt user's preference
	shouldSave := !selection.HadSavedPref || selection.UsedFamilyMatch
	if shouldSave {
		c.preferences.SetModelForProvider(provider, selection.Model)
		if err := SavePreferences(c.preferences); err != nil {
			if c.config.UI.Debug {
				fmt.Fprintf(os.Stderr, "Warning: Failed to save preferences: %v\n", err)
			}
		}
	}

	// Create the actual LLM client with the selected model
	switch provider {
	case "anthropic":
		c.llm = NewAnthropicClient(
			c.config.LLM.AnthropicAPIKey,
			c.config.LLM.Model,
			c.config.LLM.MaxTokens,
			c.config.LLM.Temperature,
			c.config.UI.Debug,
			c.config.LLM.AnthropicBaseURL,
			false,
		)
	case "openai":
		c.llm = NewOpenAIClient(
			c.config.LLM.OpenAIAPIKey,
			c.config.LLM.Model,
			c.config.LLM.MaxTokens,
			c.config.LLM.Temperature,
			c.config.UI.Debug,
			c.config.LLM.OpenAIBaseURL,
			false,
		)
	case "ollama":
		c.llm = NewOllamaClient(
			c.config.LLM.OllamaURL,
			c.config.LLM.Model,
			c.config.UI.Debug,
			false,
		)
	}

	return nil
}

// selectModel determines the best model to use based on:
// 1. Command-line flag (if set via config)
// 2. Saved preference - exact match
// 3. Saved preference - family match (e.g., claude-opus-4-5-20251101 -> claude-opus-4-5-20251217)
// 4. Default for provider (if available)
// 5. First available model from provider's list
func (c *Client) selectModel(provider string, availableModels []string) pkgchat.ModelSelectionResult {
	debug := c.config.UI.Debug

	// If model was already set (via flag), use it (trust the user)
	if c.config.LLM.Model != "" {
		if debug {
			fmt.Fprintf(os.Stderr, "[DEBUG] Model set via flag: %s\n", c.config.LLM.Model)
		}
		return pkgchat.ModelSelectionResult{Model: c.config.LLM.Model, FromSavedPref: false, HadSavedPref: false}
	}

	// Check saved preference for this provider
	savedModel := c.preferences.GetModelForProvider(provider)
	if debug {
		fmt.Fprintf(os.Stderr, "[DEBUG] Saved model preference for %s: %q\n", provider, savedModel)
		if len(availableModels) > 0 {
			fmt.Fprintf(os.Stderr, "[DEBUG] Available models (%d): %v\n", len(availableModels), availableModels)
		} else {
			fmt.Fprintf(os.Stderr, "[DEBUG] No available models list (API call may have failed)\n")
		}
	}

	// Use shared model selection logic
	result := pkgchat.SelectModelFromPreferences(provider, savedModel, availableModels)

	if debug {
		if result.FromSavedPref {
			if result.UsedFamilyMatch {
				fmt.Fprintf(os.Stderr, "[DEBUG] Family match found: %s -> %s\n", savedModel, result.Model)
			} else {
				fmt.Fprintf(os.Stderr, "[DEBUG] Using saved model (exact match): %s\n", result.Model)
			}
		} else if result.HadSavedPref {
			fmt.Fprintf(os.Stderr, "[DEBUG] Saved preference %q not available, using: %s\n", savedModel, result.Model)
		} else {
			fmt.Fprintf(os.Stderr, "[DEBUG] No saved preference, using: %s\n", result.Model)
		}
	}

	return result
}

// Re-export model selection functions from pkg/chat for local use and tests
var (
	findModelFamilyMatch       = pkgchat.FindModelFamilyMatch
	extractModelFamily         = pkgchat.ExtractModelFamily
	isModelAvailable           = pkgchat.IsModelAvailable
	getDefaultModelForProvider = pkgchat.GetDefaultModelForProvider
)
