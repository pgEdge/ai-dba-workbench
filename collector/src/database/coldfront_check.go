/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/pgedge/ai-workbench/collector/src/coldfront"
)

// coldFrontInstalledQuery reports whether each coldfront function the
// collector relies on exists: $1 is create_iceberg_table, whose presence
// means the extension is installed; $2 identifies a build new enough for
// the Workbench; and $3 attaches the Iceberg catalog. Testing for the
// functions rather than the pg_extension row checks exactly what the
// collector calls, and lets the database tests stand a stub schema in
// for the extension.
const coldFrontInstalledQuery = `
    SELECT to_regprocedure($1) IS NOT NULL,
           to_regprocedure($2) IS NOT NULL,
           to_regprocedure($3) IS NOT NULL
`

// The two statements that create the Iceberg namespace of the metrics
// tables. Both are constants: the namespace is always metrics
// (coldfront.IcebergNamespace), so no identifier is interpolated.
const (
	attachIcebergCatalogSQL   = `SELECT coldfront.ensure_attached()`
	createIcebergNamespaceSQL = `SELECT duckdb.raw_query('CREATE SCHEMA IF NOT EXISTS ice.metrics')`
)

// storageConn is what PrepareMetricsStorage needs from a datastore
// connection that is not inside a transaction.
type storageConn interface {
	rowQuerier
	ddlExecer
	txBeginner
}

// PrepareMetricsStorage readies the datastore for the configured metrics
// storage mode before any migration runs: it runs CheckMetricsStorage
// and, under ColdFront, then creates the Iceberg namespace with
// EnsureColdFrontNamespace and the insert helper with
// EnsureColdFrontInsertHelper. In the default mode it only checks.
func PrepareMetricsStorage(ctx context.Context, conn storageConn, useColdFront bool) error {
	if err := CheckMetricsStorage(ctx, conn, useColdFront); err != nil {
		return err
	}
	if !useColdFront {
		return nil
	}
	if err := EnsureColdFrontNamespace(ctx, conn); err != nil {
		return err
	}
	return EnsureColdFrontInsertHelper(ctx, conn)
}

// EnsureColdFrontNamespace creates the Iceberg namespace the metrics
// tables live in, ice.metrics, if it does not already exist.
//
// coldfront.create_iceberg_table issues CREATE SCHEMA IF NOT EXISTS and
// CREATE TABLE inside one function call, and a Lakekeeper REST catalog
// does not see the namespace before the table request arrives, so the
// first table in a new namespace fails with "Namespace ... does not
// exist". Creating the namespace first, as ColdFront's own installation
// steps do, avoids that. e must not be inside a transaction: each
// statement has to commit on its own so the catalog sees the namespace
// before migration 1 creates the tables. Both statements are idempotent.
func EnsureColdFrontNamespace(ctx context.Context, e ddlExecer) error {
	for _, stmt := range []string{attachIcebergCatalogSQL, createIcebergNamespaceSQL} {
		if _, err := e.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("failed to create the Iceberg namespace %q for "+
				"the ColdFront metrics tables; the Lakekeeper warehouse "+
				"configured in coldfront.warehouse must be reachable and "+
				"writable: %w", coldfront.IcebergNamespace, err)
		}
	}
	return nil
}

// metricsRelationKindsQuery lists the known metrics relations that exist
// in the datastore, split into those stored as PostgreSQL tables (plain
// or partitioned) and those that are views, as ColdFront creates them.
const metricsRelationKindsQuery = `
    SELECT COALESCE(array_agg(c.relname ORDER BY c.relname)
                    FILTER (WHERE c.relkind IN ('r', 'p')), '{}'),
           COALESCE(array_agg(c.relname ORDER BY c.relname)
                    FILTER (WHERE c.relkind = 'v'), '{}')
    FROM pg_class c
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'metrics'
      AND c.relname = ANY($1)
`

// CheckMetricsStorage verifies, before any migration runs, that the
// datastore can hold metrics in the storage mode the collector is
// configured for.
//
// With useColdFront set, the coldfront extension must be installed and
// be a build that includes coldfront._partition_clause. In either mode
// the existing metrics relations, if any, must already be stored the
// configured way: switching an existing datastore between partitioned
// tables and ColdFront is not supported, so a mismatch fails here with
// an explanation rather than part-way through a migration or a probe.
func CheckMetricsStorage(ctx context.Context, q rowQuerier, useColdFront bool) error {
	if useColdFront {
		var installed, recentEnough, canAttach bool
		if err := q.QueryRow(ctx, coldFrontInstalledQuery,
			coldfront.CreateTableProcedure, coldfront.MinimumBuildProcedure,
			coldfront.AttachProcedure).Scan(
			&installed, &recentEnough, &canAttach); err != nil {
			return fmt.Errorf("failed to check for the coldfront extension: %w", err)
		}
		if !installed {
			return fmt.Errorf("use_coldfront_for_metrics is enabled but the "+
				"coldfront extension is not installed in the datastore; "+
				"install %s and run CREATE EXTENSION coldfront, or disable "+
				"use_coldfront_for_metrics", coldfront.MinimumBuildDescription)
		}
		for _, p := range []struct {
			present bool
			name    string
		}{
			{recentEnough, coldfront.MinimumBuildProcedure},
			{canAttach, coldfront.AttachProcedure},
		} {
			if !p.present {
				return fmt.Errorf("use_coldfront_for_metrics is enabled but the "+
					"installed coldfront extension is too old: %s is missing; "+
					"upgrade to %s", p.name, coldfront.MinimumBuildDescription)
			}
		}
	}

	var tables, views []string
	if err := q.QueryRow(ctx, metricsRelationKindsQuery,
		coldfront.TableNames()).Scan(&tables, &views); err != nil {
		return fmt.Errorf("failed to inspect existing metrics tables: %w", err)
	}

	if useColdFront && len(tables) > 0 {
		return fmt.Errorf("use_coldfront_for_metrics is enabled but the "+
			"datastore already stores metrics in PostgreSQL tables (%s); "+
			"switching an existing datastore to ColdFront is not supported, "+
			"so use a new datastore or disable use_coldfront_for_metrics",
			describeMetricsRelations(tables))
	}
	if !useColdFront && len(views) > 0 {
		return fmt.Errorf("use_coldfront_for_metrics is disabled but the "+
			"datastore stores metrics in ColdFront views (%s); "+
			"switching an existing datastore away from ColdFront is not "+
			"supported, so enable use_coldfront_for_metrics or use a new "+
			"datastore", describeMetricsRelations(views))
	}
	return nil
}

// maxNamedRelations bounds how many relations an error message names.
const maxNamedRelations = 3

// describeMetricsRelations names the first few relations of a list for
// an error message.
func describeMetricsRelations(names []string) string {
	shown := names
	if len(shown) > maxNamedRelations {
		shown = shown[:maxNamedRelations]
	}
	qualified := make([]string, len(shown))
	for i, n := range shown {
		qualified[i] = "metrics." + n
	}
	desc := strings.Join(qualified, ", ")
	if extra := len(names) - len(shown); extra > 0 {
		desc += fmt.Sprintf(" and %d more", extra)
	}
	return desc
}
