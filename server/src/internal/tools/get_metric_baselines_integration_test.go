/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Integration tests for the get_metric_baselines tool. They execute real
// queries against the Postgres instance named by TEST_AI_WORKBENCH_SERVER and
// skip gracefully when that variable is unset. They exercise the metric_name
// matching behavior introduced for issue #287: partial, case-insensitive
// matching against fully-qualified stored names, literal escaping of LIKE
// wildcards, and the "no match" helper that lists available metric names.
//
// These tests reuse the schema and pool helper from tools_integration_test.go
// (newToolsTestPool / toolsIntegrationSchema), which now provisions the
// metric_baselines table.

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// seedBaselineConnection inserts a connection row and returns its id.
func seedBaselineConnection(t *testing.T, pool *pgxpool.Pool, name string, shared bool, owner string) int {
	t.Helper()
	ctx := context.Background()
	var id int
	err := pool.QueryRow(ctx,
		`INSERT INTO connections (name, host, port, database_name, is_shared, owner_username)
		 VALUES ($1, 'localhost', 5432, 'postgres', $2, $3) RETURNING id`,
		name, shared, owner).Scan(&id)
	if err != nil {
		t.Fatalf("failed to seed connection %q: %v", name, err)
	}
	return id
}

// seedBaseline inserts a single baseline row for a connection.
func seedBaseline(t *testing.T, pool *pgxpool.Pool, connID int, metricName string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx,
		`INSERT INTO metric_baselines
		   (connection_id, metric_name, period_type, hour_of_day,
		    mean, stddev, min, max, sample_count)
		 VALUES ($1, $2, 'hourly', 10, 1.0, 0.5, 0.0, 2.0, 100)`,
		connID, metricName)
	if err != nil {
		t.Fatalf("failed to seed baseline %q: %v", metricName, err)
	}
}

func mustSuccess(t *testing.T, tool Tool, args map[string]any) string {
	t.Helper()
	resp, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if resp.IsError {
		if len(resp.Content) > 0 {
			t.Fatalf("handler returned error response: %s", resp.Content[0].Text)
		}
		t.Fatal("handler returned error response with no content")
	}
	if len(resp.Content) == 0 {
		t.Fatal("expected non-empty response content")
	}
	return resp.Content[0].Text
}

// ---------------------------------------------------------------------------
// Single-connection: partial, case-insensitive match
// ---------------------------------------------------------------------------

// TestGetMetricBaselinesSingleShorthandMatchIntegration verifies that a
// shorthand metric_name matches the fully-qualified stored name in
// single-connection mode (issue #287).
func TestGetMetricBaselinesSingleShorthandMatchIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	connID := seedBaselineConnection(t, pool, "single-conn", true, "")
	seedBaseline(t, pool, connID, "pg_stat_database.cache_hit_ratio")

	tool := GetMetricBaselinesTool(pool, nil, nil)

	cases := []struct {
		name   string
		metric string
	}{
		{"shorthand", "cache_hit_ratio"},
		{"uppercase", "CACHE_HIT_RATIO"},
		{"substring", "cache_hit"},
		{"fully qualified", "pg_stat_database.cache_hit_ratio"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := mustSuccess(t, tool, map[string]any{
				"connection_id": connID,
				"metric_name":   tc.metric,
			})
			if !strings.Contains(body, "pg_stat_database.cache_hit_ratio") {
				t.Errorf("expected fully-qualified name in output for %q, got: %s", tc.metric, body)
			}
			if !strings.Contains(body, "(1 baselines)") {
				t.Errorf("expected exactly one baseline for %q, got: %s", tc.metric, body)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Single-connection: literal escaping of LIKE wildcards
// ---------------------------------------------------------------------------

// TestGetMetricBaselinesSingleLiteralEscapeIntegration verifies that a
// literal '%' or '_' in metric_name is treated literally rather than as a
// wildcard.
func TestGetMetricBaselinesSingleLiteralEscapeIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	connID := seedBaselineConnection(t, pool, "escape-conn", true, "")
	// A real metric and a decoy that a wildcard '%' would also match.
	seedBaseline(t, pool, connID, "pg_stat_database.cache_hit_ratio")
	seedBaseline(t, pool, connID, "pg_stat_database.tup_returned")

	tool := GetMetricBaselinesTool(pool, nil, nil)

	// A literal "%" must not match anything (no stored name contains a
	// literal percent sign). If escaping failed, "%" would match every row.
	body := mustSuccess(t, tool, map[string]any{
		"connection_id": connID,
		"metric_name":   "%",
	})
	if strings.Contains(body, "(1 baselines)") || strings.Contains(body, "(2 baselines)") {
		t.Errorf("literal '%%' should not match any baseline, got: %s", body)
	}
	// Should fall through to the helpful "no match" message listing names.
	if !strings.Contains(body, "available metric names") {
		t.Errorf("expected available-names helper for literal '%%', got: %s", body)
	}
	if !strings.Contains(body, "pg_stat_database.cache_hit_ratio") ||
		!strings.Contains(body, "pg_stat_database.tup_returned") {
		t.Errorf("expected both stored names listed, got: %s", body)
	}
}

// TestGetMetricBaselinesSingleLiteralUnderscoreIntegration verifies that a
// literal '_' is treated as a literal character, not a single-char wildcard.
func TestGetMetricBaselinesSingleLiteralUnderscoreIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	connID := seedBaselineConnection(t, pool, "underscore-conn", true, "")
	// "aXb" would match the wildcard pattern "a_b" if '_' were not escaped.
	seedBaseline(t, pool, connID, "schema.aXb")

	tool := GetMetricBaselinesTool(pool, nil, nil)

	body := mustSuccess(t, tool, map[string]any{
		"connection_id": connID,
		"metric_name":   "a_b",
	})
	// With escaping, "a_b" is a literal substring and must NOT match "aXb".
	if strings.Contains(body, "(1 baselines)") {
		t.Errorf("literal '_' should not act as a wildcard; 'a_b' must not match 'aXb', got: %s", body)
	}
	if !strings.Contains(body, "schema.aXb") {
		t.Errorf("expected available-names helper listing schema.aXb, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Single-connection: empty results with and without a filter
// ---------------------------------------------------------------------------

// TestGetMetricBaselinesSingleEmptyWithFilterIntegration verifies the helper
// message lists available metric names when a filter matches nothing.
func TestGetMetricBaselinesSingleEmptyWithFilterIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	connID := seedBaselineConnection(t, pool, "empty-filter-conn", true, "")
	seedBaseline(t, pool, connID, "pg_stat_database.cache_hit_ratio")

	tool := GetMetricBaselinesTool(pool, nil, nil)

	body := mustSuccess(t, tool, map[string]any{
		"connection_id": connID,
		"metric_name":   "does_not_exist",
	})
	if !strings.Contains(body, "No metric baselines matched") {
		t.Errorf("expected 'No metric baselines matched' message, got: %s", body)
	}
	if !strings.Contains(body, "available metric names") {
		t.Errorf("expected available-names list, got: %s", body)
	}
	if !strings.Contains(body, "pg_stat_database.cache_hit_ratio") {
		t.Errorf("expected stored name in available list, got: %s", body)
	}
}

// TestGetMetricBaselinesSingleEmptyNoFilterIntegration verifies the original
// "no baselines at all" message is preserved when no metric_name is supplied.
func TestGetMetricBaselinesSingleEmptyNoFilterIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	connID := seedBaselineConnection(t, pool, "empty-nofilter-conn", true, "")
	// No baselines seeded.

	tool := GetMetricBaselinesTool(pool, nil, nil)

	body := mustSuccess(t, tool, map[string]any{
		"connection_id": connID,
	})
	if !strings.Contains(body, "No metric baselines found for connection") {
		t.Errorf("expected original no-baselines message, got: %s", body)
	}
	if strings.Contains(body, "available metric names") {
		t.Errorf("should not show available-names helper without a filter, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Multi-connection: partial match and helper message
// ---------------------------------------------------------------------------

// TestGetMetricBaselinesMultiShorthandMatchIntegration verifies partial
// case-insensitive matching in multi-connection mode. A nil rbacChecker
// yields unrestricted (all-connections) visibility.
func TestGetMetricBaselinesMultiShorthandMatchIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	connA := seedBaselineConnection(t, pool, "multi-a", true, "")
	connB := seedBaselineConnection(t, pool, "multi-b", true, "")
	seedBaseline(t, pool, connA, "pg_stat_database.cache_hit_ratio")
	seedBaseline(t, pool, connB, "pg_stat_bgwriter.checkpoints_timed")

	tool := GetMetricBaselinesTool(pool, nil, nil)

	body := mustSuccess(t, tool, map[string]any{
		"metric_name": "CACHE_HIT",
	})
	if !strings.Contains(body, "pg_stat_database.cache_hit_ratio") {
		t.Errorf("expected matched fully-qualified name, got: %s", body)
	}
	if strings.Contains(body, "checkpoints_timed") {
		t.Errorf("non-matching baseline should be excluded, got: %s", body)
	}
	if !strings.Contains(body, "multi-a") {
		t.Errorf("expected connection name in multi-connection output, got: %s", body)
	}
}

// TestGetMetricBaselinesMultiEmptyWithFilterIntegration verifies the
// available-names helper across accessible connections in multi mode.
func TestGetMetricBaselinesMultiEmptyWithFilterIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	connA := seedBaselineConnection(t, pool, "multi-empty-a", true, "")
	connB := seedBaselineConnection(t, pool, "multi-empty-b", true, "")
	seedBaseline(t, pool, connA, "pg_stat_database.cache_hit_ratio")
	seedBaseline(t, pool, connB, "pg_stat_bgwriter.checkpoints_timed")

	tool := GetMetricBaselinesTool(pool, nil, nil)

	body := mustSuccess(t, tool, map[string]any{
		"metric_name": "no_such_metric",
	})
	if !strings.Contains(body, "No metric baselines matched") {
		t.Errorf("expected no-match message, got: %s", body)
	}
	if !strings.Contains(body, "pg_stat_database.cache_hit_ratio") ||
		!strings.Contains(body, "pg_stat_bgwriter.checkpoints_timed") {
		t.Errorf("expected names from both connections listed, got: %s", body)
	}
}

// TestGetMetricBaselinesMultiEmptyNoFilterIntegration verifies the original
// multi-connection no-baselines message is preserved with no filter.
func TestGetMetricBaselinesMultiEmptyNoFilterIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	// A connection with no baselines.
	seedBaselineConnection(t, pool, "multi-bare", true, "")

	tool := GetMetricBaselinesTool(pool, nil, nil)

	body := mustSuccess(t, tool, map[string]any{})
	if !strings.Contains(body, "No metric baselines found across accessible connections") {
		t.Errorf("expected original multi no-baselines message, got: %s", body)
	}
	if strings.Contains(body, "available metric names") {
		t.Errorf("should not show available-names helper without a filter, got: %s", body)
	}
}

// ---------------------------------------------------------------------------
// RBAC scoping for the multi-connection available-names helper
// ---------------------------------------------------------------------------

// TestGetMetricBaselinesMultiRBACScopedHelperIntegration verifies that the
// available-names helper only lists metric names from connections the caller
// can actually see. Bob owns nothing and may only see the shared connection,
// so the unshared connection's metric names must not leak.
func TestGetMetricBaselinesMultiRBACScopedHelperIntegration(t *testing.T) {
	pool, ds, cleanup := newToolsTestPool(t)
	defer cleanup()

	authStore, authCleanup := newRBACTestStore(t)
	defer authCleanup()

	if err := authStore.CreateUser("bob", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := authStore.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	sharedConn := seedBaselineConnection(t, pool, "shared-conn", true, "alice")
	privateConn := seedBaselineConnection(t, pool, "alice-private", false, "alice")
	seedBaseline(t, pool, sharedConn, "pg_stat_database.shared_metric")
	seedBaseline(t, pool, privateConn, "pg_stat_database.secret_metric")

	rbac := auth.NewRBACChecker(authStore)
	lister := database.NewVisibilityLister(ds)
	tool := GetMetricBaselinesTool(pool, rbac, lister)

	userCtx := nonSuperuserContextInt(userID, "bob")
	body := mustSuccess(t, tool, map[string]any{
		"__context":   userCtx,
		"metric_name": "no_match_here",
	})
	if !strings.Contains(body, "pg_stat_database.shared_metric") {
		t.Errorf("expected shared connection's metric name in helper, got: %s", body)
	}
	if strings.Contains(body, "secret_metric") {
		t.Errorf("RBAC leak: private connection's metric name appeared, got: %s", body)
	}
}

// TestGetMetricBaselinesSingleRBACDeniedIntegration verifies that a
// non-superuser denied access to a specific connection_id receives an
// "Access denied" error (exercises the single-connection RBAC branch).
func TestGetMetricBaselinesSingleRBACDeniedIntegration(t *testing.T) {
	pool, ds, cleanup := newToolsTestPool(t)
	defer cleanup()

	authStore, authCleanup := newRBACTestStore(t)
	defer authCleanup()

	if err := authStore.CreateUser("bob", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := authStore.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	// Alice's private connection: bob has no access.
	privateConn := seedBaselineConnection(t, pool, "alice-only", false, "alice")

	rbac := auth.NewRBACCheckerForDatastore(authStore, ds)
	lister := database.NewVisibilityLister(ds)
	tool := GetMetricBaselinesTool(pool, rbac, lister)

	userCtx := nonSuperuserContextInt(userID, "bob")
	resp, err := tool.Handler(map[string]any{
		"__context":     userCtx,
		"connection_id": privateConn,
	})
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatal("expected error response for denied connection")
	}
	if len(resp.Content) == 0 {
		t.Fatal("expected error content")
	}
	if !strings.Contains(resp.Content[0].Text, "Access denied") {
		t.Errorf("expected 'Access denied' message, got: %s", resp.Content[0].Text)
	}
}

// TestAvailableMetricNamesQueryErrorIntegration verifies the error paths of
// the available-names helpers: when the metric_baselines table is missing
// the query fails and the helper returns nil rather than surfacing an error.
func TestAvailableMetricNamesQueryErrorIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	ctx := context.Background()
	// Remove the table so the helper queries fail.
	if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS metric_baselines CASCADE"); err != nil {
		t.Fatalf("failed to drop metric_baselines: %v", err)
	}
	// Recreate it afterwards so teardown and other tests are unaffected.
	defer func() {
		if _, err := pool.Exec(context.Background(), toolsIntegrationSchema); err != nil {
			t.Logf("failed to recreate schema: %v", err)
		}
	}()

	if got := availableMetricNamesSingle(ctx, pool, 1); got != nil {
		t.Errorf("expected nil on query error (single), got: %v", got)
	}
	if got := availableMetricNamesMulti(ctx, pool, true, nil); got != nil {
		t.Errorf("expected nil on query error (multi), got: %v", got)
	}
}

// TestGetMetricBaselinesInvalidConnectionExistsIntegration verifies that an
// integer connection_id that does not exist yields the "does not exist"
// message listing valid IDs (exercises the connection-existence check with a
// real pool).
func TestGetMetricBaselinesInvalidConnectionExistsIntegration(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()

	seedBaselineConnection(t, pool, "exists-conn", true, "")

	tool := GetMetricBaselinesTool(pool, nil, nil)

	resp, err := tool.Handler(map[string]any{
		"connection_id": 999999,
	})
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	if !resp.IsError {
		t.Fatal("expected error response for non-existent connection_id")
	}
	if len(resp.Content) == 0 {
		t.Fatal("expected error content")
	}
	if !strings.Contains(resp.Content[0].Text, "does not exist") {
		t.Errorf("expected 'does not exist' message, got: %s", resp.Content[0].Text)
	}
}
