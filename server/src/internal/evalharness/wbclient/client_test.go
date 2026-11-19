/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package wbclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDoSendsJSONAndToken(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		var in map[string]string
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Errorf("decode: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"echo": in["say"]})
	}))
	defer srv.Close()

	c := New(srv.URL+"/", "tok", time.Second)
	var out map[string]string
	if err := c.Do(context.Background(), http.MethodPost, "/x", map[string]string{"say": "hi"}, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if out["echo"] != "hi" {
		t.Errorf("echo = %q", out["echo"])
	}
}

func TestDoWithoutBodies(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected headers %v", r.Header)
		}
		_, _ = w.Write([]byte("ignored"))
	}))
	defer srv.Close()

	c := New(srv.URL, "", time.Second)
	if err := c.Do(context.Background(), http.MethodDelete, "/x", nil, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
}

func TestDoErrors(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fail":
			http.Error(w, "nope", http.StatusForbidden)
		case "/bad":
			_, _ = w.Write([]byte("{"))
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "", time.Second)
	ctx := context.Background()

	err := c.Do(ctx, http.MethodGet, "/fail", nil, nil)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusForbidden {
		t.Fatalf("expected HTTPError 403, got %v", err)
	}
	if !strings.Contains(he.Error(), "HTTP 403: nope") {
		t.Errorf("error text %q", he.Error())
	}

	var out map[string]any
	if err := c.Do(ctx, http.MethodGet, "/bad", nil, &out); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Errorf("expected decode error, got %v", err)
	}
	if err := c.Do(ctx, http.MethodGet, "/x", make(chan int), nil); err == nil || !strings.Contains(err.Error(), "encode") {
		t.Errorf("expected encode error, got %v", err)
	}
	if err := c.Do(ctx, "BAD METHOD", "/x", nil, nil); err == nil || !strings.Contains(err.Error(), "build") {
		t.Errorf("expected build error, got %v", err)
	}
	dead := New("http://127.0.0.1:1", "", time.Second)
	if err := dead.Do(ctx, http.MethodGet, "/x", nil, nil); err == nil {
		t.Error("expected transport error")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("connection reset") }
func (failingBody) Close() error             { return nil }

func TestDoBodyReadErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		want   string
	}{
		{"error status", http.StatusBadGateway, "HTTP 502: (reading the body failed: connection reset)"},
		{"discarded body", http.StatusOK, "read GET /x response: connection reset"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := New("http://example.invalid", "", time.Second)
			c.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(failingBody{}), Header: http.Header{}}, nil
			})
			if err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}
}
