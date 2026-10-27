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

// coldFrontInstalledQuery reports whether the coldfront extension is
// installed in the datastore and, if so, whether it is a build new
// enough for the Workbench.
const coldFrontInstalledQuery = `
    SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'coldfront'),
           to_regprocedure($1) IS NOT NULL
`

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
		var installed, recentEnough bool
		if err := q.QueryRow(ctx, coldFrontInstalledQuery,
			coldfront.MinimumBuildProcedure).Scan(&installed, &recentEnough); err != nil {
			return fmt.Errorf("failed to check for the coldfront extension: %w", err)
		}
		if !installed {
			return fmt.Errorf("use_coldfront_for_metrics is enabled but the "+
				"coldfront extension is not installed in the datastore; "+
				"install %s and run CREATE EXTENSION coldfront, or disable "+
				"use_coldfront_for_metrics", coldfront.MinimumBuildDescription)
		}
		if !recentEnough {
			return fmt.Errorf("use_coldfront_for_metrics is enabled but the "+
				"installed coldfront extension is too old: %s is missing; "+
				"upgrade to %s", coldfront.MinimumBuildProcedure,
				coldfront.MinimumBuildDescription)
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
