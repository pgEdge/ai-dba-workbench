# Notification Channels

The AI DBA Workbench sends alert notifications through
configurable notification channels. Administrators
manage channels through the admin panel under the
Notifications section. The alerter uses these channels
to deliver alert notifications when rules fire, clear,
or require reminders.

## Channel Types

The workbench supports five notification channel types:

- Email channels send alerts via SMTP to configured
  recipients; the channel supports TLS/STARTTLS,
  authentication, and per-channel recipient management.
- Slack channels send alerts to a Slack channel through
  an incoming webhook URL.
- Mattermost channels send alerts to a Mattermost
  channel through an incoming webhook URL.
- Telegram channels send alerts to a Telegram chat,
  group, or channel through a bot that posts with the
  Telegram Bot API.
- Webhook channels send alerts to an arbitrary HTTP
  endpoint with configurable HTTP methods, custom
  headers, authentication, and JSON payload templates.

## Managing Channels

Each channel type has a dedicated page in the admin
panel sidebar under the Notifications section. All
channel types share a common set of management
operations.

The following operations are available for all channel
types:

- The Add Channel button creates a new notification
  channel.
- The Edit icon opens the channel configuration dialog.
- The Delete icon removes a channel after confirmation.
- The Send icon sends a test notification to verify
  the channel configuration.
- The inline switch toggles a channel between enabled
  and disabled states.

Administrators must have the
`manage_notification_channels` permission to access
these operations.

## Delivery Behaviour

The alerter delivers each notification and records the
outcome against the alert in its notification history.
The rules in this section apply to every channel,
whatever the channel type.

### Retries

A delivery that fails is retried, and a notification
that never succeeds is recorded as `failed`. The
`max_retry_attempts` and `retry_backoff_minutes`
settings control how many attempts the alerter makes
and how long it waits between them; see
[Alerter Configuration](../getting-started/configuration/alerter.md).
The alerter makes three attempts by default, with waits
of 5 minutes and then 15 minutes between them. The
history record keeps the error from the most recent
attempt, which is the place to look when a notification
never arrives.

### Redirects

Slack, Mattermost, Telegram, and webhook deliveries do
not follow HTTP redirects. A provider that answers a
delivery with a 3xx response fails that delivery. The
alerter then retries the delivery on the schedule above
and marks the notification `failed` once the attempts
run out, and the recorded error names the 3xx status
code.

Redirects are refused to protect the credentials that
these channels carry in their request URLs. Go copies
the full URL of the previous request into the `Referer`
header of a redirected request. The Slack and
Mattermost webhook URLs are themselves secrets, and the
Telegram URL carries the bot token in its path, so
following a redirect would disclose the credential to
whichever host the `Location` header names. A webhook
channel gains a second protection, because the
workbench validates the endpoint host when the channel
is saved, and a redirect could carry the request past
that check to a private host. The measure is
preventive; the Send icon has always refused redirects,
and delivery now behaves in the same way.

Configure the final URL in the channel when an endpoint
answers with a redirect. Email channels are unaffected,
because email is delivered over SMTP rather than HTTP.

## Email Channels

Email channels deliver alert notifications through
SMTP to a list of configured recipients.

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

The Recipients tab manages individual email recipients
for the channel. Each recipient has an email address, a
display name, and an enabled toggle. Administrators can
add recipients during channel creation or later through
the edit dialog.

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

To create an incoming webhook for Slack, follow these
steps:

1. Create a Slack App in the Slack API dashboard.
2. Enable the Incoming Webhooks feature for the app.
3. Create a new webhook and select a target channel.
4. Copy the generated webhook URL into the channel
   settings.

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

To create an incoming webhook in Mattermost, follow
these steps:

1. Navigate to Main Menu, then Integrations.
2. Select Incoming Webhooks and create a new webhook.
3. Choose the target channel for notifications.
4. Copy the generated webhook URL into the channel
   settings.

For detailed instructions, see the
[Mattermost Incoming Webhooks documentation](https://developers.mattermost.com/integrate/webhooks/incoming/).

## Telegram Channels

Telegram channels deliver alert notifications to a
Telegram chat, group, or channel through the Telegram
Bot API. A Telegram channel stores two values rather
than a single webhook URL: a bot token that identifies
the sending bot, and a chat ID that identifies the
destination.

### Configuration

The following settings configure a Telegram channel:

| Setting | Required | Description |
|---------|----------|-------------|
| Name | Yes | A descriptive name for the channel. |
| Description | No | An optional description of the channel. |
| Bot Token | Yes | The token that BotFather issued for the bot, in the form `<bot id>:<token>`. |
| Chat ID | Yes | The numeric chat ID, or an `@channelusername` for a public channel. |

The workbench encrypts the bot token before storing it
and never returns the token after saving. The channel
list reports only whether a token is configured, and
the Bot Token field is blank when an administrator
edits a channel; enter a value only to replace the
stored token. The chat ID is not a secret, so the admin
panel and the REST API display the stored value.

The Send icon delivers a test message to the configured
chat, which confirms that the bot token and the chat ID
are correct and that the bot may post.

The REST API carries the two values in the
`telegram_bot_token` and `telegram_chat_id` fields of a
create or update request. A response reports
`telegram_bot_token_set` in place of the token, and an
update that omits `telegram_bot_token` keeps the stored
token.

### Creating a Telegram Bot

BotFather is the Telegram bot that registers and
manages other bots. To create a bot for the workbench,
follow these steps:

1. Open a chat with [@BotFather](https://t.me/BotFather)
   and send the `/newbot` command.
2. Choose a display name and a username for the bot;
   the username must end in `bot`.
3. Copy the token that BotFather returns into the Bot
   Token field, and treat the token as a password.
4. Add the bot to the group or channel that receives
   the notifications.
5. Promote the bot to administrator when the
   destination is a channel, so that the bot may post
   messages.

### Finding the Chat ID

The chat ID identifies the destination that the bot
posts to. To find the chat ID of a chat, group, or
channel, follow these steps:

1. Send any message in the target chat, group, or
   channel.
2. Request
   `https://api.telegram.org/bot<token>/getUpdates`,
   replacing `<token>` with the bot token.
3. Read the chat ID from the `result[].message.chat.id`
   field of the JSON response.

Group and supergroup IDs are negative numbers, such as
`-1001234567890`, whereas a chat with a single user has
a positive ID. For a public channel, enter
`@channelusername` in place of the numeric ID.

For details of the Bot API methods, see the
[Telegram Bot API documentation](https://core.telegram.org/bots/api).

### Securing the Bot Token

The bot token is a bearer credential; anyone who holds
the token can post as the bot and read the updates that
the bot receives. The workbench encrypts the token with
the server secret, never displays the token after
saving, and redacts the token from log messages and
from delivery errors recorded against a notification.
The alerter also refuses HTTP redirects when it
delivers a message, so the token cannot travel to
another host in a `Referer` header; see
[Delivery Behaviour](#delivery-behaviour).

If a token is exposed, use BotFather to revoke the
token and issue a replacement, then save the new token
in the channel configuration.

### Templates

Telegram channels support custom templates through the
`template_alert_fire`, `template_alert_clear`, and
`template_reminder` fields of the REST API, and the
templates use the variables listed in
[Template Variables](#template-variables).

A Telegram template renders the message text, not a
JSON request body. This differs from the Slack,
Mattermost, and webhook templates, which render a
complete JSON payload. The alerter builds the request
body itself and sends the rendered text with the
`sendMessage` method of the Bot API, so a template that
renders JSON delivers the JSON as visible message text.

The alerter sends messages in HTML parse mode rather
than MarkdownV2, because MarkdownV2 requires escaping
eighteen characters, among them `_`, `.`, `-`, `(`, and
`)`, which occur constantly in PostgreSQL relation and
index names. A template may use only the tags that
Telegram supports:

- The `<b>` and `<i>` tags set bold and italic text.
- The `<u>` and `<s>` tags set underlined and
  struck-through text.
- The `<code>` and `<pre>` tags set inline and block
  fixed-width text.
- The `<a href="...">` tag inserts a link.
- The `<blockquote>` tag sets a block quotation.
- The `<tg-spoiler>` tag, or a `<span>` element with
  the `tg-spoiler` class, hides text behind a spoiler.

Telegram rejects a message that contains any other tag,
or a `<`, `>`, or `&` character that is neither part of
a tag nor written as an HTML entity. The alerter
escapes the template variables before rendering, so an
alert title or a relation name that contains those
characters is delivered safely, while the markup in the
template is left intact.

Telegram limits a message to 4096 characters after
parsing. The alerter truncates a longer message on a
character boundary and appends an ellipsis. The cut is
also kept off a markup boundary, so truncation never
severs an HTML entity or an opening tag. The rewind
that achieves this is bounded. The alerter looks back
at most 16 bytes for an unterminated entity, and at
most 512 bytes for an unterminated tag. Those windows
cover a character entity and an `<a href="...">` tag;
a stray `&` or `<` further back does not move the cut.
The 4096-character limit applies after Telegram parses
the entities, so a long message that also carries
markup can still be refused.

If Telegram refuses a message because it cannot parse
the markup, the alerter sends the same text once more
with no parse mode. The message then arrives as plain
text rather than being lost. The Bot API reports such a
refusal as a 400 response whose description reads
`can't parse entities` or `can't find end tag`; the
alerter treats no other failure this way, and it makes
this retry only once.

The fallback matters most for a custom template whose
markup is malformed. Without the fallback, such a
template costs the operator the alert: the delivery is
retried on the usual schedule, recorded as `failed`,
and never seen in the chat. With the fallback, the
alert arrives, but the formatting is gone and the HTML
entities are visible, so `&amp;` appears in place of
`&`. An unformatted alert with visible entities is
therefore a reliable sign that the template contains
broken markup. Correct the template rather than leaving
the channel to fall back on every alert.

A custom template is trusted markup. An administrator
who holds `manage_notification_channels` can put any
markup that Telegram supports into a template,
including links, and every recipient of the channel
sees the result in a workbench message. That permission
already allows a webhook channel to post any body to
any host, so the two carry comparable trust; grant the
permission accordingly.

### Default Telegram Templates

The alerter provides a default template for each
notification type. Administrators can copy and
customize these templates.

The following template handles alert fire
notifications:

```html
{{.SeverityEmoji}} <b>Alert: {{.AlertTitle}}</b>

<b>Server:</b> {{.ServerName}} (<code>{{.ServerHost}}:{{.ServerPort}}</code>)
<b>Severity:</b> {{.Severity}}
{{if .DatabaseName}}<b>Database:</b> <code>{{.DatabaseName}}</code>
{{end}}{{if .MetricName}}<b>Metric:</b> <code>{{.MetricName}}</code>{{if .MetricValue}} = {{.MetricValue}}{{end}}{{if .ThresholdValue}} (threshold {{.Operator}} {{.ThresholdValue}}){{end}}
{{end}}<b>Triggered:</b> {{.TriggeredAt.Format "2006-01-02 15:04:05 MST"}}

{{.AlertDescription}}
```

The following template handles alert clear
notifications:

```html
✅ <b>Resolved: {{.AlertTitle}}</b>

<b>Server:</b> {{.ServerName}} (<code>{{.ServerHost}}:{{.ServerPort}}</code>)
<b>Severity:</b> {{.Severity}}
{{if .DatabaseName}}<b>Database:</b> <code>{{.DatabaseName}}</code>
{{end}}{{if .MetricName}}<b>Metric:</b> <code>{{.MetricName}}</code>
{{end}}<b>Duration:</b> {{.Duration}}
<b>Triggered:</b> {{.TriggeredAt.Format "2006-01-02 15:04:05 MST"}}
{{if .ClearedAt}}<b>Cleared:</b> {{.ClearedAt.Format "2006-01-02 15:04:05 MST"}}
{{end}}
{{.AlertDescription}}
```

The following template handles reminder notifications:

```html
⏰ <b>Reminder: {{.AlertTitle}}</b> is still active

<b>Reminder:</b> #{{.ReminderCount}}
<b>Server:</b> {{.ServerName}} (<code>{{.ServerHost}}:{{.ServerPort}}</code>)
<b>Severity:</b> {{.Severity}}
{{if .DatabaseName}}<b>Database:</b> <code>{{.DatabaseName}}</code>
{{end}}{{if .MetricName}}<b>Metric:</b> <code>{{.MetricName}}</code>{{if .MetricValue}} = {{.MetricValue}}{{end}}
{{end}}<b>Active since:</b> {{.TriggeredAt.Format "2006-01-02 15:04:05 MST"}}

{{.AlertDescription}}
```

## Webhook Channels

Webhook channels deliver alert notifications to any
HTTP endpoint. The webhook channel offers the most
flexibility through configurable HTTP methods, custom
headers, authentication options, and JSON payload
templates.

### Settings Tab

The Settings tab configures the core webhook
properties:

| Setting | Required | Default | Description |
|---------|----------|---------|-------------|
| Name | Yes | - | A descriptive name for the channel. |
| Description | No | - | An optional description. |
| Endpoint URL | Yes | - | The URL to send notifications to. |
| HTTP Method | No | POST | The HTTP method: POST, GET, PUT, or PATCH. |
| Enabled | No | On | Toggles the channel on or off. |

The Endpoint URL must be the final URL that handles the
request, because the alerter does not follow redirects;
see [Delivery Behaviour](#delivery-behaviour).

### Headers Tab

The Headers tab manages custom HTTP headers as
key-value pairs. Administrators can add or remove
headers dynamically to meet the requirements of the
target endpoint.

### Authentication Tab

The Authentication tab configures credentials for the
target endpoint. The following authentication types
are available:

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

- The Alert Fire template formats the payload when an
  alert triggers.
- The Alert Clear template formats the payload when an
  alert resolves.
- The Reminder template formats the payload for
  recurring alert reminders.

If left blank, the system uses sensible default
templates for each notification type.

### Template Variables

Templates have access to the following context
variables:

| Variable | Type | Description |
|----------|------|-------------|
| `AlertID` | integer | The unique alert identifier. |
| `AlertTitle` | string | The alert rule name. |
| `AlertDescription` | string | A detailed description of the alert. |
| `Severity` | string | The severity level: `critical`, `warning`, or `info`. |
| `SeverityColor` | string | A hex color for the severity. |
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
`MetricValue`, `ThresholdValue`, `Operator`, and
`ClearedAt` should use `{{if .FieldName}}...{{end}}`
conditionals in templates to handle empty values.

Time fields support formatting with the Go time layout
syntax. In the following example, the `TriggeredAt`
field uses ISO 8601 format:

```
{{.TriggeredAt.Format "2006-01-02T15:04:05Z07:00"}}
```

### Default Templates

The system provides default templates for each
notification type. Administrators can copy and
customize these templates.

The following template handles alert fire
notifications:

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

The following template handles alert clear
notifications:

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

## Estate Defaults

Notification channels can be designated as estate
defaults. A channel marked as an estate default is
active for all monitored servers unless explicitly
overridden at a lower level. Administrators toggle the
estate default flag in the channel create or edit
dialog.

The estate default flag provides a convenient way to
enable a channel across the entire monitoring estate
without creating individual overrides for each server,
cluster, or group.

## Channel Overrides

Channel overrides control which notification channels
are active at each level of the server hierarchy. The
override system uses the following precedence order,
from highest to lowest priority:

1. Server overrides apply to a specific server
   connection.
2. Cluster overrides apply to all servers in a cluster.
3. Group overrides apply to all clusters in a group.
4. Estate defaults apply when no override exists.

When the alerter resolves notification channels for a
server, the system checks for a server-level override
first. If none exists, the system checks the cluster
level, then the group level, and finally falls back to
the channel's estate default setting.

### Managing Overrides

Overrides are managed through the Notification Channels
tab in the server, cluster, or group edit dialogs. The
override panel displays all enabled channels with
their current state:

- Channels without an override inherit the estate
  default and appear with dimmed styling.
- Channels with an override display at normal opacity
  with a highlight indicator.
- The Enabled switch toggles the channel on or off at
  the current scope level.
- The Reset button removes the override, reverting to
  the inherited value.

### Override Resolution Example

Consider a Slack channel marked as an estate default.
A group override disables the channel for a development
group. A server override re-enables the channel for one
server in that group. The alerter resolves notifications
as follows:

- Servers in other groups receive notifications
  because the estate default applies.
- Servers in the development group do not receive
  notifications because the group override applies.
- The one server with a server override does receive
  notifications because the server override takes
  precedence.

## REST API

The notification channel REST API provides endpoints
for managing channels, testing delivery, and managing
email recipients. All endpoints require the
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

### Channel Override Endpoints

The channel override REST API manages per-scope channel
settings. Write operations require the
`manage_notification_channels` permission.

The following table lists the available endpoints:

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/channel-overrides/{scope}/{scopeId}` | List channel overrides for a scope. |
| `PUT` | `/api/v1/channel-overrides/{scope}/{scopeId}/{channelId}` | Create or update a channel override. |
| `DELETE` | `/api/v1/channel-overrides/{scope}/{scopeId}/{channelId}` | Remove a channel override. |

The `scope` parameter accepts `server`, `cluster`, or
`group`. The `scopeId` parameter is the numeric
identifier for the server connection, cluster, or
group. The PUT request body contains a single field:

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | boolean | Whether the channel is active at this scope. |

The GET response returns an array of channel override
objects:

| Field | Type | Description |
|-------|------|-------------|
| `channel_id` | integer | The notification channel identifier. |
| `channel_name` | string | The channel display name. |
| `channel_type` | string | The channel type (email, slack, mattermost, telegram, webhook). |
| `description` | string | The channel description; may be null. |
| `is_estate_default` | boolean | Whether the channel is an estate default. |
| `has_override` | boolean | Whether an override exists at this scope. |
| `override_enabled` | boolean | The override enabled state; null when no override exists. |

## Related Documentation

- [Alert Rules](alert-rules.md) describes the rules
  that trigger notifications.
- [Managing Users and Permissions](managing-users-and-permissions/permission_model.md) covers the
  permissions required for channel management.
- [API Reference](api/reference.md) provides
  interactive API documentation.
