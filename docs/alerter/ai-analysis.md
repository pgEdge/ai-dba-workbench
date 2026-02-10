# AI Alert Analysis

The AI alert analysis feature uses a large language model
to examine alerts and provide actionable remediation
guidance. The analysis considers historical patterns,
server context, and metric baselines to generate reports
tailored to each alert.

## Overview

Each alert in the status panel displays a brain icon that
triggers an AI-powered analysis. The system sends the
alert details, server context, and historical data to an
LLM through an agentic loop. The LLM gathers additional
context by calling built-in tools before producing its
final report.

The AI alert analysis feature provides the following
capabilities:

- The LLM analyzes alert severity, metric values, and
  threshold configurations.
- The system gathers historical alert patterns and metric
  baselines automatically.
- The analysis includes server-specific context such as
  PostgreSQL version and system resources.
- Users can execute suggested SQL queries directly from
  the analysis report.
- The system caches analysis results to avoid redundant
  LLM calls.

## Triggering an Analysis

The status panel displays a brain icon beside each alert.
Clicking the brain icon opens the analysis dialog and
starts the LLM analysis process.

The analysis follows these steps:

1. The system checks for a cached analysis result.
2. The system fetches server context from the connection.
3. The system sends the alert details and context to the
   LLM.
4. The LLM calls tools to gather historical data and
   metric baselines.
5. The LLM produces a structured analysis report.

The dialog displays a loading skeleton while the analysis
runs. The final report renders as formatted markdown with
syntax-highlighted code blocks.

## Analysis Reports

Each analysis report contains four sections that provide
a complete picture of the alert and recommended actions.

### Summary

The summary section provides a brief description of the
alert and its significance. The LLM explains what
triggered the alert and why the current value is
noteworthy.

### Analysis

The analysis section examines historical patterns and
correlations. The LLM reviews the frequency of similar
alerts, compares the current value against baselines,
and identifies contributing factors.

### Remediation Steps

The remediation section provides a numbered list of
specific actions to address the issue. Each step includes
SQL queries or configuration changes that the user can
apply.

### Threshold Tuning

The threshold tuning section recommends adjustments when
the current threshold appears misconfigured. The LLM
provides a rationale for the suggested changes based on
observed metric patterns.

## Running SQL Queries

The analysis report often includes SQL code blocks with
diagnostic queries and remediation commands. Users can
execute these queries directly from the report.

### Run Button

Each SQL code block displays a play button in the upper
right corner. Clicking the play button executes the SQL
against the alert's connection and database. The tooltip
shows the target server and database name.

### Inline Results

The system displays query results in a table directly
below the code block. Each result shows the column
headers, data rows, and a row count. The system truncates
large result sets and displays a notice.

### Write Statement Confirmation

The system detects write statements such as `ALTER`,
`CREATE`, `DROP`, `INSERT`, `UPDATE`, and `DELETE`. When
a code block contains write statements, the system
displays a confirmation prompt listing the detected
statements. The user must click Execute to proceed or
Cancel to abort.

### SQL Validation

The system extracts only executable SQL from code blocks.
The extraction process filters out configuration file
snippets, shell commands, and explanatory prose. The
system identifies SQL statements by matching recognized
keywords at the start of each statement.

## Caching

The system caches analysis results at two levels to avoid
redundant LLM calls and improve response times.

### Cache Indicators

A green brain icon indicates that a cached analysis
exists for the alert. Clicking a green brain icon opens
the cached report instantly without calling the LLM.

### Tolerance-Based Invalidation

The cache uses a tolerance-based invalidation strategy.
The system considers a cached analysis valid when the
current metric value is within 10% of the value at the
time of the original analysis. The system generates a new
analysis when the metric value changes beyond this
tolerance.

### Server-Side and Client-Side Caches

The system maintains both server-side and client-side
caches. The server stores the analysis text and metric
value in the database alongside the alert record. The
client maintains an in-memory cache that persists across
dialog open and close cycles within a session.

### Downloading Reports

The dialog footer includes a Download button that saves
the analysis as a markdown file. The downloaded file
includes the alert details, the full analysis report, and
a generation timestamp.

## Server Context

The analysis includes server context to help the LLM
generate version-appropriate recommendations. The system
fetches the context from the connection before starting
the analysis.

The server context includes the following information:

- The PostgreSQL version and key configuration settings
  such as `shared_buffers` and `work_mem`.
- The maximum connection count and installed extensions.
- The operating system name, version, and architecture.
- The CPU model and core count.
- The total memory and disk usage for each mount point.

The LLM uses this context to ensure that suggested SQL
queries use valid syntax and column names for the
specific PostgreSQL version. The LLM also considers
available system resources when recommending configuration
changes.

## Available Tools

The LLM has access to built-in tools that gather data
during the analysis process. The agentic loop allows the
LLM to call these tools multiple times before producing
the final report.

The following tools are available to the LLM:

- The `get_alert_history` tool retrieves historical
  alerts for the same rule or metric on a connection.
- The `get_alert_rules` tool returns current alerting
  rules and threshold configurations.
- The `get_metric_baselines` tool provides statistical
  baselines including mean, standard deviation, minimum,
  and maximum values.
- The `query_metrics` tool queries historical metric
  values with time-based aggregation.
