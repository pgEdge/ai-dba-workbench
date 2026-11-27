/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/evalharness/report"
)

const repoIncidents = "../../../../evaluation/incidents"

// unreachableDSN refuses connections at once, so every run fails while
// staging without needing a database or a model.
const unreachableDSN = "postgres://postgres@127.0.0.1:1/postgres?connect_timeout=2"

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func fullEnv() func(string) string {
	return env(map[string]string{envToken: "tok", envAdminDSN: unreachableDSN, envDatastoreDSN: unreachableDSN})
}

func toolServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/mcp/tools" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "nope", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"tools": []}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func promptFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "prompt.txt")
	if err := os.WriteFile(p, []byte("You are Ellie."), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunCommand(t *testing.T) {
	t.Parallel()
	srv := toolServer(t)
	out := filepath.Join(t.TempDir(), "results")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"run", "-server-url", srv.URL, "-incidents", repoIncidents,
		"-only", "lock-pileup,table-bloat", "-target", "anthropic", "-target", "openai:gpt-x", "-runs", "1",
		"-system-prompt", promptFile(t), "-out", out}, &stdout, &stderr, fullEnv())
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "[4/4] table-bloat on openai:gpt-x, run 1: FAILED") {
		t.Errorf("stdout:\n%s", stdout.String())
	}
	md, err := os.ReadFile(filepath.Join(out, "report.md"))
	if err != nil || !strings.Contains(string(md), "staging: connect with the admin DSN") {
		t.Errorf("report.md (%v):\n%s", err, md)
	}

	results := filepath.Join(out, "results.json")
	stdout.Reset()
	if code := run(context.Background(), []string{"report", results}, &stdout, &stderr, nil); code != 0 ||
		!strings.Contains(stdout.String(), "# Ellie evaluation report") {
		t.Errorf("report exit %d:\n%s", code, stdout.String())
	}
	stdout.Reset()
	if code := run(context.Background(), []string{"compare", "before=" + results, results}, &stdout, &stderr, nil); code != 0 ||
		!strings.Contains(stdout.String(), "| before: anthropic |") || !strings.Contains(stdout.String(), results+": openai:gpt-x") {
		t.Errorf("compare exit %d:\n%s", code, stdout.String())
	}
}

func TestRunFailures(t *testing.T) {
	t.Parallel()
	srv := toolServer(t)
	prompt := promptFile(t)
	notDir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notDir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	base := []string{"run", "-server-url", srv.URL, "-incidents", repoIncidents, "-system-prompt", prompt,
		"-only", "lock-pileup", "-runs", "1"}
	for _, tc := range []struct {
		name string
		args []string
		env  func(string) string
		code int
		want string
	}{
		{"no command", nil, nil, 2, "Usage: ai-dba-evaluate <command>"},
		{"unknown command", []string{"frobnicate"}, nil, 2, `unknown command "frobnicate"`},
		{"bad flag", []string{"run", "-nope"}, fullEnv(), 2, "flag provided but not defined"},
		{"run help", []string{"run", "-h"}, fullEnv(), 0, envToken},
		{"extra args", []string{"run", "x"}, fullEnv(), 2, "unexpected arguments"},
		{"missing token", []string{"run"}, env(map[string]string{envAdminDSN: "x", envDatastoreDSN: "x"}), 2, envToken + " is not set"},
		{"bad runs", []string{"run", "-runs", "0"}, fullEnv(), 2, "must be at least 1"},
		{"bad target", append(base, "-target", "acme"), fullEnv(), 2, "provider must be"},
		{"no incidents", append(base, "-incidents", t.TempDir()), fullEnv(), 1, "no incident files"},
		{"unknown incident", append(base, "-only", "nope"), fullEnv(), 2, `unknown incident "nope"`},
		{"missing prompt", append(base, "-system-prompt", "/nonexistent/prompt.txt"), fullEnv(), 1, "read system prompt"},
		{"wrong token", base, env(map[string]string{envToken: "bad", envAdminDSN: "x", envDatastoreDSN: "x"}), 1, "checking the server"},
		{"bad output directory", append(base, "-out", filepath.Join(notDir, "sub")), fullEnv(), 1, "create output directory"},
		{"report usage", []string{"report"}, nil, 2, "expected one results file"},
		{"report bad flag", []string{"report", "-x"}, nil, 2, "flag provided"},
		{"report help", []string{"report", "-h"}, nil, 0, "Usage: ai-dba-evaluate report"},
		{"report missing file", []string{"report", "/nonexistent.json"}, nil, 1, "no such file"},
		{"compare usage", []string{"compare"}, nil, 2, "at least one results file"},
		{"compare bad flag", []string{"compare", "-x"}, nil, 2, "flag provided"},
		{"compare help", []string{"compare", "-h"}, nil, 0, "Usage: ai-dba-evaluate compare"},
		{"compare missing file", []string{"compare", "a=/nonexistent.json"}, nil, 1, "no such file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), tc.args, &stdout, &stderr, tc.env)
			if code != tc.code || !strings.Contains(stderr.String()+stdout.String(), tc.want) {
				t.Errorf("exit %d (want %d), output lacks %q:\n%s%s", code, tc.code, tc.want, stdout.String(), stderr.String())
			}
		})
	}
}

func TestHelp(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	if code := run(context.Background(), []string{"help"}, &stdout, &stdout, nil); code != 0 || !strings.Contains(stdout.String(), "Commands:") {
		t.Errorf("help exit %d:\n%s", code, stdout.String())
	}
}

func resultsFixture() report.Results { return report.Results{} }

func TestWriteResultsErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := writeResults(filepath.Join(dir, "missing"), resultsFixture()); err == nil {
		t.Error("writeResults into a missing directory succeeded")
	}
	if err := os.Mkdir(filepath.Join(dir, "report.md"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := writeResults(dir, resultsFixture()); err == nil || !strings.Contains(err.Error(), "create report") {
		t.Errorf("writeResults over a directory: %v", err)
	}
}

func TestRunStopsEarly(t *testing.T) {
	t.Parallel()
	srv := toolServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	// The canceled context fails the server check before any run.
	code := run(ctx, []string{"run", "-server-url", srv.URL, "-incidents", repoIncidents,
		"-system-prompt", promptFile(t), "-out", out}, &stdout, &stderr, fullEnv())
	if code != 1 || !strings.Contains(stderr.String(), "checking the server") {
		t.Errorf("exit %d:\n%s", code, stderr.String())
	}
}
