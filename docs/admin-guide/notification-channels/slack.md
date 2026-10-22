# Configuring Slack Channels

Slack channels deliver alert notifications to a Slack workspace channel through
an incoming webhook URL.
[Managing Notification Channels](index.md) describes the operations that every
channel type shares, such as creating, testing, and enabling a channel.

## Creating a Slack Channel

Use the following steps to create a Slack channel in the Workbench:

1. Create an incoming webhook in Slack, as described in
   [Creating a Slack Webhook](#creating-a-slack-webhook).
2. Open the `Administration` console, and select `Slack Channels` in the
   `NOTIFICATIONS` section of the navigation pane.
3. Select `Add Channel` to open the `Create Slack channel` dialog.
4. Enter a name for the channel and, optionally, a description; then paste the
   webhook URL into the `Webhook URL` field.
5. Leave `Enabled` on, and turn on `Estate Default` if the channel should be
   active for every server that has no override.
6. Select `Create` to save the channel.
7. Select the send icon (tooltip `Send test notification`) in the channel's row
   to send a test message and confirm the configuration.

To change a channel later, select the edit icon in the channel's row on the
`Slack Channels` page.

## Slack Channel Settings

The following table describes the settings in the `Create Slack channel`
dialog:

| Setting | Required | Description |
|---------|----------|-------------|
| Name | Yes | A descriptive name for the channel. |
| Description | No | An optional description of the channel. |
| Webhook URL | Yes | The Slack incoming webhook URL. |
| Enabled | No | Toggles the channel on or off; on by default. |
| Estate Default | No | Makes the channel active for every server that has no override; off by default. See [Estate Defaults](index.md#estate-defaults). |

## Creating a Slack Webhook

To create an incoming webhook for Slack, follow these steps:

1. Create a Slack App in the Slack API dashboard.
2. Enable the Incoming Webhooks feature for the app.
3. Create a new webhook and select a target channel.
4. Copy the generated webhook URL into the channel settings.

For detailed instructions, see the
[Slack Webhooks documentation](https://api.slack.com/messaging/webhooks).

## Slack and Mattermost Templates

Slack and Mattermost channels support custom templates through the
`template_alert_fire`, `template_alert_clear`, and `template_reminder` fields
of the REST API. Each template renders the complete JSON body of the webhook
request, using the variables listed in
[Template Variables](webhook.md#template-variables).

The alerter escapes the template variables for the service's markup before
rendering, and leaves the template text intact. Alert titles, descriptions, and
server and database names can contain text from outside the Workbench, so the
escaping stops that text from adding a mention, a link, or formatting to a
message:

- For Slack, the alerter replaces `&`, `<`, and `>` with `&amp;`, `&lt;`, and
  `&gt;`, as the Slack formatting documentation requires, so a value cannot
  form a `<!channel>` announcement or a `<url|text>` link.
- For Mattermost, the alerter places a backslash before each Markdown character
  in a value, and a zero-width space after each `@`, so a value cannot format
  text, form a link, or mention `@channel`, `@here`, `@all`, or a user.

Mentions, links, and formatting written into a custom template itself still
work. Place variables in fields that the service renders as Markdown, such as
`text` or a field `value`; in a field that the service shows as plain text,
such as a field `title`, the escape characters may appear literally.
