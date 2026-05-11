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

import "github.com/jackc/pgx/v5"

// scanAll iterates the given rows, calling scan for each row, and returns
// the accumulated slice. The helper takes ownership of the cursor: it
// always closes rows before returning, so callers MUST NOT also defer
// rows.Close() after handing them off.
//
// scan is invoked with the current pgx.Rows and a pointer to a freshly
// zero-valued T; it should populate *out using rows.Scan and may perform
// per-row post-processing (e.g. converting sql.NullString into a string
// pointer) before returning. A non-nil error from scan aborts iteration
// and is returned to the caller wrapped with no additional context, so
// scan callbacks should attach their own context where useful.
//
// The returned slice is nil when no rows are produced (matching the
// behavior of the loops this helper replaces). After the loop the helper
// returns rows.Err() if non-nil so callers do not need to check it
// themselves.
//
// Example usage:
//
//	rows, err := d.pool.Query(ctx, query, clusterID)
//	if err != nil {
//	    return nil, fmt.Errorf("failed to query relationships: %w", err)
//	}
//	return scanAll(rows, func(r pgx.Rows, rel *NodeRelationship) error {
//	    return r.Scan(
//	        &rel.ID, &rel.ClusterID,
//	        &rel.SourceConnectionID, &rel.TargetConnectionID,
//	        &rel.SourceName, &rel.TargetName,
//	        &rel.RelationshipType, &rel.IsAutoDetected,
//	    )
//	})
func scanAll[T any](rows pgx.Rows, scan func(pgx.Rows, *T) error) ([]T, error) {
	defer rows.Close()

	var out []T
	for rows.Next() {
		var item T
		if err := scan(rows, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
