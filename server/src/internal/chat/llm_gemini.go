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
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/pkg/embedding"
)

// -------------------------------------------------------------------------
// Gemini Client
// -------------------------------------------------------------------------

// geminiClient implements LLMClient for Google Gemini
type geminiClient struct {
	apiKey                 string
	model                  string
	maxTokens              int
	temperature            float64
	debug                  bool
	baseURL                string
	useCompactDescriptions bool
	client                 *http.Client
}

// NewGeminiClient creates a new Google Gemini client
func NewGeminiClient(apiKey, model string, maxTokens int, temperature float64, debug bool, baseURL string, useCompactDescriptions bool) LLMClient {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	return &geminiClient{
		apiKey:                 apiKey,
		model:                  model,
		maxTokens:              maxTokens,
		temperature:            temperature,
		debug:                  debug,
		baseURL:                baseURL,
		useCompactDescriptions: useCompactDescriptions,
		client:                 sharedHTTPClient,
	}
}

// geminiContent represents a content block in the Gemini API
type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

// geminiPart represents a part within a content block
type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
}

// geminiFunctionCall represents a function call from the model
type geminiFunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

// geminiFunctionResponse represents the result of a function call
type geminiFunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

// geminiTool represents a tool definition for Gemini
type geminiTool struct {
	FunctionDeclarations []geminiFunctionDecl `json:"functionDeclarations"`
}

// geminiFunctionDecl represents a function declaration
type geminiFunctionDecl struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters,omitempty"`
}

type geminiRequest struct {
	Contents          []geminiContent        `json:"contents"`
	Tools             []geminiTool           `json:"tools,omitempty"`
	SystemInstruction *geminiContent         `json:"systemInstruction,omitempty"`
	GenerationConfig  map[string]interface{} `json:"generationConfig,omitempty"`
}

type geminiCandidate struct {
	Content      geminiContent `json:"content"`
	FinishReason string        `json:"finishReason"`
}

type geminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type geminiResponse struct {
	Candidates    []geminiCandidate   `json:"candidates"`
	UsageMetadata geminiUsageMetadata `json:"usageMetadata"`
}

type geminiErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// extractGeminiError extracts an error message from Gemini's JSON error response.
func extractGeminiError(body []byte) string {
	var errResp geminiErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error.Message != "" {
		return errResp.Error.Message
	}
	return ""
}

func (c *geminiClient) Chat(ctx context.Context, messages []Message, tools interface{}, customSystemPrompt string) (LLMResponse, error) {
	startTime := time.Now()
	operation := "chat"
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", c.baseURL, c.model, c.apiKey)

	embedding.LogLLMCallDetails("gemini", c.model, operation, c.baseURL+"/v1beta/models/"+c.model+":generateContent", len(messages))

	// Convert interface{} tools to []mcp.Tool
	mcpTools, err := convertToMCPTools(tools)
	if err != nil {
		return LLMResponse{}, err
	}

	// Convert MCP tools to Gemini format
	var geminiTools []geminiTool
	if len(mcpTools) > 0 {
		decls := make([]geminiFunctionDecl, 0, len(mcpTools))
		for _, tool := range mcpTools {
			desc := tool.Description
			if c.useCompactDescriptions && tool.CompactDescription != "" {
				desc = tool.CompactDescription
			}
			decls = append(decls, geminiFunctionDecl{
				Name:        tool.Name,
				Description: desc,
				Parameters:  tool.InputSchema,
			})
		}
		geminiTools = []geminiTool{{FunctionDeclarations: decls}}
	}

	// Convert messages to Gemini format
	geminiContents := make([]geminiContent, 0, len(messages))

	for _, msg := range messages {
		switch content := msg.Content.(type) {
		case string:
			role := msg.Role
			if role == "assistant" {
				role = "model"
			}
			geminiContents = append(geminiContents, geminiContent{
				Role:  role,
				Parts: []geminiPart{{Text: content}},
			})
		case []ToolResult:
			// Handle []ToolResult directly
			parts := make([]geminiPart, 0, len(content))
			for _, tr := range content {
				contentStr := extractTextFromContent(tr.Content)
				if contentStr == "" {
					contentStr = "{}"
				}
				parts = append(parts, geminiPart{
					FunctionResponse: &geminiFunctionResponse{
						Name: tr.ToolUseID, // Use the tool name stored in ToolUseID
						Response: map[string]interface{}{
							"result": contentStr,
						},
					},
				})
			}
			geminiContents = append(geminiContents, geminiContent{
				Role:  "user",
				Parts: parts,
			})
		case []interface{}:
			// Handle complex content (text, tool use, and tool results)
			var modelParts []geminiPart
			var userParts []geminiPart

			for _, item := range content {
				switch v := item.(type) {
				case TextContent:
					modelParts = append(modelParts, geminiPart{Text: v.Text})
				case ToolUse:
					modelParts = append(modelParts, geminiPart{
						FunctionCall: &geminiFunctionCall{
							Name: v.Name,
							Args: v.Input,
						},
					})
				case ToolResult:
					contentStr := extractTextFromContent(v.Content)
					if contentStr == "" {
						contentStr = "{}"
					}
					userParts = append(userParts, geminiPart{
						FunctionResponse: &geminiFunctionResponse{
							Name: v.ToolUseID,
							Response: map[string]interface{}{
								"result": contentStr,
							},
						},
					})
				default:
					itemMap, ok := item.(map[string]interface{})
					if !ok {
						continue
					}
					itemType, ok := itemMap["type"].(string)
					if !ok {
						continue
					}
					switch itemType {
					case "text":
						if text, ok := itemMap["text"].(string); ok {
							modelParts = append(modelParts, geminiPart{Text: text})
						}
					case "tool_use":
						name, ok1 := itemMap["name"].(string)
						input, ok2 := itemMap["input"].(map[string]interface{})
						if ok1 && ok2 {
							modelParts = append(modelParts, geminiPart{
								FunctionCall: &geminiFunctionCall{
									Name: name,
									Args: input,
								},
							})
						}
					case "tool_result":
						toolUseID, ok := itemMap["tool_use_id"].(string)
						if !ok {
							continue
						}
						resultContent := itemMap["content"]
						contentStr := extractTextFromContent(resultContent)
						if contentStr == "" {
							contentStr = "{}"
						}
						userParts = append(userParts, geminiPart{
							FunctionResponse: &geminiFunctionResponse{
								Name: toolUseID,
								Response: map[string]interface{}{
									"result": contentStr,
								},
							},
						})
					}
				}
			}

			// Add model content if we have model parts
			if len(modelParts) > 0 {
				geminiContents = append(geminiContents, geminiContent{
					Role:  "model",
					Parts: modelParts,
				})
			}
			// Add user content (function responses) if we have them
			if len(userParts) > 0 {
				geminiContents = append(geminiContents, geminiContent{
					Role:  "user",
					Parts: userParts,
				})
			}
		}
	}

	// Build system instruction; use custom prompt if provided, otherwise default
	activePrompt := systemPrompt
	if customSystemPrompt != "" {
		activePrompt = customSystemPrompt
	}
	systemInstruction := &geminiContent{
		Parts: []geminiPart{{Text: activePrompt}},
	}

	// Build generation config
	genConfig := map[string]interface{}{
		"maxOutputTokens": c.maxTokens,
		"temperature":     c.temperature,
	}

	req := geminiRequest{
		Contents:          geminiContents,
		Tools:             geminiTools,
		SystemInstruction: systemInstruction,
		GenerationConfig:  genConfig,
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return LLMResponse{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	embedding.LogLLMRequestTrace("gemini", c.model, operation, string(reqData))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqData))
	if err != nil {
		return LLMResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		embedding.LogConnectionError("gemini", url, err)
		duration := time.Since(startTime)
		embedding.LogLLMCall("gemini", c.model, operation, 0, 0, duration, err)
		return LLMResponse{}, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		duration := time.Since(startTime)
		readErr := fmt.Errorf("failed to read response body: %w", err)
		embedding.LogLLMCall("gemini", c.model, operation, 0, 0, duration, readErr)
		return LLMResponse{}, readErr
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == 429 {
			embedding.LogRateLimitError("gemini", c.model, resp.StatusCode, string(body))
		}

		userFriendlyMsg := extractErrorMessage(resp.StatusCode, body, "Gemini API error", extractGeminiError)

		duration := time.Since(startTime)
		apiErr := fmt.Errorf("%s", userFriendlyMsg)
		embedding.LogLLMCall("gemini", c.model, operation, 0, 0, duration, apiErr)
		return LLMResponse{}, apiErr
	}

	var geminiResp geminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		duration := time.Since(startTime)
		embedding.LogLLMCall("gemini", c.model, operation, 0, 0, duration, err)
		return LLMResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(geminiResp.Candidates) == 0 {
		duration := time.Since(startTime)
		err := fmt.Errorf("no candidates in response")
		embedding.LogLLMCall("gemini", c.model, operation, 0, 0, duration, err)
		return LLMResponse{}, err
	}

	candidate := geminiResp.Candidates[0]
	duration := time.Since(startTime)

	// Convert response content to typed structs
	responseContent := make([]interface{}, 0, len(candidate.Content.Parts))
	hasToolCalls := false

	for i, part := range candidate.Content.Parts {
		if part.FunctionCall != nil {
			hasToolCalls = true
			responseContent = append(responseContent, ToolUse{
				Type:  "tool_use",
				ID:    fmt.Sprintf("gemini-tool-%d", i),
				Name:  part.FunctionCall.Name,
				Input: part.FunctionCall.Args,
			})
		} else if part.Text != "" {
			responseContent = append(responseContent, TextContent{
				Type: "text",
				Text: part.Text,
			})
		}
	}

	stopReason := "end_turn"
	if hasToolCalls {
		stopReason = "tool_use"
	}

	embedding.LogLLMResponseTrace("gemini", c.model, operation, resp.StatusCode, stopReason)
	embedding.LogLLMCall("gemini", c.model, operation,
		geminiResp.UsageMetadata.PromptTokenCount,
		geminiResp.UsageMetadata.CandidatesTokenCount,
		duration, nil)

	// Build token usage for debug
	var tokenUsage *TokenUsage
	if c.debug {
		tokenUsage = &TokenUsage{
			Provider:         "gemini",
			PromptTokens:     geminiResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      geminiResp.UsageMetadata.TotalTokenCount,
		}

		logTokenUsage("Gemini",
			geminiResp.UsageMetadata.PromptTokenCount,
			geminiResp.UsageMetadata.CandidatesTokenCount,
			geminiResp.UsageMetadata.TotalTokenCount,
			0, 0, 0)
	}

	return LLMResponse{
		Content:    responseContent,
		StopReason: stopReason,
		TokenUsage: tokenUsage,
	}, nil
}

// ListModels returns available Gemini models that support content generation
func (c *geminiClient) ListModels(ctx context.Context) ([]string, error) {
	url := fmt.Sprintf("%s/v1beta/models?key=%s", c.baseURL, c.apiKey)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body) //nolint:errcheck // Error response body read is best effort
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	var response struct {
		Models []struct {
			Name                       string   `json:"name"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]string, 0, len(response.Models))
	for _, model := range response.Models {
		// Only include models that support content generation
		supportsGenerate := false
		for _, method := range model.SupportedGenerationMethods {
			if method == "generateContent" {
				supportsGenerate = true
				break
			}
		}
		if !supportsGenerate {
			continue
		}

		// Strip the "models/" prefix from the name
		name := strings.TrimPrefix(model.Name, "models/")
		models = append(models, name)
	}

	return models, nil
}
