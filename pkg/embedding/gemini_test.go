/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewGeminiProvider(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		provider, err := NewGeminiProvider("AIza-test-key-12345678", "text-embedding-004", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if provider == nil {
			t.Fatal("expected non-nil provider")
		}
	})

	t.Run("empty API key", func(t *testing.T) {
		_, err := NewGeminiProvider("", "text-embedding-004", "")
		if err == nil {
			t.Fatal("expected error for empty API key")
		}
		if err.Error() != "Gemini API key cannot be empty" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("default model", func(t *testing.T) {
		provider, err := NewGeminiProvider("AIza-test-key-12345678", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if provider.ModelName() != "text-embedding-004" {
			t.Errorf("expected default model 'text-embedding-004', got %q", provider.ModelName())
		}
	})

	t.Run("custom model", func(t *testing.T) {
		provider, err := NewGeminiProvider("AIza-test-key-12345678", "embedding-001", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if provider.ModelName() != "embedding-001" {
			t.Errorf("expected model 'embedding-001', got %q", provider.ModelName())
		}
	})

	t.Run("unsupported model", func(t *testing.T) {
		_, err := NewGeminiProvider("AIza-test-key-12345678", "unsupported-model", "")
		if err == nil {
			t.Fatal("expected error for unsupported model")
		}
	})

	t.Run("custom base URL", func(t *testing.T) {
		provider, err := NewGeminiProvider("AIza-test-key-12345678", "text-embedding-004", "https://custom.example.com")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if provider.baseURL != "https://custom.example.com" {
			t.Errorf("expected custom base URL, got %q", provider.baseURL)
		}
	})
}

func TestGeminiProvider_Dimensions(t *testing.T) {
	tests := []struct {
		model      string
		dimensions int
	}{
		{"text-embedding-004", 768},
		{"embedding-001", 768},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			provider, err := NewGeminiProvider("AIza-test-key", tt.model, "")
			if err != nil {
				t.Fatalf("failed to create provider: %v", err)
			}
			if provider.Dimensions() != tt.dimensions {
				t.Errorf("expected %d dimensions, got %d", tt.dimensions, provider.Dimensions())
			}
		})
	}
}

func TestGeminiProvider_ModelName(t *testing.T) {
	provider, err := NewGeminiProvider("AIza-test-key-12345678", "text-embedding-004", "")
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if provider.ModelName() != "text-embedding-004" {
		t.Errorf("expected model 'text-embedding-004', got %q", provider.ModelName())
	}
}

func TestGeminiProvider_ProviderName(t *testing.T) {
	provider, err := NewGeminiProvider("AIza-test-key-12345678", "text-embedding-004", "")
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if provider.ProviderName() != "gemini" {
		t.Errorf("expected provider 'gemini', got %q", provider.ProviderName())
	}
}

func TestGeminiProvider_Embed_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Verify the API key is in the query parameter
		if r.URL.Query().Get("key") != "AIza-test-key-12345678" {
			t.Errorf("missing or invalid API key in query parameter")
		}

		// Verify request body
		var reqBody geminiEmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("failed to decode request body: %v", err)
		}
		if reqBody.Model != "models/text-embedding-004" {
			t.Errorf("expected model 'models/text-embedding-004', got %q", reqBody.Model)
		}

		// Return mock embedding response
		response := geminiEmbeddingResponse{}
		response.Embedding.Values = make([]float64, 768)
		for i := range response.Embedding.Values {
			response.Embedding.Values[i] = 0.01 * float64(i)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()

	provider := &GeminiProvider{
		apiKey:  "AIza-test-key-12345678",
		model:   "text-embedding-004",
		baseURL: server.URL,
		client:  server.Client(),
	}

	embedding, err := provider.Embed(context.Background(), "test text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(embedding) != 768 {
		t.Errorf("expected 768 dimensions, got %d", len(embedding))
	}
}

func TestGeminiProvider_Embed_EmptyText(t *testing.T) {
	provider, err := NewGeminiProvider("AIza-test-key-12345678", "text-embedding-004", "")
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	_, err = provider.Embed(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty text")
	}
	if err.Error() != "text cannot be empty" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGeminiProvider_Embed_APIError(t *testing.T) {
	// Create mock server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		if _, err := w.Write([]byte(`{"error": {"message": "Invalid API key"}}`)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()

	provider := &GeminiProvider{
		apiKey:  "invalid-key",
		model:   "text-embedding-004",
		baseURL: server.URL,
		client:  server.Client(),
	}

	_, err := provider.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for API error response")
	}
}

func TestGeminiProvider_Embed_RateLimit(t *testing.T) {
	// Create mock server that returns rate limit error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		if _, err := w.Write([]byte(`{"error": {"message": "Rate limit exceeded"}}`)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()

	provider := &GeminiProvider{
		apiKey:  "AIza-test-key-12345678",
		model:   "text-embedding-004",
		baseURL: server.URL,
		client:  server.Client(),
	}

	_, err := provider.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for rate limit")
	}
}

func TestGeminiProvider_Embed_EmptyResponse(t *testing.T) {
	// Create mock server that returns empty embedding
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := geminiEmbeddingResponse{}
		// Leave Embedding.Values empty

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()

	provider := &GeminiProvider{
		apiKey:  "AIza-test-key-12345678",
		model:   "text-embedding-004",
		baseURL: server.URL,
		client:  server.Client(),
	}

	_, err := provider.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for empty embedding")
	}
	if err.Error() != "received empty embedding from API" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGeminiProvider_Embed_MalformedJSON(t *testing.T) {
	// Create mock server that returns malformed JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{invalid json`)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()

	provider := &GeminiProvider{
		apiKey:  "AIza-test-key-12345678",
		model:   "text-embedding-004",
		baseURL: server.URL,
		client:  server.Client(),
	}

	_, err := provider.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}
