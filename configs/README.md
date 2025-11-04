# pgEdge AI Workbench Configuration

This directory contains sample configuration files for the pgEdge AI Workbench.

## Configuration Files

### ai-workbench.conf.sample

This is the sample configuration file for both the collector and MCP server components. It contains all available configuration options with detailed comments.

To use this configuration:

1. Copy the sample file:
   ```bash
   cp ai-workbench.conf.sample ai-workbench.conf
   ```

2. Edit `ai-workbench.conf` with your specific settings

3. **Important**: Update the `server_secret` to a strong random string for production use

4. Place the configuration file in one of these locations:
   - Same directory as the collector or server binary (default)
   - Any custom location (specify with `-config` flag)

## Configuration Format

The configuration file uses a simple key-value format:

```
# Comments start with #
key = value
key = "quoted value"
```

## Shared Configuration

The configuration file is shared between:

- **Collector**: Uses datastore settings and server_secret
- **MCP Server**: Uses datastore settings, server_secret, and TLS settings

## Security Notes

- **Never commit `ai-workbench.conf`** (without .sample suffix) to version control as it may contain sensitive information
- Use `pg_password_file` to store passwords in a separate file with restricted permissions
- Generate a strong random string for `server_secret` and keep it secure
- Use TLS/SSL connections in production environments
- Set appropriate file permissions (e.g., `chmod 600 ai-workbench.conf`)

## Environment-Specific Configurations

You can maintain multiple configuration files for different environments:

```
ai-workbench-dev.conf
ai-workbench-staging.conf
ai-workbench-prod.conf
```

Then specify the appropriate config when running:

```bash
./collector -config /path/to/ai-workbench-prod.conf
```

## Configuration Precedence

Settings are applied in the following order (later takes precedence):

1. Default values (hardcoded in the application)
2. Configuration file values
3. Command line flag values

This allows you to set common settings in the configuration file and override specific values via command line flags as needed.
