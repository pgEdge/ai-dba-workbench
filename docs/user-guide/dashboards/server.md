# Server Dashboard

The server dashboard provides detailed metrics for a
single PostgreSQL server. The dashboard appears when
users select a server node in the cluster navigator.

## System Resources

The system resources section displays the following
metrics:

- CPU usage percentage with a time-series chart.
- Memory usage percentage with a time-series chart.
- Disk usage percentage with a time-series chart.
- Load average values with a time-series chart.
- Network I/O throughput with a time-series chart.

## PostgreSQL Overview

The PostgreSQL overview section displays server-level
database metrics:

- Active connections relative to the maximum allowed.
- Transactions per second with a time-series chart.
- Cache hit ratio as a percentage with trend data.
- Temporary files created with a time-series chart.

## WAL and Replication

The WAL and replication section shows write-ahead log
activity and replication status for the server. The
section includes WAL generation rates and replication
slot details.

## Database Summaries

The database summaries section lists all databases on
the server with high-level metrics for each database.
Users can click a database entry to navigate to the
[database dashboard](database.md).

## Top Queries

The top queries section ranks queries by resource
consumption. The section displays execution time, call
count, and rows returned for the most active queries.

The "Hide monitoring queries" toggle filters out the
workbench's own monitoring queries from the list. The
toggle is on by default to focus on application
queries.
