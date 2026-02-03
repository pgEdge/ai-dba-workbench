/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package llm

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	maxRetries     = 5
	initialBackoff = 2 * time.Second
	maxBackoff     = 60 * time.Second
)

// doRequestWithRetry executes an HTTP request with retry on rate limiting.
// It implements exponential backoff for 429 (rate limit) and 5xx errors.
func doRequestWithRetry(ctx context.Context, client *http.Client, req *http.Request) (*http.Response, error) {
	// Store the original body for retries
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body.Close()
	}

	backoff := initialBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Check context before attempting
		if ctx.Err() != nil {
			return nil, ErrContextCanceled
		}

		// Reset body for retry
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ErrContextCanceled
			}
			// Network error - retry
			if attempt < maxRetries {
				if !sleep(ctx, backoff) {
					return nil, ErrContextCanceled
				}
				backoff = minDuration(backoff*2, maxBackoff)
				continue
			}
			return nil, err
		}

		// Check for retryable status codes
		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			// Rate limited - check for Retry-After header
			resp.Body.Close()
			if attempt < maxRetries {
				retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
				if retryAfter > 0 {
					backoff = retryAfter
				}
				if !sleep(ctx, backoff) {
					return nil, ErrContextCanceled
				}
				backoff = minDuration(backoff*2, maxBackoff)
				continue
			}
			return nil, ErrRateLimited

		case http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout,
			529: // Anthropic "overloaded" status code
			// Server error - retry
			resp.Body.Close()
			if attempt < maxRetries {
				if !sleep(ctx, backoff) {
					return nil, ErrContextCanceled
				}
				backoff = minDuration(backoff*2, maxBackoff)
				continue
			}
			return resp, nil

		default:
			// Success or non-retryable error
			return resp, nil
		}
	}

	return nil, ErrRateLimited
}

// parseRetryAfter parses the Retry-After header value.
// It supports both seconds and HTTP-date formats.
func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}

	// Try parsing as seconds
	if seconds, err := strconv.Atoi(value); err == nil {
		return time.Duration(seconds) * time.Second
	}

	// Try parsing as HTTP-date
	if t, err := http.ParseTime(value); err == nil {
		return time.Until(t)
	}

	return 0
}

// sleep sleeps for the specified duration, respecting context cancellation.
// Returns false if the context was canceled.
func sleep(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// minDuration returns the minimum of two durations.
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
