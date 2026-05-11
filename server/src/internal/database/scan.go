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
// The returned slice is always non-nil: callers that produce no rows get
// a freshly allocated zero-length slice rather than nil. This keeps the
// empty-result contract consistent across every helper user and lets
// JSON encoders emit `[]` rather than `null` for empty responses without
// per-caller normalization. After the loop the helper returns rows.Err()
// if non-nil so callers do not need to check it themselves.
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

	// Initialize as an empty (non-nil) slice so that empty result sets
	// produce a normal []T{} rather than nil. This keeps the helper's
	// empty-result contract uniform across every caller and matches the
	// JSON wire format ([]) most HTTP handlers expect.
	out := make([]T, 0)
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
