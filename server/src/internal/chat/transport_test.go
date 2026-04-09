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
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHeaderTransport_InjectsHeaders(t *testing.T) {
	receivedHeaders := make(http.Header)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range r.Header {
			receivedHeaders[k] = v
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	headers := map[string]string{
		"X-Custom-Header": "custom-value",
		"X-Another":       "another-value",
	}
	transport := NewHeaderTransport(headers)
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequest("GET", server.URL, nil)
	_, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if receivedHeaders.Get("X-Custom-Header") != "custom-value" {
		t.Errorf("expected X-Custom-Header=custom-value, got %s", receivedHeaders.Get("X-Custom-Header"))
	}
	if receivedHeaders.Get("X-Another") != "another-value" {
		t.Errorf("expected X-Another=another-value, got %s", receivedHeaders.Get("X-Another"))
	}
}

func TestHeaderTransport_PreservesExistingHeaders(t *testing.T) {
	receivedHeaders := make(http.Header)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range r.Header {
			receivedHeaders[k] = v
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	headers := map[string]string{
		"X-Custom-Header": "transport-value",
	}
	transport := NewHeaderTransport(headers)
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequest("GET", server.URL, nil)
	req.Header.Set("X-Custom-Header", "request-value")
	_, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if receivedHeaders.Get("X-Custom-Header") != "request-value" {
		t.Errorf("expected request header to take precedence, got %s", receivedHeaders.Get("X-Custom-Header"))
	}
}

func TestHeaderTransport_NilHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := NewHeaderTransport(nil)
	client := &http.Client{Transport: transport}

	req, _ := http.NewRequest("GET", server.URL, nil)
	_, err := client.Do(req)
	if err != nil {
		t.Fatalf("request should succeed with nil headers: %v", err)
	}
}
