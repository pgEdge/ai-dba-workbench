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
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "modernc.org/sqlite"

	"github.com/pgedge/ai-workbench/pkg/fileutil"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// newServerWithSecretFile builds a minimal Server fixture wired up
// only with the SecretFile field. This avoids depending on the
// full server initialisation path while still exercising the
// secret-loading method end to end.
func newServerWithSecretFile(secretFile string) *Server {
	return &Server{cfg: &config.Config{SecretFile: secretFile}}
}

// TestLoadServerSecret_ExplicitFile verifies that an explicit
// SecretFile in the config is read with only its trailing newline
// trimmed.
func TestLoadServerSecret_ExplicitFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "server.secret")
	if err := os.WriteFile(path, []byte("explicit-secret\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s := newServerWithSecretFile(path)

	got, err := s.loadServerSecret()
	if err != nil {
		t.Fatalf("loadServerSecret: %v", err)
	}
	if got != "explicit-secret" {
		t.Errorf("got %q, want %q", got, "explicit-secret")
	}
}

// TestLoadServerSecret_DefaultUserDir verifies that when no
// explicit SecretFile is set the helper picks up a file from the
// per-user pgedge config directory.
func TestLoadServerSecret_DefaultUserDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)
	// Isolate the system-path branch so the test reflects only
	// the per-user candidate, even on hosts where /etc/pgedge
	// happens to contain a real ai-dba-server.secret.
	fileutil.SetSystemConfigDirForTest(t, filepath.Join(base, "absent-etc-pgedge"))

	userDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("os.UserConfigDir: %v", err)
	}
	pgedgeDir := filepath.Join(userDir, "pgedge")
	if err := os.MkdirAll(pgedgeDir, 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(pgedgeDir, "ai-dba-server.secret"),
		[]byte("auto-discovered"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	s := newServerWithSecretFile("")
	got, err := s.loadServerSecret()
	if err != nil {
		t.Fatalf("loadServerSecret: %v", err)
	}
	if got != "auto-discovered" {
		t.Errorf("got %q, want %q", got, "auto-discovered")
	}
}

// TestLoadServerSecret_NoneFound verifies that the helper returns
// a descriptive error when neither an explicit path nor any of the
// default search paths yield a secret file. Both the per-user and
// the system-wide candidates are redirected at directories
// guaranteed not to exist, so the assertion is deterministic
// regardless of the host's real /etc/pgedge contents.
func TestLoadServerSecret_NoneFound(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("HOME", base)
	t.Setenv("AppData", base)
	fileutil.SetSystemConfigDirForTest(t, filepath.Join(base, "absent-etc-pgedge"))

	s := newServerWithSecretFile("")
	_, err := s.loadServerSecret()
	if err == nil {
		t.Fatal("expected error when no secret file is reachable")
	}
	if !strings.Contains(err.Error(), "not found in any") {
		t.Errorf("err = %v, want it to mention 'not found in any'", err)
	}
}

// TestLoadServerSecret_EmptyFile verifies that a secret file
// containing only newlines is rejected as empty. ReadSecretFile strips
// only the trailing CR/LF sequence before validation, but content that
// is empty or consists solely of whitespace is still treated as empty
// and rejected.
func TestLoadServerSecret_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "blank.secret")
	if err := os.WriteFile(path, []byte("\n\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s := newServerWithSecretFile(path)

	_, err := s.loadServerSecret()
	if err == nil {
		t.Fatal("expected error when secret file is empty")
	}
	if !strings.Contains(err.Error(), "is empty") {
		t.Errorf("err = %v, want it to mention 'is empty'", err)
	}
}

// TestVerifySchemaHealth_NoDatastore exercises the Server wrapper
// around the datastore's schema health check. With no datastore
// configured the wrapper must return nil immediately so callers
// (main.go) can invoke it unconditionally; without this guard the
// wrapper would panic before the underlying check could surface a
// useful error. The nil-receiver case proves the same guard applies
// before the field dereference.
func TestVerifySchemaHealth_NoDatastore(t *testing.T) {
	s := &Server{}
	if err := s.VerifySchemaHealth(context.Background()); err != nil {
		t.Errorf("expected nil error with no datastore, got %v", err)
	}

	var nilServer *Server
	if err := nilServer.VerifySchemaHealth(context.Background()); err != nil {
		t.Errorf("expected nil error for nil Server, got %v", err)
	}
}

// TestVerifySchemaHealth_DelegatesToDatastore confirms that when a
// datastore is configured the Server wrapper forwards the call
// through to it; the underlying datastore probe is exercised
// directly by tests in the database package, so the assertion here
// is limited to "we reached the datastore and got something back",
// which is enough to lift the wrapper's coverage above the project
// floor without re-testing the substantive check.
func TestVerifySchemaHealth_DelegatesToDatastore(t *testing.T) {
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping delegation test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("Test database ping failed: %v", err)
	}

	// Aggressively drop schema_version so the delegated call deflects
	// to the missing-table branch; we only care that the wrapper
	// reached the datastore, not which branch it took.
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS schema_version CASCADE`); err != nil {
		t.Fatalf("failed to drop schema_version: %v", err)
	}

	srv := &Server{datastore: database.NewTestDatastore(pool)}
	err = srv.VerifySchemaHealth(ctx)
	if err == nil {
		t.Fatal("expected error from delegated datastore probe")
	}
	if !strings.Contains(err.Error(), "schema_version") {
		t.Errorf("expected delegated error to mention schema_version, got %v", err)
	}
}

// TestLoadServerSecret_ExplicitMissing verifies that an explicit
// SecretFile pointing at a non-existent path returns a read error
// (not the "not found in any default search path" message).
func TestLoadServerSecret_ExplicitMissing(t *testing.T) {
	s := newServerWithSecretFile("/definitely/not/a/real/path.secret")

	_, err := s.loadServerSecret()
	if err == nil {
		t.Fatal("expected error for missing explicit secret file")
	}
	if !strings.Contains(err.Error(), "failed to read secret file") {
		t.Errorf("err = %v, want it to mention 'failed to read secret file'", err)
	}
}

// captureStderr runs fn with os.Stderr redirected to a pipe and
// returns everything fn writes to it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer func() { _ = r.Close() }()
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return string(out)
}

// TestLogStartupInfo_KnowledgebaseAPIKeyStatus exercises the
// knowledgebase API-key status branch in logStartupInfo, including
// the Gemini branch that previously never set "loaded".
func TestLogStartupInfo_KnowledgebaseAPIKeyStatus(t *testing.T) {
	tests := []struct {
		name string
		kb   config.KnowledgebaseConfig
		want string
	}{
		{
			name: "voyage key loaded",
			kb: config.KnowledgebaseConfig{
				Enabled:               true,
				EmbeddingProvider:     "voyage",
				EmbeddingVoyageAPIKey: "voyage-key",
			},
			want: "API key: loaded",
		},
		{
			name: "openai key loaded",
			kb: config.KnowledgebaseConfig{
				Enabled:               true,
				EmbeddingProvider:     "openai",
				EmbeddingOpenAIAPIKey: "openai-key",
			},
			want: "API key: loaded",
		},
		{
			name: "gemini key loaded",
			kb: config.KnowledgebaseConfig{
				Enabled:               true,
				EmbeddingProvider:     "gemini",
				EmbeddingModel:        "gemini-embedding-001",
				EmbeddingGeminiAPIKey: "gemini-key",
			},
			want: "API key: loaded",
		},
		{
			name: "no key set",
			kb: config.KnowledgebaseConfig{
				Enabled:           true,
				EmbeddingProvider: "gemini",
			},
			want: "API key: not set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{cfg: &config.Config{Knowledgebase: tt.kb}}
			out := captureStderr(t, s.logStartupInfo)
			if !strings.Contains(out, tt.want) {
				t.Errorf("logStartupInfo output = %q, want it to contain %q", out, tt.want)
			}
			if !strings.Contains(out, "Knowledgebase: ENABLED") {
				t.Errorf("expected knowledgebase enabled line, got %q", out)
			}
		})
	}
}

// TestLogStartupInfo_KnowledgebaseDisabled covers the disabled
// knowledgebase branch of logStartupInfo.
func TestLogStartupInfo_KnowledgebaseDisabled(t *testing.T) {
	s := &Server{cfg: &config.Config{}, debug: true}
	out := captureStderr(t, s.logStartupInfo)
	if !strings.Contains(out, "Knowledgebase: DISABLED") {
		t.Errorf("expected disabled knowledgebase line, got %q", out)
	}
	if !strings.Contains(out, "Debug logging: ENABLED") {
		t.Errorf("expected debug line, got %q", out)
	}
}

// seedAuditEvent appends one keyed audit event backdated to
// occurredAt, written and signed as the store would have written it
// then, so that the retention purge accepts it and can remove it.
func seedAuditEvent(t *testing.T, store *auth.AuthStore, occurredAt time.Time) {
	t.Helper()

	if err := auth.SeedAuditEventForTesting(store, occurredAt); err != nil {
		t.Fatalf("failed to seed audit event: %v", err)
	}
}

// countAuditEvents returns the total number of audit_events rows
// currently stored, via the exported ListAuditEvents query.
func countAuditEvents(t *testing.T, store *auth.AuthStore) int {
	t.Helper()

	_, total, err := store.ListAuditEvents(auth.AuditFilter{})
	if err != nil {
		t.Fatalf("failed to list audit events: %v", err)
	}
	return total
}

func TestPurgeAuditEventsZeroDaysKeepsForever(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.NewAuthStore(dir, 0, 10, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("failed to create auth store: %v", err)
	}
	defer store.Close()

	seedAuditEvent(t, store, time.Now().Add(-48*time.Hour))
	seedAuditEvent(t, store, time.Now())

	zero := 0
	s := &Server{
		cfg: &config.Config{
			HTTP: config.HTTPConfig{
				Auth: config.AuthConfig{AuditRetentionDaysPtr: &zero},
			},
		},
		authStore: store,
	}

	s.purgeAuditEvents()

	if got := countAuditEvents(t, store); got != 2 {
		t.Errorf("expected both audit events to remain with retention 0, got %d", got)
	}
}

func TestPurgeAuditEventsRemovesOldEvents(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.NewAuthStore(dir, 0, 10, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("failed to create auth store: %v", err)
	}
	defer store.Close()

	seedAuditEvent(t, store, time.Now().Add(-48*time.Hour))
	seedAuditEvent(t, store, time.Now())

	one := 1
	s := &Server{
		cfg: &config.Config{
			HTTP: config.HTTPConfig{
				Auth: config.AuthConfig{AuditRetentionDaysPtr: &one},
			},
		},
		authStore: store,
	}

	s.purgeAuditEvents()

	// The retained event plus the audit.purge event the purge records.
	if got := countAuditEvents(t, store); got != 2 {
		t.Errorf("expected two audit events to remain after purge, got %d", got)
	}
}

func TestPurgeAuditEventsNoAuthStore(t *testing.T) {
	// Must not panic when there is no auth store wired up.
	s := &Server{cfg: &config.Config{}}
	s.purgeAuditEvents()
}

func TestPurgeAuditEventsLogsError(t *testing.T) {
	dir := t.TempDir()
	store, err := auth.NewAuthStore(dir, 0, 10, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("failed to create auth store: %v", err)
	}
	// Close the store up front so PurgeAuditEvents fails against the
	// now-closed database, exercising purgeAuditEvents' error path.
	if err := store.Close(); err != nil {
		t.Fatalf("failed to close auth store: %v", err)
	}

	one := 1
	s := &Server{
		cfg: &config.Config{
			HTTP: config.HTTPConfig{
				Auth: config.AuthConfig{AuditRetentionDaysPtr: &one},
			},
		},
		authStore: store,
	}

	// Must not panic; the error is only logged.
	s.purgeAuditEvents()
}
