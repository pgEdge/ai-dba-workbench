# Troubleshooting

This page shares some common issues you might encounter when using the
Workbench.

## Troubleshooting Collector Issues

The following sections describe common error messages and corrective steps.

### "Configuration file not found"

This error indicates a problem locating the configuration file.

- Check the file path for typos and correct any errors found.
- Use absolute paths instead of relative paths.
- Verify that file permissions allow the collector to read the file.

### "Failed to parse configuration"

This error indicates a YAML syntax problem in the configuration file.

- Check for YAML syntax errors in indentation and correct them.
- Ensure nested keys are properly indented.
- Validate the YAML syntax using an online validator.

### "Too many connections"

This error indicates that the pool size exceeds the database limit.

- Reduce `datastore_max_connections` to a lower value.
- Reduce `max_connections_per_server` to a lower value.
- Check the PostgreSQL `max_connections` setting on the target servers.

### "Connection timeout"

This error indicates that connections are not available within the configured
wait period.

- Increase the `*_max_wait_seconds` values.
- Increase the pool sizes for the affected component.
- Check network connectivity to the database server.
- Verify that the database server is responsive.