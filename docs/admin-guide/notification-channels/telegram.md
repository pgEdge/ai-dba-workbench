# Configuring Telegram Channels

Telegram channels deliver alert notifications to a Telegram chat, group, or
channel through the Telegram Bot API. A Telegram channel stores two values
rather than a single webhook URL: a bot token that identifies the sending bot,
and a chat ID that identifies the destination.
[Managing Notification Channels](index.md) describes the operations that every
channel type shares, such as creating, testing, and enabling a channel.

## Creating a Telegram Channel

Use the following steps to create a Telegram channel in the Workbench:

1. Create a bot and find the chat ID, as described in
   [Creating a Telegram Bot](#creating-a-telegram-bot) and
   [Finding the Chat ID](#finding-the-chat-id).
2. Open the `Administration` console, and select `Telegram Channels` in the
   `NOTIFICATIONS` section of the navigation pane.
3. Select `Add Channel` to open the `Create Telegram channel` dialog.
4. Enter a name for the channel and, optionally, a description; then enter the
   bot token in the `Bot Token` field and the chat ID in the `Chat ID` field.
5. Leave `Enabled` on, and turn on `Estate Default` if the channel should be
   active for every server that has no override.
6. Select `Create` to save the channel.
7. Select the send icon (tooltip `Send test notification`) in the channel's row
   to send a test message and confirm the configuration.

To change a channel later, select the edit icon in the channel's row on the
`Telegram Channels` page.

## Telegram Channel Settings

The following table describes the settings in the `Create Telegram channel`
dialog:

| Setting | Required | Description |
|---------|----------|-------------|
| Name | Yes | A descriptive name for the channel. |
| Description | No | An optional description of the channel. |
| Bot Token | Yes | The token that BotFather issued for the bot, in the form `<bot id>:<token>`. |
| Chat ID | Yes | The numeric chat ID, or an `@channelusername` for a public channel. |
| Enabled | No | Toggles the channel on or off; on by default. |
| Estate Default | No | Makes the channel active for every server that has no override; off by default. See [Estate Defaults](index.md#estate-defaults). |

The Workbench encrypts the bot token before storing it and never returns the
token after saving. The channel list reports only whether a token is
configured, and the Bot Token field is blank when an administrator edits a
channel; enter a value only to replace the stored token. The chat ID is not a
secret, so the admin panel and the REST API display the stored value.

The send icon (tooltip `Send test notification`) delivers a test message to the
configured chat, which confirms that the bot token and the chat ID are correct
and that the bot may post.

The REST API carries the two values in the `telegram_bot_token` and
`telegram_chat_id` fields of a create or update request. A response reports
`telegram_bot_token_set` in place of the token, and an update that omits
`telegram_bot_token` keeps the stored token.

## Creating a Telegram Bot

BotFather is the Telegram bot that registers and manages other bots. To create
a bot for the workbench, follow these steps:

1. Open a chat with [@BotFather](https://t.me/BotFather) and send the `/newbot`
   command.
2. Choose a display name and a username for the bot; the username must end in
   `bot`.
3. Copy the token that BotFather returns into the Bot Token field, and treat
   the token as a password.
4. Add the bot to the group or channel that receives the notifications.
5. Promote the bot to administrator when the destination is a channel, so that
   the bot may post messages.

## Finding the Chat ID

The chat ID identifies the destination that the bot posts to. To find the chat
ID of a chat, group, or channel, follow these steps:

1. Send a message in the target chat, group, or channel. In a group where the
   bot's privacy mode is on, which is the default, send a command addressed to
   the bot, such as `/start@your_bot`, because the bot does not receive other
   group messages.
2. Request `https://api.telegram.org/bot<token>/getUpdates`, replacing
   `<token>` with the bot token.
3. Read the chat ID from the JSON response. A chat or group message carries the
   ID in the `result[].message.chat.id` field, and a channel post carries the
   ID in the `result[].channel_post.chat.id` field.

Group and supergroup IDs are negative numbers, such as `-1001234567890`,
whereas a chat with a single user has a positive ID. For a public channel,
enter `@channelusername` in place of the numeric ID.

For details of the Bot API methods, see the
[Telegram Bot API documentation](https://core.telegram.org/bots/api).

## Securing the Bot Token

The bot token is a bearer credential; anyone who holds the token can post as
the bot and read the updates that the bot receives. The workbench encrypts the
token with the server secret, never displays the token after saving, and
redacts the token from log messages and from delivery errors recorded against a
notification. The alerter also refuses HTTP redirects when it delivers a
message, so the token cannot travel to another host in a `Referer` header; see
[Handling HTTP Redirects](index.md#handling-http-redirects).

If a token is exposed, use BotFather to revoke the token and issue a
replacement, then save the new token in the channel configuration.

## Templates

Telegram channels support custom templates through the `template_alert_fire`,
`template_alert_clear`, and `template_reminder` fields of the REST API, and the
templates use the variables listed in
[Template Variables](webhook.md#template-variables).

A Telegram template renders the message text, not a JSON request body. This
differs from the Slack, Mattermost, and webhook templates, which render a
complete JSON payload. The alerter builds the request body itself and sends the
rendered text with the `sendMessage` method of the Bot API, so a template that
renders JSON delivers the JSON as visible message text.

The alerter sends messages in HTML parse mode rather than MarkdownV2, because
MarkdownV2 requires escaping eighteen characters, among them `_`, `.`, `-`,
`(`, and `)`, which occur constantly in PostgreSQL relation and index names. A
template may use only the tags that Telegram supports:

- The `<b>` and `<i>` tags set bold and italic text.
- The `<u>` and `<s>` tags set underlined and struck-through text.
- The `<code>` and `<pre>` tags set inline and block fixed-width text.
- The `<a href="...">` tag inserts a link.
- The `<blockquote>` tag sets a block quotation.
- The `<tg-spoiler>` tag, or a `<span>` element with the `tg-spoiler` class,
  hides text behind a spoiler.

Telegram rejects a message that contains any other tag, or a `<`, `>`, or `&`
character that is neither part of a tag nor written as an HTML entity. The
alerter escapes the template variables before rendering, so an alert title or a
relation name that contains those characters is delivered safely, while the
markup in the template is left intact.

Telegram limits a message to 4096 characters after parsing. The alerter
applies the same 4096-character limit to the rendered text before Telegram
parses it, counting the HTML tags and the escaped entities, so the alerter can
truncate a message whose parsed text would have fitted; a message with heavy
markup or many escaped characters loses its end sooner than the limit
suggests. The alerter truncates a longer message on a character boundary and
appends an ellipsis. The
cut is also kept off a markup boundary, so truncation never severs an HTML
entity or an opening tag. The rewind that achieves this is bounded. The alerter
looks back at most 16 bytes for an unterminated entity, and at most 512 bytes
for an unterminated tag. Those windows cover a character entity and an
`<a href="...">` tag; a stray `&` or `<` further back does not move the cut.
The 4096-character limit applies after Telegram parses the entities, so a long
message that also carries markup can still be refused.

If Telegram refuses a message because it cannot parse the markup, the alerter
sends the same text once more with no parse mode. The message then arrives as
plain text rather than being lost. The Bot API reports such a refusal as a 400
response whose description reads `can't parse entities` or
`can't find end tag`; the alerter treats no other failure this way, and it
makes this retry only once.

The fallback matters most for a custom template whose markup is malformed.
Without the fallback, such a template costs the operator the alert; the alerter
retries the delivery on the usual schedule and records it as `failed`, and the
alert never appears in the chat. With the fallback, the alert arrives, but the
formatting is gone and the HTML entities are visible, so `&amp;` appears in
place of `&`. An unformatted alert with visible entities is therefore a
reliable sign that the template contains broken markup. Correct the template
rather than leaving the channel to fall back on every alert.

A custom template is trusted markup. An administrator who holds
`manage_notification_channels` can put any markup that Telegram supports into a
template, including links, and every recipient of the channel sees the result
in a workbench message. That permission already allows a webhook channel to
post any body to any host, so the two carry comparable trust; grant the
permission accordingly.

## Default Telegram Templates

The alerter provides a default template for each notification type.
Administrators can copy and customize these templates.

The following template handles alert fire notifications:

```html
{{.SeverityEmoji}} <b>Alert: {{.AlertTitle}}</b>

<b>Server:</b> {{.ServerName}} (<code>{{.ServerHost}}:{{.ServerPort}}</code>)
<b>Severity:</b> {{.Severity}}
{{if .DatabaseName}}<b>Database:</b> <code>{{.DatabaseName}}</code>
{{end}}{{if .MetricName}}<b>Metric:</b> <code>{{.MetricName}}</code>{{if .MetricValue}} = {{.MetricValue}}{{end}}{{if .ThresholdValue}} (threshold {{.Operator}} {{.ThresholdValue}}){{end}}
{{end}}<b>Triggered:</b> {{.TriggeredAt.Format "2006-01-02 15:04:05 MST"}}

{{.AlertDescription}}
```

The following template handles alert clear notifications:

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
