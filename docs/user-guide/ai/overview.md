# AI Overview

The AI Overview generates concise, AI-powered summaries
of your database estate or selected objects. The summary
appears at the top of the status panel and provides a
quick understanding of the current state.

## Overview

The AI Overview uses a large language model to produce a
natural-language summary of database health and status.
The system collects current alerts, events, and server
metadata; it then sends this context to the configured
LLM for summarization.

The AI Overview provides the following capabilities:

- The summary adapts to the selected scope in the
  cluster navigator.
- The system caches summaries for five minutes to reduce
  LLM calls.
- Estate-wide summaries refresh automatically every 60
  seconds.
- The panel displays a stale indicator when the cached
  summary expires.
- Users can collapse the overview panel; the collapse
  state persists across sessions.
- A refresh button forces immediate regeneration of
  the current summary.

## Scope

The AI Overview adapts the summary based on the current
selection in the cluster navigator. The system sends
different context to the LLM depending on the scope.

The following table describes the available scopes:

| Scope | Context Sent to LLM |
|-------|---------------------|
| Estate | All servers, active alerts, and recent events across the entire installation. |
| Cluster | Servers, alerts, and events within a specific cluster. |
| Server | A single server's status, alerts, and recent events. |
| Group | Servers, alerts, and events within a cluster group. |

The estate scope activates when no specific object is
selected. Selecting a cluster, server, or group in the
navigator updates the overview to reflect that scope.

## Server and Cluster Analysis

The AI Overview panel displays a brain icon when a server
or cluster is selected in the navigator. Clicking the
brain icon opens a full-screen AI analysis dialog.

The analysis uses an agentic LLM loop that accesses
monitoring tools to gather data. The LLM can query
metrics, fetch baselines, review alerts, query databases,
and inspect schemas during the analysis process.

The analysis covers the following areas depending on the
selected scope:

- For individual servers, the analysis examines system
  resources, PostgreSQL configuration, alert patterns,
  and metric trends.
- For clusters, the analysis compares metrics across all
  member servers and examines replication health.

The dialog displays real-time progress as the AI gathers
data from different tools. Each tool invocation appears
in the dialog so users can follow the analysis workflow.

SQL code blocks in the generated report include a Run
button. Read-only queries execute immediately when the
user clicks Run. Write statements display a confirmation
dialog before the system executes the query.

An amber brain icon indicates that a cached analysis is
available. The system caches analyses for 30 minutes
before requiring a new analysis run.

The dialog includes a download button that saves the
report as a markdown file. The system names the file
using the format
`{type}-analysis-{name}-{YYYY-MM-DD}.md`.

## Caching

The system caches overview summaries for five minutes to
reduce LLM usage and improve response times.

Estate-wide summaries refresh automatically every 60
seconds in the background. The client displays the cached
summary immediately and updates the panel when a new
summary arrives.

Scoped summaries for clusters, servers, and groups are
generated on demand. The system returns a cached summary
when the cache entry has not expired. The system
generates a new summary when the cache entry is stale
or missing.

The status panel displays a visual indicator when the
displayed summary has passed its expiration timestamp.
The indicator signals that the summary may not reflect
the most recent state.

A refresh button appears next to the "Updated N min ago"
timestamp. Clicking the refresh button forces the system
to regenerate the summary immediately, bypassing the
cache. The button displays a spinning animation while
the system generates a new summary.

## Running Without AI

The AI Overview is automatically disabled when the server
starts without valid LLM credentials. The web client
hides the AI Overview panel and displays a static welcome
message instead.

The server logs the following message at startup when AI
is not available:

```
AI Overview: DISABLED (requires datastore and LLM
configuration)
```

All monitoring, alerting, and dashboard features continue
to operate normally without AI.

## Related Documentation

- [Ask Ellie](ask-ellie.md) describes the AI-powered
  database assistant.
- [AI Alert Analysis](../alerts/ai-analysis.md) covers
  the AI analysis feature for individual alerts.
