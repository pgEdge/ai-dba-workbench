# AI Overview

If you have enabled AI in your workbench instance, the `AI Overview` appears
at the top of each object-related page in the console, above the status
panel. The AI Overview generates concise, AI-powered summaries of your
database estate or selected objects.

![Reviewing the AI Overview](../../images/ai_overview.png)

AI features help you understand and resolve issues faster by turning raw
metrics and events into plain-language insight. Using AI in the Workbench
provides the following benefits:

- The brain icon on a server, cluster, chart, KPI tile, or query opens a
  detailed AI analysis with actionable recommendations.
- Run SQL statements that an analysis report suggests directly from the
  report, without leaving the Workbench.
- The AI Overview summarizes database health at a glance, without digging
  through individual metrics.
- Ask Ellie answers questions about your databases, alerts, and performance
  in plain language through the built-in chat assistant.

## Enabling AI Mode

AI features require a configured LLM provider and a valid API key. When you
disable AI mode, the Workbench hides the AI analysis icon, Ask Ellie chat,
alert analysis, and chart analysis; see
[Running Without AI](#running-without-ai) for details. Follow these steps to
enable AI mode:

1. Choose a provider. The server supports `anthropic`, `openai`, `gemini`, and
   `ollama`.
2. Obtain an API key from your chosen provider, or install
   [Ollama](https://ollama.com) for a self-hosted local model. Ollama does not
   require an API key.
3. Store the API key in a file readable only by the server process. In the
   following example, the `echo` command writes the key and `chmod` restricts
   access to the file owner:

    ```bash
    echo "your-api-key" > ~/.anthropic-api-key
    chmod 600 ~/.anthropic-api-key
    ```

4. Update the `LLM CONFIGURATION` section of the server configuration file
   (`/etc/pgedge/ai-dba-server.yaml`), adding your LLM provider details and
   specifying the path to your provider's API key:

    ```yaml
    #=========================================================================
    # LLM CONFIGURATION (Web Client Chat Proxy)
    #=========================================================================
    #
    # The server proxies LLM requests for web clients and CLI tools that
    # don't have direct access to LLM APIs. API keys must be configured
    # for the chosen provider.
    #
    llm:
      # LLM provider: "anthropic", "openai", "gemini", or "ollama"
      # Default: anthropic
      # Default: claude-sonnet-4-5
      model: "claude-sonnet-4-5"

      #-----------------------------------------------------------------------
      # Anthropic Configuration
      #-----------------------------------------------------------------------

      # Anthropic API key — provide the path to a file containing the
      # key via anthropic_api_key_file (raw keys cannot be set inline
      # for security).
      # anthropic_api_key_file: "~/.anthropic-api-key"
    ```

5. Save the file and start the server; for example:

    ```bash
    /opt/ai-workbench/ai-dba-server -config /etc/pgedge/ai-dba-server.yaml &
    ```

## Summary Panel

The AI Overview uses a large language model to produce a natural-language
summary of database health and status. The system collects current alerts,
events, and server metadata; it then sends this context to the configured LLM
for summarization. The summary describes server health, active alerts that
need attention, and any ongoing or upcoming blackouts; when everything is
healthy, the overview states so briefly.

A sparkle icon and the "AI Overview" label identify the panel. While the
server prepares the first summary, the panel displays a loading placeholder
followed by a "Generating overview..." message.

![Reviewing the AI Overview](../../images/ai_overview.png)

The AI Overview provides the following capabilities:

- The summary adapts to the selected scope in the cluster navigator.
- The system caches summaries for five minutes to reduce LLM calls.
- Estate-wide summaries refresh automatically every 60 seconds.
- The panel displays a "(stale)" indicator when the cached summary expires.
- Users can collapse the overview panel using the expand/collapse icon on the
  right of the header; the header remains visible when collapsed, and the
  collapse state persists across sessions.
- A refresh button forces immediate regeneration of the current summary.

## Scope

The AI Overview adapts the summary based on the current selection in the
cluster navigator. The system sends different context to the LLM depending on
the scope.

The following table describes the available scopes:

| Scope | Context Sent to LLM |
|-------|---------------------|
| Estate | All servers, active alerts, and recent events across the entire installation. |
| Cluster | Servers, alerts, and events within a specific cluster. |
| Server | A single server's status, alerts, and recent events. |
| Group | Servers, alerts, and events within a cluster group. |

The estate scope activates by default, before you select a specific object.
Selecting a cluster, server, or group in the navigator updates the overview
to reflect that scope.

## AI-Powered Analysis

Beyond the summary panel, the Workbench offers a deeper, agentic AI analysis
for servers, clusters, charts, KPI tiles, and queries. Each variant follows
the same pattern: click a brain icon to open a full-screen dialog, the LLM
gathers additional context using built-in tools, and the dialog renders a
structured markdown report. Clicking Run immediately executes any read-only
SQL that the report suggests; write statements display a confirmation dialog
first.
[Analysis Caching](#analysis-caching) and
[Downloading Analysis Reports](#downloading-analysis-reports) below describe
the report and its caching behavior once, since they work the same way for
every scope.

!!! hint

    If a Workbench feature displays a purple brain icon in the object's
    header, you can select that icon to generate a detailed analysis of the
    selected object or metrics. If a feature displays an amber brain icon, a
    cached analysis is available for review.

### Server and Cluster Analysis

The AI Overview panel displays a brain icon when you select a server or
cluster in the navigator. Clicking the brain icon opens a full-screen AI
analysis dialog.

The analysis uses an agentic LLM loop that accesses monitoring tools to gather
data. The LLM can query metrics, fetch baselines, review alerts, query
databases, and inspect schemas during the analysis process.

The analysis covers the following areas depending on the selected scope:

- For individual servers, the analysis examines system resources, PostgreSQL
  configuration, alert patterns, and metric trends.
- For clusters, the analysis compares metrics across all member servers and
  examines replication health.

The dialog displays real-time progress as the AI gathers data from different
tools. Each tool invocation appears in the dialog so users can follow the
analysis workflow.

### Chart and KPI Tile Analysis

Charts, KPI tiles, leaderboards, and the vacuum status section each display a
brain icon in the upper-right corner. Clicking the icon opens an analysis
dialog and starts the LLM analysis, which examines the chart data alongside
server context and timeline events for the selected range to identify trends,
anomalies, and actionable recommendations.

The analysis follows these steps:

1. The Workbench checks for a cached analysis result.
2. The Workbench fetches server context from the connection.
3. The Workbench fetches timeline events for the time range.
4. The Workbench serializes the chart data and sends it to the LLM.
5. The LLM returns a structured analysis report.

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

The analysis includes timeline events from the chart's time range to identify
correlations between metric changes and system events. The LLM considers the
following event types:

- Configuration changes to PostgreSQL settings.
- Alert activations and resolutions.
- Server restarts and recovery events.
- Extension installations and upgrades.
- Blackout periods and maintenance windows.

### Query Analysis

The query detail view displays an AI Overview panel below the query text when
the server has a configured LLM provider. The panel provides a brief
plain-text summary of the query's performance characteristics in two to three
sentences, assessing whether the query appears healthy or has potential
performance issues. A refresh button regenerates the summary on demand, and a
relative timestamp shows when the Workbench last generated it. The Workbench
caches these summaries for 30 minutes, and the panel hides when the server
has no configured LLM provider.

Click the brain icon in the AI Overview panel to open a full-screen analysis
dialog. The analysis uses an agentic LLM loop with the following tools to
gather additional context before producing a structured report:

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

Each analysis report contains four sections:

- The Summary section describes the query and its current performance
  characteristics.
- The Performance Analysis section examines execution metrics, trends, and
  resource consumption.
- The Optimization Opportunities section identifies potential improvements to
  the query or schema.
- The Recommendations section suggests specific actions with supporting SQL
  examples.

### Analysis Caching

Each analysis dialog caches its results on the client side for 30 minutes to
avoid redundant LLM calls. An amber brain icon indicates that a cached
analysis is available; click it to reopen the cached report instantly instead
of running a new analysis. For chart and KPI tile analysis, the cache key
combines the metric description, connection, database, and time range.

### Downloading Analysis Reports

Each analysis dialog's footer includes a `Download` button that saves the
report as a markdown file, including the report details, the full analysis
text, and a generation timestamp. For server and cluster analysis, the
Workbench names the file using the format
`{type}-analysis-{name}-{YYYY-MM-DD}.md`.

## Summary Caching

The system caches overview summaries for five minutes to reduce LLM usage and
improve response times.

Estate-wide summaries refresh automatically every 60 seconds in the background.
The client displays the cached summary immediately and updates the panel when a
new summary arrives. The server also regenerates the overview when the estate
state changes significantly; the Workbench receives these updates in real time
and refreshes the panel automatically, without waiting for the next scheduled
refresh.

The system generates scoped summaries for clusters, servers, and groups on
demand. It returns a cached summary when the cache entry has not expired,
and generates a new summary when the cache entry is stale or missing.

The status panel displays a visual indicator when the displayed summary has
passed its expiration timestamp. The indicator signals that the summary may not
reflect the most recent state.

A refresh button appears next to the "Updated N min ago" timestamp. Clicking
the refresh button forces the system to regenerate the summary immediately,
bypassing the cache. The button displays a spinning animation while the system
generates a new summary.

## Running Without AI

When the server starts without valid LLM credentials, the Workbench
automatically hides the AI Overview and the analysis dialogs for servers,
clusters, charts, KPI tiles, leaderboards, the vacuum status section, and
queries. The web client also displays a static welcome message in place of
the AI Overview panel; dashboards, charts, and KPI tiles continue to display
data normally without AI.

The server logs the following message at startup when AI is not available:

```
AI Overview: DISABLED (requires datastore and LLM
configuration)
```

All monitoring, alerting, and dashboard features continue to operate normally
without AI.

## Related Documentation

- [Ask Ellie](ask-ellie.md) describes the AI-powered database assistant.
- [Connecting MCP Clients](mcp-clients.md) describes how external AI tools can
  use these same monitoring capabilities.
- [AI Alert Analysis](../alerts/ai-analysis.md) covers the AI analysis feature
  for individual alerts.
