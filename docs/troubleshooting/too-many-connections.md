# PostgreSQL Refuses Collector Connections

When a PostgreSQL server refuses a connection because the server reached its
`max_connections` limit, the collector log shows the PostgreSQL error
`sorry, too many clients already` (SQLSTATE 53300). A server that has only its
reserved slots left reports the same SQLSTATE with a message that begins
`remaining connection slots are reserved`.
The collector keeps one connection pool for the datastore and, for each
monitored server, a pool for server-wide probes plus one for each database that
a probe visits. The pools for one monitored server share the
`max_connections_per_server` limit, so identify the server that refused the
connection before you change a limit. Use the following checks to resolve the
error:

- Check the collector log; an error for a monitored server names the server and
  the probe, and any other connection error comes from the datastore.
- If the datastore refused the connection, reduce `datastore_max_connections`
  or raise `max_connections` on the datastore server.
- If a monitored server refused the connection, reduce
  `max_connections_per_server` or raise `max_connections` on that server.
