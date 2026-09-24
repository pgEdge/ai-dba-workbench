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
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// sendTestGenericWebhook sends a test message to a generic REST webhook endpoint.
// It supports GET, POST, PUT, and PATCH methods with optional authentication
// and custom headers.
//
// The endpoint URL may carry a credential in its path or query string, so no
// error returned from here wraps with %w anything net/http produced, and no
// text borrowed from the endpoint is echoed raw: both go through the helpers
// in sanitize.go, since the caller logs what this returns.
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
	if !isValidWebhookHTTPMethod(httpMethod) {
		// The handlers reject any other method at create and update
		// time, but a row written before that check existed may still
		// carry one. http.NewRequest would otherwise fail on it and be
		// reported as a malformed URL.
		return fmt.Errorf("invalid HTTP method %q: must be GET, POST, PUT or PATCH",
			sanitizeConfigEcho(httpMethod))
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
		// http.NewRequest fails here only when url.Parse rejects the
		// URL, and (*url.Error).Error quotes the raw stored string
		// back: for Slack and Mattermost that string is the
		// credential, and it need not carry a scheme for redactURLPath
		// to anchor on. Report nothing borrowed from it. The handler
		// parses the URL before calling either sender, so this is
		// unreachable from it today; the senders do not rely on that.
		return fmt.Errorf("failed to create request: the URL is malformed")
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
		return fmt.Errorf("failed to send request: %s", webhookTransportError(err, endpointURL))
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if readErr != nil {
			return fmt.Errorf("webhook returned status %d (failed to read body: %s)",
				resp.StatusCode, sanitizeWebhookEcho(readErr.Error(), endpointURL))
		}
		return fmt.Errorf("webhook returned status %d: %s",
			resp.StatusCode, sanitizeWebhookEcho(string(respBody), endpointURL))
	}

	return nil
}

// sendTestWebhook sends a test message to a Slack or Mattermost webhook URL.
//
// The whole webhook URL is the credential for both services, so no error
// returned from here wraps with %w anything net/http produced, and no text
// borrowed from the far end is echoed raw: both go through the helpers in
// sanitize.go, since the caller logs what this returns.
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
		// http.NewRequest fails here only when url.Parse rejects the
		// URL, and (*url.Error).Error quotes the raw stored string
		// back: for Slack and Mattermost that string is the
		// credential, and it need not carry a scheme for redactURLPath
		// to anchor on. Report nothing borrowed from it. The handler
		// parses the URL before calling either sender, so this is
		// unreachable from it today; the senders do not rely on that.
		return fmt.Errorf("failed to create request: the URL is malformed")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req) //nolint:gosec // G704: URL host validated upstream and redirects disabled so the validated host cannot be bypassed via Location header; DNS rebinding between validation and dial is a known, admin-scope residual risk
	if err != nil {
		return fmt.Errorf("failed to send request: %s", webhookTransportError(err, webhookURL))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if readErr != nil {
			return fmt.Errorf("webhook returned status %d (failed to read body: %s)",
				resp.StatusCode, sanitizeWebhookEcho(readErr.Error(), webhookURL))
		}
		return fmt.Errorf("webhook returned status %d: %s",
			resp.StatusCode, sanitizeWebhookEcho(string(respBody), webhookURL))
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

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body)) //nolint:gosec // G704: no host validation happens here and none is required. The host is the compile-time constant telegramAPIBaseURL, reached through the unexported telegramSendBaseURL that only in-package tests reassign, so no operator input reaches it. The tainted component is the bot token, and it lands in the path, where telegramBotTokenPattern forbids whitespace, '/', '?' and '#' and so leaves it unable to alter the path structure or the authority.
	if err != nil {
		// The error from net/http carries the request URL, which embeds
		// the bot token; report only the redacted form.
		return fmt.Errorf("failed to create request: %s", sanitizeTelegramEcho(err.Error()))
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req) //nolint:gosec // G704: the request built above, whose host is the constant telegramAPIBaseURL rather than anything operator-supplied; CheckRedirect refuses every 3xx, so no Location header can move the request to another host.
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
