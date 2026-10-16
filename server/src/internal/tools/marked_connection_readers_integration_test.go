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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// These tests cover every tool that reads connections.deleted_at
// directly: a connection marked for deletion but not yet purged (issues
// #668 and #669) must be gone from each, even for a superuser, and even
// when its alert and baseline rows were written after the mark.

// markedReadersSchema adds the alert tables get_alert_history reads to
// the tools integration schema; newToolsTestPool's teardown does not
// know them, so the test drops them itself.
const markedReadersSchema = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;

CREATE TABLE alerts (
    id BIGSERIAL PRIMARY KEY,
    alert_type VARCHAR(20) NOT NULL DEFAULT 'threshold',
    rule_id BIGINT,
    connection_id INTEGER,
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    severity VARCHAR(20) NOT NULL DEFAULT 'warning',
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    metric_name TEXT,
    metric_value DOUBLE PRECISION,
    threshold_value DOUBLE PRECISION,
    operator VARCHAR(4),
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    cleared_at TIMESTAMPTZ
);

CREATE TABLE alert_acknowledgments (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT NOT NULL,
    acknowledged_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    acknowledged_by TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    false_positive BOOLEAN NOT NULL DEFAULT FALSE
);
`

const markedReadersTeardown = `
DROP TABLE IF EXISTS alert_acknowledgments CASCADE;
DROP TABLE IF EXISTS alerts CASCADE;
`

// markedFixture holds a live connection and a marked one, each with an
// alert and a baseline.
type markedFixture struct {
	pool       *pgxpool.Pool
	ds         *database.Datastore
	live, gone int
}

func newMarkedFixture(t *testing.T) *markedFixture {
	t.Helper()
	pool, ds, cleanup := newToolsTestPool(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	if _, err := pool.Exec(ctx, markedReadersSchema); err != nil {
		t.Fatalf("create alert tables: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), markedReadersTeardown); err != nil {
			t.Logf("drop alert tables: %v", err)
		}
	})

	f := &markedFixture{pool: pool, ds: ds}
	for _, c := range []struct {
		name string
		id   *int
	}{{"live-conn", &f.live}, {"gone-conn", &f.gone}} {
		if err := pool.QueryRow(ctx, `
            INSERT INTO connections (name, host, owner_username, is_shared)
            VALUES ($1, '192.0.2.1', 'test-owner', TRUE)
            RETURNING id`, c.name).Scan(c.id); err != nil {
			t.Fatalf("seed %s: %v", c.name, err)
		}
		if _, err := pool.Exec(ctx, `
            INSERT INTO alerts (connection_id, title, metric_name)
            VALUES ($1, $2, 'pg_connectivity.response_time_ms')`, *c.id, c.name+" alert"); err != nil {
			t.Fatalf("seed alert for %s: %v", c.name, err)
		}
		if _, err := pool.Exec(ctx, `
            INSERT INTO metric_baselines (connection_id, metric_name, period_type, mean)
            VALUES ($1, 'pg_connectivity.response_time_ms', 'all', 1.5)`, *c.id); err != nil {
			t.Fatalf("seed baseline for %s: %v", c.name, err)
		}
	}
	if _, err := pool.Exec(ctx,
		`UPDATE connections SET deleted_at = now(), is_monitored = FALSE WHERE id = $1`, f.gone); err != nil {
		t.Fatalf("mark connection: %v", err)
	}
	return f
}

// toolText runs a tool handler and returns its text and error flag.
func toolText(t *testing.T, tool Tool, args map[string]any) (string, bool) {
	t.Helper()
	resp, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if len(resp.Content) == 0 {
		t.Fatal("handler returned no content")
	}
	return resp.Content[0].Text, resp.IsError
}

func TestMarkedConnectionHiddenFromToolsIntegration(t *testing.T) {
	f := newMarkedFixture(t)
	rbac := testRBACChecker(t)
	lister := database.NewVisibilityLister(f.ds)

	t.Run("list_connections", func(t *testing.T) {
		text, isErr := toolText(t, asSuperuser(ListConnectionsTool(f.pool, rbac, lister)), map[string]any{})
		if isErr || !strings.Contains(text, "live-conn") || strings.Contains(text, "gone-conn") {
			t.Errorf("list_connections = %q (error %v)", text, isErr)
		}
	})

	// resolveAccessibleConnection: a superuser passes the access check,
	// so only the lookup's deleted_at test refuses the marked connection;
	// visibleConnectionSuggestions then lists only the live one.
	for name, tool := range map[string]Tool{
		"get_alert_history":    GetAlertHistoryTool(f.pool, rbac, lister),
		"get_metric_baselines": GetMetricBaselinesTool(f.pool, rbac, lister),
	} {
		t.Run(name+" names the marked connection", func(t *testing.T) {
			text, isErr := toolText(t, asSuperuser(tool), map[string]any{"connection_id": float64(f.gone)})
			if !isErr || !strings.Contains(text, connectionNotAccessibleMsg) {
				t.Fatalf("expected the generic denial, got %q", text)
			}
			if !strings.Contains(text, "live-conn") || strings.Contains(text, "gone-conn") {
				t.Errorf("suggestions should list only the live connection: %q", text)
			}
		})
	}

	t.Run("get_alert_history across connections", func(t *testing.T) {
		text, isErr := toolText(t, asSuperuser(GetAlertHistoryTool(f.pool, rbac, lister)), map[string]any{"status": "all"})
		if isErr || !strings.Contains(text, "live-conn alert") || strings.Contains(text, "gone-conn") {
			t.Errorf("get_alert_history = %q (error %v)", text, isErr)
		}
	})

	t.Run("get_metric_baselines across connections", func(t *testing.T) {
		text, isErr := toolText(t, asSuperuser(GetMetricBaselinesTool(f.pool, rbac, lister)), map[string]any{})
		if isErr || !strings.Contains(text, "live-conn") || strings.Contains(text, "gone-conn") {
			t.Errorf("get_metric_baselines = %q (error %v)", text, isErr)
		}
	})

	t.Run("query_metrics names the marked connection", func(t *testing.T) {
		text, isErr := toolText(t, asSuperuser(QueryMetricsTool(f.pool, rbac)), map[string]any{
			"connection_id": float64(f.gone), "probe_name": "pg_connectivity"})
		if !isErr || !strings.Contains(text, "connection not found or not accessible") {
			t.Errorf("query_metrics = %q (error %v)", text, isErr)
		}
	})
}

// TestMarkedConnectionSuggestionsForUserIntegration covers the
// suggestion query a caller with a limited visible set gets: it is
// given the IDs and must still drop a marked connection among them.
func TestMarkedConnectionSuggestionsForUserIntegration(t *testing.T) {
	f := newMarkedFixture(t)

	store, cleanup := newRBACTestStore(t)
	defer cleanup()
	if err := store.CreateUser("test-user", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := store.GetUserID("test-user")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	rbac := auth.NewRBACChecker(store)

	// A lister that still reports the marked connection, as one racing
	// the mark might, so the suggestion query's own test is what drops it.
	lister := staleLister{ids: []int{f.live, f.gone}}
	got := visibleConnectionSuggestions(nonSuperuserContextInt(userID, "test-user"), f.pool, rbac, lister)
	if len(got) != 1 || !strings.Contains(got[0], "live-conn") {
		t.Errorf("suggestions = %v, want only live-conn", got)
	}
}

// staleLister reports a fixed set of shared connections.
type staleLister struct{ ids []int }

func (l staleLister) GetAllConnections(_ context.Context) ([]auth.ConnectionVisibilityInfo, error) {
	out := make([]auth.ConnectionVisibilityInfo, 0, len(l.ids))
	for _, id := range l.ids {
		out = append(out, auth.ConnectionVisibilityInfo{ID: id, IsShared: true, OwnerUsername: "test-owner"})
	}
	return out, nil
}
