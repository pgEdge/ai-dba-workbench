# Object Dashboard

The object dashboard provides the most detailed view of a single database
object. Select a database in the
[cluster navigator](index.md#using-the-cluster-navigator) to open its database
dashboard, then click a table, index, or query in the leaderboards to open the
object dashboard.

## Table Detail

The table detail view displays the following metrics:

- Table size and total size including indexes and TOAST.
- Table bloat estimate as a percentage.
- Sequential scan count versus index scan count.
- Insert, update, and delete modification counts.
- Live tuple and dead tuple counts with trend data.

## Index Detail

The index detail view displays the following metrics:

- Index size in a human-readable format.
- Index scan count with a time-series chart.
- Tuples read and tuples fetched counts.

## Query Detail

The query detail view displays the following metrics:

- Total and mean execution time.
- Total rows returned and rows per call.
- Call count with a time-series chart.

## Query Plan

The query detail view displays a query plan section below the AI Overview
panel, with Visual and Text tabs for reviewing the PostgreSQL `EXPLAIN` output.
See [Query Plan](../query-plan.md) for details on the visual diagram, node
details, and plan caching.

## AI Query Analysis

The query detail view includes an AI-generated performance summary and a full
analysis dialog when the server has a configured LLM provider. See
[AI Query Analysis](../ai/index.md#query-analysis) for details on the summary panel,
the analysis report, and running suggested SQL from the report.
