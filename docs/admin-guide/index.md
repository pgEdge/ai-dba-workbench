# Administrator's Guide

The Workbench's `Administration` console provides a graphical interface for
managing account creation and management. To access the Administration console,
click the `Settings` gear icon in the web client. The navigation pane on the
left side of the console provides quick access to options that allow you to
manage a Workbench deployment:

- The `Users` tab lists all users with expandable rows that show effective
  permissions including group memberships, connection privileges, MCP
  privileges, and admin permissions.
- The `Groups` tab lists all groups with expandable rows that show members,
  connection privileges, MCP privileges, and admin permissions.
- The `Permissions` tab displays the role and privilege definitions that
  determine what each user and group can access.
- The `Tokens` tab lists all tokens with expandable rows that show the token
  scope including connection access levels, MCP privileges, and admin
  permissions.
- The `Probe Defaults` tab sets the default probe settings that the Workbench
  applies when it monitors database connections.
- The `Alert Defaults` tab configures the default thresholds and rules that
  trigger alerts across monitored connections.
- The `Email Channels` tab manages the email destinations that receive alert
  notifications from the Workbench.
- The `Slack Channels` tab manages the Slack destinations that receive alert
  notifications from the Workbench.
- The `Mattermost Channels` tab manages the Mattermost destinations that
  receive alert notifications from the Workbench.
- The `Webhook Channels` tab manages the webhook endpoints that receive alert
  notifications from the Workbench.

## Security Best Practices

The following practices help protect user credentials, API tokens, and active
sessions.

Follow these guidelines for password security:

- Encourage users to choose long passphrases that satisfy the policy described
  in [Password Policy](password.md).
- Never log or display passwords in output.
- Always use HTTPS in production environments.
- Rotate passwords when a credential leak is suspected rather than on a fixed
  schedule.

Follow these guidelines for token security:

- Do not store API tokens in version control.
- Use environment variables for application secrets.
- Assign different tokens to different services and users.
- Set appropriate expiry times for each token.
- Regularly audit and remove unused tokens.
- Use service accounts for automated workflows instead of personal user tokens.

Follow these guidelines for session management:

- Store session tokens securely in the application.
- Re-authenticate before session tokens expire.
- Implement proper logout with token deletion.
- Monitor the server logs for suspicious activity.

## Understanding Authentication Flow

Interactive users authenticate with a username and password to obtain a
session token; that session token is used for subsequent requests until the
token expires:

1. A user authenticates with a username and password using the login API.

2. The client receives the session token in the response.

3. The user can use the session token for subsequent requests until the token
   expires.

Machine-to-machine integrations use API tokens instead of session tokens. You
can create an API token for a service account or regular user via the
Administration console or command line. Use the token directly in all requests
as a Bearer token.

In the following example, an API token authenticates a request:

```bash
curl -X POST http://localhost:8080/mcp/v1 \
  -H "Authorization: Bearer <api_token>" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

## Error Responses

The server returns standard HTTP error codes for authentication failures.

| Error Type | JSON Response | HTTP Status |
|------------|---------------|-------------|
| Missing Token | `{ "error": "Unauthorized" }` | 401 |
| Invalid Token | `{ "error": "Unauthorized" }` | 401 |
| Expired Token | `{ "error": "Unauthorized" }` | 401 |
| Rate Limited | `{ "error": "Too many requests" }` | 429 |

The server does not expose specific error details for security reasons.

## Related Documentation

The following pages provide additional administration reference material.

- [Alert Rules](alert-rules.md) describes the rules that trigger
  notifications.
- [Managing Users and Permissions](managing-users-and-permissions/permission_model.md)
  covers tokens and access control.
- [API Reference](api/reference.md) provides interactive API documentation.
