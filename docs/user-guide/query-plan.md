# Understanding the Query Plan

The query plan section displays in the query detail view of the object
dashboard, below the AI Overview panel. The section defaults to expanded, and
the expand/collapse state persists across browser sessions.

## Viewing a Plan

The panel fetches PostgreSQL `EXPLAIN` output when the section first renders. A
refresh button in the section header regenerates the plan on demand.

Two tabs display the plan data:

- The Visual tab shows a graphical flow diagram that the Workbench builds
  from the JSON `EXPLAIN` plan.
- The Text tab shows the standard `EXPLAIN` output in monospace format for a
  concise view.

## Visual Diagram

The visual diagram uses a left-to-right layout. Leaf scan nodes display on the
left and the root node displays on the right. SVG bezier arrows connect each
child node to its parent.

Each tile in the diagram displays the node type and the relation or index name.
A colored left border indicates the cost ratio relative to the root node:

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

## Plan Options

The Workbench uses `EXPLAIN VERBOSE` for JSON plans to provide comprehensive
detail in the visual mode. The text plan uses standard `EXPLAIN` without
`VERBOSE` for a concise view.

For parameterized queries that use `$1`, `$2` placeholders, the Workbench uses
the `GENERIC_PLAN` option available in PostgreSQL 16 and later. Older
PostgreSQL versions display a friendly informational message instead of the
plan.

The Workbench caches plans for five minutes to avoid redundant queries against
the database server.
