# Collector Connections Time Out

When no connection becomes available within the configured wait period, the
collector logs an error that contains one of the following messages:

- `timed out after N seconds while waiting for a connection from the
  monitored connection pool` names the monitored server and the probe; the
  `pool.monitored_max_wait_seconds` property sets the wait.
- `timed out after N seconds while waiting for a connection from the datastore
  pool` names the probe whose metrics the collector could not store; the
  `pool.datastore_max_wait_seconds` property sets the wait.

The same `pool.monitored_max_wait_seconds` value also bounds the whole run of a
probe; a probe that runs out of time logs
`Probe NAME execution timed out for connection SERVER`. Use the following
checks to resolve the error:

- Increase the `pool.monitored_max_wait_seconds` or
  `pool.datastore_max_wait_seconds` value that the message names.
- Increase the pool sizes for the affected component.
- Check network connectivity to the database server.
- Verify that the database server is responsive.
