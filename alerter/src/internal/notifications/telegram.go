/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// telegramAPIBaseURL is the Bot API host. It is a constant on purpose:
// unlike the Slack, Mattermost and generic webhook channels, a Telegram
// channel stores no operator-supplied URL, so no configuration this
// service reads can make it issue a request to an arbitrary host. That
// removes the SSRF surface those channels have.
const telegramAPIBaseURL = "https://api.telegram.org"

// telegramMaxMessageRunes is the Bot API limit on the text field of
// sendMessage: 1-4096 characters after entity parsing. Telegram counts
// characters, not bytes.
const telegramMaxMessageRunes = 4096

// telegramTruncationMarker is appended, within the limit, when a
// rendered message has to be shortened.
const telegramTruncationMarker = "…"

// telegramParseModeHTML is the parse_mode every message is sent with.
// The empty string is the deliberate opposite: it asks the Bot API to
// treat the text as plain, which is what the retry in Send falls back
// to when Telegram cannot parse the markup.
const telegramParseModeHTML = "HTML"

// telegramMaxEntityBytes bounds how far truncateTelegramText rewinds to
// avoid cutting inside an HTML character entity. The entities
// html.EscapeString emits are at most six bytes ("&#34;"), and the
// longest named entity HTML5 defines is ten; sixteen covers both while
// keeping a stray '&' in a custom template from dragging the cut back
// any distance.
const telegramMaxEntityBytes = 16

// telegramMaxTagBytes bounds the same rewind for an unterminated tag.
// The tags Telegram's HTML parse mode supports are short, but
// <a href="..."> carries a URL, so the window has to be wide enough for
// one of those.
const telegramMaxTagBytes = 512

// telegramNotifier implements Notifier for the Telegram Bot API.
//
// Telegram is not an incoming-webhook service: messages are posted to
// https://api.telegram.org/bot<token>/sendMessage with a JSON body
// carrying chat_id and text, so a channel needs both a bot token and a
// chat ID, the template renders the message text rather than the whole
// request body, and failures are reported in the response body rather
// than through the status code.
type telegramNotifier struct {
	httpClient *http.Client
	renderer   TemplateRenderer

	// apiBaseURL is the Bot API host to talk to. It exists solely so
	// tests can point the notifier at an httptest server; it is not
	// exposed through the constructor and is not settable from any
	// configuration, so production traffic always goes to
	// telegramAPIBaseURL.
	apiBaseURL string
}

// telegramSendMessageRequest is the JSON body of a sendMessage call.
// The body is always built by marshaling this struct so that rendered
// message text, whatever it contains, cannot break out of the envelope.
type telegramSendMessageRequest struct {
	// ChatID is a string because the Bot API accepts either a numeric
	// chat ID or an @channelusername, and it accepts a numeric ID sent
	// in string form.
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
	// ParseMode is omitted when empty so that the plain-text retry
	// sends no parse_mode at all rather than an empty one.
	ParseMode             string `json:"parse_mode,omitempty"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

// telegramResponse is the envelope every Bot API method returns.
// A call that fails reports ok:false with an error_code and a
// description, frequently alongside a 200 status, so the body has to be
// decoded and ok checked rather than the status code trusted.
type telegramResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
}

// telegramAPIError is a decoded ok:false response. It is a distinct
// type so that Send can recognize the particular 400 the Bot API
// returns when it cannot parse the message markup and retry the send
// as plain text; every other failure is reported as a plain error.
//
// Description has already been through sanitizeTelegramEcho, so it
// carries no bot token, no control characters and no more than
// telegramMaxEchoedBytes of remote text.
type telegramAPIError struct {
	Code        int
	Description string
}

// Error implements error.
func (e *telegramAPIError) Error() string {
	return fmt.Sprintf("telegram API error %d: %s", e.Code, e.Description)
}

// telegramParseFailureMarkers are the descriptions the Bot API returns
// when the text is not valid for the requested parse_mode: a split or
// unknown entity gives "can't parse entities", an unclosed tag gives
// "can't find end tag". They are matched case-insensitively and as
// substrings because Telegram appends detail, as in `Bad Request: can't
// parse entities: Unsupported start tag "am" at byte offset 4051`.
var telegramParseFailureMarkers = []string{
	"can't parse entities",
	"can't find end tag",
}

// isParseFailure reports whether this failure is Telegram refusing the
// message because of its markup rather than for any other reason. Only
// such a failure is worth retrying without parse_mode: the alert cannot
// be delivered as written, but it can be delivered as plain text, and
// an undelivered alert is the worst outcome available.
func (e *telegramAPIError) isParseFailure() bool {
	if e.Code != http.StatusBadRequest {
		return false
	}
	lowered := strings.ToLower(e.Description)
	for _, marker := range telegramParseFailureMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

// NewTelegramNotifier creates a new Telegram notifier
func NewTelegramNotifier(httpClient *http.Client, renderer TemplateRenderer) Notifier {
	return &telegramNotifier{
		httpClient: httpClient,
		renderer:   renderer,
		apiBaseURL: telegramAPIBaseURL,
	}
}

// Type implements Notifier.Type
func (n *telegramNotifier) Type() database.NotificationChannelType {
	return database.ChannelTypeTelegram
}

// Validate implements Notifier.Validate
// Checks that both the bot token and the chat ID are configured
func (n *telegramNotifier) Validate(channel *database.NotificationChannel) error {
	if channel.TelegramBotToken == nil || *channel.TelegramBotToken == "" {
		return fmt.Errorf("telegram channel requires a bot token")
	}
	if channel.TelegramChatID == nil || *channel.TelegramChatID == "" {
		return fmt.Errorf("telegram channel requires a chat ID")
	}
	return nil
}

// Send implements Notifier.Send
func (n *telegramNotifier) Send(ctx context.Context, channel *database.NotificationChannel, payload *database.NotificationPayload) error {
	if err := n.Validate(channel); err != nil {
		return err
	}

	// Select template based on notification type
	var template, defaultTemplate string
	switch payload.NotificationType {
	case string(database.NotificationTypeAlertFire):
		template = deref(channel.TemplateAlertFire)
		defaultTemplate = DefaultTelegramAlertFireTemplate
	case string(database.NotificationTypeAlertClear):
		template = deref(channel.TemplateAlertClear)
		defaultTemplate = DefaultTelegramAlertClearTemplate
	case string(database.NotificationTypeReminder):
		template = deref(channel.TemplateReminder)
		defaultTemplate = DefaultTelegramReminderTemplate
	default:
		return fmt.Errorf("unknown notification type: %s", payload.NotificationType)
	}

	// Render the message text. RenderHTML escapes the payload values so
	// that alert text containing markup characters cannot produce
	// malformed HTML, which Telegram rejects outright.
	text, err := n.renderer.RenderHTML(template, payload, defaultTemplate)
	if err != nil {
		return fmt.Errorf("failed to render telegram template: %w", err)
	}

	text = truncateTelegramText(text)

	err = n.postMessage(ctx, channel, text, telegramParseModeHTML)

	// Last-resort fallback for a message Telegram will not parse.
	// truncateTelegramText keeps the cut off markup boundaries, but a
	// custom template can still produce a tag the Bot API rejects, and
	// a rejection is a 400 rather than a transport error, so the queue
	// would retry it twice more and then mark the notification failed -
	// the operator never sees the alert. Sending the same text once
	// more with no parse_mode delivers it as plain text instead. The
	// HTML entities become visible ("&amp;" rather than "&"), which is
	// a poor message but an infinitely better outcome than silence.
	var apiErr *telegramAPIError
	if errors.As(err, &apiErr) && apiErr.isParseFailure() {
		if retryErr := n.postMessage(ctx, channel, text, ""); retryErr != nil {
			return fmt.Errorf("telegram rejected the formatted message (%s) "+
				"and the plain-text retry failed: %s", apiErr.Error(), retryErr.Error())
		}
		return nil
	}

	return err
}

// postMessage performs one sendMessage call and reports the outcome.
// parseMode is sent as given; the empty string omits the field, which
// tells the Bot API to treat the text as plain.
//
// Errors are never wrapped with %w around anything net/http produced:
// the bot token sits in the request path, so every string borrowed from
// the transport or from the API goes through sanitizeTelegramEcho
// first. The alerter logs what Send returns and writes it to
// notification_history.error_message, both of which are read by people
// who are not entitled to the credential.
func (n *telegramNotifier) postMessage(ctx context.Context, channel *database.NotificationChannel,
	text, parseMode string) error {

	body, err := json.Marshal(telegramSendMessageRequest{
		ChatID:                *channel.TelegramChatID,
		Text:                  text,
		ParseMode:             parseMode,
		DisableWebPagePreview: true,
	})
	if err != nil {
		return fmt.Errorf("failed to encode telegram request: %w", err)
	}

	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", n.apiBaseURL, *channel.TelegramBotToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		// The error from net/http carries the request URL, which
		// embeds the bot token; report only the redacted form.
		return fmt.Errorf("failed to create telegram request: %s", sanitizeTelegramEcho(err.Error()))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send telegram notification: %s", telegramTransportError(err))
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return fmt.Errorf("telegram API returned %d (failed to read body: %s)",
			resp.StatusCode, sanitizeTelegramEcho(readErr.Error()))
	}

	// Telegram reports failures in the body, so decode it whatever the
	// status code was.
	var decoded telegramResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("telegram API returned %d: %s",
				resp.StatusCode, sanitizeTelegramEcho(string(respBody)))
		}
		return fmt.Errorf("failed to decode telegram response: %s",
			sanitizeTelegramEcho(err.Error()))
	}

	// A message counts as delivered only when ok is true.
	if !decoded.OK {
		return &telegramAPIError{
			Code:        decoded.ErrorCode,
			Description: sanitizeTelegramEcho(decoded.Description),
		}
	}

	return nil
}

// truncateTelegramText shortens text to Telegram's 4096-character limit,
// cutting on a rune boundary so a multi-byte character is never split in
// half, and appending a marker inside the limit when anything was
// dropped.
//
// The cut is also kept off markup boundaries. The text handed here has
// already been rendered and escaped, so the 4095th rune can easily land
// in the middle of "&#39;" or of "<code>": Telegram then rejects the
// whole message with a 400, the queue retries it twice and gives up,
// and the alert is never delivered. backOffTelegramMarkup rewinds the
// cut to before any entity or tag the limit would have split. Escaping
// inflates the text before it gets here - a single '"' becomes six
// bytes - so this is reachable with an ordinary long alert
// description, not just a contrived one.
//
// The limit applies after entity parsing, so a message close to it that
// also carries markup may still be refused; Send's plain-text retry is
// the net under that case.
func truncateTelegramText(text string) string {
	if utf8.RuneCountInString(text) <= telegramMaxMessageRunes {
		return text
	}

	keep := telegramMaxMessageRunes - utf8.RuneCountInString(telegramTruncationMarker)
	cut := telegramByteOffsetOfRune(text, keep)
	cut = backOffTelegramMarkup(text[:cut])
	return text[:cut] + telegramTruncationMarker
}

// telegramByteOffsetOfRune returns the byte offset at which rune n of s
// begins, or len(s) when s holds fewer than n runes.
//
// Ranging over the string this way avoids materializing a []rune copy -
// four bytes per character, for every message sent - to find a single
// offset.
func telegramByteOffsetOfRune(s string, n int) int {
	count := 0
	for i := range s {
		if count == n {
			return i
		}
		count++
	}
	return len(s)
}

// backOffTelegramMarkup returns the length of the longest prefix of s
// that does not end inside an HTML character entity or an HTML tag.
//
// s is a candidate truncation of already-escaped text. If it ends with
// an unterminated "&..." or "<...", Telegram parses the message as
// malformed and rejects it outright, so the cut is rewound to just
// before the offending run. Both rewinds are bounded: an unterminated
// '<' or '&' much further back than a real tag or entity is not
// something truncation created, and rewinding to it would throw away
// the message to no purpose.
func backOffTelegramMarkup(s string) int {
	cut := len(s)

	// An unterminated tag: the last '<' with no '>' after it.
	if i := strings.LastIndexByte(s, '<'); i >= 0 &&
		!strings.ContainsRune(s[i:], '>') &&
		len(s)-i <= telegramMaxTagBytes {
		cut = i
	}

	// An unterminated entity: the last '&' with no ';' after it, whose
	// tail still looks like an entity name. Checking the tail keeps an
	// ordinary literal '&' - "shared_buffers & work_mem" - from being
	// mistaken for one. A '&' inside an unterminated tag is already
	// covered by the rewind above, which sits further left.
	if i := strings.LastIndexByte(s, '&'); i >= 0 && i < cut &&
		!strings.ContainsRune(s[i:], ';') &&
		len(s)-i <= telegramMaxEntityBytes &&
		isTelegramEntityBody(s[i+1:]) {
		cut = i
	}

	return cut
}

// isTelegramEntityBody reports whether s could be the start of an HTML
// character entity's name: the letters and digits of a named entity, or
// the '#' and digits of a numeric one.
func isTelegramEntityBody(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c == '#':
		default:
			return false
		}
	}
	return true
}
