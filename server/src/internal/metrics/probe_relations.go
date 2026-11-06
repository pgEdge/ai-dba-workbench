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
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CatalogQuerier is the part of a pgx pool the probe catalog lookups use.
type CatalogQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// ProbeRelation is a relation in the metrics schema that the server may
// serve as a probe.
type ProbeRelation struct {
	// Name is the probe's name, which is also the relation's.
	Name string
	// Description is the relation's comment, or empty.
	Description string
	// IsView reports a view, as every probe is on a pgEdge ColdFront
	// datastore (issue #755); on plain PostgreSQL a probe is a table.
	IsView bool
	// HasBookkeeping reports that the relation carries the
	// connection_id and collected_at columns every probe has.
	HasBookkeeping bool
	// RowCount is the planner's estimate of the rows a table holds,
	// summed over the leaves of a partitioned one, or nil for a view: a
	// view has no statistics, and count(*) would scan the probe's whole
	// history in object storage. Only ListProbeRelations fills it.
	RowCount *int64
}

// probeRelationColumns and probeRelationSource read the probe relations
// from the catalog alone. They must never read a metrics relation
// itself: on ColdFront a statement that reads a view is planned by
// DuckDB, which cannot run the catalog functions used here.
//
// A relation is a candidate when it is a table, a partitioned table or a
// view (relkind 'r', 'p' or 'v'), is not a partition, and the datastore
// role can SELECT from it. Partitions are excluded by catalog state
// (relispartition), not by name (issue #644). The Go side then admits a
// candidate only under a known probe's name (IsKnownProbe).
const probeRelationColumns = `
        SELECT
            c.relname::text,
            COALESCE(obj_description(c.oid, 'pg_class'), ''),
            c.relkind = 'v',
            EXISTS (SELECT 1 FROM pg_attribute a
                    WHERE a.attrelid = c.oid AND a.attname = 'connection_id'
                        AND a.attnum > 0 AND NOT a.attisdropped)
                AND EXISTS (SELECT 1 FROM pg_attribute a
                    WHERE a.attrelid = c.oid AND a.attname = 'collected_at'
                        AND a.attnum > 0 AND NOT a.attisdropped),`

const probeRelationSource = `
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'metrics'
            AND c.relkind IN ('r', 'p', 'v')
            AND NOT c.relispartition
            AND has_table_privilege(c.oid, 'SELECT')`

// probeRelationQuery finds the probe relation named $1. It leaves the
// row count out: every metrics request runs it, and only list_probes
// reports the count.
const probeRelationQuery = probeRelationColumns + `
            NULL::bigint` + probeRelationSource + `
            AND c.relname = $1`

// probeRelationsQuery lists every probe relation in name order, with the
// planner's row estimate. A partitioned table holds no rows itself, so
// its count is the sum over the leaf partitions at every level of its
// tree.
const probeRelationsQuery = probeRelationColumns + `
            CASE WHEN c.relkind = 'v' THEN NULL
            WHEN c.relkind = 'p' THEN (
                SELECT COALESCE(SUM(ps.n_live_tup), 0)
                FROM pg_partition_tree(c.oid) pt
                JOIN pg_stat_user_tables ps ON ps.relid = pt.relid
                WHERE pt.isleaf
            )::bigint
            ELSE COALESCE((SELECT s.n_live_tup FROM pg_stat_user_tables s
                           WHERE s.relid = c.oid), 0)
            END` + probeRelationSource + `
        ORDER BY c.relname`

// scanProbeRelation reads one row of probeRelationsSelect.
func scanProbeRelation(row pgx.Row) (ProbeRelation, error) {
	var rel ProbeRelation
	err := row.Scan(&rel.Name, &rel.Description, &rel.IsView,
		&rel.HasBookkeeping, &rel.RowCount)
	return rel, err
}

// LookupProbeRelation returns the probe relation called name, and
// whether there is one the server may serve. It is the single decision,
// with ListProbeRelations, on which metrics relations every route reads,
// so the routes cannot disagree.
//
// Only a known probe's name is admitted (IsKnownProbe), whatever the
// relation's kind, and this is a security rule. The read-side masking
// (ProbeRedactions, ProbeColumnMask, RedactProbeRows, MaskedProbeColumn,
// RawStatementColumns) is keyed by the probe's exact name, so a relation
// under any other name would serve the probe's credentials and statement
// text in clear: a collector's weekly partition such as
// pg_stat_activity_20261102 (excluded by relispartition too, but not
// once detached), a table created under another name, or a view such as
// CREATE VIEW metrics.activity_report AS SELECT * FROM
// metrics.pg_stat_activity. An unknown name is refused without a query.
func LookupProbeRelation(ctx context.Context, q CatalogQuerier, name string) (ProbeRelation, bool, error) {
	if !IsKnownProbe(name) {
		return ProbeRelation{}, false, nil
	}
	rel, err := scanProbeRelation(q.QueryRow(ctx, probeRelationQuery, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return ProbeRelation{}, false, nil
	}
	if err != nil {
		return ProbeRelation{}, false, err
	}
	return rel, true, nil
}

// ListProbeRelations returns every probe relation the server may serve,
// in name order, by the rule LookupProbeRelation applies.
func ListProbeRelations(ctx context.Context, q CatalogQuerier) ([]ProbeRelation, error) {
	rows, err := q.Query(ctx, probeRelationsQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProbeRelation
	for rows.Next() {
		rel, err := scanProbeRelation(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to read probe relations: %w", err)
		}
		if IsKnownProbe(rel.Name) {
			out = append(out, rel)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read probe relations: %w", err)
	}
	return out, nil
}
