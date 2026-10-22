# Configuring Mattermost Channels

Mattermost channels deliver alert notifications to a Mattermost channel through
an incoming webhook URL.
[Managing Notification Channels](index.md) describes the operations that every
channel type shares, such as creating, testing, and enabling a channel.

## Creating a Mattermost Channel

Use the following steps to create a Mattermost channel in the Workbench:

1. Create an incoming webhook in Mattermost, as described in
   [Creating a Mattermost Webhook](#creating-a-mattermost-webhook).
2. Open the `Administration` console, and select `Mattermost Channels` in the
   `NOTIFICATIONS` section of the navigation pane.
3. Select `Add Channel` to open the `Create Mattermost channel` dialog.
4. Enter a name for the channel and, optionally, a description; then paste the
   webhook URL into the `Webhook URL` field.
5. Leave `Enabled` on, and turn on `Estate Default` if the channel should be
   active for every server that has no override.
6. Select `Create` to save the channel.
7. Select the send icon (tooltip `Send test notification`) in the channel's row
   to send a test message and confirm the configuration.

To change a channel later, select the edit icon in the channel's row on the
`Mattermost Channels` page.

## Mattermost Channel Settings

The following table describes the settings in the `Create Mattermost channel`
dialog:

| Setting | Required | Description |
|---------|----------|-------------|
| Name | Yes | A descriptive name for the channel. |
| Description | No | An optional description of the channel. |
| Webhook URL | Yes | The Mattermost incoming webhook URL. |
| Enabled | No | Toggles the channel on or off; on by default. |
| Estate Default | No | Makes the channel active for every server that has no override; off by default. See [Estate Defaults](index.md#estate-defaults). |

## Creating a Mattermost Webhook

To create an incoming webhook in Mattermost, follow these steps:

1. Navigate to Main Menu, then Integrations.
2. Select Incoming Webhooks and create a new webhook.
3. Choose the target channel for notifications.
4. Copy the generated webhook URL into the channel settings.

For detailed instructions, see the
[Mattermost Incoming Webhooks documentation](https://developers.mattermost.com/integrate/webhooks/incoming/).

## Mattermost Templates

Mattermost channels support custom templates in the same way as Slack channels;
[Slack and Mattermost Templates](slack.md#slack-and-mattermost-templates)
describes the template fields and how the alerter escapes template variables
for Mattermost.
