/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCORSMiddleware_SetsHeadersAndCallsNext verifies the CORS headers,
// including the Access-Control-Expose-Headers entry that lets a browser
// client read the X-Total-Count pagination total, and confirms the request
// still reaches the wrapped handler.
func TestCORSMiddleware_SetsHeadersAndCallsNext(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Header().Set("X-Total-Count", "42")
		w.WriteHeader(http.StatusOK)
	})

	handler := CORSMiddleware("https://dba.example.com")(next)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/api/v1/metrics/top-queries", nil))

	if !called {
		t.Fatal("wrapped handler was not called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	want := map[string]string{
		"Access-Control-Allow-Origin":      "https://dba.example.com",
		"Access-Control-Allow-Methods":     "GET, POST, PUT, DELETE, OPTIONS",
		"Access-Control-Allow-Headers":     "Content-Type, Authorization",
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Expose-Headers":    "X-Total-Count",
		"X-Total-Count":                    "42",
	}
	for header, value := range want {
		if got := rec.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}
}

// TestCORSMiddleware_PreflightShortCircuits verifies an OPTIONS preflight
// returns 204 without invoking the wrapped handler, whilst still
// advertising the exposed headers.
func TestCORSMiddleware_PreflightShortCircuits(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := CORSMiddleware("https://dba.example.com")(next)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec,
		httptest.NewRequest(http.MethodOptions, "/api/v1/metrics/top-queries",
			nil))

	if called {
		t.Fatal("wrapped handler must not be called for a preflight request")
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Expose-Headers"); got !=
		"X-Total-Count" {
		t.Errorf("Access-Control-Expose-Headers = %q, want \"X-Total-Count\"",
			got)
	}
}
