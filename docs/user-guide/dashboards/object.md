# Object Dashboard

The object dashboard provides the most detailed view of
a single database object. The dashboard appears when
users click a table, index, or query in the database
dashboard leaderboards.

## Table Detail

The table detail view displays the following metrics:

- Table size and total size including indexes and TOAST.
- Table bloat estimate as a percentage.
- Live tuple and dead tuple counts with trend data.
- The cumulative sequential scan count for the table.

The activity charts plot rates rather than the raw
cumulative counters, so a rising line shows a busier
table rather than the simple passage of time:

- The Tuple Operations Over Time chart plots the rows
  inserted, updated, deleted, and HOT updated per
  second.
- The Sequential vs Index Scans Over Time chart plots
  sequential scans and index scans per second.
- The Dead Tuple Ratio Over Time chart plots the
  percentage of tuples that are dead.

## Index Detail

The index detail view displays the following metrics:

- Index size in a human-readable format.
- The cumulative index scan, tuples read, and tuples
  fetched counts.

The Scan Activity chart plots index scans per second
across the selected time range.

## Query Detail

The query detail view displays the following tiles:

- The Total Calls tile counts the calls the query made
  within the selected time range.
- The Total Time tile sums the execution time of those
  calls.
- The Mean Time tile divides that total time by that call
  count, so the figure also covers the selected range.
- The Min Time (All Time) and Max Time (All Time) tiles
  report the fastest and slowest single execution over the
  lifetime of the query.
- The Avg Rows/Call tile divides the rows returned within
  the range by the calls made within the range.

PostgreSQL reports `pg_stat_statements` counters
cumulatively, so the Workbench derives a windowed figure
by subtracting consecutive collected samples. That
subtraction works for a counter that only ever grows, such
as the call count or the total execution time, but it
cannot recover the extremes: the minimum and the maximum
of a window are not the difference between two lifetime
extremes. The two lifetime tiles carry the "All Time"
qualifier for that reason, and a maximum far above the
mean is expected rather than a fault, because one slow
execution at any point in the life of the query sets that
figure permanently.

A query that made no calls within the selected range drops
out of the results, whether the range comes from a preset
or from a custom window. Narrowing the range whilst the
overlay is open therefore leaves every tile showing a dash
rather than a zero, and widening the range again restores
the figures.

The Calls Over Time chart plots the calls per second
across the selected time range.

The view also names the database role that ran the
query, beside the query text under the Database User
heading. The view displays "Unknown" when the collector
cannot resolve the role that owns the statement.

Beside that value, the Last Observed Client heading
names the client last seen running the query. The
value shows the client address on its own, or
"hostname (address)" when the monitored server
resolved a hostname, and a tooltip reports when the
collector saw the client. The value reads "local" for
a client that connected over a Unix-domain socket on
the database host, because such a connection has no
network address of its own.

The client attribution is best effort, because the
collector samples `pg_stat_activity` at intervals
rather than watching every statement. The view can
therefore name a client only for statements that
happened to be in flight when a sample was taken, and
it reports the client observed most recently rather
than the only client that ever ran the query. The
view displays "Not observed" when no sample has
caught the query in flight, which is always the case
on PostgreSQL releases before 14 and on servers where
`compute_query_id` is off.

The execution time and call count charts cover only the
selected query, so the values reconcile with the tiles
above.

## Query Plan

The query plan section appears in the query detail view
below the AI Overview panel. The section defaults to
expanded, and the expand/collapse state persists across
browser sessions.

The panel fetches PostgreSQL `EXPLAIN` output when the
section first renders. A refresh button in the section
header regenerates the plan on demand.

Two tabs display the plan data:

- The Visual tab shows a graphical flow diagram built
  from the JSON `EXPLAIN` plan.
- The Text tab shows the standard `EXPLAIN` output in
  monospace format for a concise view.

### Visual Diagram

The visual diagram uses a left-to-right layout. Leaf
scan nodes appear on the left and the root node appears
on the right. SVG bezier arrows connect each child node
to its parent.

Each tile in the diagram displays the node type and the
relation or index name. A colored left border indicates
the cost ratio relative to the root node:

- A red border marks nodes that consume over 80 percent
  of the total cost.
- An orange border marks nodes that consume over 50
  percent of the total cost.
- The default border color applies to all other nodes.

Clicking a tile opens a popover with comprehensive node
details. The popover displays the following information:

- The cost range from startup cost to total cost.
- The estimated row count and row width.
- The output columns produced by the node.
- The execution strategy and scan direction.
- The planned and launched worker counts.
- Any filter, join, or index conditions.

### Plan Options

The system uses `EXPLAIN VERBOSE` for JSON plans to
provide comprehensive detail in the visual mode. The
text plan uses standard `EXPLAIN` without `VERBOSE` for
a concise view.

For parameterized queries that use `$1`, `$2`
placeholders, the system uses the `GENERIC_PLAN` option
available in PostgreSQL 16 and later. Older PostgreSQL
versions display a friendly informational message
instead of the plan.

The system caches plans for five minutes to avoid
redundant queries against the database server.

## AI Query Overview

The query detail view displays an AI Overview panel
below the query text when an LLM provider is
configured. The panel provides a brief plain-text
summary of the query's performance characteristics in
two to three sentences.

The AI Overview panel includes the following behaviors:

- The summary assesses whether the query appears
  healthy or has potential performance issues.
- A refresh button regenerates the summary on demand.
- A relative timestamp shows when the summary was last
  generated.
- A brain icon button opens the full AI Query Analysis
  dialog.
- The system caches summaries for 30 minutes.
- The panel is hidden when no LLM provider is
  configured.

## AI Query Analysis

Clicking the brain icon in the AI Overview panel opens
a full-screen analysis dialog. The analysis uses an
agentic LLM loop with tools to gather additional
context before producing a structured report.

The following tools are available to the LLM:

- The query metrics tool retrieves historical metric
  values with time-based aggregation.
- The metric baselines tool provides statistical
  baselines including mean, standard deviation, and
  extremes.
- The database query tool executes diagnostic queries
  against the relevant database.
- The schema inspection tool retrieves table and index
  definitions for referenced objects.
- The query validation tool checks SQL syntax and
  execution plans.
- The knowledgebase search tool finds relevant entries
  using similarity matching.

The dialog displays real-time progress indicators
showing which tools the LLM is currently using. The
final report renders as formatted markdown.

Each analysis report contains four sections:

- The Summary section describes the query and its
  current performance characteristics.
- The Performance Analysis section examines execution
  metrics, trends, and resource consumption.
- The Optimization Opportunities section identifies
  potential improvements to the query or schema.
- The Recommendations section suggests specific actions
  with supporting SQL examples.

SQL code blocks in the report include a Run button that
executes the query against the correct database server.
Write statements display a confirmation dialog before
the system executes the query.

The Workbench validates each generated SQL block as the
report renders, by planning the statement with `EXPLAIN`
in a read-only transaction that is rolled back; the check
never executes anything. A block that fails the check
shows the error and replaces Run with a Run anyway
button, a statement that PostgreSQL cannot plan in
advance stays runnable under a "Not validated" notice,
and a block that uses `$1`-style parameter placeholders
is labelled a template and offers no Run button, because
it needs parameter values before it can run. The
[AI Alert Analysis](../alerts/ai-analysis.md) document
describes these states in more detail.

The system caches analysis results for 30 minutes. A
Download button in the dialog footer saves the report
as a markdown file.
