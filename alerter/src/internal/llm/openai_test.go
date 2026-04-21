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

func TestNewOpenAIReasoning_Defaults(t *testing.T) {
	p := NewOpenAIReasoning("", "", "")
	if p.model != openaiDefaultReasoningModel {
		t.Errorf("model = %q, want %q", p.model, openaiDefaultReasoningModel)
	}
	if p.baseURL != openaiBaseURL {
		t.Errorf("baseURL = %q, want %q", p.baseURL, openaiBaseURL)
	}
	if p.client == nil {
		t.Fatal("client is nil")
	}
}

func TestNewOpenAIReasoning_Overrides(t *testing.T) {
	p := NewOpenAIReasoning("sk-xxx", "gpt-4o", "http://local")
	if p.apiKey != "sk-xxx" {
		t.Errorf("apiKey = %q, want sk-xxx", p.apiKey)
	}
	if p.model != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o", p.model)
	}
	if p.baseURL != "http://local" {
		t.Errorf("baseURL = %q, want http://local", p.baseURL)
	}
	if got := p.ModelName(); got != "gpt-4o" {
		t.Errorf("ModelName = %q, want gpt-4o", got)
	}
}

func TestOpenAIReasoning_Classify_Success(t *testing.T) {
	var gotAuth, gotCT string
	var gotBody openaiChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode: %v", err)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("path = %q, want .../chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		writeOrFail(t, w, `{"choices":[{"message":{"role":"assistant","content":"alert"}}]}`)
	}))
	defer srv.Close()

	p := NewOpenAIReasoning("sk-test", "", srv.URL)
	out, err := p.Classify(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if out != "alert" {
		t.Errorf("response = %q, want alert", out)
	}
	if gotAuth != "Bearer sk-test" {
		t.Errorf("Authorization = %q, want Bearer sk-test", gotAuth)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
	if gotBody.Model != openaiDefaultReasoningModel {
		t.Errorf("model = %q, want %q", gotBody.Model, openaiDefaultReasoningModel)
	}
	if len(gotBody.Messages) != 2 ||
		gotBody.Messages[0].Role != "system" ||
		gotBody.Messages[1].Role != "user" ||
		gotBody.Messages[1].Content != "test prompt" {
		t.Errorf("unexpected messages: %+v", gotBody.Messages)
	}
	if gotBody.MaxTokens != 500 {
		t.Errorf("max_tokens = %d, want 500", gotBody.MaxTokens)
	}
}

func TestOpenAIReasoning_Classify_NoAuthHeaderWhenKeyEmpty(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeOrFail(t, w, `{"choices":[{"message":{"content":"suppress"}}]}`)
	}))
	defer srv.Close()

	p := NewOpenAIReasoning("", "", srv.URL)
	if _, err := p.Classify(context.Background(), "p"); err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization sent as %q, want empty", gotAuth)
	}
}

func TestOpenAIReasoning_Classify_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeOrFail(t, w, `{"error":"bad"}`)
	}))
	defer srv.Close()

	p := NewOpenAIReasoning("k", "", srv.URL)
	_, err := p.Classify(context.Background(), "p")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "status 400") {
		t.Errorf("err = %v, want contains status 400", err)
	}
}

func TestOpenAIReasoning_Classify_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOrFail(t, w, `not json`)
	}))
	defer srv.Close()

	p := NewOpenAIReasoning("k", "", srv.URL)
	_, err := p.Classify(context.Background(), "p")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "parse response") {
		t.Errorf("err = %v, want parse error", err)
	}
}

func TestOpenAIReasoning_Classify_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOrFail(t, w, `{"choices":[]}`)
	}))
	defer srv.Close()

	p := NewOpenAIReasoning("k", "", srv.URL)
	_, err := p.Classify(context.Background(), "p")
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("err = %v, want ErrInvalidResponse", err)
	}
}

func TestOpenAIReasoning_Classify_InvalidBaseURL(t *testing.T) {
	p := NewOpenAIReasoning("k", "", "://bad")
	_, err := p.Classify(context.Background(), "p")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOpenAIReasoning_Classify_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block long enough for cancellation to fire.
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := NewOpenAIReasoning("k", "", srv.URL)
	_, err := p.Classify(ctx, "p")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOpenAIReasoning_Classify_ResponseBodyReadLimit(t *testing.T) {
	// Sanity: make sure a normal-sized body is read in full.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// return valid minimal chat response
		writeOrFail(t, w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	p := NewOpenAIReasoning("k", "m", srv.URL)
	out, err := p.Classify(context.Background(), "p")
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if out != "ok" {
		t.Errorf("out = %q, want ok", out)
	}
}
