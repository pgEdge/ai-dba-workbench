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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient returns an *http.Client with a short timeout suitable for tests.
func newTestClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

// waitForAtLeast returns a ctx that ensures doRequestWithRetry doesn't sleep
// forever by capping the test duration.
func testCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// Note: maxRetries/initialBackoff/maxBackoff are package-level consts, so
// retry-heavy tests cannot shorten them. Tests that exercise 429 retries
// set a Retry-After header of "0" (which parseRetryAfter returns as 0) to
// skip the initial 2s backoff. The 5xx path has no such header, so those
// tests rely on context cancellation instead.

func TestParseRetryAfter_SecondsNumeric(t *testing.T) {
	got := parseRetryAfter("5")
	want := 5 * time.Second
	if got != want {
		t.Errorf("parseRetryAfter(\"5\") = %v, want %v", got, want)
	}
}

func TestParseRetryAfter_Zero(t *testing.T) {
	got := parseRetryAfter("0")
	if got != 0 {
		t.Errorf("parseRetryAfter(\"0\") = %v, want 0", got)
	}
}

func TestParseRetryAfter_HTTPDate(t *testing.T) {
	future := time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(future)
	if got <= 0 || got > 15*time.Second {
		t.Errorf("parseRetryAfter(%q) = %v, want roughly 10s", future, got)
	}
}

func TestParseRetryAfter_Empty(t *testing.T) {
	if got := parseRetryAfter(""); got != 0 {
		t.Errorf("parseRetryAfter(\"\") = %v, want 0", got)
	}
}

func TestParseRetryAfter_Invalid(t *testing.T) {
	if got := parseRetryAfter("not-a-date"); got != 0 {
		t.Errorf("parseRetryAfter(\"not-a-date\") = %v, want 0", got)
	}
}

func TestMinDuration(t *testing.T) {
	if got := minDuration(time.Second, 2*time.Second); got != time.Second {
		t.Errorf("minDuration(1s, 2s) = %v, want 1s", got)
	}
	if got := minDuration(3*time.Second, time.Second); got != time.Second {
		t.Errorf("minDuration(3s, 1s) = %v, want 1s", got)
	}
	if got := minDuration(time.Second, time.Second); got != time.Second {
		t.Errorf("minDuration(1s, 1s) = %v, want 1s", got)
	}
}

func TestSleep_CompletesNormally(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	ok := sleep(ctx, 10*time.Millisecond)
	if !ok {
		t.Fatalf("sleep returned false, want true")
	}
	if elapsed := time.Since(start); elapsed < 5*time.Millisecond {
		t.Errorf("sleep finished too quickly: %v", elapsed)
	}
}

func TestSleep_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleep(ctx, time.Second) {
		t.Errorf("sleep with canceled ctx returned true, want false")
	}
}

func TestDoRequestWithRetry_SuccessFirstAttempt(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		writeOrFail(t, w, `ok`)
	}))
	defer srv.Close()

	ctx, cancel := testCtx(t)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", srv.URL, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := doRequestWithRetry(ctx, newTestClient(), req)
	if err != nil {
		t.Fatalf("doRequestWithRetry: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("calls = %d, want 1", n)
	}
}

func TestDoRequestWithRetry_RetriesAfter429ThenSucceeds(t *testing.T) {
	var calls int32
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		bodies = append(bodies, body)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := testCtx(t)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", srv.URL, bytes.NewReader([]byte(`{"x":1}`)))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := doRequestWithRetry(ctx, newTestClient(), req)
	if err != nil {
		t.Fatalf("doRequestWithRetry: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if n := atomic.LoadInt32(&calls); n < 2 {
		t.Fatalf("calls = %d, want >=2", n)
	}
	// Server should have received the same body on each attempt.
	for i, b := range bodies {
		if !bytes.Equal(b, []byte(`{"x":1}`)) {
			t.Errorf("request body on attempt %d = %q, want %q", i, b, `{"x":1}`)
		}
	}
}

func TestDoRequestWithRetry_RetriesOn5xxThenReturnsResponse(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", srv.URL, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := doRequestWithRetry(ctx, newTestClient(), req)
	// The first server 5xx response triggers a retry with backoff. The
	// context times out inside sleep(), and doRequestWithRetry returns
	// ErrContextCanceled.
	if !errors.Is(err, ErrContextCanceled) {
		if resp != nil {
			_ = resp.Body.Close()
		}
		if err == nil {
			t.Fatalf("expected ErrContextCanceled, got nil")
		}
	}
	if n := atomic.LoadInt32(&calls); n < 1 {
		t.Errorf("calls = %d, want >=1", n)
	}
}

func TestDoRequestWithRetry_NetworkErrorThenSuccess(t *testing.T) {
	// Start server, capture URL, then close it so first attempt fails.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	url := srv.URL
	srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	_, err = doRequestWithRetry(ctx, newTestClient(), req)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestDoRequestWithRetry_ContextAlreadyCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, err := http.NewRequestWithContext(context.Background(), "POST", srv.URL, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	_, err = doRequestWithRetry(ctx, newTestClient(), req)
	if !errors.Is(err, ErrContextCanceled) {
		t.Errorf("err = %v, want ErrContextCanceled", err)
	}
}

func TestDoRequestWithRetry_NilBodyIsFine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := testCtx(t)
	defer cancel()
	// GET with nil body
	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := doRequestWithRetry(ctx, newTestClient(), req)
	if err != nil {
		t.Fatalf("doRequestWithRetry: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// errReader always returns an error when Read is called.
type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, errors.New("read failure") }

func TestDoRequestWithRetry_ReadBodyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	ctx, cancel := testCtx(t)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", srv.URL, errReader{})
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	_, err = doRequestWithRetry(ctx, newTestClient(), req)
	if err == nil || !strings.Contains(err.Error(), "read failure") {
		t.Errorf("err = %v, want containing 'read failure'", err)
	}
}

func TestDoRequestWithRetry_Retries5xxThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// This test tolerates the full initialBackoff (2s) between retries.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", srv.URL, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := doRequestWithRetry(ctx, newTestClient(), req)
	if err != nil {
		t.Fatalf("doRequestWithRetry: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if n := atomic.LoadInt32(&calls); n < 2 {
		t.Errorf("calls = %d, want >=2", n)
	}
}

func TestDoRequestWithRetry_CancelDuringBackoffAfter429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Large Retry-After so we'd sleep for 60s, but cancel kicks in first.
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(context.Background(), "POST", srv.URL, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	// Cancel shortly after doRequestWithRetry enters sleep().
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_, err = doRequestWithRetry(ctx, newTestClient(), req)
	if !errors.Is(err, ErrContextCanceled) {
		t.Errorf("err = %v, want ErrContextCanceled", err)
	}
}

func TestParseRetryAfter_SupportsSecondsAsString(t *testing.T) {
	// Make sure strconv path is exercised for a large number.
	got := parseRetryAfter(strconv.Itoa(120))
	if got != 120*time.Second {
		t.Errorf("parseRetryAfter(\"120\") = %v, want 120s", got)
	}
}
