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
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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
