/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Database Row Scanning
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Database Row Scanning

The server's database package centralises the
`pool.Query` -> iterate -> append -> close scaffold behind a generic
helper. Use it for every new query helper that returns a slice of
rows, and migrate existing helpers when you touch them.

## The Helper

The helper lives at `server/src/internal/database/scan.go` and has
the signature:

```go
func scanAll[T any](
    rows pgx.Rows,
    scan func(pgx.Rows, *T) error,
) ([]T, error)
```

It is package-private to `internal/database`. The callback receives
the live `pgx.Rows` and a pointer to a freshly zero-valued `T`; it
populates `*out` via `rows.Scan` and may perform any per-row
post-processing (for example converting `sql.NullString` into a
`*string`, decoding `json.RawMessage`, or deriving a status field)
before returning.

## When To Use It

Reach for `scanAll` any time you have the canonical shape:

```
pool.Query -> for rows.Next() { rows.Scan ... } -> return []T
```

This is the default pattern for new query helpers in
`server/src/internal/database/`. Existing helpers that follow this
shape should be migrated opportunistically when nearby code is
already being touched.

## Why The Pointer Shape

The callback shape is `func(pgx.Rows, *T) error`, not
`func(pgx.Rows) (T, error)`. The pointer form is deliberate:

- Zero per-row heap allocation; the caller populates the struct in
  place inside the slice's backing array.
- Per-row post-processing (`sql.NullString` -> `*string`,
  `json.RawMessage` decoding, computed status) fits naturally inside
  the callback before the value is appended.
- Field-by-field scanning into a local pointer reads identically to
  the hand-written `for rows.Next()` form it replaces, so callsites
  stay obvious during review.

## Contract And Guarantees

The helper makes three guarantees that affect callers:

- `scanAll` always calls `rows.Close()` via `defer`. Callers MUST
  NOT also defer `rows.Close()` after handing the cursor off. The
  cursor is released when `scanAll` returns, which is what makes it
  safe to call recursive query helpers immediately afterwards on a
  single-connection pool (see `buildManualGroupsTopology` in
  `topology_queries.go`).
- The returned slice is always non-nil; an empty result produces a
  freshly allocated zero-length `[]T{}`. JSON encoders emit `[]`
  rather than `null`, so downstream HTTP handlers do not need
  `if x == nil { x = []T{} }` normalisation.
- A non-nil error from the callback aborts iteration and is
  returned unwrapped; after the loop, `rows.Err()` is returned
  unwrapped as well. Callers should wrap once with site context,
  for example
  `fmt.Errorf("failed to read cluster groups: %w", err)`.

## When NOT To Use It

Five categories of caller stay on the hand-written loop:

- Map accumulators (`map[K]V`, not `[]T`). Examples include
  `getClusterOverridesInternal`, `getGroupOverridesInternal`, and
  the map-building branch of `buildManualClusterHierarchy`.
- Skip-on-error iteration. When a malformed row should be logged
  and skipped (`continue`) rather than aborting the whole query,
  the hand loop stays. `scanAll` aborts on the first callback
  error. Examples include `populateTopologyRelationships` and
  parts of `buildManualClusterHierarchy`.
- Callers that genuinely need `nil`-on-empty to distinguish "no
  rows" from "empty result set". This is rare and currently has no
  in-tree caller; the non-nil contract is preferred otherwise.

## Adjacent Packages

`alerter/src/internal/database/queries.go` and
`server/src/internal/metrics/query.go` contain the same scaffold
duplicated locally. The helper is package-private to
`server/src/internal/database` today. If a future PR exports it (or
moves it to a shared internal package) those sibling sites should
be migrated in the same change.

## Worked Example

The canonical use site is in `topology_queries.go`:

```go
rows, err := d.pool.Query(ctx, query, clusterID)
if err != nil {
    return nil, fmt.Errorf("failed to query relationships: %w", err)
}
return scanAll(rows, func(r pgx.Rows, rel *NodeRelationship) error {
    return r.Scan(
        &rel.ID, &rel.ClusterID,
        &rel.SourceConnectionID, &rel.TargetConnectionID,
        &rel.SourceName, &rel.TargetName,
        &rel.RelationshipType, &rel.IsAutoDetected,
    )
})
```

A callback that does per-row post-processing looks like:

```go
return scanAll(rows, func(r pgx.Rows, g *ClusterGroup) error {
    var desc sql.NullString
    if err := r.Scan(&g.ID, &g.Name, &desc); err != nil {
        return err
    }
    if desc.Valid {
        g.Description = &desc.String
    }
    return nil
})
```

The slice is returned directly; no nil-check, no Close, no
rows.Err. The site-level wrap supplies the only error context the
caller needs.
