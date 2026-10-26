/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package redact_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/redact"
)

func TestProviderLogError_KeepsEndpoint(t *testing.T) {
	got := redact.ProviderLogError("dial tcp 192.0.2.10:11434: connection refused; key=abc123\n", []string{"s3cret"})
	if !strings.Contains(got, "192.0.2.10:11434") || strings.Contains(got, "abc123") || strings.Contains(got, "\n") {
		t.Errorf("got %q", got)
	}
}

func TestBaseURLSecrets(t *testing.T) {
	got := redact.BaseURLSecrets("https://svc-user:p%40ss-word@llm.example/v1?api=tok-9999&x=1")
	for _, want := range []string{"svc-user:p%40ss-word", "svc-user", "p@ss-word", "p%40ss-word", "tok-9999"} {
		found := false
		for _, s := range got {
			if s == want {
				found = true
			}
		}
		if !found {
			t.Errorf("secrets %q lack %q", got, want)
		}
	}
	for i, s := range got {
		if len(s) < 4 {
			t.Errorf("secret %q is shorter than the minimum", s)
		}
		if i > 0 && len(got[i-1]) < len(s) {
			t.Errorf("secrets not longest first: %q", got)
		}
	}
	if redact.BaseURLSecrets("http://[::1") != nil {
		t.Error("unparsable URL yielded secrets")
	}
	if len(redact.BaseURLSecrets("http://localhost:11434")) != 0 {
		t.Error("plain URL yielded secrets")
	}
	if s := redact.BaseURLSecrets("http://onlyuser@h.example"); len(s) != 1 || s[0] != "onlyuser" {
		t.Errorf("user without password: %q", s)
	}
}

// TestBaseURLSecrets_MalformedQuery covers query pairs that url.Query
// drops: one split by ';', one with a bad escape, and a bare value.
func TestBaseURLSecrets_MalformedQuery(t *testing.T) {
	got := redact.BaseURLSecrets("https://llm.example/v1?token=abcd1234;debug=1&k=ef%zz5678&bare-value-90")
	for _, want := range []string{"abcd1234", "ef%zz5678", "bare-value-90"} {
		if !slices.Contains(got, want) {
			t.Errorf("secrets %q lack %q", got, want)
		}
	}
	msg := redact.ProviderLogError("upstream rejected abcd1234 and ef%zz5678", got)
	if strings.Contains(msg, "abcd1234") || strings.Contains(msg, "ef%zz5678") {
		t.Errorf("secret left in %q", msg)
	}
}

func TestProviderLogError_RemovesCredentials(t *testing.T) {
	tests := []struct {
		name, in, leak string
	}{
		{"configured secret", "call failed with s3cret-value inside", "s3cret-value"},
		{"bearer", "header Authorization: Bearer abcdef123456", "abcdef123456"},
		{"url query", "POST https://llm.example/v1/chat?key=qwerty99 failed", "qwerty99"},
		{"key parameter", `body {"api_key": "zxcv-5678"}`, "zxcv-5678"},
		{"openai key", "Incorrect API key provided: sk-proj1234567890abcdef", "sk-proj1234567890abcdef"},
		{"gemini key", "bad AIzaSyA0123456789abcdefghijklmn", "AIzaSyA0123456789abcdefghijklmn"},
		{"userinfo", "dial https://user:pw99@llm.example/v1", "user:pw99"},
		{"userinfo with @ in password", "dial https://user:a@longsecret@llm.example/v1", "longsecret"},
		{"userinfo with # in password", "dial https://user:hunter2#x@llm.example/v1/chat", "hunter2"},
		{"userinfo with ? in password", "dial https://user:hunter2?x@llm.example/v1/chat", "hunter2"},
		{"@ in query with no path", "dial https://llm.example?key=abc@leakme failed", "leakme"},
		{"@ then / in query with no path", "dial https://llm.example?token=abc@def/remaining-secret failed", "remaining-secret"},
		{"@ in fragment with no path", "dial https://llm.example#abc@leakme failed", "leakme"},
		{"userinfo and @ in query", "dial https://user:pw99@llm.example/v1?key=abc@leakme", "leakme"},
		{"userinfo and @ in query, no path", "dial https://user:pw99@llm.example?key=abc@leakme", "pw99"},
		{"format character", "bad\u202eevil", "\u202e"},
		{"line separator", "one\u2028two", "\u2028"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redact.ProviderLogError(tt.in, []string{"", "s3cret-value"})
			if strings.Contains(got, tt.leak) {
				t.Errorf("ProviderLogError(%q) = %q, still holds %q", tt.in, got, tt.leak)
			}
		})
	}
	if got := redact.ProviderLogError("bad \xff byte", nil); got != "bad \uFFFD byte" {
		t.Errorf("invalid UTF-8 not replaced: %q", got)
	}
}
