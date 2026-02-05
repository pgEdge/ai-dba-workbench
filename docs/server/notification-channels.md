# Notification Channels

The AI DBA Workbench sends alert notifications through
configurable notification channels. Administrators manage
channels through the admin panel under the Notifications
section. The alerter uses these channels to deliver alert
notifications when rules fire, clear, or require reminders.

## Channel Types

The workbench supports four notification channel types:

- Email channels send alerts via SMTP to configured
  recipients; the channel supports TLS/STARTTLS,
  authentication, and per-channel recipient management.
- Slack channels send alerts to a Slack channel through an
  incoming webhook URL.
- Mattermost channels send alerts to a Mattermost channel
  through an incoming webhook URL.
- Webhook channels send alerts to an arbitrary HTTP endpoint
  with configurable HTTP methods, custom headers,
  authentication, and JSON payload templates.

## Managing Channels

Each channel type has its own page in the admin panel sidebar
under the Notifications section. All channel types share a
common set of management operations.

The following operations are available for all channel types:

- The Add Channel button creates a new notification channel.
- The Edit icon opens the channel configuration dialog.
- The Delete icon removes a channel after confirmation.
- The Send icon sends a test notification to verify the
  channel configuration.
- The inline switch toggles a channel between enabled and
  disabled states.

Administrators must have the
`manage_notification_channels` permission to access these
operations.

## Email Channels

Email channels deliver alert notifications through SMTP to
a list of configured recipients.

### SMTP Configuration

The following settings configure the SMTP connection:

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| SMTP Host | Yes | - | The SMTP server hostname. |
| SMTP Port | No | 587 | The SMTP server port number. |
| SMTP Username | No | - | The username for SMTP authentication. |
| SMTP Password | No | - | The password for SMTP authentication. |
| Use TLS | No | On | Enables TLS encryption for the connection. |
| From Address | Yes | - | The sender email address. |
| From Name | No | - | The sender display name. |

### Recipients

The Recipients tab manages individual email recipients for
the channel. Each recipient has an email address, a display
name, and an enabled toggle. Administrators can add
recipients during channel creation or later through the
edit dialog.

## Slack Channels

Slack channels deliver alert notifications to a Slack
workspace channel through an incoming webhook URL.

### Configuration

The following settings configure a Slack channel:

| Setting | Required | Description |
|---------|----------|-------------|
| Name | Yes | A descriptive name for the channel. |
| Description | No | An optional description of the channel. |
| Webhook URL | Yes | The Slack incoming webhook URL. |

### Creating a Slack Webhook

To create an incoming webhook for Slack, follow these steps:

1. Create a Slack App in the Slack API dashboard.
2. Enable the Incoming Webhooks feature for the app.
3. Create a new webhook and select a target channel.
4. Copy the generated webhook URL into the channel settings.

For detailed instructions, see the
[Slack Webhooks documentation](https://api.slack.com/messaging/webhooks).

## Mattermost Channels

Mattermost channels deliver alert notifications to a
Mattermost channel through an incoming webhook URL.

### Configuration

The following settings configure a Mattermost channel:

| Setting | Required | Description |
|---------|----------|-------------|
| Name | Yes | A descriptive name for the channel. |
| Description | No | An optional description of the channel. |
| Webhook URL | Yes | The Mattermost incoming webhook URL. |

### Creating a Mattermost Webhook

To create an incoming webhook in Mattermost, follow these
steps:

1. Navigate to Main Menu, then Integrations.
2. Select Incoming Webhooks and create a new webhook.
3. Choose the target channel for notifications.
4. Copy the generated webhook URL into the channel settings.

For detailed instructions, see the
[Mattermost Incoming Webhooks documentation](https://developers.mattermost.com/integrate/webhooks/incoming/).

## Webhook Channels

Webhook channels deliver alert notifications to any HTTP
endpoint. The webhook channel offers the most flexibility
through configurable HTTP methods, custom headers,
authentication options, and JSON payload templates.

### Settings Tab

The Settings tab configures the core webhook properties:

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| Name | Yes | - | A descriptive name for the channel. |
| Description | No | - | An optional description. |
| Endpoint URL | Yes | - | The URL to send notifications to. |
| HTTP Method | No | POST | The HTTP method: POST, GET, PUT, or PATCH. |
| Enabled | No | On | Toggles the channel on or off. |

### Headers Tab

The Headers tab manages custom HTTP headers as key-value
pairs. Administrators can add or remove headers dynamically
to meet the requirements of the target endpoint.

### Authentication Tab

The Authentication tab configures credentials for the
target endpoint. The following authentication types are
available:

| Auth Type | Fields | Description |
|-----------|--------|-------------|
| None | - | The request sends no authentication. |
| Basic | Username, Password | The request uses HTTP Basic authentication. |
| Bearer Token | Token | The request includes a Bearer token header. |
| API Key | Header Name, Key | The request sends the key in a custom header. |

For the API Key type, specify the header name (such as
`X-API-Key`) and the corresponding key value.

### Templates Tab

Webhook channels support customizable JSON payload
templates using Go `text/template` syntax. For template
syntax details, see the
[Go template documentation](https://pkg.go.dev/text/template).

The Templates tab provides three template editors:

- The Alert Fire template formats the payload when an alert
  triggers.
- The Alert Clear template formats the payload when an
  alert resolves.
- The Reminder template formats the payload for recurring
  alert reminders.

If left blank, the system uses sensible default templates
for each notification type.

### Template Variables

Templates have access to the following context variables:

| Variable | Type | Description |
|----------|------|-------------|
| `AlertID` | integer | The unique alert identifier. |
| `AlertTitle` | string | The alert rule name. |
| `AlertDescription` | string | A detailed description of the alert. |
| `Severity` | string | The severity level: `critical`, `warning`, or `info`. |
| `SeverityColor` | string | A hex color for the severity: `#dc3545`, `#ffc107`, or `#17a2b8`. |
| `SeverityEmoji` | string | An emoji for the severity level. |
| `Status` | string | The current alert status. |
| `ServerName` | string | The friendly name of the monitored server. |
| `ServerHost` | string | The hostname of the monitored server. |
| `ServerPort` | integer | The port number of the monitored server. |
| `DatabaseName` | string | The database name; may be empty. |
| `MetricName` | string | The name of the metric that triggered the alert; may be empty. |
| `MetricValue` | float | The current metric value; may be empty. |
| `ThresholdValue` | float | The threshold that was crossed; may be empty. |
| `Operator` | string | The comparison operator (such as `>`, `<`, or `=`). |
| `TriggeredAt` | time | The timestamp when the alert fired. |
| `ClearedAt` | time | The timestamp when the alert cleared; may be empty. |
| `Duration` | string | A human-readable duration the alert was active. |
| `Timestamp` | time | The timestamp when the notification was created. |
| `ReminderCount` | integer | The reminder sequence number. |
| `NotificationType` | string | The notification type: `alert_fire`, `alert_clear`, or `reminder`. |
| `ConnectionID` | integer | The internal connection identifier. |

Optional fields such as `DatabaseName`, `MetricName`,
`MetricValue`, `ThresholdValue`, `Operator`, and `ClearedAt`
should use `{{if .FieldName}}...{{end}}` conditionals in
templates to handle empty values.

Time fields support formatting with the Go time layout
syntax. In the following example, the `TriggeredAt` field
uses ISO 8601 format:

```
{{.TriggeredAt.Format "2006-01-02T15:04:05Z07:00"}}
```

### Default Templates

The system provides default templates for each notification
type. Administrators can copy and customize these templates.

The following template handles alert fire notifications:

```json
{
  "event": "alert_fire",
  "alert_id": {{.AlertID}},
  "title": "{{.AlertTitle}}",
  "description": "{{.AlertDescription}}",
  "severity": "{{.Severity}}",
  "server": {
    "name": "{{.ServerName}}",
    "host": "{{.ServerHost}}",
    "port": {{.ServerPort}}
  },
  {{- if .DatabaseName}}
  "database": "{{.DatabaseName}}",
  {{- end}}
  {{- if .MetricName}}
  "metric": {
    "name": "{{.MetricName}}"
    {{- if .MetricValue}},
      "value": {{.MetricValue}}
    {{- end}}
    {{- if .ThresholdValue}},
      "threshold": {{.ThresholdValue}}
    {{- end}}
    {{- if .Operator}},
      "operator": "{{.Operator}}"
    {{- end}}
  },
  {{- end}}
  "triggered_at":
    "{{.TriggeredAt.Format "2006-01-02T15:04:05Z07:00"}}"
}
```

The following template handles alert clear notifications:

```json
{
  "event": "alert_clear",
  "alert_id": {{.AlertID}},
  "title": "{{.AlertTitle}}",
  "server": {
    "name": "{{.ServerName}}",
    "host": "{{.ServerHost}}",
    "port": {{.ServerPort}}
  },
  "triggered_at":
    "{{.TriggeredAt.Format "2006-01-02T15:04:05Z07:00"}}"
  {{- if .ClearedAt}},
  "cleared_at":
    "{{.ClearedAt.Format "2006-01-02T15:04:05Z07:00"}}"
  {{- end}},
  "duration": "{{.Duration}}"
}
```

The following template handles reminder notifications:

```json
{
  "event": "reminder",
  "alert_id": {{.AlertID}},
  "title": "{{.AlertTitle}}",
  "description": "{{.AlertDescription}}",
  "severity": "{{.Severity}}",
  "server": {
    "name": "{{.ServerName}}",
    "host": "{{.ServerHost}}",
    "port": {{.ServerPort}}
  },
  "triggered_at":
    "{{.TriggeredAt.Format "2006-01-02T15:04:05Z07:00"}}",
  "reminder_count": {{.ReminderCount}}
}
```

## REST API

The notification channel REST API provides endpoints for
managing channels, testing delivery, and managing email
recipients. All endpoints require the
`manage_notification_channels` permission.

The following table lists the available endpoints:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/notification-channels` | List all channels. |
| `POST` | `/api/v1/notification-channels` | Create a channel. |
| `GET` | `/api/v1/notification-channels/{id}` | Get a channel. |
| `PUT` | `/api/v1/notification-channels/{id}` | Update a channel. |
| `DELETE` | `/api/v1/notification-channels/{id}` | Delete a channel. |
| `POST` | `/api/v1/notification-channels/{id}/test` | Send a test notification. |
| `GET` | `/api/v1/notification-channels/{id}/recipients` | List email recipients. |
| `POST` | `/api/v1/notification-channels/{id}/recipients` | Add a recipient. |
| `PUT` | `/api/v1/notification-channels/{id}/recipients/{rid}` | Update a recipient. |
| `DELETE` | `/api/v1/notification-channels/{id}/recipients/{rid}` | Delete a recipient. |
