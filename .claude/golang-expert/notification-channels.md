/*-----------------------------------------------------------
 *
 * pgEdge AI DBA Workbench - Notification Channels
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-----------------------------------------------------------
 */

# Notification Channels in the Alerter

Channel types live in `alerter/src/internal/database/
notification_types.go` and each has a `Notifier` registered in
`NewManager` (`alerter/src/internal/notifications/manager.go`). The
columns behind them are created by the consolidated migration #1 in
`collector/src/database/schema.go`; every later addition needs both a
change to that `CREATE TABLE notification_channels` block, so fresh
installs are correct, and a numbered migration, so existing ones are.

That rule governs `notification_channels`, a plain, non-partitioned
configuration table, and not every table in the schema. The
partitioned `metrics.*` tables follow the opposite convention, where
the migration is the only place the column appears: see
`partitioning.md`.

Slack, Mattermost and the generic webhook all share
`sendWebhookNotification` in `webhook_sender.go`: one stored URL, one
`RenderJSON` call that produces the whole request body, one POST.
Telegram does not fit that shape and must not be forced into it.

## Telegram Is the Bot API, Not a Webhook

`telegram.go` posts to
`https://api.telegram.org/bot<token>/sendMessage` with a JSON body
carrying `chat_id`, `text`, `parse_mode` and
`disable_web_page_preview`. Four consequences drive the design:

- A channel needs two secrets' worth of configuration:
  `telegram_bot_token_encrypted` (decrypted in
  `decryptChannelSecrets`) and `telegram_chat_id`. The chat ID is
  TEXT and is sent as a JSON *string*, because the Bot API accepts
  either a numeric ID or an `@channelusername`.
- The template renders the message *text*, not the envelope. The body
  is always produced by `json.Marshal` on
  `telegramSendMessageRequest`; never build it by concatenation.
- Failures arrive as `{"ok":false,"error_code":...,"description":...}`,
  frequently alongside a 200. Check `ok`, not the status code.
- `text` is limited to 4096 characters *after entity parsing*, counted
  in characters. `truncateTelegramText` cuts on a rune boundary **and
  off markup boundaries**: the text is already escaped, so a cut inside
  `&amp;` or `<code>` produces a 400 that no retry can fix. See
  "Truncation and the Plain-Text Fallback" below.

`apiBaseURL` on `telegramNotifier` exists only so tests can point at an
`httptest` server. It is not reachable from the constructor or from
configuration, which is what keeps this channel free of the SSRF
surface the operator-supplied webhook URLs carry. Do not expose it.

## The Bot Token Must Never Reach an Error String

The token is a bearer credential and it sits in the request path, so
every error `net/http` produces about a failed request repeats it. The
alerter logs notifier errors and writes them to
`notification_history.error_message`, both readable by people who are
not entitled to the credential.

Nothing in `telegram.go` wraps a transport error with `%w`.
`telegramTransportError` reduces a `*url.Error` to its `Op` and cause,
and `redactTelegramToken` rewrites `/bot<token>` to `/bot<redacted>` in
any borrowed text, including a `description` the API echoed back.
`TestTelegramNotifier_Send_TransportErrorRedactsToken` and its
siblings assert the token never appears; keep them.

**`redactTelegramToken` must fail closed.** Once it finds `/bot` it
always replaces the token run that follows, whether or not the `/` that
terminates `/bot<token>/sendMessage` is present. An earlier version gave
up when no `/` followed and copied the remainder out verbatim, which
printed a live credential for any input in an unexpected shape: a
truncated URL in a proxy error, a `description` echoing a partial path,
a future caller building some other Bot API URL. Never reintroduce that
bail-out. An *empty* run (the literal `/bot`, or `/bot/`) is not a
secret and is emitted unchanged, because a placeholder there would only
mislead.

**The terminator class must be a subset of what the validator forbids.**
`isTelegramTokenTerminator` is exactly `c <= ' '` (all whitespace and
control characters, which cannot appear in a URL) plus `/`, `?` and `#`.
Nothing else. The server's `telegramBotTokenPattern` forbids every one
of those *and more*, so every character a token may legally contain is
swallowed into the redacted run rather than ending it.

That invariant was inverted once and it cost a credential leak
(VULN-002). The class used to include `"`, `'`, `` ` ``, `<`, `>`, `)`,
`]`, `,` and `;` on the reasoning that those characters *surround* a URL
in an error or a log line - but the validator let a token *contain* them,
so a token carrying any one of them stopped redaction dead and printed
its whole tail. Fuzzing validator-accepted tokens leaked runs of up to 37
characters. Reason about what a token may contain, never about what sits
around it. Do not re-add "defensive" quoting or bracketing characters.

The price is over-redaction of a few bytes of boilerplate: the truncated
shape `Post "https://api.telegram.org/bot<TOKEN>": EOF` now redacts the
trailing `":` too, giving `Post "https://api.telegram.org/bot<redacted>
EOF`. That is the right trade and the table tests pin it; do not "fix"
those expectations by widening the class.

`TestRedactTelegramTokenNeverEmitsTheToken` varies the *context* around
a fixed token; `TestRedactTelegramTokenNeverEmitsAFuzzedToken` varies
the *token* over the whole validator-accepted alphabet and asserts no
four-byte run of the body survives. Both are needed - the first alone is
what missed VULN-002.

**Bound what is echoed, not just what is read.** `sanitizeTelegramEcho`
wraps every borrowed string before it is interpolated into an error:
redact the token, map control characters to spaces (a hostile endpoint
can otherwise forge log lines, and the result also stays storable in a
Postgres text column), then cap at `telegramMaxEchoedBytes` (256) on a
rune boundary. The response body is read under a 1 MiB `io.LimitReader`,
but that is a *read* limit; without the echo cap a captive portal writes
a megabyte into the log and into `notification_history.error_message` on
every one of the three delivery attempts. Redaction runs before the cap
so the cap can never slice a token run in half.

**Both modules must stay in step.** `sanitizeTelegramEcho`,
`redactTelegramToken`, `isTelegramTokenTerminator` and
`telegramTransportError` are duplicated verbatim in
`alerter/src/internal/notifications/telegram.go` and
`server/src/internal/api/webhook_test_sender.go` (separate Go modules,
so no sharing). The block from `// telegramMaxEchoedBytes bounds` to the
end of each file is character-for-character identical; diff it
mechanically after any change, because a divergence reopens the hole in
one module only.

## The Shared HTTP Client Refuses Redirects

`NewManager` builds one `http.Client` and hands it to every notifier,
and it sets `CheckRedirect` to return `http.ErrUseLastResponse`. Leaving
it nil makes `net/http` follow up to ten redirects **and copy the
previous request's full URL into the `Referer` header** of each one.
That was VULN-001: a 3xx from the endpoint handed
`https://api.telegram.org/bot<token>/sendMessage` - the live bot token -
to whatever host `Location` named. Slack and Mattermost leak their whole
webhook URL, which is itself the secret, identically, and for the
generic webhook channel following a `Location` also bypasses the host
validation done before the URL was stored.

Set it on the *shared* client, not on a Telegram-only one. The cost is
real and accepted: a provider that answers a delivery with a 3xx now
fails that delivery instead of following it, which matches the
test-send path in `webhook_test_sender.go`, where all three senders have
always refused redirects.
`TestManager_HTTPClientRefusesRedirects` asserts the redirect target is
never reached, and it exercises all four channel types that share the
client: Telegram, Slack, Mattermost and the generic webhook. The webhook
case needs two departures from the others - its URL lives in
`EndpointURL`, not `WebhookURL`, and the notifier is rebuilt with
`NewWebhookNotifierAllowInternal` because its SSRF guard would otherwise
reject the `httptest` address before the shared client ever ran. Email
is out of scope there: it does not use the HTTP client.

## Truncation and the Plain-Text Fallback

`truncateTelegramText` cuts at rune 4095 of the *already escaped*
string, so the naive cut lands inside `&amp;` or inside `<code>` for
any message of the wrong length - and escaping inflates the text before
truncation (`"` becomes six bytes), so this is reachable with an
ordinary long alert description. Telegram answers a split entity with
`400 ... can't parse entities` and an unclosed tag with `400 ... can't
find end tag`, which are not transport errors: the queue retries twice
and marks the notification `failed`, and the alert is never delivered.

Two defences, and both are needed:

- `backOffTelegramMarkup` rewinds the cut to before an unterminated
  `&...` or `<...`. Both rewinds are bounded
  (`telegramMaxEntityBytes`, `telegramMaxTagBytes`) so that a stray `<`
  far back in a custom template cannot swallow the whole message.
- `Send` retries once with no `parse_mode` when the Bot API reports a
  400 whose description matches `telegramParseFailureMarkers`. The
  operator gets the alert as plain text, entities visible, rather than
  nothing. `telegramAPIError.isParseFailure` keeps the fallback narrow:
  every other failure stays with the queue's own retry and backoff.

Use `utf8.RuneCountInString` plus `telegramByteOffsetOfRune` rather than
materializing `[]rune(text)` - four bytes per character - for every
message.

## RenderHTML, Not Render, for Telegram Templates

`Render` routes to `html/template` only when `isHTMLTemplate` matches,
and that helper recognizes just `<!doctype html`, `<html`, `<body` and
`<div`. The Telegram defaults start with an emoji, so `Render` would
apply **no escaping at all** and an alert title containing `<` or `&`
would produce markup Telegram rejects with a 400.

`RenderHTML` (`template.go`) compiles with text/template and escapes
the *data* with `htmlEscapeStringValues`, so literal `<b>` tags in the
template survive while payload values become entities.
`html.EscapeString` also emits `&#39;` and `&#34;`, which Telegram
accepts because it supports all numeric entities.

`htmlEscapeStringValues` is **total**: `htmlEscapeValue` recurses into
`[]string`, `[]any`, `map[string]string` and `map[string]any`, rebuilding
nested containers rather than mutating them (they are shared with the
payload that the same alert fans out to every other channel). It used to
skip every non-string, which was safe only by accident - every
non-string the payload produces today is numeric or a `time.Time`.
`TestNotificationPayloadFieldTypesAreEscapable` reflects over
`database.NotificationPayload` and fails when an exported field has a
type outside the set `string`, `*string`, `bool`, numeric, `time.Time`
and `*time.Time`, so the first field of affected relations or map of
labels forces somebody back to this function before it can reach a
`parse_mode: HTML` message.

HTML parse mode is used rather than MarkdownV2 deliberately: MarkdownV2
requires escaping eighteen characters, among them `_`, `.`, `-`, `(`
and `)`, which occur constantly in PostgreSQL relation and index names.

`TestTelegramDefaultTemplatesAreNotMatchedByIsHTMLTemplate` guards the
trap: never start a Telegram default with `<div`, `<html` or `<body`.

## Widening the channel_type CHECK

Migration #14 widens the constraint by dropping and re-adding it; a
CHECK cannot be altered in place. `addConstraintIfMissing` is the wrong
tool here because it only adds a constraint that is absent and leaves a
narrower existing definition alone. The original was declared inline,
so PostgreSQL named it `notification_channels_channel_type_check`; the
widened one is added explicitly as
`chk_notification_channels_channel_type`. Drop both names IF EXISTS
first so the migration is idempotent from either state.

Any further channel type must repeat that dance and must update three
places in `alerter/src/internal/database/notification_queries.go`:
`GetNotificationChannel`, `GetNotificationChannelsForConnection` and
`GetDueReminders` each select and scan the channel columns by hand, and
a column added to one but not the others fails only at run time.

A slip *between* adjacent columns of the same type is quieter still.
`telegram_chat_id` sits between `telegram_bot_token_encrypted` and
`template_alert_fire`, all nullable text, so a SELECT list and a scan
list that drift apart produce no type error at all; the symptom would be
Telegram alerts arriving in the wrong chat. Three tests give each of
those columns a distinct marker and assert the field that owns it:
`TestGetNotificationChannel_TelegramFields`,
`TestGetNotificationChannelsForConnection_TelegramFields` and
`TestGetDueReminders_TelegramFields`.

## The Server Half

The MCP server keeps its own copy of the channel model in
`server/src/internal/database/notification_queries.go`. A new column has
to be added at **six** sites there, not three: the SELECT in
`ListNotificationChannels`, the SELECT in `GetNotificationChannel`, the
INSERT in `CreateNotificationChannel`, the SET list in
`UpdateNotificationChannel`, and the argument lists of both
`scanNotificationChannel` (pgx.Rows) and `scanNotificationChannelRow`
(pgx.Row). Missing the UPDATE is the quiet failure: creation works, and
the value disappears the first time anybody edits the channel. Unlike
the alerter, the server does not alias the encrypted columns, so
`telegram_bot_token_encrypted` is selected under its own name and
scanned into `TelegramBotToken`.

`TelegramBotToken` carries `json:"-"` with a companion
`TelegramBotTokenSet` (issue #187), set in
`decryptNotificationChannelSecrets` alongside `WebhookURLSet`.
`TelegramChatID` is **not** redacted: it addresses the destination chat
rather than authenticating to it, and the UI displays it.

## sendTestTelegram Has No Host Validation, Deliberately

`server/src/internal/api/webhook_test_sender.go` sits next to two
senders that are each preceded by `h.hostValidator.ValidateHost(...)` in
`testChannel`, because both dispatch to an operator-supplied URL.
`sendTestTelegram` is not, and must not be: it builds its destination
from the constant `telegramAPIBaseURL`, so there is no host to validate.
The seam `telegramSendBaseURL` exists only for in-package tests. Making
it configurable would manufacture the SSRF surface the validator exists
to close.

gosec's G704 (SSRF via taint analysis) nonetheless flags both
`http.NewRequest` and `client.Do` in `sendTestTelegram`, and the flow it
follows is not the obvious one. The token value is not request data:
`testChannel` reads it from a persisted `notification_channels` row that
`h.datastore.GetNotificationChannel` returns. What carries the taint is
the *lookup key*. `handleChannelSubpath` parses `channelID` out of
`r.URL.Path`, and a `*http.Request` parameter is one of G704's
configured sources, so `channelID` reaches `testChannel` tainted. The
datastore is a different Go package, so the analyzer has no SSA body for
`GetNotificationChannel` and applies its conservative fallback: any
tainted non-context argument taints the returned value. The whole
`*database.NotificationChannel` is tainted from there on, and so is
every field read off it, `TelegramBotToken` included.

Mutating a scratch copy pins each step down. Replacing `r.URL.Path` with
a literal silences both findings, and so does replacing `channelID` with
a literal even though `r.Context()` still reaches the query, because the
engine skips `context.Context` arguments. Answering the same lookup from
a method inside the analyzed package silences them too, which is what
identifies the missing callee body rather than any real flow as the
cause. Redo those mutations before trusting any other account of this
finding.

Each of the two lines therefore carries a `//nolint:gosec // G704: ...`
directive. The justification must not claim that a host check happens,
because none does and none is needed. The destination host is the
compile-time constant `telegramAPIBaseURL`, which no operator input can
reach; `telegramBotTokenPattern` confines the token to the path by
forbidding whitespace, `/`, `?` and `#`; and `CheckRedirect` refuses
every 3xx, so no `Location` header can move the request.

G704 is a recent rule. gosec v2.22.8, bundled with golangci-lint
v2.5.0, has no G7xx rules at all; gosec v2.28.0, bundled with
golangci-lint v2.13.2, has G701 to G710. CI installs
`golangci-lint@latest`, so a locally pinned v2.5.0 reports zero issues
on code that CI rejects.

`postMessage` in `alerter/src/internal/notifications/telegram.go`
builds the same URL but is not flagged, and the reason is the absence of
a source rather than anything about where the token comes from. The
alerter serves no HTTP traffic, so no `*http.Request` exists anywhere in
that chain, and the channel is fetched with `*history.ChannelID`, a
value that came out of the database. Do not read the silence as proof
that the flow was checked. Injecting `os.Getenv` into the alerter's
channel lookup still produces no finding, because the channel then has
to cross the `Notifier` interface dispatch in `manager.go`, and gosec
mis-indexes the arguments of an interface invoke: the receiver occupies
parameter 0 of the method but is absent from the call site's argument
list, so the taint is dropped at that hop. `postMessage` carries no
`//nolint` and must not gain one, because a directive for a rule that
never fires is noise.

The server is a separate Go module from the alerter, so
`redactTelegramToken` and `telegramTransportError` are duplicated into
`webhook_test_sender.go` rather than imported. They must stay in step
with `alerter/src/internal/notifications/telegram.go`; both copies carry
a comment saying so. The reason is the same on both sides: the bot token
is in the request path, `testChannel` logs the sender's error with
`log.Printf("[ERROR] ...")`, and a `%w` wrap would put a live credential
in the server log.

## Validating Telegram Identifiers Loosely

`validateTelegramFields` in `notification_channel_handlers.go` checks
`<digits>:<body>` for the token and `-?<digits>` or `@<username>` for the
chat ID, and nothing more. Telegram publishes no stable contract for
token length or ID range, and both have widened, so a tighter check
rejects working channels.

The token body is ``[^\x00-\x20\x7f/?#"'`<>)\],;]+``. The first half is
what would change the meaning of the request path; the second is what
surrounds a URL in an error or a log line. That second half exists to
satisfy the subset invariant above - the redactor's terminator class has
to be a subset of what this pattern forbids - not because Telegram would
ever issue such a token. Real BotFather tokens use `[A-Za-z0-9_-]`. The
range is written `\x00-\x20` rather than `\s` so that it lines up byte
for byte with `c <= ' '` in `isTelegramTokenTerminator`; `\s` would leave
the other control characters legal inside a token, and gocritic rejects
having both.
`TestTelegramBotTokenPatternExcludesRedactionTerminators` pins it from
this side and `TestTelegramFuzzAlphabetIsValidatorAccepted` ties the
fuzz alphabet to it.

On update the validator runs against *effective* values: the request
value when the field was supplied, the stored value when it was omitted.
Omission has to keep the stored token, because the GET response never
returns it and a fetch-then-edit round trip therefore always omits it.

**A stored token is shape-checked only when the request converts the
channel to `telegram`.** `decryptNotificationSecret` returns the raw
stored value when decryption fails - a rotated or lost server secret -
and a stored ciphertext is `base64.StdEncoding`, whose alphabet holds
no `:`, so it can never match the pattern. Shape-checking it on every
`PUT` rejected them all, including the UI's enable/disable toggle,
whose whole body is `{"enabled": false}`, so an operator who had lost
the secret could not even switch the broken channel off (H-004). A
`PUT` on a channel that is *already* `telegram` and omits the token
therefore checks the stored value for *presence* only;
`TestUpdateChannel_TelegramUndecryptableStoredToken` covers it.

Changing `channel_type` **to** `telegram` is the exception, and it is
what keeps H-004 from reopening the hole it closed. A conversion is a
deliberate act, and the stored token it adopts may never have been
checked, so `updateChannel` shape-checks the stored value on that
transition;
`TestUpdateChannel_TelegramTypeSwitchChecksStoredToken` and
`TestUpdateChannel_TelegramTypeSwitchAcceptsValidStoredToken` pin both
outcomes. `createChannel` closes the `POST` end by assigning
`TelegramBotToken` and `TelegramChatID` only when `channel_type` is
`telegram`: its validation runs in that branch alone, so an
unconditional assignment let a `POST` for any other type park an
unchecked token in the row.
`TestCreateChannel_TelegramFieldsIgnoredForOtherTypes` covers that.
`updateChannel` still merges both fields whatever the type, so a `PUT`
can park an unchecked token on a non-telegram row; the conversion check
is what stops that value reaching `telegram`. Together the rules bound
what the type admits: every token written into a `telegram` row passed
the shape check at that write - in `createChannel`'s telegram branch,
on a `PUT` that supplied it, or at the conversion - and no unchecked
value is ever admitted to the type. A row that is already `telegram` is
never re-checked, so one exception stands: after a secret rotation
`decryptNotificationSecret` returns ciphertext rather than the checked
plaintext, and that stored value would fail a shape check today.

The same lock-out shape exists in principle for `telegram_chat_id`,
which is stored in plaintext and so cannot become unreadable - do not
extend the exemption to it without a reason.

## Postgres Caches Plans, Which Breaks Scanner Error Tests

The two scan-failure branches are only reachable by corrupting a column
type (`ALTER TABLE ... ALTER COLUMN headers_json TYPE TEXT`, then
storing invalid JSON). The poisoned SELECT must be the first one the
datastore issues: altering a column invalidates any plan Postgres has
already cached for a statement reading it, and "cached plan must not
change result type" then fires before the scan does. See
`TestScanNotificationChannelHeaderDecodeErrors` and
`TestScanNotificationChannelColumnTypeMismatch`.
