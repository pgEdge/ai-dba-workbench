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
	"context"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// These tests cover the Server's start-up and shut-down path: NewServer
// and the init steps it runs, Run, the SIGHUP reload handler and Close.
// The ones that need a datastore connect only to the loopback Postgres
// named by TEST_AI_WORKBENCH_SERVER, and skip when it is unset.

// testDatabaseConfig turns TEST_AI_WORKBENCH_SERVER into a datastore
// configuration, skipping the test when the variable is unset and
// failing it when the URL names anything but a loopback host, because
// these tests must never reach a shared database.
func testDatabaseConfig(t *testing.T) *config.DatabaseConfig {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping datastore test")
	}

	parsed, err := url.Parse(connStr)
	if err != nil {
		t.Fatalf("parsing TEST_AI_WORKBENCH_SERVER: %v", err)
	}
	host := parsed.Hostname()
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		t.Fatalf("TEST_AI_WORKBENCH_SERVER must name a loopback host, not %q", host)
	}
	port := 5432
	if p := parsed.Port(); p != "" {
		if port, err = strconv.Atoi(p); err != nil {
			t.Fatalf("parsing the port in TEST_AI_WORKBENCH_SERVER: %v", err)
		}
	}
	password, _ := parsed.User.Password()

	return &config.DatabaseConfig{
		Host:         host,
		Port:         port,
		Database:     strings.TrimPrefix(parsed.Path, "/"),
		User:         parsed.User.Username(),
		Password:     password,
		SSLMode:      "disable",
		PoolMaxConns: 2,
	}
}

// newLifecycleConfig returns the defaults LoadConfig produces with the
// datastore and secret file filled in. The LLM provider is cleared so
// that no API key inherited from the environment can switch the AI
// overview generator on and have it call a real provider.
func newLifecycleConfig(t *testing.T, db *config.DatabaseConfig) *config.Config {
	t.Helper()

	cfg, err := config.LoadConfig("", config.CLIFlags{})
	if err != nil {
		t.Fatalf("config.LoadConfig: %v", err)
	}
	cfg.Database = db
	cfg.LLM.Provider = ""
	cfg.HTTP.Address = "127.0.0.1:0"

	secretPath := filepath.Join(t.TempDir(), "server.secret")
	if err := os.WriteFile(secretPath, []byte("lifecycle-test-secret\n"), 0600); err != nil {
		t.Fatalf("writing the secret file: %v", err)
	}
	cfg.SecretFile = secretPath
	return cfg
}

func TestNewServerRunAndClose(t *testing.T) {
	cfg := newLifecycleConfig(t, testDatabaseConfig(t))
	// An unusable listen address makes Run return as soon as it tries
	// to listen, after it has wired the handlers, logged its start-up
	// information and installed its signal handlers.
	cfg.HTTP.Address = "127.0.0.1:-1"

	var server *Server
	out := captureStderr(t, func() {
		var err error
		server, err = NewServer(&ServerConfig{Config: cfg, DataDir: t.TempDir()})
		if err != nil {
			t.Fatalf("NewServer: %v", err)
		}
	})
	for _, want := range []string{
		"Datastore: connected to",
		"per-session connections",
		"Conversation store: PostgreSQL datastore",
		"AI Overview: DISABLED",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("NewServer output is missing %q:\n%s", want, out)
		}
	}
	if server.mcpServer == nil || server.toolProvider == nil || server.convStore == nil {
		t.Fatal("NewServer left the MCP server, tool provider or conversation store unset")
	}

	var runErr error
	captureStderr(t, func() {
		runErr = server.Run(&Flags{}, filepath.Join(t.TempDir(), "absent.yaml"))
	})
	if runErr == nil {
		t.Error("Run returned nil for an address it cannot listen on")
	}

	server.Close()
	if server.ctx.Err() == nil {
		t.Error("Close did not cancel the server's context")
	}
}

func TestNewServerStopsAtTheFirstFailingStep(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, cfg *config.Config, sc *ServerConfig)
		want   string
	}{
		{
			name: "missing TLS certificate",
			mutate: func(t *testing.T, cfg *config.Config, _ *ServerConfig) {
				cfg.HTTP.TLS.Enabled = true
				cfg.HTTP.TLS.CertFile = filepath.Join(t.TempDir(), "absent.pem")
			},
			want: "certificate file not found",
		},
		{
			name: "unreadable secret file",
			mutate: func(t *testing.T, cfg *config.Config, _ *ServerConfig) {
				cfg.SecretFile = filepath.Join(t.TempDir(), "absent.secret")
			},
			want: "failed to read secret file",
		},
		{
			name: "data directory that cannot be created",
			mutate: func(t *testing.T, _ *config.Config, sc *ServerConfig) {
				blocker := filepath.Join(t.TempDir(), "file")
				if err := os.WriteFile(blocker, nil, 0600); err != nil {
					t.Fatalf("writing the blocking file: %v", err)
				}
				sc.DataDir = filepath.Join(blocker, "data")
			},
			want: "failed to create data directory",
		},
		{
			name: "OIDC provider that cannot be discovered",
			mutate: func(_ *testing.T, cfg *config.Config, _ *ServerConfig) {
				cfg.HTTP.Auth.OIDC = config.OIDCConfig{
					Enabled:      config.BoolPtr(true),
					Issuer:       "https://127.0.0.1:1",
					ClientID:     "client",
					ClientSecret: "secret",
				}
			},
			want: "OIDC",
		},
		{
			name: "no datastore configuration",
			mutate: func(_ *testing.T, cfg *config.Config, _ *ServerConfig) {
				cfg.Database = nil
			},
			want: "database configuration is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newLifecycleConfig(t, &config.DatabaseConfig{User: "unused"})
			sc := &ServerConfig{Config: cfg, DataDir: t.TempDir()}
			tt.mutate(t, cfg, sc)

			var err error
			captureStderr(t, func() {
				var server *Server
				server, err = NewServer(sc)
				if server != nil {
					t.Error("NewServer returned a server alongside an error")
				}
			})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("NewServer error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

// TestInitTracingDoesNotFailTheStartUpOnAnUnopenableFile covers the
// warning branch only. tracing.Initialize runs once per process, so a
// test binary gets a single chance to exercise it, and a trace file that
// cannot be opened is the case where initTracing does anything more
// than report what it was given.
func TestInitTracingDoesNotFailTheStartUpOnAnUnopenableFile(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		t.Fatalf("writing the blocking file: %v", err)
	}
	server := &Server{cfg: &config.Config{TraceFile: filepath.Join(blocker, "trace.jsonl")}}

	out := captureStderr(t, func() {
		if err := server.initTracing(); err != nil {
			t.Errorf("initTracing must not fail the start-up: %v", err)
		}
	})
	if !strings.Contains(out, "WARNING: Failed to initialize tracing") {
		t.Errorf("output = %q, want a tracing warning", out)
	}
}

func TestValidateTLS(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "present.pem")
	if err := os.WriteFile(present, []byte("x"), 0600); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}
	absent := filepath.Join(dir, "absent.pem")

	tests := []struct {
		name  string
		tls   config.TLSConfig
		want  string
		valid bool
	}{
		{name: "disabled", tls: config.TLSConfig{CertFile: absent}, valid: true},
		{name: "all present", tls: config.TLSConfig{
			Enabled: true, CertFile: present, KeyFile: present, ChainFile: present,
		}, valid: true},
		{name: "no chain", tls: config.TLSConfig{
			Enabled: true, CertFile: present, KeyFile: present,
		}, valid: true},
		{name: "missing certificate", tls: config.TLSConfig{
			Enabled: true, CertFile: absent, KeyFile: present,
		}, want: "certificate file not found"},
		{name: "missing key", tls: config.TLSConfig{
			Enabled: true, CertFile: present, KeyFile: absent,
		}, want: "key file not found"},
		{name: "missing chain", tls: config.TLSConfig{
			Enabled: true, CertFile: present, KeyFile: present, ChainFile: absent,
		}, want: "chain file not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.HTTP.TLS = tt.tls
			err := (&Server{cfg: cfg}).validateTLS()
			if tt.valid {
				if err != nil {
					t.Errorf("validateTLS: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("validateTLS error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestInitDatastoreRefusesAnUnreachableDatabase(t *testing.T) {
	cfg := &config.Config{Database: &config.DatabaseConfig{
		Host: "127.0.0.1", Port: 1, Database: "absent", User: "absent", SSLMode: "disable",
	}}
	err := (&Server{cfg: cfg}).initDatastore("a-server-secret")
	if err == nil || !strings.Contains(err.Error(), "failed to connect to datastore") {
		t.Errorf("initDatastore error = %v, want a connection failure", err)
	}
}

func TestInitClientManagerReportsAnUnconfiguredDatabase(t *testing.T) {
	server := &Server{cfg: &config.Config{}}
	out := captureStderr(t, func() {
		if err := server.initClientManager(); err != nil {
			t.Errorf("initClientManager: %v", err)
		}
	})
	if server.clientManager == nil {
		t.Error("initClientManager left the client manager unset")
	}
	if !strings.Contains(out, "Database: Not configured") {
		t.Errorf("output = %q, want it to report no database", out)
	}
}

func TestInitConversationStoreNeedsTheAuthStoreAndDatastore(t *testing.T) {
	if err := (&Server{}).initConversationStore(); err != nil {
		t.Errorf("without an auth store the conversation store is skipped, got %v", err)
	}

	store, _ := newWrapperTestStore(t)
	err := (&Server{authStore: store}).initConversationStore()
	if err == nil || !strings.Contains(err.Error(), "datastore required") {
		t.Errorf("initConversationStore error = %v, want it to require a datastore", err)
	}
}

func TestStartTokenCleanupWithoutAnAuthStoreStartsNothing(t *testing.T) {
	server := &Server{cfg: &config.Config{}}
	server.ctx, server.cancel = context.WithCancel(context.Background())
	defer server.cancel()

	// A nil auth store must return before touching it; reaching the
	// store would panic.
	server.startTokenCleanup()
}

func TestHasValidLLMConfig(t *testing.T) {
	tests := []struct {
		name string
		llm  config.LLMConfig
		want bool
	}{
		{name: "anthropic with key", llm: config.LLMConfig{Provider: "anthropic", AnthropicAPIKey: "k"}, want: true},
		{name: "anthropic without key", llm: config.LLMConfig{Provider: "anthropic"}},
		{name: "openai with key", llm: config.LLMConfig{Provider: "openai", OpenAIAPIKey: "k"}, want: true},
		{name: "openai with base URL", llm: config.LLMConfig{Provider: "openai", OpenAIBaseURL: "http://127.0.0.1:1"}, want: true},
		{name: "openai without either", llm: config.LLMConfig{Provider: "openai"}},
		{name: "gemini with key", llm: config.LLMConfig{Provider: "gemini", GeminiAPIKey: "k"}, want: true},
		{name: "gemini without key", llm: config.LLMConfig{Provider: "gemini"}},
		{name: "ollama with URL", llm: config.LLMConfig{Provider: "ollama", OllamaURL: "http://127.0.0.1:1"}, want: true},
		{name: "ollama without URL", llm: config.LLMConfig{Provider: "ollama"}},
		{name: "unknown provider", llm: config.LLMConfig{Provider: "unknown", AnthropicAPIKey: "k"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := &Server{cfg: &config.Config{LLM: tt.llm}}
			if got := server.hasValidLLMConfig(); got != tt.want {
				t.Errorf("hasValidLLMConfig() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestBuildOverviewGeneratorWiresTheHub checks the generator wiring
// without starting it. Start runs a goroutine that writes to os.Stderr
// and cannot be waited for, which would race with every later test that
// captures os.Stderr.
func TestBuildOverviewGeneratorWiresTheHub(t *testing.T) {
	cfg := &config.Config{}
	cfg.LLM.Provider = "ollama"
	cfg.LLM.OllamaURL = "http://127.0.0.1:1"
	server := &Server{cfg: cfg}
	server.ctx, server.cancel = context.WithCancel(context.Background())

	server.buildOverviewGenerator()
	if server.overviewGen == nil || server.overviewHub == nil {
		t.Fatal("buildOverviewGenerator left the generator or its hub unset")
	}
	if server.aiEnabled {
		t.Error("building the generator must not mark the overview enabled")
	}

	// Close stops a generator that was never started without blocking.
	server.Close()
}

func TestLogStartupInfoReportsTLSAndDebug(t *testing.T) {
	cfg := &config.Config{}
	cfg.HTTP.Address = "127.0.0.1:8443"
	cfg.HTTP.TLS = config.TLSConfig{
		Enabled:   true,
		CertFile:  "/etc/example/cert.pem",
		KeyFile:   "/etc/example/key.pem",
		ChainFile: "/etc/example/chain.pem",
	}
	out := captureStderr(t, (&Server{cfg: cfg, debug: true}).logStartupInfo)

	for _, want := range []string{
		"Starting MCP server in HTTPS mode on 127.0.0.1:8443",
		"Certificate: /etc/example/cert.pem",
		"Key: /etc/example/key.pem",
		"Chain: /etc/example/chain.pem",
		"Debug logging: ENABLED",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("logStartupInfo output is missing %q:\n%s", want, out)
		}
	}
}

func TestCleanupExpiredConnectionsRemovesTheClients(t *testing.T) {
	server := &Server{clientManager: database.NewClientManager(nil)}
	out := captureStderr(t, func() {
		server.cleanupExpiredConnections([]string{"hash-without-a-client"})
	})
	if out != "" {
		t.Errorf("a clean removal wrote %q", out)
	}
}

// TestSetupSIGHUPReloadsTheConfiguration sends the test process a real
// SIGHUP. signal.Notify has already claimed it by then, so it reaches
// the reload goroutine instead of terminating the process.
func TestSetupSIGHUPReloadsTheConfiguration(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "ai-dba-server.yaml")
	if err := os.WriteFile(configPath, []byte(
		"database:\n  host: 127.0.0.1\n  port: 5432\n  database: reloaded\n  user: reloaded\n"),
		0600); err != nil {
		t.Fatalf("writing the config file: %v", err)
	}

	cfg := &config.Config{Database: &config.DatabaseConfig{
		Host: "127.0.0.1", Port: 5432, Database: "original", User: "original",
	}}
	server := &Server{cfg: cfg, clientManager: database.NewClientManager(cfg.Database)}

	out := captureStderr(t, func() {
		// Stop the handler before captureStderr restores os.Stderr, so
		// no reload can write to the closed pipe.
		stop := server.setupSIGHUP(&Flags{}, configPath)
		defer stop()
		defer stop() // a second call must be a no-op
		if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
			t.Fatalf("sending SIGHUP: %v", err)
		}

		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if db := server.clientManager.GetDatabaseConfig(); db != nil && db.Database == "reloaded" {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Error("the client manager never received the reloaded database configuration")
	})
	if !strings.Contains(out, "Received SIGHUP, reloading configuration") {
		t.Errorf("output = %q, want the reload announced", out)
	}
}
