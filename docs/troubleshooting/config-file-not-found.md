# Collector Cannot Find Its Configuration File

When the `-config` flag names a file that does not exist, the collector exits
and logs a `Failed to load configuration: specified config file not found`
error that names the path. When you omit the `-config` flag and no file exists
in the default search path, the collector does not fail; the collector logs the
informational message `No configuration file found in default search paths`
and starts with its built-in defaults. Use the following checks to resolve the
error, or to make the collector find a file that it started without:

- Check the file path for typos and correct any errors found.
- Use absolute paths instead of relative paths.
- Verify that file permissions allow the collector to read the file.

[Configuration File Properties](../configuration/collector/configuration_file.md)
describes the locations that the collector searches for its configuration file.
