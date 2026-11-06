# Collector Connections Time Out

The collector logs a `Connection timeout` error when no connection becomes
available within the configured wait period. Use the following checks to
resolve the error:

- Increase the `*_max_wait_seconds` values.
- Increase the pool sizes for the affected component.
- Check network connectivity to the database server.
- Verify that the database server is responsive.
