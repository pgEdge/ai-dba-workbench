/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// secretColumns lists every column that must never be readable through
// query_datastore, whichever table it lives in.
var secretColumns = []string{
	"password_encrypted",
	"sslkey",
	"sslcert",
	"sslrootcert",
	"owner_token",
	"config",
	"webhook_url_encrypted",
	"auth_credentials_encrypted",
	"smtp_password_encrypted",
	"telegram_bot_token_encrypted",
	"headers_json",
	"conninfo",
}

func TestConfigurationSourcesExposeNoSecrets(t *testing.T) {
	for _, src := range configurationSources() {
		for _, c := range src.Columns {
			for _, secret := range secretColumns {
				if c.Name == secret {
					t.Errorf("source %q exposes secret column %q", src.Name, c.Name)
				}
			}
			if isSecretColumnName(c.Name) {
				t.Errorf("source %q exposes secret-looking column %q", src.Name, c.Name)
			}
		}
	}
}

func TestConfigurationSourcesOmitExcludedTables(t *testing.T) {
	excluded := []string{
		"notification_channels", "email_recipients", "conversations",
		"chat_memories", "anomaly_embeddings", "notification_history",
		"schema_version", "users", "service_tokens",
	}
	for _, src := range configurationSources() {
		for _, name := range excluded {
			if src.Name == name || src.Table == name {
				t.Errorf("source list includes excluded table %q", name)
			}
		}
	}
}

func TestConfigurationSourcesWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, src := range configurationSources() {
		if seen[src.Name] {
			t.Errorf("duplicate source %q", src.Name)
		}
		seen[src.Name] = true
		if src.Kind != datastoreKindConfiguration {
			t.Errorf("source %q has kind %q", src.Name, src.Kind)
		}
		if src.Description == "" || src.Table == "" {
			t.Errorf("source %q lacks a table or description", src.Name)
		}
		if len(src.Columns) == 0 {
			t.Errorf("source %q has no columns", src.Name)
		}
		names := map[string]bool{}
		for _, c := range src.Columns {
			if !metrics.IsValidIdentifier(c.Name) {
				t.Errorf("source %q column %q is not a valid identifier", src.Name, c.Name)
			}
			if names[c.Name] {
				t.Errorf("source %q lists column %q twice", src.Name, c.Name)
			}
			names[c.Name] = true
			if c.DataType == "" {
				t.Errorf("source %q column %q has no type", src.Name, c.Name)
			}
		}
		if src.TimeColumn != "" && !names[src.TimeColumn] {
			t.Errorf("source %q time column %q is not a permitted column", src.Name, src.TimeColumn)
		}
		switch src.Scope {
		case scopeConnection, scopeAlert:
			if src.ConnectionColumn == "" || !names[src.ConnectionColumn] {
				t.Errorf("source %q needs a permitted connection column", src.Name)
			}
		case scopeHierarchy:
			for _, c := range []string{"connection_id", "cluster_id", "group_id", "scope"} {
				if !names[c] {
					t.Errorf("hierarchy source %q lacks column %q", src.Name, c)
				}
			}
		case scopeViaAlert:
			if !names["alert_id"] {
				t.Errorf("via_alert source %q lacks alert_id", src.Name)
			}
		}
	}
}

func TestConnectionsSourceOmitsCredentials(t *testing.T) {
	src, err := lookupDatastoreSource(context.Background(), nil, "connections")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	for _, name := range []string{"password_encrypted", "sslkey", "sslcert", "sslrootcert", "owner_token"} {
		if _, ok := src.column(name); ok {
			t.Errorf("connections exposes %q", name)
		}
	}
	// username and hostaddr are not secrets. The REST API returns
	// username; it omits hostaddr, which is no more sensitive than host.
	for _, name := range []string{"name", "username", "hostaddr"} {
		if _, ok := src.column(name); !ok {
			t.Errorf("connections should expose %q", name)
		}
	}
}

func TestDatastoreScopeStrings(t *testing.T) {
	scopes := []datastoreScope{
		scopeConnection, scopeViaAlert, scopeHierarchy, scopeCluster,
		scopeGroup, scopeGlobal, scopeAlert,
	}
	seen := map[string]bool{}
	for _, s := range scopes {
		name := s.String()
		if name == "unknown" || seen[name] {
			t.Errorf("scope %d has name %q", s, name)
		}
		seen[name] = true
		if s.Description() == "No rows." {
			t.Errorf("scope %q has no description", name)
		}
	}
	if datastoreScope(99).String() != "unknown" {
		t.Error("unknown scope should name itself unknown")
	}
	if datastoreScope(99).Description() != "No rows." {
		t.Error("unknown scope should describe no rows")
	}
}

func TestIsSecretColumnName(t *testing.T) {
	for name, want := range map[string]bool{
		"conninfo":           true,
		"SSLKEY":             true,
		"password_encrypted": true,
		"db_passwd":          true,
		"client_secret":      true,
		"api_token":          true,
		"subconninfo":        true,
		"ssl_passphrase":     true,
		"primary_connstr":    true,
		"connection_string":  true,
		"target_dsn":         true,
		"sslrootcert_path":   true,
		"subscription_name":  false,
		"auto_group_key":     false,
		"xact_commit":        false,
		"datname":            false,
	} {
		if got := isSecretColumnName(name); got != want {
			t.Errorf("isSecretColumnName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestLookupDatastoreSourceWithoutPool(t *testing.T) {
	ctx := context.Background()
	if _, err := lookupDatastoreSource(ctx, nil, "bad name;"); !errors.Is(err, errUnknownDatastoreSource) {
		t.Errorf("invalid identifier: got %v", err)
	}
	if !strings.Contains(errUnknownDatastoreSource.Error(), "describe_datastore") {
		t.Error("unknown source error should name describe_datastore")
	}
	_, err := lookupDatastoreSource(ctx, nil, "pg_stat_database")
	if err == nil || errors.Is(err, errUnknownDatastoreSource) {
		t.Errorf("metric lookup without a pool: got %v", err)
	}
	for _, src := range configurationSources() {
		got, err := lookupDatastoreSource(ctx, nil, src.Name)
		if err != nil || got.Name != src.Name {
			t.Errorf("lookup %q: %v", src.Name, err)
		}
	}
}

// TestLookupDatastoreSourceMetricDiscovery checks live discovery of a
// metric source against the local test database.
func TestLookupDatastoreSourceMetricDiscovery(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	// Cleanups run last-in first-out, so the probe tables registered
	// below are dropped before the pool closes.
	t.Cleanup(cleanup)
	ctx := context.Background()
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS metrics.ds_test_probe, metrics.ds_test_nokeys CASCADE`); err != nil {
			t.Logf("drop test probe tables: %v", err)
		}
	})

	stmts := []string{
		`CREATE SCHEMA IF NOT EXISTS metrics`,
		`DROP TABLE IF EXISTS metrics.ds_test_probe CASCADE`,
		`DROP TABLE IF EXISTS metrics.ds_test_nokeys CASCADE`,
		`CREATE TABLE metrics.ds_test_probe (
            connection_id integer NOT NULL,
            collected_at timestamptz NOT NULL,
            datname text,
            xact_commit bigint,
            conninfo text,
            db_password text
        ) PARTITION BY RANGE (collected_at)`,
		`COMMENT ON TABLE metrics.ds_test_probe IS 'Test probe for datastore discovery.'`,
		`CREATE TABLE metrics.ds_test_probe_20260101 PARTITION OF metrics.ds_test_probe
            FOR VALUES FROM ('2026-01-01') TO ('2026-01-08')`,
		`CREATE TABLE metrics.ds_test_nokeys (value bigint)`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}

	src, err := lookupDatastoreSource(ctx, pool, "ds_test_probe")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if src.Kind != datastoreKindMetric || src.Scope != scopeConnection ||
		src.TimeColumn != "collected_at" || src.ConnectionColumn != "connection_id" ||
		src.Table != "ds_test_probe" {
		t.Errorf("unexpected source: %+v", src)
	}
	if src.Description != "Test probe for datastore discovery." {
		t.Errorf("description = %q", src.Description)
	}
	if got := strings.Join(src.columnNames(), ","); got != "connection_id,collected_at,datname,xact_commit" {
		t.Errorf("columns = %s", got)
	}
	if c, _ := src.column("xact_commit"); !c.IsMetric {
		t.Error("xact_commit should be a metric column")
	}
	if c, _ := src.column("datname"); c.IsMetric {
		t.Error("datname should be a dimension")
	}

	for _, name := range []string{"ds_test_probe_20260101", "ds_test_nokeys", "no_such_probe"} {
		if _, err := lookupDatastoreSource(ctx, pool, name); !errors.Is(err, errUnknownDatastoreSource) {
			t.Errorf("lookup %q: got %v, want unknown source", name, err)
		}
	}

	if _, err := pool.Exec(ctx, `COMMENT ON TABLE metrics.ds_test_probe IS NULL`); err != nil {
		t.Fatalf("clear comment: %v", err)
	}
	src, err = lookupDatastoreSource(ctx, pool, "ds_test_probe")
	if err != nil {
		t.Fatalf("lookup without comment: %v", err)
	}
	if !strings.Contains(src.Description, "ds_test_probe") {
		t.Errorf("default description = %q", src.Description)
	}

	done, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := lookupDatastoreSource(done, pool, "ds_test_probe"); err == nil {
		t.Error("a context that is already done should fail the lookup")
	}
}
