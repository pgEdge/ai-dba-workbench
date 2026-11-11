/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package metrics

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestLookupProbeRelationRefusesUnknownNames checks that a name that is
// not a known probe's is refused before any query: the querier is nil,
// so a query would panic. A collector partition, a renamed view and a
// table under another name all fall here, since the masking is keyed by
// the probe's exact name.
func TestLookupProbeRelationRefusesUnknownNames(t *testing.T) {
	for _, name := range []string{
		"pg_stat_activity_20261102",
		"pg_settings_20261026",
		"activity_report",
		"pg_stat_activity_report",
		"ds_masking_secret_probe",
		"",
	} {
		rel, ok, err := LookupProbeRelation(context.Background(), nil, name)
		if ok || err != nil || rel != (ProbeRelation{}) {
			t.Errorf("LookupProbeRelation(%q) = %+v, %v, %v; want refused", name, rel, ok, err)
		}
	}
}

// fakeCatalog is a CatalogQuerier that returns canned results.
type fakeCatalog struct {
	row     pgx.Row
	rows    pgx.Rows
	queryEr error
}

func (f fakeCatalog) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return f.rows, f.queryEr
}

func (f fakeCatalog) QueryRow(context.Context, string, ...any) pgx.Row { return f.row }

// errRow is a pgx.Row whose Scan fails with err.
type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

// fakeRows is a pgx.Rows over values, one []any per row, that fails
// Scan with scanErr and reports err once iteration ends.
type fakeRows struct {
	values  [][]any
	i       int
	scanErr error
	err     error
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return r.err }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return r.values[r.i-1], nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeRows) TypeMap() *pgtype.Map                         { return nil }

func (r *fakeRows) Next() bool {
	if r.i >= len(r.values) {
		return false
	}
	r.i++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	v := r.values[r.i-1]
	*dest[0].(*string) = v[0].(string)
	*dest[1].(*string) = v[1].(string)
	*dest[2].(*bool) = v[2].(bool)
	*dest[3].(*bool) = v[3].(bool)
	return nil
}

func TestLookupProbeRelationErrors(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")
	if _, ok, err := LookupProbeRelation(ctx, fakeCatalog{row: errRow{pgx.ErrNoRows}}, "pg_stat_activity"); ok || err != nil {
		t.Errorf("no rows: ok %v, err %v; want missing without error", ok, err)
	}
	if _, ok, err := LookupProbeRelation(ctx, fakeCatalog{row: errRow{boom}}, "pg_stat_activity"); ok || !errors.Is(err, boom) {
		t.Errorf("scan failure: ok %v, err %v; want boom", ok, err)
	}
}

func TestListProbeRelations(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")

	rows := &fakeRows{values: [][]any{
		{"pg_stat_activity", "Sessions", false, true},
		// Not known probes, so never listed: a renamed view and a
		// detached partition.
		{"activity_report", "", true, true},
		{"pg_stat_activity_20261102", "", false, true},
		{"pg_settings", "", true, true},
	}}
	got, err := ListProbeRelations(ctx, fakeCatalog{rows: rows})
	if err != nil {
		t.Fatalf("ListProbeRelations: %v", err)
	}
	var names []string
	for _, rel := range got {
		names = append(names, rel.Name)
	}
	if strings.Join(names, ",") != "pg_stat_activity,pg_settings" {
		t.Errorf("listed %v, want pg_stat_activity and pg_settings", names)
	}
	if !got[1].IsView || got[0].Description != "Sessions" {
		t.Errorf("relations read wrongly: %+v", got)
	}

	for name, q := range map[string]fakeCatalog{
		"query": {queryEr: boom},
		"scan":  {rows: &fakeRows{values: [][]any{{"pg_settings"}}, scanErr: boom}},
		"rows":  {rows: &fakeRows{err: boom}},
	} {
		if _, err := ListProbeRelations(ctx, q); !errors.Is(err, boom) {
			t.Errorf("%s failure: err %v, want boom", name, err)
		}
	}
}

// TestProbeRelationsOnCollectorSchema runs the catalog lookups against
// the schema the collector really creates, with a weekly partition of a
// masked probe, a renamed view of it and a table under another name
// beside it. Only the probe itself may resolve: the masking is keyed by
// the probe's exact name, so the others would serve it in clear.
func TestProbeRelationsOnCollectorSchema(t *testing.T) {
	pool := newMigratedMetricsPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
        CREATE TABLE metrics.pg_stat_activity_20261102
            PARTITION OF metrics.pg_stat_activity DEFAULT;
        CREATE VIEW metrics.activity_report AS SELECT * FROM metrics.pg_stat_activity;
        CREATE TABLE metrics.ds_extra (connection_id integer, collected_at timestamptz);`); err != nil {
		t.Fatalf("create the decoy relations: %v", err)
	}

	rel, ok, err := LookupProbeRelation(ctx, pool, "pg_stat_activity")
	if err != nil || !ok || rel.IsView || !rel.HasBookkeeping || rel.RowCount != nil {
		t.Errorf("pg_stat_activity = %+v, %v, %v; want a table with bookkeeping columns and no row count", rel, ok, err)
	}
	for _, name := range []string{"pg_stat_activity_20261102", "activity_report", "ds_extra"} {
		if _, ok, err := LookupProbeRelation(ctx, pool, name); ok || err != nil {
			t.Errorf("LookupProbeRelation(%q) = %v, %v; want refused", name, ok, err)
		}
	}

	list, err := ListProbeRelations(ctx, pool)
	if err != nil {
		t.Fatalf("ListProbeRelations: %v", err)
	}
	if len(list) != len(knownProbes) {
		t.Errorf("listed %d relations, want the %d known probes", len(list), len(knownProbes))
	}
	for _, rel := range list {
		if !IsKnownProbe(rel.Name) || rel.IsView || !rel.HasBookkeeping || rel.RowCount == nil {
			t.Errorf("listed %+v", rel)
		}
	}
}
