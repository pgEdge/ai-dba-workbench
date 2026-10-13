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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/database"
)

// seedHiddenDatabase adds two more shared connections, so the restricted
// caller sees three, and gives the hidden connection a database name no
// visible row has. It returns the three visible connection IDs.
func seedHiddenDatabase(t *testing.T, f *datastoreFixture) []int {
	t.Helper()
	visible := []int{
		f.visible,
		seedBaselineConnection(t, f.pool, "visible-conn-2", true, ""),
		seedBaselineConnection(t, f.pool, "visible-conn-3", true, ""),
	}
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO metrics.ds_probe (connection_id, collected_at, datname, numbackends, xact_commit) VALUES
            ($1, now() - interval '5 minutes', 'other', 1, 1),
            ($2, now() - interval '5 minutes', 'other', 1, 1),
            ($3, now() - interval '5 minutes', 'hiddendb', 4, 4)`,
		visible[1], visible[2], f.hidden); err != nil {
		t.Fatalf("seed hidden database: %v", err)
	}
	return visible
}

// TestQueryDatastoreHiddenRowsIntegration shows that a LIKE pattern with
// a trailing escape, which PostgreSQL rejects only when it compares the
// pattern with a matching row, tells a restricted caller nothing about a
// connection it cannot see. The validation, the scoped inner query and
// the general error message are each checked on their own.
func TestQueryDatastoreHiddenRowsIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	visible := seedHiddenDatabase(t, f)
	user := f.userCtx()
	ctx := context.Background()

	ids, all, err := f.checker.VisibleConnectionIDs(user, database.NewVisibilityLister(f.ds))
	if err != nil || all {
		t.Fatalf("VisibleConnectionIDs = %v, %v, %v", ids, all, err)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, visible) {
		t.Fatalf("the restricted caller should see %v, got %v", visible, ids)
	}

	// The hidden row exists, and an unscoped query for it raises the
	// error the attack relies on.
	if _, err := f.pool.Exec(ctx,
		`SELECT count(*) FROM metrics.ds_probe WHERE datname LIKE $1`, `hidden%\`); err == nil {
		t.Fatal("an unscoped trailing-escape pattern should fail on the hidden row")
	}

	t.Run("validation rejects the pattern", func(t *testing.T) {
		text, isErr := call(t, f.tool(), user, map[string]any{
			"source":  "ds_probe",
			"filters": []any{map[string]any{"column": "datname", "op": "like", "value": `hidden%\`}},
		})
		if !isErr || !strings.Contains(text, "must not end with a single backslash") {
			t.Errorf("got (error=%v) %s", isErr, text)
		}
		if strings.Contains(text, "hiddendb") {
			t.Errorf("the refusal names hidden data: %s", text)
		}
	})

	t.Run("scope is applied before the caller's filters", func(t *testing.T) {
		src, err := lookupDatastoreSource(ctx, f.pool, "ds_probe")
		if err != nil {
			t.Fatalf("lookup: %v", err)
		}
		const placeholder = "placeholder%"
		for _, shape := range []string{"rows", "latest", "aggregate"} {
			args := map[string]any{
				"source":  "ds_probe",
				"shape":   shape,
				"filters": []any{map[string]any{"column": "datname", "op": "like", "value": placeholder}},
			}
			if shape == "aggregate" {
				args["group_by"] = []any{"datname"}
				args["aggregation"] = "count"
			}
			req, err := parseDatastoreRequest(args)
			if err != nil {
				t.Fatalf("%s: parse: %v", shape, err)
			}
			sql, queryArgs, err := buildDatastoreQuery(src, req, visibleSet{IDs: ids}, time.Now())
			if err != nil {
				t.Fatalf("%s: build: %v", shape, err)
			}
			// Swap in the pattern the validation would refuse, so this
			// checks the SQL alone.
			replaced := false
			for i, a := range queryArgs {
				if a == placeholder {
					queryArgs[i] = `hidden%\`
					replaced = true
				}
			}
			if !replaced {
				t.Fatalf("%s: the pattern is not a bound argument: %v", shape, queryArgs)
			}
			rows, err := f.pool.Query(ctx, sql, queryArgs...)
			if err != nil {
				t.Fatalf("%s: query: %v", shape, err)
			}
			n := 0
			for rows.Next() {
				n++
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				t.Errorf("%s: the caller's filter reached a hidden row: %v", shape, err)
			}
			if n != 0 {
				t.Errorf("%s: got %d rows, want none", shape, n)
			}
		}
	})

	t.Run("an equality filter on hidden data finds nothing", func(t *testing.T) {
		rows := mustQuery(t, f.tool(), user, map[string]any{
			"source":  "ds_probe",
			"filters": []any{map[string]any{"column": "datname", "op": "=", "value": "hiddendb"}},
		})
		if len(rows) != 0 {
			t.Errorf("got hidden rows: %v", rows)
		}
	})

	t.Run("explain is refused, so plan estimates cannot count hidden rows", func(t *testing.T) {
		text, isErr := call(t, f.tool(), user, map[string]any{
			"source":  "ds_probe",
			"explain": true,
			"filters": []any{map[string]any{"column": "datname", "op": "like", "value": "hidden%"}},
		})
		if !isErr || text != errDatastoreExplainRestricted.Error() {
			t.Errorf("got (error=%v) %q", isErr, text)
		}
	})

	t.Run("a failing statement gets a general message", func(t *testing.T) {
		text, isErr := call(t, f.tool(), user, map[string]any{
			"source":  "ds_probe",
			"filters": []any{map[string]any{"column": "collected_at", "op": ">", "value": "not-a-time"}},
		})
		if !isErr || text != datastoreQueryFailedMsg {
			t.Errorf("got (error=%v) %q, want %q", isErr, text, datastoreQueryFailedMsg)
		}
		text, isErr = call(t, f.tool(), superuserContext(), map[string]any{
			"source":  "ds_probe",
			"explain": true,
			"filters": []any{map[string]any{"column": "collected_at", "op": ">", "value": "not-a-time"}},
		})
		if !isErr || text != datastoreQueryFailedMsg {
			t.Errorf("explain: got (error=%v) %q, want %q", isErr, text, datastoreQueryFailedMsg)
		}
	})
}

// TestQueryDatastoreReviewBehaviourIntegration covers the hints, the
// "now" time value and the unrestricted caller naming a connection that
// does not exist.
func TestQueryDatastoreReviewBehaviourIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	user := f.userCtx()

	t.Run("zero-row hint on the default window", func(t *testing.T) {
		text, isErr := call(t, f.tool(), user, map[string]any{
			"source":  "ds_probe",
			"filters": []any{map[string]any{"column": "datname", "op": "=", "value": "absent"}},
		})
		if isErr || !strings.Contains(text, datastoreDefaultWindowHint) {
			t.Errorf("got (error=%v) %s", isErr, text)
		}
		text, isErr = call(t, f.tool(), user, map[string]any{
			"source":     "ds_probe",
			"time_start": "30d",
			"filters":    []any{map[string]any{"column": "datname", "op": "=", "value": "absent"}},
		})
		if isErr || strings.Contains(text, "No rows in the default window") {
			t.Errorf("an explicit time_start should not get the hint: (error=%v) %s", isErr, text)
		}
		text, _ = call(t, f.tool(), user, map[string]any{"source": "ds_probe"})
		if strings.Contains(text, "No rows in the default window") {
			t.Errorf("a non-empty page should not get the hint: %s", text)
		}
	})

	t.Run("now as a time filter value", func(t *testing.T) {
		if _, err := f.pool.Exec(context.Background(),
			`UPDATE blackouts SET end_time = now() + interval '1 hour' WHERE id = 1`); err != nil {
			t.Fatalf("seed end_time: %v", err)
		}
		rows := mustQuery(t, f.tool(), superuserContext(), map[string]any{
			"source":  "blackouts",
			"filters": []any{map[string]any{"column": "end_time", "op": ">", "value": "now"}},
		})
		assertColumn(t, rows, "id", "1")
	})

	t.Run("unrestricted caller naming a missing connection", func(t *testing.T) {
		missing, _ := call(t, f.tool(), superuserContext(), map[string]any{
			"source": "ds_probe", "connection_ids": []any{99999},
		})
		hidden, _ := call(t, f.tool(), user, map[string]any{
			"source": "ds_probe", "connection_ids": []any{f.hidden},
		})
		const denial = "connection not found or not accessible."
		if !strings.HasPrefix(missing, denial) || !strings.HasPrefix(hidden, denial) {
			t.Errorf("a missing connection should get the hidden-connection answer:\nmissing: %s\nhidden: %s", missing, hidden)
		}
		rows := mustQuery(t, f.tool(), superuserContext(), map[string]any{
			"source": "ds_probe", "connection_ids": []any{f.hidden},
		})
		assertColumn(t, rows, "datname", "app")
	})

	t.Run("no context", func(t *testing.T) {
		resp, err := f.tool().Handler(map[string]any{"source": "alert_rules"})
		if err != nil || resp.IsError {
			t.Errorf("no context: %v %+v", err, resp)
		}
	})

	t.Run("pages are stable", func(t *testing.T) {
		args := func(page int) map[string]any {
			return map[string]any{
				"source": "ds_probe", "time_start": "1h", "page_size": 1, "page": page,
				"columns":  []any{"datname"},
				"order_by": []any{map[string]any{"column": "connection_id"}},
			}
		}
		var got []string
		for page := 1; page <= 3; page++ {
			got = append(got, column(mustQuery(t, f.tool(), user, args(page)), "datname")...)
		}
		if strings.Join(got, ",") != "app,app,postgres" {
			t.Errorf("tied rows should page in key order, got %v", got)
		}
	})
}

// TestDescribeDatastorePartitionedIntegration covers a partitioned probe
// table: discovery lists the parent only, and a time-windowed query
// plans only the partitions in the window, through the scoped inner
// query.
func TestDescribeDatastorePartitionedIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	ctx := context.Background()
	// A partition bound may be any variable-free expression, evaluated
	// once when the partition is created, so the current partition spans
	// the two days either side of the server's clock.
	stmts := []fixtureStatement{
		{sql: `CREATE TABLE metrics.ds_part (
            connection_id INTEGER NOT NULL,
            collected_at TIMESTAMPTZ NOT NULL,
            value BIGINT
        ) PARTITION BY RANGE (collected_at)`},
		{sql: `CREATE TABLE metrics.ds_part_old PARTITION OF metrics.ds_part
            FOR VALUES FROM ('2000-01-01 00:00:00+00') TO ('2000-02-01 00:00:00+00')`},
		{sql: `CREATE TABLE metrics.ds_part_current PARTITION OF metrics.ds_part
            FOR VALUES FROM (now() - interval '48 hours') TO (now() + interval '48 hours')`},
		{sql: `CREATE INDEX ON metrics.ds_part (connection_id, collected_at)`},
		{sql: `INSERT INTO metrics.ds_part VALUES
            ($1, '2000-01-15 00:00:00+00', 1), ($1, now() - interval '5 minutes', 2)`, args: []any{f.visible}},
	}
	for _, stmt := range stmts {
		if _, err := f.pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("schema %q: %v", stmt.sql, err)
		}
	}

	text, isErr := call(t, DescribeDatastoreTool(f.pool), superuserContext(), map[string]any{})
	if isErr || !strings.Contains(text, "ds_part\tmetric\tcollected_at\tconnection\t") {
		t.Fatalf("the partitioned parent should be listed: %s", text)
	}
	if strings.Contains(text, "ds_part_old") || strings.Contains(text, "ds_part_current") {
		t.Errorf("partitions should not be listed: %s", text)
	}

	rows := mustQuery(t, f.tool(), f.userCtx(), map[string]any{"source": "ds_part"})
	assertColumn(t, rows, "value", "2")

	plan, isErr := call(t, f.tool(), superuserContext(), map[string]any{"source": "ds_part", "explain": true})
	if isErr || !strings.Contains(plan, "ds_part_current") || strings.Contains(plan, "ds_part_old") {
		t.Errorf("a windowed query should plan only the current partition:\n%s", plan)
	}
}

// TestDatastoreSourceLookupErrorsIntegration covers the catalog lookups'
// failure paths: a database error is reported as such, and a metrics
// table whose name is not a valid identifier is never listed.
func TestDatastoreSourceLookupErrorsIntegration(t *testing.T) {
	f := newDatastoreFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx,
		`CREATE TABLE metrics."ds-bad-name" (connection_id INTEGER, collected_at TIMESTAMPTZ)`); err != nil {
		t.Fatalf("create badly named probe: %v", err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), `DROP TABLE IF EXISTS metrics."ds-bad-name"`); err != nil {
			t.Logf("drop badly named probe: %v", err)
		}
	})

	summaries, err := listDatastoreSources(ctx, f.pool)
	if err != nil {
		t.Fatalf("listDatastoreSources: %v", err)
	}
	for _, s := range summaries {
		if s.Name == "ds-bad-name" {
			t.Errorf("a table whose name is not a valid identifier should not be listed")
		}
	}

	doneCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := lookupDatastoreSource(doneCtx, f.pool, "ds_probe"); err == nil ||
		!strings.Contains(err.Error(), "failed to look up source") {
		t.Errorf("lookup on a canceled context = %v, want a look-up failure", err)
	}
	if _, err := lookupMetricColumns(doneCtx, f.pool, "ds_probe"); err == nil ||
		!strings.Contains(err.Error(), "failed to read source columns") {
		t.Errorf("column lookup on a canceled context = %v, want a read failure", err)
	}
}
