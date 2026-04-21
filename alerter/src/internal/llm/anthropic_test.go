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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewAnthropicReasoning_Defaults(t *testing.T) {
	p := NewAnthropicReasoning("", "", "")
	if p.model != anthropicDefaultModel {
		t.Errorf("model = %q, want %q", p.model, anthropicDefaultModel)
	}
	if p.baseURL != anthropicBaseURL {
		t.Errorf("baseURL = %q, want %q", p.baseURL, anthropicBaseURL)
	}
}

func TestNewAnthropicReasoning_Overrides(t *testing.T) {
	p := NewAnthropicReasoning("k", "claude-x", "http://local")
	if p.apiKey != "k" || p.model != "claude-x" || p.baseURL != "http://local" {
		t.Errorf("unexpected fields: %+v", p)
	}
	if got := p.ModelName(); got != "claude-x" {
		t.Errorf("ModelName = %q, want claude-x", got)
	}
}

func TestAnthropicReasoning_Classify_Success(t *testing.T) {
	var gotKey, gotVer string
	var gotBody anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotVer = r.Header.Get("anthropic-version")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode: %v", err)
		}
		if !strings.HasSuffix(r.URL.Path, "/messages") {
			t.Errorf("path = %q, want .../messages", r.URL.Path)
		}
		writeOrFail(t, w, `{"content":[{"type":"text","text":"alert"}],"stop_reason":"end_turn"}`)
	}))
	defer srv.Close()

	p := NewAnthropicReasoning("sk-ant", "", srv.URL)
	out, err := p.Classify(context.Background(), "prompt-x")
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if out != "alert" {
		t.Errorf("out = %q, want alert", out)
	}
	if gotKey != "sk-ant" {
		t.Errorf("x-api-key = %q, want sk-ant", gotKey)
	}
	if gotVer != anthropicAPIVersion {
		t.Errorf("anthropic-version = %q, want %q", gotVer, anthropicAPIVersion)
	}
	if gotBody.System != classificationSystemPrompt {
		t.Errorf("system prompt not forwarded")
	}
	if gotBody.MaxTokens != 500 {
		t.Errorf("max_tokens = %d, want 500", gotBody.MaxTokens)
	}
	if len(gotBody.Messages) != 1 || gotBody.Messages[0].Content != "prompt-x" {
		t.Errorf("messages = %+v", gotBody.Messages)
	}
}

func TestAnthropicReasoning_Classify_SkipsNonTextBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOrFail(t, w, `{"content":[{"type":"tool_use","text":""},{"type":"text","text":"suppress"}]}`)
	}))
	defer srv.Close()

	p := NewAnthropicReasoning("k", "m", srv.URL)
	out, err := p.Classify(context.Background(), "p")
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if out != "suppress" {
		t.Errorf("out = %q, want suppress", out)
	}
}

func TestAnthropicReasoning_Classify_EmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOrFail(t, w, `{"content":[]}`)
	}))
	defer srv.Close()

	p := NewAnthropicReasoning("k", "m", srv.URL)
	_, err := p.Classify(context.Background(), "p")
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("err = %v, want ErrInvalidResponse", err)
	}
}

func TestAnthropicReasoning_Classify_NoTextBlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOrFail(t, w, `{"content":[{"type":"tool_use","text":""}]}`)
	}))
	defer srv.Close()

	p := NewAnthropicReasoning("k", "m", srv.URL)
	_, err := p.Classify(context.Background(), "p")
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("err = %v, want ErrInvalidResponse", err)
	}
}

func TestAnthropicReasoning_Classify_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		writeOrFail(t, w, `{"error":"nope"}`)
	}))
	defer srv.Close()

	p := NewAnthropicReasoning("k", "m", srv.URL)
	_, err := p.Classify(context.Background(), "p")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Errorf("err = %v, want status 401", err)
	}
}

func TestAnthropicReasoning_Classify_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOrFail(t, w, `not json`)
	}))
	defer srv.Close()

	p := NewAnthropicReasoning("k", "m", srv.URL)
	_, err := p.Classify(context.Background(), "p")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAnthropicReasoning_Classify_InvalidBaseURL(t *testing.T) {
	p := NewAnthropicReasoning("k", "m", "://bad")
	_, err := p.Classify(context.Background(), "p")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAnthropicReasoning_Classify_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := NewAnthropicReasoning("k", "m", srv.URL)
	_, err := p.Classify(ctx, "p")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
