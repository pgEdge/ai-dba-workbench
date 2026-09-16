/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// sendTestGenericWebhook sends a test message to a generic REST webhook endpoint.
// It supports GET, POST, PUT, and PATCH methods with optional authentication
// and custom headers.
func sendTestGenericWebhook(endpointURL, httpMethod string, headers map[string]string, authType, authCredentials string) error {
	client := &http.Client{
		Timeout: 30 * time.Second,
		// Refuse to follow redirects. hostValidator.ValidateHost is
		// applied to the originally configured URL only; following a
		// 3xx Location header would allow an attacker-controlled
		// endpoint to bounce the request to a private/metadata host
		// and bypass SSRF protections.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	if httpMethod == "" {
		httpMethod = http.MethodPost
	}

	// Build request body for non-GET methods
	var reqBody io.Reader
	if httpMethod != http.MethodGet {
		body := `{"text":"This is a test message from the AI DBA Workbench to verify your webhook configuration."}`
		reqBody = strings.NewReader(body)
	}

	// The endpoint URL host is validated by hostValidator.ValidateHost at
	// the call site to reject private/loopback/metadata endpoints before
	// the request is ever dispatched, so this is not an unchecked
	// user-supplied URL.
	req, err := http.NewRequest(httpMethod, endpointURL, reqBody) //nolint:gosec // G704: URL host validated upstream; DNS rebinding between validation and dial is a known, admin-scope residual risk
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set content type for non-GET requests
	if httpMethod != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
	}

	// Apply custom headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Apply authentication
	if authType != "" && authCredentials != "" {
		switch authType {
		case "basic":
			parts := strings.SplitN(authCredentials, ":", 2)
			if len(parts) == 2 {
				req.SetBasicAuth(parts[0], parts[1])
			}
		case "bearer":
			req.Header.Set("Authorization", "Bearer "+authCredentials)
		case "api_key":
			parts := strings.SplitN(authCredentials, ":", 2)
			if len(parts) == 2 {
				req.Header.Set(parts[0], parts[1])
			}
		}
	}

	resp, err := client.Do(req) //nolint:gosec // G704: URL host validated upstream and redirects disabled so the validated host cannot be bypassed via Location header; DNS rebinding between validation and dial is a known, admin-scope residual risk
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("webhook returned status %d (failed to read body: %v)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// sendTestWebhook sends a test message to a Slack or Mattermost webhook URL.
func sendTestWebhook(webhookURL string, channelType string) error {
	client := &http.Client{
		Timeout: 30 * time.Second,
		// Refuse to follow redirects. hostValidator.ValidateHost is
		// applied to the originally configured URL only; following a
		// 3xx Location header would allow an attacker-controlled
		// endpoint to bounce the request to a private/metadata host
		// and bypass SSRF protections.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	body := fmt.Sprintf(
		`{"text":"This is a test message from the AI DBA Workbench to verify your %s webhook configuration."}`,
		channelType,
	)

	// The webhook URL host is validated by hostValidator.ValidateHost at
	// the call site to reject private/loopback/metadata endpoints before
	// the request is ever dispatched, so this is not an unchecked
	// user-supplied URL.
	req, err := http.NewRequest(http.MethodPost, webhookURL, strings.NewReader(body)) //nolint:gosec // G704: URL host validated upstream; DNS rebinding between validation and dial is a known, admin-scope residual risk
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req) //nolint:gosec // G704: URL host validated upstream and redirects disabled so the validated host cannot be bypassed via Location header; DNS rebinding between validation and dial is a known, admin-scope residual risk
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("webhook returned status %d (failed to read body: %v)", resp.StatusCode, readErr)
		}
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// telegramAPIBaseURL is the Telegram Bot API host. It is a constant on
// purpose. See the comment on sendTestTelegram.
const telegramAPIBaseURL = "https://api.telegram.org"

// telegramTestMessage is the probe body delivered by sendTestTelegram.
const telegramTestMessage = "This is a test message from the AI DBA Workbench " +
	"to verify your Telegram configuration."

// telegramSendBaseURL is the base URL sendTestTelegram posts to. It is a
// package variable solely so that in-package tests can point the sender
// at an httptest server. It is deliberately unexported and is never
// derived from request data, channel configuration or any other operator
// input, so production traffic always reaches telegramAPIBaseURL. Do not
// add a way to set it from outside this package.
var telegramSendBaseURL = telegramAPIBaseURL

// telegramSendMessageRequest is the JSON body of a Bot API sendMessage
// call. The body is always produced by marshaling this struct so that
// the message text, whatever it contains, cannot break out of the
// envelope.
type telegramSendMessageRequest struct {
	// ChatID is a string because the Bot API accepts either a numeric
	// chat ID or an @channelusername, and it accepts a numeric ID sent
	// in string form.
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	ParseMode             string `json:"parse_mode"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

// telegramResponse is the envelope every Bot API method returns. A call
// that fails reports ok:false with an error_code and a description,
// frequently alongside a 200 status, so the body has to be decoded and
// `ok` checked rather than the status code trusted.
type telegramResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
}

// sendTestTelegram sends a test message to a Telegram chat through the
// Bot API.
//
// Unlike its neighbors in this file, this sender is NOT preceded by
// h.hostValidator.ValidateHost at the call site, and that is correct
// rather than an oversight. sendTestWebhook and sendTestGenericWebhook
// dispatch to a URL the operator stored on the channel, so they carry an
// SSRF surface that host validation closes. A Telegram channel stores no
// URL at all: the destination is built here from the constant
// telegramAPIBaseURL, and the only operator-supplied parts are the bot
// token and the chat ID, which occupy the path and the request body. No
// configuration can steer this request at another host. Please do not
// "fix" the missing validation call by making the base URL configurable
// -- that would create the very surface the validator exists to close.
//
// The bot token is a bearer credential and it sits in the request path,
// so every error net/http produces about a failed request repeats it
// verbatim. The caller logs these errors, so no error returned from here
// may wrap the original with %w; borrowed text is passed through
// redactTelegramToken instead.
func sendTestTelegram(botToken, chatID string) error {
	client := &http.Client{
		Timeout: 30 * time.Second,
		// Consistent with the neighboring senders: never follow a
		// redirect. The Bot API does not redirect in normal operation,
		// and a 3xx Location must not be allowed to move the request
		// (which carries the bot token) to another host.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	body, err := json.Marshal(telegramSendMessageRequest{
		ChatID:                chatID,
		Text:                  telegramTestMessage,
		ParseMode:             "HTML",
		DisableWebPagePreview: true,
	})
	if err != nil {
		return fmt.Errorf("failed to encode telegram request: %s",
			sanitizeTelegramEcho(err.Error()))
	}

	endpoint := fmt.Sprintf("%s/bot%s/sendMessage", telegramSendBaseURL, botToken)

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		// The error from net/http carries the request URL, which embeds
		// the bot token; report only the redacted form.
		return fmt.Errorf("failed to create request: %s", sanitizeTelegramEcho(err.Error()))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %s", telegramTransportError(err))
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if readErr != nil {
		return fmt.Errorf("telegram API returned %d (failed to read body: %s)",
			resp.StatusCode, sanitizeTelegramEcho(readErr.Error()))
	}

	// Telegram reports failures in the body, frequently alongside a 200,
	// so decode it whatever the status code was.
	var decoded telegramResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("telegram API returned %d: %s",
				resp.StatusCode, sanitizeTelegramEcho(string(respBody)))
		}
		return fmt.Errorf("failed to decode telegram response: %s",
			sanitizeTelegramEcho(err.Error()))
	}

	// A message counts as delivered only when ok is true.
	if !decoded.OK {
		return fmt.Errorf("telegram API error %d: %s",
			decoded.ErrorCode, sanitizeTelegramEcho(decoded.Description))
	}

	return nil
}

// telegramMaxEchoedBytes bounds how much borrowed text any error built
// from a Bot API exchange may repeat. See sanitizeTelegramEcho.
const telegramMaxEchoedBytes = 256

// telegramEchoTruncationMarker marks text sanitizeTelegramEcho cut
// short.
const telegramEchoTruncationMarker = "…"

// sanitizeTelegramEcho prepares a string that came from the far end of
// a Bot API call - a response body, an API description, a transport
// error - for inclusion in an error a caller will record: the alerter
// writes it to notification_history.error_message and its own log, the
// server writes it to the server log. Every borrowed string
// interpolated into an error goes through here; that rule is what makes
// the property easy to check.
//
// It does three things, all of which are load-bearing:
//
//   - Redacts the bot token, because the text may repeat the request
//     URL, which carries the credential in its path.
//   - Maps control characters to spaces, so a hostile or broken
//     endpoint cannot inject newlines and forge log lines. Invalid
//     UTF-8 is replaced at the same time, which also keeps the value
//     storable in a Postgres text column.
//   - Caps the result at telegramMaxEchoedBytes. What is read and what
//     is echoed are deliberately separate limits: the body is read
//     under a 1 MiB io.LimitReader, but a captive portal or a hostile
//     endpoint would otherwise put the whole megabyte into the log and
//     the database on every one of the three delivery attempts.
//
// Redaction runs before the cap so the cap can never slice a token run
// and leave the tail of it visible.
//
// This helper is duplicated between
// alerter/src/internal/notifications/telegram.go and
// server/src/internal/api/webhook_test_sender.go; the alerter and the
// server are separate Go modules, so it cannot be shared. The two
// copies must stay character-for-character identical - diff them after
// any change.
func sanitizeTelegramEcho(s string) string {
	s = redactTelegramToken(s)
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if len(s) <= telegramMaxEchoedBytes {
		return s
	}
	cut := telegramMaxEchoedBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + telegramEchoTruncationMarker
}

// redactTelegramToken replaces the bot token in any text that may have
// come from a URL with a fixed placeholder.
//
// The token is a bearer credential and it sits in the request path, so
// anything net/http reports about a failed request - a *url.Error, a
// redirect message, a proxy error - repeats it verbatim, as does a
// description the Bot API echoes back. Errors from these callers are
// logged and stored, and are read by people who are not entitled to the
// credential, so borrowed text is passed through here rather than
// wrapped with %w.
//
// The redactor fails closed. Once "/bot" is found the token run that
// follows is always replaced, whether or not the terminating '/' of
// "/bot<token>/sendMessage" is present: a truncated URL in a proxy
// error, a description echoing a partial path, or a caller building a
// different Bot API URL must never be able to carry a live credential
// through untouched. Do not reintroduce a bail-out that copies the
// remainder of the string out verbatim.
//
// This helper is duplicated between
// alerter/src/internal/notifications/telegram.go and
// server/src/internal/api/webhook_test_sender.go; the alerter and the
// server are separate Go modules, so it cannot be shared. The two
// copies must stay character-for-character identical - diff them after
// any change.
func redactTelegramToken(s string) string {
	const prefix = "/bot"
	var b strings.Builder
	for {
		i := strings.Index(s, prefix)
		if i < 0 {
			break
		}
		// The token runs from just after the prefix to the first
		// character that cannot appear in a token interpolated into a
		// URL path, or to the end of the string when none follows.
		rest := s[i+len(prefix):]
		j := 0
		for j < len(rest) && !isTelegramTokenTerminator(rest[j]) {
			j++
		}
		b.WriteString(s[:i])
		if j == 0 {
			// An empty run is not a credential: the literal text
			// "/bot", or "/bot/" with nothing between the slashes,
			// carries no token and redacting it would only mislead.
			// Emit it unchanged and keep scanning after it.
			b.WriteString(prefix)
		} else {
			b.WriteString("/bot<redacted>")
		}
		// Whatever terminated the run is left in place, so the '/' of
		// the usual "/bot<token>/sendMessage" survives in the output.
		s = rest[j:]
	}
	b.WriteString(s)
	return b.String()
}

// isTelegramTokenTerminator reports whether c ends a bot token that was
// interpolated into a URL path.
//
// The invariant: this class must be a SUBSET of the characters the
// server's telegramBotTokenPattern forbids inside a token. Any
// character a token may legally contain has to be swallowed into the
// redacted run; if it ends the run instead, redaction stops in the
// middle of the credential and prints the rest of it verbatim. The
// class is therefore exactly whitespace and control characters, which
// cannot appear in a URL at all, plus the three path delimiters '/',
// '?' and '#'.
//
// Do NOT add "defensive" quoting or bracketing characters - the double
// quote, the apostrophe, the backquote, the angle brackets, the closing
// paren, the closing square bracket, the comma or the semicolon - on
// the grounds that they surround a URL in an error or a log line. An
// earlier version did exactly that, reasoning about the text around a
// token rather than about the token itself, and any token containing
// one of them leaked its whole tail. Over-redacting the boilerplate
// that follows a token is free; under-redacting the token is not.
//
// This helper is duplicated between
// alerter/src/internal/notifications/telegram.go and
// server/src/internal/api/webhook_test_sender.go; the alerter and the
// server are separate Go modules, so it cannot be shared. The two
// copies must stay character-for-character identical - diff them after
// any change.
func isTelegramTokenTerminator(c byte) bool {
	// Space and everything below it: all ASCII whitespace and control
	// characters. UTF-8 continuation bytes are >= 0x80, so a multi-byte
	// character is never mistaken for a terminator.
	if c <= ' ' {
		return true
	}
	switch c {
	case '/', '?', '#':
		return true
	}
	return false
}

// telegramTransportError renders a transport failure without the request
// URL. *url.Error stringifies as `Op "URL": Err`, which would leak the
// bot token, so only its operation and cause are reported.
//
// This helper is duplicated between
// alerter/src/internal/notifications/telegram.go and
// server/src/internal/api/webhook_test_sender.go; the alerter and the
// server are separate Go modules, so it cannot be shared. The two
// copies must stay character-for-character identical - diff them after
// any change.
func telegramTransportError(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return fmt.Sprintf("%s request failed: %s", urlErr.Op,
			sanitizeTelegramEcho(urlErr.Err.Error()))
	}
	return sanitizeTelegramEcho(err.Error())
}
