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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewOllamaReasoning_Defaults(t *testing.T) {
	p := NewOllamaReasoning("", "")
	if p.baseURL != ollamaDefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", p.baseURL, ollamaDefaultBaseURL)
	}
	if p.model != ollamaDefaultReasoningModel {
		t.Errorf("model = %q, want %q", p.model, ollamaDefaultReasoningModel)
	}
	if p.client == nil {
		t.Fatal("client nil")
	}
}

func TestNewOllamaReasoning_Overrides(t *testing.T) {
	p := NewOllamaReasoning("http://x", "llama-custom")
	if p.baseURL != "http://x" || p.model != "llama-custom" {
		t.Errorf("fields = %+v", p)
	}
	if got := p.ModelName(); got != "llama-custom" {
		t.Errorf("ModelName = %q, want llama-custom", got)
	}
}

func TestOllamaReasoning_Classify_Success(t *testing.T) {
	var gotBody ollamaGenerateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/generate") {
			t.Errorf("path = %q, want .../api/generate", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode: %v", err)
		}
		writeOrFail(t, w, `{"response":"alert","done":true}`)
	}))
	defer srv.Close()

	p := NewOllamaReasoning(srv.URL, "my-model")
	out, err := p.Classify(context.Background(), "user-prompt")
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if out != "alert" {
		t.Errorf("out = %q, want alert", out)
	}
	if gotBody.Model != "my-model" {
		t.Errorf("model = %q, want my-model", gotBody.Model)
	}
	if gotBody.Stream != false {
		t.Error("stream should be false")
	}
	if !strings.Contains(gotBody.Prompt, classificationSystemPrompt) {
		t.Error("system prompt not prepended")
	}
	if !strings.Contains(gotBody.Prompt, "user-prompt") {
		t.Error("user prompt not included")
	}
	if gotBody.Options["temperature"] == nil {
		t.Error("options.temperature missing")
	}
	if gotBody.Options["num_predict"] == nil {
		t.Error("options.num_predict missing")
	}
}

func TestOllamaReasoning_Classify_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		writeOrFail(t, w, `model not found`)
	}))
	defer srv.Close()

	p := NewOllamaReasoning(srv.URL, "m")
	_, err := p.Classify(context.Background(), "p")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "status 404") {
		t.Errorf("err = %v, want status 404", err)
	}
}

func TestOllamaReasoning_Classify_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOrFail(t, w, `not-json`)
	}))
	defer srv.Close()

	p := NewOllamaReasoning(srv.URL, "m")
	_, err := p.Classify(context.Background(), "p")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOllamaReasoning_Classify_InvalidBaseURL(t *testing.T) {
	p := NewOllamaReasoning("://bad", "m")
	_, err := p.Classify(context.Background(), "p")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOllamaReasoning_Classify_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := NewOllamaReasoning(srv.URL, "m")
	_, err := p.Classify(ctx, "p")
	// ollama.go translates transport-level cancellation to ErrContextCanceled
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOllamaReasoning_Classify_ConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	p := NewOllamaReasoning(url, "m")
	_, err := p.Classify(context.Background(), "p")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
