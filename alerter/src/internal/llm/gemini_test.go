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

func TestNewGeminiReasoning_Defaults(t *testing.T) {
	p := NewGeminiReasoning("", "", "")
	if p.model != geminiDefaultModel {
		t.Errorf("model = %q, want %q", p.model, geminiDefaultModel)
	}
	if p.baseURL != geminiBaseURL {
		t.Errorf("baseURL = %q, want %q", p.baseURL, geminiBaseURL)
	}
}

func TestNewGeminiReasoning_Overrides(t *testing.T) {
	p := NewGeminiReasoning("k", "gemini-foo", "http://local")
	if p.apiKey != "k" || p.model != "gemini-foo" || p.baseURL != "http://local" {
		t.Errorf("unexpected fields: %+v", p)
	}
	if got := p.ModelName(); got != "gemini-foo" {
		t.Errorf("ModelName = %q, want gemini-foo", got)
	}
}

func TestGeminiReasoning_Classify_Success(t *testing.T) {
	var gotKey string
	var gotPath string
	var gotBody geminiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-goog-api-key")
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode: %v", err)
		}
		writeOrFail(t, w, `{"candidates":[{"content":{"parts":[{"text":"alert"}]}}]}`)
	}))
	defer srv.Close()

	p := NewGeminiReasoning("goog-k", "gemini-test", srv.URL)
	out, err := p.Classify(context.Background(), "prompt-z")
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if out != "alert" {
		t.Errorf("out = %q, want alert", out)
	}
	if gotKey != "goog-k" {
		t.Errorf("x-goog-api-key = %q, want goog-k", gotKey)
	}
	if !strings.Contains(gotPath, "/v1beta/models/gemini-test:generateContent") {
		t.Errorf("path = %q, want to contain gemini-test:generateContent", gotPath)
	}
	if len(gotBody.Contents) != 1 || len(gotBody.Contents[0].Parts) != 1 {
		t.Errorf("body contents = %+v", gotBody.Contents)
	}
	if !strings.Contains(gotBody.Contents[0].Parts[0].Text, "prompt-z") {
		t.Errorf("prompt not forwarded: %q", gotBody.Contents[0].Parts[0].Text)
	}
	if !strings.Contains(gotBody.Contents[0].Parts[0].Text, classificationSystemPrompt) {
		t.Errorf("system prompt not prepended")
	}
}

func TestGeminiReasoning_Classify_SkipsEmptyPart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOrFail(t, w, `{"candidates":[{"content":{"parts":[{"text":""},{"text":"suppress"}]}}]}`)
	}))
	defer srv.Close()

	p := NewGeminiReasoning("k", "m", srv.URL)
	out, err := p.Classify(context.Background(), "p")
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if out != "suppress" {
		t.Errorf("out = %q, want suppress", out)
	}
}

func TestGeminiReasoning_Classify_NoCandidates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOrFail(t, w, `{"candidates":[]}`)
	}))
	defer srv.Close()

	p := NewGeminiReasoning("k", "m", srv.URL)
	_, err := p.Classify(context.Background(), "p")
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("err = %v, want ErrInvalidResponse", err)
	}
}

func TestGeminiReasoning_Classify_NoParts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOrFail(t, w, `{"candidates":[{"content":{"parts":[]}}]}`)
	}))
	defer srv.Close()

	p := NewGeminiReasoning("k", "m", srv.URL)
	_, err := p.Classify(context.Background(), "p")
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("err = %v, want ErrInvalidResponse", err)
	}
}

func TestGeminiReasoning_Classify_AllPartsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOrFail(t, w, `{"candidates":[{"content":{"parts":[{"text":""},{"text":""}]}}]}`)
	}))
	defer srv.Close()

	p := NewGeminiReasoning("k", "m", srv.URL)
	_, err := p.Classify(context.Background(), "p")
	if !errors.Is(err, ErrInvalidResponse) {
		t.Errorf("err = %v, want ErrInvalidResponse", err)
	}
}

func TestGeminiReasoning_Classify_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		writeOrFail(t, w, `{"error":"denied"}`)
	}))
	defer srv.Close()

	p := NewGeminiReasoning("k", "m", srv.URL)
	_, err := p.Classify(context.Background(), "p")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "status 403") {
		t.Errorf("err = %v, want status 403", err)
	}
}

func TestGeminiReasoning_Classify_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeOrFail(t, w, `not json`)
	}))
	defer srv.Close()

	p := NewGeminiReasoning("k", "m", srv.URL)
	_, err := p.Classify(context.Background(), "p")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGeminiReasoning_Classify_InvalidBaseURL(t *testing.T) {
	p := NewGeminiReasoning("k", "m", "://bad")
	_, err := p.Classify(context.Background(), "p")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGeminiReasoning_Classify_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := NewGeminiReasoning("k", "m", srv.URL)
	_, err := p.Classify(ctx, "p")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
