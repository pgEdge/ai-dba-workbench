# Collector Cannot Find Its Configuration File

The collector logs a `Configuration file not found` error when the collector
cannot locate the configuration file that the `-config` flag or the default
search path names. Use the following checks to resolve the error:

- Check the file path for typos and correct any errors found.
- Use absolute paths instead of relative paths.
- Verify that file permissions allow the collector to read the file.

[Configuration File Properties](../configuration/collector/configuration_file.md)
describes the locations that the collector searches for its configuration file.
