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
)

// On a pgEdge ColdFront datastore every metrics.* relation is a view over
// Apache Iceberg storage rather than a partitioned table (issue #755).
// These tests stand a plain view in for a ColdFront one, which the
// catalog reports the same way (relkind 'v', table_type VIEW), and check
// that every probe tool finds and reads it. A view is served only under a
// known probe's name (metrics.AdmitsProbeRelation), so the stand-ins take
// the names of real probes no other fixture here seeds, and a view under
// any other name must be refused.
const (
	cfProbe     = "pg_stat_recovery_prefetch"
	dsViewProbe = "pg_statio_all_sequences"
)

// TestProbeToolsAcceptViewsIntegration covers describe_probe and
// query_metrics, which check that the probe exists before reading it.
func TestProbeToolsAcceptViewsIntegration(t *testing.T) {
	f := newMarkedFixture(t)
	ctx := context.Background()

	if _, err := f.pool.Exec(ctx, `
		CREATE TABLE metrics.cf_base (
		    connection_id INTEGER NOT NULL,
		    collected_at TIMESTAMPTZ NOT NULL,
		    response_time_ms DOUBLE PRECISION
		);
		CREATE VIEW metrics.pg_stat_recovery_prefetch AS SELECT * FROM metrics.cf_base;
		COMMENT ON VIEW metrics.pg_stat_recovery_prefetch IS 'ColdFront stand-in probe';
		CREATE VIEW metrics.cf_report AS SELECT * FROM metrics.cf_base;`); err != nil {
		t.Fatalf("create probe view: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO metrics.cf_base VALUES
		    ($1, now() - interval '5 minutes', 2.5),
		    ($1, now() - interval '2 minutes', 2.5)`, f.live); err != nil {
		t.Fatalf("seed probe view: %v", err)
	}

	t.Run("describe_probe", func(t *testing.T) {
		text, isErr := toolText(t, DescribeProbeTool(f.pool), map[string]any{
			"__context": ctx, "probe_name": cfProbe})
		if isErr {
			t.Fatalf("describe_probe on a view failed: %s", text)
		}
		if !strings.Contains(text, "response_time_ms") ||
			!strings.Contains(text, "ColdFront stand-in probe") {
			t.Errorf("describe_probe should describe the view's columns: %s", text)
		}
	})

	t.Run("describe_probe without an injected context", func(t *testing.T) {
		text, isErr := toolText(t, DescribeProbeTool(f.pool), map[string]any{"probe_name": cfProbe})
		if isErr || !strings.Contains(text, "Probe: "+cfProbe) {
			t.Errorf("describe_probe = %q (error %v)", text, isErr)
		}
	})

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	for _, tt := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"missing probe_name", map[string]any{"__context": ctx}, "Missing or invalid 'probe_name'"},
		{"invalid probe_name", map[string]any{"__context": ctx, "probe_name": "cf-probe"}, "Invalid probe name"},
		{"unknown probe", map[string]any{"__context": ctx, "probe_name": "no_such_probe"}, "not found"},
		{"view under another name", map[string]any{"__context": ctx, "probe_name": "cf_report"}, "not found"},
		{"existence check fails", map[string]any{"__context": canceled, "probe_name": cfProbe}, "Failed to check probe existence"},
	} {
		t.Run("describe_probe "+tt.name, func(t *testing.T) {
			text, isErr := toolText(t, DescribeProbeTool(f.pool), tt.args)
			if !isErr || !strings.Contains(text, tt.want) {
				t.Errorf("describe_probe = %q (error %v), want an error containing %q", text, isErr, tt.want)
			}
		})
	}

	t.Run("query_metrics", func(t *testing.T) {
		text, isErr := toolText(t, asSuperuser(QueryMetricsTool(f.pool, testRBACChecker(t), nil)),
			map[string]any{"connection_id": float64(f.live), "probe_name": cfProbe})
		if isErr {
			t.Fatalf("query_metrics on a view failed: %s", text)
		}
		if !strings.Contains(text, "Probe: "+cfProbe) || !strings.Contains(text, "2.5") {
			t.Errorf("query_metrics should read the view's rows: %s", text)
		}
	})

	t.Run("query_metrics refuses a view under another name", func(t *testing.T) {
		text, isErr := toolText(t, asSuperuser(QueryMetricsTool(f.pool, testRBACChecker(t), nil)),
			map[string]any{"connection_id": float64(f.live), "probe_name": "cf_report"})
		if !isErr || !strings.Contains(text, "not found") {
			t.Errorf("query_metrics on a renamed view = %q (error %v), want not found", text, isErr)
		}
	})
}

// TestDatastoreToolsAcceptViewsIntegration covers describe_datastore and
// query_datastore, which discover metric sources from pg_class, and
// checks that a view is still scoped to the caller's connections.
func TestDatastoreToolsAcceptViewsIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	ctx := context.Background()

	if _, err := f.pool.Exec(ctx, `
		CREATE VIEW metrics.pg_statio_all_sequences AS SELECT * FROM metrics.ds_probe;
		COMMENT ON VIEW metrics.pg_statio_all_sequences IS 'ColdFront stand-in source.';
		CREATE VIEW metrics.ds_report AS SELECT * FROM metrics.ds_probe;`); err != nil {
		t.Fatalf("create source view: %v", err)
	}

	text, isErr := call(t, DescribeDatastoreTool(f.pool), superuserContext(), map[string]any{})
	if isErr || !strings.Contains(text, dsViewProbe+"\tmetric\tcollected_at\tconnection\t") {
		t.Fatalf("describe_datastore should list the view as a metric source: %s", text)
	}
	if strings.Contains(text, "ds_report") {
		t.Errorf("describe_datastore should not list a view under another name: %s", text)
	}
	if text, isErr := call(t, f.tool(), f.userCtx(), map[string]any{"source": "ds_report"}); !isErr ||
		!strings.Contains(text, "unknown source") {
		t.Errorf("query_datastore on a renamed view = %q (error %v), want unknown source", text, isErr)
	}

	rows := mustQuery(t, f.tool(), f.userCtx(), map[string]any{"source": dsViewProbe})
	if len(rows) == 0 {
		t.Fatal("query_datastore on the view returned no rows")
	}
	for _, row := range rows {
		if row["numbackends"] == "9" {
			t.Errorf("the view leaked a row of a connection the caller cannot read: %v", row)
		}
	}
}
