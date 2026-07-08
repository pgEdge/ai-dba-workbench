# User Guide

The pgEdge AI DBA Workbench provides a browser-based interface for monitoring
and managing PostgreSQL database estates. This guide covers the features
available to users of the web client.

## What This Guide Covers

The User Guide includes the following sections:

- [Dashboards](dashboards/index.md) describes the monitoring dashboard
  hierarchy and the metrics each level displays.
- [Alerts](alerts/index.md) explains how to view, acknowledge, and manage
  alerts in the web interface.
- [AI Features](ai/index.md) covers AI-powered summaries, analysis, and the
  Ask Ellie assistant.
- [MCP Tools](mcp-tools.md) lists the Model Context Protocol tools and
  resources available to compatible AI clients.
- [Blackouts](blackouts.md) describes how maintenance windows suppress alerts
  for servers, clusters, and groups.

## Getting Started

The web client connects to an MCP server instance. Open a browser and navigate
to the server address to access the workbench. Log in with your user
credentials to begin monitoring your PostgreSQL estate.

## Using Ask Ellie

Ask Ellie is an AI-powered database assistant built into the Workbench. See
[Ask Ellie](ai/ask-ellie.md) for details on opening the chat, conversation
history, available tools, and chat memory.


## Using the AI Overview

The AI Overview presents a concise, AI-generated summary of database health
at the top of the status panel. See [AI Overview](ai/index.md) for details
on scope, the full-analysis dialog, and caching behavior.


## Using the AI Chart Analysis Feature

The AI chart analysis feature provides LLM-powered insights for any chart or
KPI tile in the monitoring dashboards. The analysis examines data trends,
identifies anomalies, and generates actionable recommendations.

The Workbench's AI features are available only when the server has a
configured LLM provider. See
[Enabling AI Mode](ai/index.md#enabling-ai-mode) for
configuration details.

Charts, KPI tiles, leaderboards, and the vacuum status section each display a
brain icon in the upper-right icon. Click the icon to open an analysis dialog
and start the LLM analysis.

The analysis follows these steps:

1. The Workbench checks for a cached analysis result.
2. The Workbench fetches server context from the connection.
3. The Workbench fetches timeline events for the time range.
4. The Workbench serializes the chart data and sends it to the LLM.
5. The LLM returns a structured analysis report.

The dialog displays a loading skeleton while the analysis runs. The final
report renders as formatted markdown.

### Analysis Reports

Each chart analysis report contains a structured assessment that includes:

- The `Summary` section describes the alert and its impact on the monitored
  service.
- The `Analysis` section examines the alert pattern, historical context, and
  root cause.
- The `Remediation Steps` section provides step-by-step instructions for
  resolving the issue.
- The `Threshold Tuning` section recommends adjustments to alert thresholds
  where applicable.
- The `Recommendation` section suggests long-term improvements to prevent
  recurrence.

### Timeline Event Correlation

The analysis includes timeline events from the chart's time range to identify
correlations between metric changes and system events. The LLM considers the
following event types:

- Configuration changes to PostgreSQL settings.
- Alert activations and resolutions.
- Server restarts and recovery events.
- Extension installations and upgrades.
- Blackout periods and maintenance windows.

### Running SQL Queries

SQL code blocks in analysis reports include a play button in the upper right
corner. Click the play button to execute the query against the chart's
associated database server. Results appear inline below the code block.

Write statements such as `ALTER SYSTEM` prompt a confirmation dialog before
executing. Read-only queries execute immediately.

### Caching

The Workbench caches chart analysis results on the client side to avoid
redundant LLM calls.

- An amber brain icon indicates that a cached analysis exists for the chart.
- The cache uses stable identifiers as the cache key; these include the metric
  description, connection, database, and time range.
- The cache expires after 30 minutes.
- Click an amber brain icon to open the cached report instantly.

### Downloading Reports

The dialog footer includes a `Download` button that saves the analysis as a
markdown file. The downloaded file includes the chart details, the full
analysis report, and a generation timestamp.
