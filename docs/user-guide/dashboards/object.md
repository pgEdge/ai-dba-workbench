# Object Dashboard

The object dashboard provides the most detailed view of a single database
object. Click a table, index, or query in the database dashboard leaderboards
to open the object dashboard.

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

The query plan section displays in the query detail view below the AI Overview
panel. The section defaults to expanded, and the expand/collapse state
persists across browser sessions.

The panel fetches PostgreSQL `EXPLAIN` output when the section first renders.
A refresh button in the section header regenerates the plan on demand.

Two tabs display the plan data:

- The Visual tab shows a graphical flow diagram built from the JSON `EXPLAIN`
  plan.
- The Text tab shows the standard `EXPLAIN` output in monospace format for a
  concise view.

### Visual Diagram

The visual diagram uses a left-to-right layout. Leaf scan nodes display on the
left and the root node displays on the right. SVG bezier arrows connect each
child node to its parent.

Each tile in the diagram displays the node type and the relation or index
name. A colored left border indicates the cost ratio relative to the root
node:

- A red border marks nodes that consume over 80 percent of the total cost.
- An orange border marks nodes that consume over 50 percent of the total cost.
- The default border color applies to all other nodes.

Click a tile to open a popover with comprehensive node details. The popover
displays the following information:

- The cost range from startup cost to total cost.
- The estimated row count and row width.
- The output columns the node produces.
- The execution strategy and scan direction.
- The planned and launched worker counts.
- Any filter, join, or index conditions.

### Plan Options

The Workbench uses `EXPLAIN VERBOSE` for JSON plans to provide comprehensive
detail in the visual mode. The text plan uses standard `EXPLAIN` without
`VERBOSE` for a concise view.

For parameterized queries that use `$1`, `$2` placeholders, the Workbench uses
the `GENERIC_PLAN` option available in PostgreSQL 16 and later. Older
PostgreSQL versions display a friendly informational message instead of the
plan.

The Workbench caches plans for five minutes to avoid redundant queries against
the database server.

## AI Query Overview

The query detail view displays an AI Overview panel below the query text when
the server has a configured LLM provider. The panel provides a brief
plain-text summary of the query's performance characteristics in two to three
sentences.

The AI Overview panel includes the following behaviors:

- The summary assesses whether the query appears healthy or has potential
  performance issues.
- A refresh button regenerates the summary on demand.
- A relative timestamp shows when the Workbench last generated the summary.
- A brain icon button opens the full AI Query Analysis dialog.
- The Workbench caches summaries for 30 minutes.
- The panel hides when no LLM provider is configured.

## AI Query Analysis

Click the brain icon in the AI Overview panel to open a full-screen analysis
dialog. The analysis uses an agentic LLM loop with tools to gather additional
context before producing a structured report.

The following tools are available to the LLM:

- The query metrics tool retrieves historical metric values with time-based
  aggregation.
- The metric baselines tool provides statistical baselines including mean,
  standard deviation, and extremes.
- The database query tool executes diagnostic queries against the relevant
  database.
- The schema inspection tool retrieves table and index definitions for
  referenced objects.
- The query validation tool checks SQL syntax and execution plans.
- The knowledgebase search tool finds relevant entries using similarity
  matching.

The dialog displays real-time progress indicators showing which tools the LLM
is currently using. The final report renders as formatted markdown.

Each analysis report contains four sections:

- The Summary section describes the query and its current performance
  characteristics.
- The Performance Analysis section examines execution metrics, trends, and
  resource consumption.
- The Optimization Opportunities section identifies potential improvements to
  the query or schema.
- The Recommendations section suggests specific actions with supporting SQL
  examples.

SQL code blocks in the report include a Run button that executes the query
against the correct database server. Write statements display a confirmation
dialog before the Workbench executes the query.

The Workbench caches analysis results for 30 minutes. A Download button in the
dialog footer saves the report as a markdown file.
