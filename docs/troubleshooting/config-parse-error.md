# Collector Cannot Parse Its Configuration File

The collector exits and logs a `Failed to load configuration` error that
contains the text `failed to parse YAML config` when the configuration file
contains a YAML syntax problem; the rest of the message names the line of the
problem. Use the following checks to resolve the error:

- Check for YAML syntax errors in indentation and correct them.
- Ensure nested keys are properly indented.
- Validate the YAML syntax with a local tool such as `yamllint`; the
  configuration file can contain connection details and credentials, so do not
  paste the file into an online validator.
