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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCustomHeaders_EndToEnd(t *testing.T) {
	receivedHeaders := make(http.Header)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range r.Header {
			receivedHeaders[k] = v
		}
		// Return minimal valid Ollama response
		json.NewEncoder(w).Encode(map[string]any{
			"model":   "test",
			"message": map[string]string{"role": "assistant", "content": "test"},
		})
	}))
	defer server.Close()

	// Initialize with global headers
	InitHTTPClient(map[string]string{
		"X-Global-Header": "global-value",
	}, 0)

	// Create client with provider headers
	client := NewOllamaClient(server.URL, "test", false, false, map[string]string{
		"X-Provider-Header": "provider-value",
	})

	// Make request
	_, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "test"}}, nil, "")
	if err != nil {
		t.Logf("Chat error (expected for mock): %v", err)
	}

	// Verify headers were sent
	if receivedHeaders.Get("X-Global-Header") != "global-value" {
		t.Errorf("expected global header, got %s", receivedHeaders.Get("X-Global-Header"))
	}
	if receivedHeaders.Get("X-Provider-Header") != "provider-value" {
		t.Errorf("expected provider header, got %s", receivedHeaders.Get("X-Provider-Header"))
	}
}
