/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// statementDatabaseTestHandler returns a handler whose name lookup and
// time-series query are fakes. The lookup knows dbids 100 (alpha), 300
// (gamma) and 301 (alpha again, as after a drop and re-create) and
// records the connection it was asked about; the query records the
// filters reaching it.
func statementDatabaseTestHandler(t *testing.T) (
	*MetricsHandler, *int, *metrics.MetricFilters, *bool,
) {
	t.Helper()
	lookedUp := -1
	var got metrics.MetricFilters
	queried := false
	h := &MetricsHandler{
		authStore:       newTestAuthStore(t),
		datastore:       &database.Datastore{},
		sharingLookupFn: liveConnectionLookup,
		statementDatabaseLookupFn: func(_ context.Context, _ *pgxpool.Pool,
			connID int) (map[int64]string, error) {
			lookedUp = connID
			return map[int64]string{100: "alpha", 300: "gamma", 301: "alpha"}, nil
		},
		queryTimeSeriesFn: func(_ context.Context, _ *pgxpool.Pool, _ string,
			_ []int, _ metrics.TimeWindow, filters metrics.MetricFilters,
			_ int, _ string, _ []string) (*metrics.MetricsQueryResult, error) {
			queried = true
			got = filters
			return &metrics.MetricsQueryResult{Series: []metrics.MetricSeries{}}, nil
		},
	}
	return h, &lookedUp, &got, &queried
}

func serveMetricsQuery(h *MetricsHandler, query string) *httptest.ResponseRecorder {
	req := withSuperuser(httptest.NewRequest(http.MethodGet,
		"/api/v1/metrics/query?"+query, nil))
	rec := httptest.NewRecorder()
	h.handleMetricsQuery(rec, req)
	return rec
}

func TestHandleMetricsQuery_StatementDatabaseResolved(t *testing.T) {
	h, lookedUp, got, queried := statementDatabaseTestHandler(t)
	rec := serveMetricsQuery(h, "connection_id=7&probe_name=pg_stat_statements"+
		"&queryid=42&database_name=postgres&statement_database_name=alpha")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (body %q)", rec.Code, rec.Body.String())
	}
	if !*queried || *lookedUp != 7 {
		t.Fatalf("queried = %v, lookup connection = %d", *queried, *lookedUp)
	}
	sd := got.StatementDatabase
	if sd == nil {
		t.Fatal("StatementDatabase did not reach the query layer")
	}
	if sd.Name != "alpha" || !slices.Equal(sd.DBIDs, []int64{100, 301}) ||
		!slices.Equal(sd.ResolvedDBIDs, []int64{100, 300, 301}) {
		t.Errorf("filter = %#v", sd)
	}
	// The probing database filter is independent and still applies.
	if got.DatabaseName != "postgres" {
		t.Errorf("DatabaseName = %q, want postgres", got.DatabaseName)
	}
}

func TestHandleMetricsQuery_StatementDatabaseUnknownName(t *testing.T) {
	// A name the lookup does not know matches no resolved dbid, so only
	// unresolved dbids recorded under that probing name can match.
	h, _, got, _ := statementDatabaseTestHandler(t)
	rec := serveMetricsQuery(h, "connection_id=7&probe_name=pg_stat_statements"+
		"&statement_database_name=dropped")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (body %q)", rec.Code, rec.Body.String())
	}
	if sd := got.StatementDatabase; sd == nil || sd.DBIDs == nil || len(sd.DBIDs) != 0 {
		t.Errorf("filter = %#v, want an empty, non-nil DBIDs", sd)
	}
}

func TestHandleMetricsQuery_StatementDatabaseAbsent(t *testing.T) {
	h, lookedUp, got, queried := statementDatabaseTestHandler(t)
	rec := serveMetricsQuery(h, "connection_id=7&probe_name=pg_stat_statements&queryid=42")
	if rec.Code != http.StatusOK || !*queried {
		t.Fatalf("status %d, queried %v", rec.Code, *queried)
	}
	if *lookedUp != -1 || got.StatementDatabase != nil {
		t.Errorf("an absent parameter must not run the lookup or set the filter")
	}
}

func TestHandleMetricsQuery_StatementDatabaseRejected(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"empty", "connection_id=7&probe_name=pg_stat_statements&statement_database_name=",
			"Invalid statement_database_name: database name must not be empty"},
		{"control character", "connection_id=7&probe_name=pg_stat_statements" +
			"&statement_database_name=" + url.QueryEscape("al\x00pha"),
			"Invalid statement_database_name: database name must not contain control characters"},
		{"too long", "connection_id=7&probe_name=pg_stat_statements" +
			"&statement_database_name=" + strings.Repeat("a", 64),
			"Invalid statement_database_name: database name must be 63 bytes or fewer"},
		{"other probe", "connection_id=7&probe_name=pg_stat_database&statement_database_name=alpha",
			"statement_database_name applies only to probe_name pg_stat_statements"},
		{"several connections", "connection_ids=7,8&probe_name=pg_stat_statements" +
			"&statement_database_name=alpha",
			"statement_database_name requires exactly one connection_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, lookedUp, _, queried := statementDatabaseTestHandler(t)
			rec := serveMetricsQuery(h, tc.query)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d (body %q)", rec.Code, rec.Body.String())
			}
			if resp := decodeError(t, rec); resp.Error != tc.want {
				t.Errorf("error = %q, want %q", resp.Error, tc.want)
			}
			if *lookedUp != -1 || *queried {
				t.Error("a rejected parameter must not reach the database")
			}
		})
	}
}

func TestHandleMetricsQuery_StatementDatabaseLookupFails(t *testing.T) {
	h, _, _, queried := statementDatabaseTestHandler(t)
	h.statementDatabaseLookupFn = func(context.Context, *pgxpool.Pool, int) (map[int64]string, error) {
		return nil, errors.New("relation does not exist")
	}
	rec := serveMetricsQuery(h, "connection_id=7&probe_name=pg_stat_statements"+
		"&statement_database_name=alpha")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d (body %q)", rec.Code, rec.Body.String())
	}
	if resp := decodeError(t, rec); resp.Error != "Failed to resolve statement_database_name" {
		t.Errorf("error = %q", resp.Error)
	}
	if *queried {
		t.Error("the query must not run when the lookup fails")
	}
}

func TestHandleMetricsQuery_LatestRowsStatementDatabaseRejected(t *testing.T) {
	// The latest-row path applies the same validation before it touches
	// the pool.
	h, lookedUp, _, _ := statementDatabaseTestHandler(t)
	rec := serveMetricsQuery(h, "connection_id=7&probe_name=pg_stat_statements"+
		"&limit=5&statement_database_name=")
	if rec.Code != http.StatusBadRequest || *lookedUp != -1 {
		t.Fatalf("status %d, lookup connection %d", rec.Code, *lookedUp)
	}
}

// newStatementDatabaseMetricsHandler seeds the issue #802 fixture used
// by TestTopQueries_UnresolvedDbidSharesItsFallbackRow under queryid
// 7101, and returns a metrics handler on the same pool together with the
// top-queries handler. alpha is dbid 100 plus the unresolved dbid 400,
// whose fallback under probing database alpha is alpha; gamma is dbid 300.
func newStatementDatabaseMetricsHandler(t *testing.T) (
	*MetricsHandler, *PerfSummaryHandler, func(),
) {
	t.Helper()
	ph, pool, cleanup := newTopQueriesTestHandler(t)
	latest := seedPerDatabaseCounters(t, pool, 7101, []perDatabaseCounter{
		{dbID: 100, calls: 30},
		{dbID: 400, calls: 12},
		{dbID: 300, calls: 5},
	})
	seedStatDatabase(t, pool, latest, 100, "alpha")
	seedStatDatabase(t, pool, latest, 300, "gamma")
	seedStatementsProbeInterval(t, pool)
	mh := NewMetricsHandler(database.NewTestDatastore(pool), newTestAuthStore(t))
	mh.sharingLookupFn = liveConnectionLookup
	return mh, ph, cleanup
}

// seedStatementsProbeInterval gives pg_stat_statements the five-minute
// interval its fixture collections are spaced at, which the time-series
// path reads from probe_configs. It creates a minimal probe_configs when
// none exists and drops it again afterwards, so fixtures elsewhere in the
// package that create the full table still can.
func seedStatementsProbeInterval(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	var exists bool
	if err := pool.QueryRow(ctx,
		"SELECT to_regclass('public.probe_configs') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatalf("probe_configs check failed: %v", err)
	}
	if !exists {
		if _, err := pool.Exec(ctx, `CREATE TABLE public.probe_configs (
            id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
            connection_id INTEGER,
            name TEXT NOT NULL,
            collection_interval_seconds INTEGER NOT NULL,
            scope TEXT NOT NULL)`); err != nil {
			t.Fatalf("probe_configs create failed: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO public.probe_configs
        (connection_id, name, collection_interval_seconds, scope)
        VALUES (NULL, 'pg_stat_statements', 300, 'global')`); err != nil {
		t.Fatalf("probe_configs seed failed: %v", err)
	}
	t.Cleanup(func() {
		if exists {
			_, _ = pool.Exec(context.Background(), `DELETE FROM public.probe_configs
                WHERE name = 'pg_stat_statements' AND scope = 'global'
                  AND collection_interval_seconds = 300`)
			return
		}
		_, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS public.probe_configs")
	})
}

// sumSeries adds up every non-null point of the named series.
func sumSeries(t *testing.T, rec *httptest.ResponseRecorder, metric string) float64 {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (body %q)", rec.Code, rec.Body.String())
	}
	var result metrics.MetricsQueryResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	total := 0.0
	for _, s := range result.Series {
		if s.Metric != metric {
			continue
		}
		for _, p := range s.Data {
			if p.Value != nil && !p.Filled {
				total += *p.Value
			}
		}
		return total
	}
	t.Fatalf("no %s series in %#v", metric, result.Series)
	return 0
}

// TestMetricsQuery_StatementDatabaseMatchesTopQueries is the chart side of
// issue #802: the query detail charts filter on the probing database
// (database_name) and the queryid, and one probing database carries every
// database's counters for the queryid, so a chart opened from one Top
// Queries row summed the calls of every row. With statement_database_name
// the chart counts only that row's counters, including an unresolved dbid
// the row counts under its fallback name.
func TestMetricsQuery_StatementDatabaseMatchesTopQueries(t *testing.T) {
	mh, ph, cleanup := newStatementDatabaseMetricsHandler(t)
	defer cleanup()

	rows, _ := decodeTopQueries(t, callTopQueries(t, ph,
		"connection_id=4242&time_range=1h&queryid=7101"))
	want := map[string]float64{}
	for _, row := range rows {
		want[row.DatabaseName] = float64(row.Calls)
	}
	if want["alpha"] != 42 || want["gamma"] != 5 {
		t.Fatalf("top-queries rows = %#v", rows)
	}

	base := "connection_id=4242&probe_name=pg_stat_statements&time_range=1h" +
		"&queryid=7101&database_name=alpha&metrics=calls&aggregation=sum"
	if got := sumSeries(t, serveMetricsQuery(mh, base), "calls"); got != 47 {
		t.Errorf("unfiltered chart = %v calls, want every database's 47", got)
	}
	for name, calls := range want {
		got := sumSeries(t, serveMetricsQuery(mh, base+"&statement_database_name="+name), "calls")
		if got != calls {
			t.Errorf("%s chart = %v calls, want the row's %v", name, got, calls)
		}
	}

	// The derived rate runs through the same filter: gamma's five calls
	// over the five minutes between the collections.
	rec := serveMetricsQuery(mh, "connection_id=4242&probe_name=pg_stat_statements"+
		"&time_range=1h&queryid=7101&database_name=alpha&metrics=calls_per_sec"+
		"&aggregation=sum&statement_database_name=gamma")
	if got := sumSeries(t, rec, "calls_per_sec"); got < 5.0/300-1e-9 || got > 5.0/300+1e-9 {
		t.Errorf("gamma calls_per_sec = %v, want %v", got, 5.0/300)
	}
}

// TestMetricsQuery_LatestRowsStatementDatabase checks the latest-row mode
// against the database: only gamma's dbid comes back.
func TestMetricsQuery_LatestRowsStatementDatabase(t *testing.T) {
	mh, _, cleanup := newStatementDatabaseMetricsHandler(t)
	defer cleanup()

	rec := serveMetricsQuery(mh, "connection_id=4242&probe_name=pg_stat_statements"+
		"&limit=10&queryid=7101&database_name=alpha&statement_database_name=gamma")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (body %q)", rec.Code, rec.Body.String())
	}
	var rows []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 || rows[0]["dbid"] != float64(300) {
		t.Errorf("rows = %#v, want gamma's dbid 300 alone", rows)
	}
}

// TestLookupStatementDatabases_QueryFails proves a failed lookup query is
// returned to the caller, which answers 500, rather than read as an empty
// lookup that would quietly widen the filter to the probing database.
func TestLookupStatementDatabases_QueryFails(t *testing.T) {
	_, pool, cleanup := newTopQueriesTestHandler(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if names, err := lookupStatementDatabases(ctx, pool, 1); err == nil {
		t.Fatalf("lookup on a finished context returned %v, want an error", names)
	}
}
