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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"golang.org/x/crypto/bcrypt"
)

// =============================================================================
// Lightweight unit tests (no database required)
//
// These cover routing and method-handling concerns that don't depend on
// a datastore. They keep the handler file's coverage above the 90%
// floor even when database integration tests are skipped.
// =============================================================================

// TestNotificationChannelHandler_NotConfiguredRoutes verifies that when
// the handler is constructed without a datastore, every route under
// `/api/v1/notification-channels` returns 503.
func TestNotificationChannelHandler_NotConfiguredRoutes(t *testing.T) {
	handler := NewNotificationChannelHandlerWithSecurity(nil, nil, nil, false, nil, nil)
	mux := http.NewServeMux()
	noopWrapper := func(h http.HandlerFunc) http.HandlerFunc { return h }
	handler.RegisterRoutes(mux, noopWrapper)

	paths := []string{
		"/api/v1/notification-channels",
		"/api/v1/notification-channels/1",
		"/api/v1/notification-channels/1/test",
		"/api/v1/notification-channels/1/recipients",
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("Path %s: expected status %d, got %d", path,
				http.StatusServiceUnavailable, rec.Code)
		}
	}
}

// TestNotificationChannelHandler_MethodNotAllowed verifies that
// unsupported HTTP methods on each notification-channel route return
// 405 with a populated Allow header.
func TestNotificationChannelHandler_MethodNotAllowed(t *testing.T) {
	authStore, cleanup := newAuthStoreForChannelTests(t)
	defer cleanup()
	handler := NewNotificationChannelHandlerWithSecurity(nil, authStore, auth.NewRBACChecker(authStore), false, nil, nil)

	cases := []struct {
		path        string
		method      string
		allow       string
		dispatcher  func(w http.ResponseWriter, r *http.Request)
		expectAllow bool
	}{
		{
			path: "/api/v1/notification-channels", method: http.MethodPut,
			allow: "GET, POST", dispatcher: handler.handleChannels, expectAllow: true,
		},
		{
			path: "/api/v1/notification-channels/1", method: http.MethodPatch,
			allow: "GET, PUT, DELETE", dispatcher: handler.handleChannelSubpath, expectAllow: true,
		},
		{
			path: "/api/v1/notification-channels/1/test", method: http.MethodGet,
			allow: "POST", dispatcher: handler.handleChannelSubpath, expectAllow: true,
		},
		{
			path: "/api/v1/notification-channels/1/recipients", method: http.MethodPut,
			allow: "GET, POST", dispatcher: handler.handleChannelSubpath, expectAllow: true,
		},
		{
			path: "/api/v1/notification-channels/1/recipients/2", method: http.MethodGet,
			allow: "PUT, DELETE", dispatcher: handler.handleChannelSubpath, expectAllow: true,
		},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		tc.dispatcher(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s: expected 405, got %d", tc.method, tc.path, rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != tc.allow {
			t.Errorf("%s %s: Allow = %q, want %q", tc.method, tc.path, got, tc.allow)
		}
	}
}

// TestNotificationChannelHandler_InvalidIDs covers the malformed-ID
// branches in the subpath router.
func TestNotificationChannelHandler_InvalidIDs(t *testing.T) {
	authStore, cleanup := newAuthStoreForChannelTests(t)
	defer cleanup()
	handler := NewNotificationChannelHandlerWithSecurity(nil, authStore, auth.NewRBACChecker(authStore), false, nil, nil)

	cases := []struct {
		path string
		want string
	}{
		{"/api/v1/notification-channels/abc", "Invalid notification channel ID"},
		{"/api/v1/notification-channels/1/recipients/xyz", "Invalid recipient ID"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPut, tc.path, nil)
		rec := httptest.NewRecorder()
		handler.handleChannelSubpath(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d", tc.path, rec.Code)
		}
		var resp ErrorResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Error != tc.want {
			t.Errorf("%s: Error = %q, want %q", tc.path, resp.Error, tc.want)
		}
	}
}

// TestNotificationChannelHandler_PermissionRequired confirms that the
// list and create routes require the manage_notification_channels
// admin permission and return 403 otherwise.
func TestNotificationChannelHandler_PermissionRequired(t *testing.T) {
	authStore, cleanup := newAuthStoreForChannelTests(t)
	defer cleanup()
	handler := NewNotificationChannelHandlerWithSecurity(nil, authStore, auth.NewRBACChecker(authStore), false, nil, nil)

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "/api/v1/notification-channels", nil)
		rec := httptest.NewRecorder()
		handler.handleChannels(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s without permission: expected 403, got %d", method, rec.Code)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/v1/notification-channels/1", nil)
		rec := httptest.NewRecorder()
		handler.handleChannelSubpath(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s without permission: expected 403, got %d", method, rec.Code)
		}
	}
}

// TestNotificationChannelHandler_NotFoundPaths covers paths that do not
// match any recognized sub-route.
func TestNotificationChannelHandler_NotFoundPaths(t *testing.T) {
	authStore, cleanup := newAuthStoreForChannelTests(t)
	defer cleanup()
	handler := NewNotificationChannelHandlerWithSecurity(nil, authStore, auth.NewRBACChecker(authStore), false, nil, nil)

	paths := []string{
		"/api/v1/notification-channels/",
		"/api/v1/notification-channels/1/unknown",
		"/api/v1/notification-channels/1/recipients/2/extra",
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.handleChannelSubpath(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: expected 404, got %d", path, rec.Code)
		}
	}
}

// =============================================================================
// Database-backed integration tests (issue #187)
//
// Each test below skips when TEST_AI_WORKBENCH_SERVER is not set,
// matching the convention used elsewhere in this package.
// =============================================================================

// notificationChannelsTestSchema mirrors the columns the notification
// channel handlers touch. It intentionally omits unrelated tables and
// foreign-key dependencies that are not exercised by the redaction
// tests.
const notificationChannelsTestSchema = `
DROP TABLE IF EXISTS email_recipients CASCADE;
DROP TABLE IF EXISTS notification_channels CASCADE;

CREATE TABLE notification_channels (
    id BIGSERIAL PRIMARY KEY,
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    channel_type TEXT NOT NULL CHECK (channel_type IN ('slack', 'mattermost', 'telegram', 'webhook', 'email')),
    name TEXT NOT NULL,
    description TEXT,
    webhook_url_encrypted TEXT,
    endpoint_url TEXT,
    http_method TEXT DEFAULT 'POST',
    headers_json JSONB DEFAULT '{}',
    auth_type TEXT,
    auth_credentials_encrypted TEXT,
    smtp_host TEXT,
    smtp_port INTEGER DEFAULT 587,
    smtp_username TEXT,
    smtp_password_encrypted TEXT,
    smtp_use_tls BOOLEAN DEFAULT TRUE,
    from_address TEXT,
    from_name TEXT,
    telegram_bot_token_encrypted TEXT,
    telegram_chat_id TEXT,
    template_alert_fire TEXT,
    template_alert_clear TEXT,
    template_reminder TEXT,
    reminder_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    reminder_interval_hours INTEGER DEFAULT 24,
    is_estate_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE email_recipients (
    id BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    email_address TEXT NOT NULL,
    display_name TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const notificationChannelsTestTeardown = `
DROP TABLE IF EXISTS email_recipients CASCADE;
DROP TABLE IF EXISTS notification_channels CASCADE;
`

// channelTestServerSecret is a deterministic 32-byte key used by the
// EncryptPassword helper. The exact value is irrelevant; tests just
// need the round-trip to succeed.
const channelTestServerSecret = "0123456789abcdef0123456789abcdef"

// newChannelTestDatastore wires a *database.Datastore (with a
// non-empty server secret so notification secret encryption succeeds)
// to the test Postgres instance. The returned cleanup tears down the
// schema and closes the pool.
func newChannelTestDatastore(t *testing.T) (*database.Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping notification channel integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	if _, err := pool.Exec(ctx, notificationChannelsTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create notification channel test schema: %v", err)
	}

	ds := database.NewTestDatastoreWithSecret(pool, channelTestServerSecret)
	cleanup := func() {
		_, _ = pool.Exec(context.Background(), notificationChannelsTestTeardown)
		pool.Close()
	}
	return ds, pool, cleanup
}

// setupChannelHandler builds a NotificationChannelHandler backed by a
// real auth store and grants the test user the
// manage_notification_channels permission. The returned userID is
// suitable for `withUser` to satisfy the permission check.
func setupChannelHandler(t *testing.T, ds *database.Datastore) (*NotificationChannelHandler, int64, func()) {
	t.Helper()
	authStore, cleanup := newAuthStoreForChannelTests(t)
	userID := setupUserWithPermission(t, authStore, "channel_admin",
		auth.PermManageNotificationChannels)
	checker := auth.NewRBACChecker(authStore)
	handler := NewNotificationChannelHandlerWithSecurity(ds, authStore, checker, false, nil, nil)
	return handler, userID, cleanup
}

// rawJSON is the dynamic shape used to inspect the JSON body without
// relying on the Go struct (which would obscure missing fields).
type rawJSON map[string]any

// decodeRaw returns the response body as a rawJSON map and fails the
// test on unexpected content.
func decodeRaw(t *testing.T, body []byte) rawJSON {
	t.Helper()
	var m rawJSON
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode: %v; body=%s", err, string(body))
	}
	return m
}

// createTestChannel inserts a channel via the datastore so the GET
// path returns a fully-populated row, including encrypted secrets.
// Returns the channel's ID.
func createTestChannel(t *testing.T, ds *database.Datastore, name string,
	webhook, authCreds, smtpUser, smtpPass *string) int64 {
	t.Helper()
	owner := "channel_admin"
	channel := &database.NotificationChannel{
		OwnerUsername:         &owner,
		Enabled:               true,
		ChannelType:           database.ChannelTypeEmail,
		Name:                  name,
		HTTPMethod:            "POST",
		SMTPPort:              587,
		SMTPHost:              ptr("smtp.example.com"),
		FromAddress:           ptr("alerts@example.com"),
		WebhookURL:            webhook,
		AuthCredentials:       authCreds,
		SMTPUsername:          smtpUser,
		SMTPPassword:          smtpPass,
		ReminderIntervalHours: 4,
	}
	if err := ds.CreateNotificationChannel(context.Background(), channel); err != nil {
		t.Fatalf("CreateNotificationChannel: %v", err)
	}
	return channel.ID
}

func ptr(s string) *string { return &s }

// -- GET /api/v1/notification-channels/{id} -----------------------------------

// TestGetChannel_RedactsSecretsAndExposesFlags is the primary
// regression test for issue #187. It exercises the GET-by-ID path with
// every secret populated and asserts:
//   - the response body contains none of the secret VALUES;
//   - the response body contains none of the redacted JSON KEYS;
//   - all four `*_set` flags are present and `true`.
func TestGetChannel_RedactsSecretsAndExposesFlags(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()

	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	webhook := "https://hooks.example.com/leak-me-not"
	authCreds := "Bearer leak-me-not"
	smtpUser := "leak-me-not@example.com"
	smtpPass := "leak-me-not-pass"
	channelID := createTestChannel(t, ds, "secret-channel",
		&webhook, &authCreds, &smtpUser, &smtpPass)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/notification-channels/"+strconv.FormatInt(channelID, 10), nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()
	handler.handleChannelSubpath(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	for _, leaked := range []string{webhook, authCreds, smtpUser, smtpPass} {
		if strings.Contains(body, leaked) {
			t.Errorf("body leaked %q; body=%s", leaked, body)
		}
	}
	for _, redactedKey := range []string{
		`"webhook_url"`, `"auth_credentials"`, `"smtp_username"`, `"smtp_password"`,
	} {
		if strings.Contains(body, redactedKey) {
			t.Errorf("redacted key %s appeared; body=%s", redactedKey, body)
		}
	}

	got := decodeRaw(t, rec.Body.Bytes())
	for _, key := range []string{
		"webhook_url_set", "auth_credentials_set",
		"smtp_username_set", "smtp_password_set",
	} {
		v, ok := got[key]
		if !ok {
			t.Errorf("missing %s in response", key)
			continue
		}
		if b, ok := v.(bool); !ok || !b {
			t.Errorf("%s = %v, want true", key, v)
		}
	}
}

// TestGetChannel_FlagsFalseWhenSecretsAbsent verifies the inverse: a
// channel created without secrets reports every `*_set` flag as false.
func TestGetChannel_FlagsFalseWhenSecretsAbsent(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()

	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	channelID := createTestChannel(t, ds, "no-secrets", nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/notification-channels/"+strconv.FormatInt(channelID, 10), nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()
	handler.handleChannelSubpath(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	got := decodeRaw(t, rec.Body.Bytes())
	// Require the *_set keys to be present, decode as bool, and report
	// false. The previous form `got[key].(bool)` returned the zero
	// value for a missing key, so a regression that dropped the flags
	// entirely from the response would silently pass.
	for _, key := range []string{
		"webhook_url_set", "auth_credentials_set",
		"smtp_username_set", "smtp_password_set",
	} {
		v, ok := got[key]
		if !ok {
			t.Errorf("missing %s in response", key)
			continue
		}
		b, isBool := v.(bool)
		if !isBool || b {
			t.Errorf("%s = %v, want false", key, v)
		}
	}
}

// createTestWebhookChannelWithHeaders inserts a webhook channel that
// carries a custom Headers map and returns its ID. Webhook channels
// are minimal — they don't need SMTP fields — but the dedicated
// helper avoids tangling the existing email-focused createTestChannel
// signature with another optional argument.
func createTestWebhookChannelWithHeaders(t *testing.T, ds *database.Datastore,
	name string, headers map[string]string) int64 {
	t.Helper()
	owner := "channel_admin"
	endpoint := "https://example.com/webhook"
	channel := &database.NotificationChannel{
		OwnerUsername:         &owner,
		Enabled:               true,
		ChannelType:           database.ChannelTypeWebhook,
		Name:                  name,
		HTTPMethod:            "POST",
		EndpointURL:           &endpoint,
		Headers:               headers,
		ReminderIntervalHours: 4,
	}
	if err := ds.CreateNotificationChannel(context.Background(), channel); err != nil {
		t.Fatalf("CreateNotificationChannel: %v", err)
	}
	return channel.ID
}

// TestGetChannel_RedactsHeaderValues is a regression test for the
// CodeRabbit finding on PR #196. Custom webhook headers commonly
// carry secrets (Authorization bearer tokens, X-API-Key, etc.), so
// the response struct uses `json:"-"` for Headers and exposes only
// header_names. This test creates a webhook channel with a
// secret-bearing header, fetches it, and asserts:
//   - the secret value is absent from the response body;
//   - the JSON key "headers" is absent from the response;
//   - the JSON key "header_names" is present and lists the configured
//     header keys in alphabetical order.
func TestGetChannel_RedactsHeaderValues(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()

	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	const bearer = "secret-token-xyz"
	channelID := createTestWebhookChannelWithHeaders(t, ds,
		"with-headers", map[string]string{
			"Authorization": "Bearer " + bearer,
			"X-Tenant-ID":   "tenant-1",
		})

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/notification-channels/"+strconv.FormatInt(channelID, 10), nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()
	handler.handleChannelSubpath(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// The secret value MUST NOT appear anywhere in the response.
	if strings.Contains(body, bearer) {
		t.Errorf("body leaked header secret %q; body=%s", bearer, body)
	}
	// The "headers" JSON key MUST be absent.
	if strings.Contains(body, `"headers"`) {
		t.Errorf("response includes redacted key \"headers\"; body=%s", body)
	}

	got := decodeRaw(t, rec.Body.Bytes())
	raw, ok := got["header_names"]
	if !ok {
		t.Fatalf("response missing header_names; body=%s", body)
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("header_names = %v (%T), want []any", raw, raw)
	}
	names := make([]string, 0, len(arr))
	for _, v := range arr {
		s, ok := v.(string)
		if !ok {
			t.Errorf("header_names entry %v is not a string", v)
			continue
		}
		names = append(names, s)
	}
	want := []string{"Authorization", "X-Tenant-ID"}
	if !equalStringSlice(names, want) {
		t.Errorf("header_names = %v, want %v (sorted)", names, want)
	}
}

// TestGetChannel_NoHeadersOmitsHeaderNames verifies that a channel
// configured without custom headers has the `header_names` field
// omitted entirely from the JSON response (omitempty + nil slice).
func TestGetChannel_NoHeadersOmitsHeaderNames(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()

	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	channelID := createTestWebhookChannelWithHeaders(t, ds, "no-headers", nil)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/notification-channels/"+strconv.FormatInt(channelID, 10), nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()
	handler.handleChannelSubpath(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"header_names"`) {
		t.Errorf("response includes header_names when none configured; body=%s",
			rec.Body.String())
	}
}

// equalStringSlice reports whether two string slices have identical
// contents and order. Used by the header-redaction test to confirm
// the alphabetical ordering of header_names.
func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestGetChannel_NotFound covers the 404 path.
func TestGetChannel_NotFound(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()

	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notification-channels/99999", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()
	handler.handleChannelSubpath(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// -- GET /api/v1/notification-channels (list) ---------------------------------

// expectBoolField asserts that `got[key]` is present, decodes as a
// bool, and matches the expected value. It centralizes the
// require-key-present + type-check + value-match pattern so list
// assertions stay readable and stay below the project complexity
// budget.
func expectBoolField(t *testing.T, got rawJSON, label, key string, want bool) {
	t.Helper()
	v, ok := got[key]
	if !ok {
		t.Errorf("%s: missing %s in response", label, key)
		return
	}
	b, isBool := v.(bool)
	if !isBool {
		t.Errorf("%s: %s = %v (%T), want bool", label, key, v, v)
		return
	}
	if b != want {
		t.Errorf("%s: %s = %v, want %v", label, key, b, want)
	}
}

// expectKeyAbsent reports an error when a redacted JSON key appears
// in the response payload. Used to keep the list-redaction loop
// concise.
func expectKeyAbsent(t *testing.T, got rawJSON, label, key string) {
	t.Helper()
	if _, ok := got[key]; ok {
		t.Errorf("%s: leaked redacted key %q; value=%v", label, key, got[key])
	}
}

// listChannelsFlagKeys are the four boolean indicators every channel
// in the list response must expose.
var listChannelsFlagKeys = []string{
	"webhook_url_set", "auth_credentials_set",
	"smtp_username_set", "smtp_password_set",
}

// listChannelsRedactedKeys are the four secret JSON keys that must
// never appear on a channel in the list response.
var listChannelsRedactedKeys = []string{
	"webhook_url", "auth_credentials", "smtp_username", "smtp_password",
}

// expectListRowFlags identifies the row by name and asserts the
// four `*_set` flags match the expected state for that row, plus
// none of the redacted keys leak. The helper keeps
// TestListChannels_RedactsSecrets below the project ccn-medium
// budget.
func expectListRowFlags(t *testing.T, item rawJSON) {
	t.Helper()
	name, _ := item["name"].(string)
	var want bool
	switch name {
	case "list-with-secrets":
		want = true
	case "list-without-secrets":
		want = false
	default:
		t.Errorf("unexpected channel name in list: %q", name)
		return
	}
	for _, key := range listChannelsFlagKeys {
		expectBoolField(t, item, name, key, want)
	}
	for _, redactedKey := range listChannelsRedactedKeys {
		expectKeyAbsent(t, item, name, redactedKey)
	}
}

// TestListChannels_RedactsSecrets verifies the list endpoint redacts
// secret values, exposes per-row `*_set` flags that match the actual
// stored state, and never echoes a redacted key. Two channels are
// created — one with all four secrets, one with none — so a
// regression that returned all-true or all-false uniformly across
// the list cannot pass this test.
func TestListChannels_RedactsSecrets(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()

	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	const (
		wh   = "https://hooks.example.com/list-leak"
		ac   = "Bearer list-leak-bearer"
		user = "list-leak-user"
		pw   = "list-leak-password"
	)
	createTestChannel(t, ds, "list-with-secrets", ptr(wh), ptr(ac), ptr(user), ptr(pw))
	createTestChannel(t, ds, "list-without-secrets", nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notification-channels", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()
	handler.handleChannels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, leaked := range []string{wh, ac, user, pw} {
		if strings.Contains(body, leaked) {
			t.Errorf("list leaked %q; body=%s", leaked, body)
		}
	}

	var arr []rawJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &arr); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(arr) != 2 {
		t.Fatalf("len(channels) = %d, want 2", len(arr))
	}
	for _, item := range arr {
		expectListRowFlags(t, item)
	}
}

// -- PUT /api/v1/notification-channels/{id} -----------------------------------

// putChannel is a small helper that performs a PUT with the given
// raw JSON body and returns the response recorder.
func putChannel(t *testing.T, h *NotificationChannelHandler,
	userID int64, channelID int64, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut,
		"/api/v1/notification-channels/"+strconv.FormatInt(channelID, 10),
		bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	req = withUsername(req, "channel_admin")
	rec := httptest.NewRecorder()
	h.handleChannelSubpath(rec, req)
	return rec
}

// readEncryptedColumn returns the raw value stored in an encrypted
// column for the given channel. It returns (value, ok) where ok is
// false when the column is SQL NULL.
func readEncryptedColumn(t *testing.T, pool *pgxpool.Pool,
	channelID int64, column string) (string, bool) {
	t.Helper()
	var v *string
	err := pool.QueryRow(context.Background(),
		fmt.Sprintf(`SELECT %s FROM notification_channels WHERE id = $1`, column),
		channelID).Scan(&v)
	if err != nil {
		t.Fatalf("read column %s: %v", column, err)
	}
	if v == nil {
		return "", false
	}
	return *v, true
}

// assertSecretPointer compares a loaded notification channel pointer
// field against an expected non-nil string. It exists so the per-field
// sub-tests below stay below the project cyclomatic complexity floor:
// each sub-test reduces to a single helper call instead of a
// `nil-or-mismatch` compound conditional.
func assertSecretPointer(t *testing.T, name string, got *string, want string) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want pointer to %q", name, want)
		return
	}
	if *got != want {
		t.Errorf("%s = %q, want %q", name, *got, want)
	}
}

// secretFieldCase describes one encrypted secret on NotificationChannel
// for the sub-test tables below. The getter pulls the pointer out of a
// loaded channel so each sub-test body is a single helper call.
type secretFieldCase struct {
	name   string
	want   string
	getter func(*database.NotificationChannel) *string
	flag   func(*database.NotificationChannel) bool
}

// allSecretFieldCases enumerates the four redacted secret columns and
// supplies the per-case expected plaintext, pointer accessor, and the
// matching `*_set` flag accessor used by the preservation/replacement
// sub-tests.
func allSecretFieldCases(webhook, authCreds, smtpUser, smtpPass string) []secretFieldCase {
	return []secretFieldCase{
		{
			name:   "webhook_url",
			want:   webhook,
			getter: func(c *database.NotificationChannel) *string { return c.WebhookURL },
			flag:   func(c *database.NotificationChannel) bool { return c.WebhookURLSet },
		},
		{
			name:   "auth_credentials",
			want:   authCreds,
			getter: func(c *database.NotificationChannel) *string { return c.AuthCredentials },
			flag:   func(c *database.NotificationChannel) bool { return c.AuthCredentialsSet },
		},
		{
			name:   "smtp_username",
			want:   smtpUser,
			getter: func(c *database.NotificationChannel) *string { return c.SMTPUsername },
			flag:   func(c *database.NotificationChannel) bool { return c.SMTPUsernameSet },
		},
		{
			name:   "smtp_password",
			want:   smtpPass,
			getter: func(c *database.NotificationChannel) *string { return c.SMTPPassword },
			flag:   func(c *database.NotificationChannel) bool { return c.SMTPPasswordSet },
		},
	}
}

// TestUpdateChannel_OmittedSecretsArePreserved ensures that a PUT body
// that does NOT mention a secret field leaves the existing decrypted
// value untouched. This is the crucial guarantee the redaction change
// depends on: clients can fetch -> edit -> submit without clobbering
// secrets they never saw.
//
// We compare the post-PUT plaintext (via the datastore's decrypt
// path) rather than the raw ciphertext because EncryptPassword salts
// each call with a fresh random salt, so a re-encrypt of the same
// plaintext yields different bytes — the persisted column changes
// even when the secret value is preserved.
//
// Per-field assertions are split into table-driven sub-tests so the
// outer function stays under the project's cyclomatic complexity
// budget (Codacy/Lizard `ccn-medium`, limit 12).
func TestUpdateChannel_OmittedSecretsArePreserved(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()

	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	const (
		origWebhook = "https://hooks.example.com/orig"
		origAuth    = "Bearer original"
		origUser    = "original-user"
		origPass    = "original-password"
	)
	channelID := createTestChannel(t, ds, "preserve-test",
		ptr(origWebhook), ptr(origAuth), ptr(origUser), ptr(origPass))

	// PUT with only non-secret fields. smtp_host and from_address are
	// already populated on the row and carry forward via the merge.
	rec := putChannel(t, handler, userID, channelID,
		`{"name":"renamed","description":"updated"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	loaded, err := ds.GetNotificationChannel(context.Background(), channelID)
	if err != nil {
		t.Fatalf("GetNotificationChannel: %v", err)
	}

	cases := allSecretFieldCases(origWebhook, origAuth, origUser, origPass)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name+"_preserved", func(t *testing.T) {
			assertSecretPointer(t, tc.name, tc.getter(loaded), tc.want)
		})
		t.Run(tc.name+"_set_flag", func(t *testing.T) {
			if !tc.flag(loaded) {
				t.Errorf("%s_set = false, want true (preservation should keep flag on)", tc.name)
			}
		})
	}

	t.Run("name_updated", func(t *testing.T) {
		if loaded.Name != "renamed" {
			t.Errorf("Name = %q, want %q", loaded.Name, "renamed")
		}
	})
}

// TestUpdateChannel_NonEmptySecretReplaces verifies that supplying a
// new value for each secret field overwrites the stored value. The
// per-field assertions are factored into a sub-test loop so the outer
// function stays under the project complexity budget.
func TestUpdateChannel_NonEmptySecretReplaces(t *testing.T) {
	ds, pool, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()

	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	channelID := createTestChannel(t, ds, "replace-test",
		ptr("https://hooks.example.com/orig"),
		ptr("Bearer original"),
		ptr("original-user"),
		ptr("original-password"),
	)

	const (
		newWebhook = "https://hooks.example.com/new"
		newAuth    = "Bearer new"
		newUser    = "new-user"
		newPass    = "new-password"
	)
	body := `{
		"webhook_url": "` + newWebhook + `",
		"auth_credentials": "` + newAuth + `",
		"smtp_username": "` + newUser + `",
		"smtp_password": "` + newPass + `"
	}`
	rec := putChannel(t, handler, userID, channelID, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// smtp_username is stored plaintext, so we can compare directly
	// against the raw column. Other secrets go through GetNotificationChannel
	// which decrypts on read.
	t.Run("smtp_username_plaintext_column", func(t *testing.T) {
		v, _ := readEncryptedColumn(t, pool, channelID, "smtp_username")
		if v != newUser {
			t.Errorf("smtp_username = %q, want %q", v, newUser)
		}
	})

	loaded, err := ds.GetNotificationChannel(context.Background(), channelID)
	if err != nil {
		t.Fatalf("GetNotificationChannel: %v", err)
	}

	cases := allSecretFieldCases(newWebhook, newAuth, newUser, newPass)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name+"_replaced", func(t *testing.T) {
			assertSecretPointer(t, tc.name, tc.getter(loaded), tc.want)
		})
		t.Run(tc.name+"_set_flag", func(t *testing.T) {
			if !tc.flag(loaded) {
				t.Errorf("%s_set = false, want true after replacement", tc.name)
			}
		})
	}
}

// TestUpdateChannel_EmptyStringClears verifies the explicit-clear
// semantics: a PUT with `"smtp_password": ""` (and likewise for the
// others) blanks the column and flips the corresponding `*_set` flag
// to false on the next read.
func TestUpdateChannel_EmptyStringClears(t *testing.T) {
	ds, pool, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()

	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	channelID := createTestChannel(t, ds, "clear-test",
		ptr("https://hooks.example.com/orig"),
		ptr("Bearer original"),
		ptr("original-user"),
		ptr("original-password"),
	)

	body := `{
		"webhook_url": "",
		"auth_credentials": "",
		"smtp_username": "",
		"smtp_password": ""
	}`
	rec := putChannel(t, handler, userID, channelID, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// smtp_username is plaintext: empty string ends up as "".
	if v, ok := readEncryptedColumn(t, pool, channelID, "smtp_username"); ok && v != "" {
		t.Errorf("smtp_username = %q, want empty", v)
	}

	loaded, err := ds.GetNotificationChannel(context.Background(), channelID)
	if err != nil {
		t.Fatalf("GetNotificationChannel: %v", err)
	}
	// `Set` flags must report "not set" since stored values are empty.
	if loaded.WebhookURLSet {
		t.Errorf("WebhookURLSet true, want false")
	}
	if loaded.AuthCredentialsSet {
		t.Errorf("AuthCredentialsSet true, want false")
	}
	if loaded.SMTPUsernameSet {
		t.Errorf("SMTPUsernameSet true, want false")
	}
	if loaded.SMTPPasswordSet {
		t.Errorf("SMTPPasswordSet true, want false")
	}
}

// TestUpdateChannel_ResponseAlsoRedacted asserts the response body of
// a successful PUT applies the same redaction as GET. A client that
// reads the PUT response must not see the values it just submitted.
func TestUpdateChannel_ResponseAlsoRedacted(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()

	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	channelID := createTestChannel(t, ds, "put-response", nil, nil, nil, nil)

	newSecret := "very-secret-do-not-echo"
	body := `{"smtp_password":"` + newSecret + `"}`
	rec := putChannel(t, handler, userID, channelID, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), newSecret) {
		t.Errorf("PUT response leaked secret %q; body=%s", newSecret, rec.Body.String())
	}
	got := decodeRaw(t, rec.Body.Bytes())
	if v, _ := got["smtp_password_set"].(bool); !v {
		t.Errorf("smtp_password_set = %v, want true", got["smtp_password_set"])
	}
}

// -- POST /api/v1/notification-channels ---------------------------------------

// postChannel sends a POST with the supplied body and returns the
// recorder. The username context is populated so that createChannel
// can record the row's owner.
func postChannel(t *testing.T, h *NotificationChannelHandler, userID int64, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notification-channels",
		bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	req = withUsername(req, "channel_admin")
	rec := httptest.NewRecorder()
	h.handleChannels(rec, req)
	return rec
}

// TestCreateChannel_HappyPath covers the create-then-reload path. The
// returned body must redact every secret it just accepted.
func TestCreateChannel_HappyPath(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	body := `{
		"channel_type": "email",
		"name": "create-happy",
		"smtp_host": "smtp.example.com",
		"from_address": "alerts@example.com",
		"smtp_username": "secret-user",
		"smtp_password": "secret-pass"
	}`
	rec := postChannel(t, handler, userID, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-pass") ||
		strings.Contains(rec.Body.String(), "secret-user") {
		t.Errorf("POST response leaked secret; body=%s", rec.Body.String())
	}
	got := decodeRaw(t, rec.Body.Bytes())
	if v, _ := got["smtp_password_set"].(bool); !v {
		t.Errorf("smtp_password_set = %v, want true", got["smtp_password_set"])
	}
	if v, _ := got["smtp_username_set"].(bool); !v {
		t.Errorf("smtp_username_set = %v, want true", got["smtp_username_set"])
	}
}

// TestCreateChannel_DefaultsAndUnknownUsername exercises the default
// branches (Enabled, SMTPPort, SMTPUseTLS, HTTPMethod, ReminderEnabled,
// ReminderIntervalHours, IsEstateDefault, Headers) and the
// unknown-username fallback when no username context is present.
func TestCreateChannel_DefaultsAndUnknownUsername(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	body := `{
		"channel_type": "slack",
		"name": "defaults-test"
	}`
	// Skip the username helper so GetUsernameFromContext returns "".
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notification-channels",
		bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	rec := httptest.NewRecorder()
	handler.handleChannels(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	got := decodeRaw(t, rec.Body.Bytes())
	if v, _ := got["smtp_port"].(float64); v != 587 {
		t.Errorf("smtp_port = %v, want 587", got["smtp_port"])
	}
	if v, _ := got["http_method"].(string); v != "POST" {
		t.Errorf("http_method = %v, want POST", got["http_method"])
	}
	if v, _ := got["reminder_interval_hours"].(float64); v != 4 {
		t.Errorf("reminder_interval_hours = %v, want 4", got["reminder_interval_hours"])
	}
	if v, _ := got["enabled"].(bool); !v {
		t.Errorf("enabled = %v, want true", got["enabled"])
	}
	if owner, _ := got["owner_username"].(string); owner != "unknown" {
		t.Errorf("owner_username = %q, want %q", owner, "unknown")
	}
}

// TestCreateChannel_HeadersAndOptionalFields covers the explicit-value
// branches for the optional `*bool` and `*int` fields plus the headers
// map. These complement TestCreateChannel_DefaultsAndUnknownUsername.
func TestCreateChannel_HeadersAndOptionalFields(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	body := `{
		"channel_type": "webhook",
		"name": "with-options",
		"endpoint_url": "https://example.com/wh",
		"http_method": "PUT",
		"headers": {"X-Custom": "value"},
		"enabled": false,
		"is_estate_default": true,
		"smtp_port": 25,
		"smtp_use_tls": false,
		"reminder_enabled": true,
		"reminder_interval_hours": 12
	}`
	rec := postChannel(t, handler, userID, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	got := decodeRaw(t, rec.Body.Bytes())
	if v, _ := got["http_method"].(string); v != "PUT" {
		t.Errorf("http_method = %v, want PUT", got["http_method"])
	}
	if v, _ := got["reminder_interval_hours"].(float64); v != 12 {
		t.Errorf("reminder_interval_hours = %v, want 12", got["reminder_interval_hours"])
	}
	if v, _ := got["is_estate_default"].(bool); !v {
		t.Errorf("is_estate_default = %v, want true", got["is_estate_default"])
	}
	if v, _ := got["enabled"].(bool); v {
		t.Errorf("enabled = %v, want false", got["enabled"])
	}
}

// TestCreateChannel_ValidationErrors covers the 400 branches: invalid
// channel type, missing name, and missing email-specific fields.
func TestCreateChannel_ValidationErrors(t *testing.T) {
	authStore, cleanupStore := newAuthStoreForChannelTests(t)
	defer cleanupStore()
	userID := setupUserWithPermission(t, authStore, "ch_validator",
		auth.PermManageNotificationChannels)
	checker := auth.NewRBACChecker(authStore)
	handler := NewNotificationChannelHandlerWithSecurity(nil, authStore, checker, false, nil, nil)

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "invalid channel type",
			body: `{"channel_type":"sms","name":"bad"}`,
			want: "Invalid channel_type: must be one of email, slack, mattermost, telegram, webhook",
		},
		{
			name: "missing name",
			body: `{"channel_type":"slack","name":""}`,
			want: "Name is required",
		},
		{
			name: "email missing smtp_host",
			body: `{"channel_type":"email","name":"e","from_address":"a@b.c"}`,
			want: "smtp_host is required for email channels",
		},
		{
			name: "email missing from_address",
			body: `{"channel_type":"email","name":"e","smtp_host":"s.example.com"}`,
			want: "from_address is required for email channels",
		},
		{
			name: "malformed JSON",
			body: `{not json`,
			want: "Invalid request body",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/notification-channels",
				bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			req = withUser(req, userID)
			req = withUsername(req, "ch_validator")
			rec := httptest.NewRecorder()
			handler.handleChannels(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			var resp ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Error != tc.want {
				t.Errorf("Error = %q, want %q", resp.Error, tc.want)
			}
		})
	}
}

// TestUpdateChannel_ValidationErrors covers the 400 branches reachable
// from the merge logic: invalid channel_type and missing required
// email fields after the merge.
func TestUpdateChannel_ValidationErrors(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	emailID := createTestChannel(t, ds, "validation-email", nil, nil, nil, nil)

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "invalid channel type",
			body: `{"channel_type":"sms"}`,
			want: "Invalid channel_type: must be one of email, slack, mattermost, telegram, webhook",
		},
		{
			name: "clear smtp_host on email channel",
			// Sending an empty string for smtp_host on an email
			// channel must trip the post-merge validation. This
			// exercises the smtp_host-required branch.
			body: `{"smtp_host":""}`,
			want: "smtp_host is required for email channels",
		},
		{
			name: "clear from_address on email channel",
			body: `{"from_address":""}`,
			want: "from_address is required for email channels",
		},
		{
			name: "malformed JSON",
			body: `{not json`,
			want: "Invalid request body",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := putChannel(t, handler, userID, emailID, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			var resp ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Error != tc.want {
				t.Errorf("Error = %q, want %q", resp.Error, tc.want)
			}
		})
	}
}

// TestUpdateChannel_AllOptionalMergeBranches exercises every branch
// in the merge logic that copies an optional `*string` or `*bool`
// field from the request onto `existing`. This covers the branches
// for smtp_host, from_address, template_alert_fire,
// template_alert_clear, and template_reminder which the other PUT
// tests don't touch.
func TestUpdateChannel_AllOptionalMergeBranches(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	channelID := createTestChannel(t, ds, "all-branches", nil, nil, nil, nil)

	body := `{
		"channel_type": "email",
		"name": "all-branches-renamed",
		"description": "merge all branches",
		"enabled": false,
		"is_estate_default": true,
		"smtp_host": "smtp.example.org",
		"smtp_port": 25,
		"smtp_use_tls": false,
		"from_address": "ops@example.org",
		"from_name": "Ops Team",
		"endpoint_url": "https://example.com/wh",
		"http_method": "PATCH",
		"headers": {"X-Trace": "yes"},
		"auth_type": "bearer",
		"template_alert_fire": "fire-template",
		"template_alert_clear": "clear-template",
		"template_reminder": "reminder-template",
		"reminder_enabled": true,
		"reminder_interval_hours": 8
	}`
	rec := putChannel(t, handler, userID, channelID, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	got := decodeRaw(t, rec.Body.Bytes())
	if v, _ := got["name"].(string); v != "all-branches-renamed" {
		t.Errorf("name = %q, want %q", v, "all-branches-renamed")
	}
	if v, _ := got["channel_type"].(string); v != "email" {
		t.Errorf("channel_type = %q, want %q", v, "email")
	}
	if v, _ := got["smtp_port"].(float64); v != 25 {
		t.Errorf("smtp_port = %v, want 25", got["smtp_port"])
	}
	if v, _ := got["template_alert_fire"].(string); v != "fire-template" {
		t.Errorf("template_alert_fire = %q, want %q", v, "fire-template")
	}
}

// TestUpdateChannel_NotFound covers the 404 branch when the target
// channel does not exist.
func TestUpdateChannel_NotFound(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()

	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	rec := putChannel(t, handler, userID, 999999, `{"name":"missing"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PUT status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

// TestNotificationChannelHandler_DatastoreErrors closes the datastore
// pool mid-test, forcing every DB-backed handler path to surface a
// 500. This covers the failure branches in listChannels, getChannel,
// createChannel, updateChannel, and deleteChannel that the
// happy-path tests do not touch.
//
// The test runs LAST in this file (Go runs tests in source order
// within a package) and creates its own datastore so that closing
// the pool does not affect other tests.
func TestNotificationChannelHandler_DatastoreErrors(t *testing.T) {
	ds, pool, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	// Create a channel BEFORE closing the pool so updateChannel and
	// deleteChannel can target a real ID.
	channelID := createTestChannel(t, ds, "doomed", nil, nil, nil, nil)

	// Close the underlying pool so every subsequent query fails.
	pool.Close()

	cases := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{"list 500", http.MethodGet, "/api/v1/notification-channels", "", http.StatusInternalServerError},
		{"get 500", http.MethodGet, "/api/v1/notification-channels/" + strconv.FormatInt(channelID, 10), "", http.StatusInternalServerError},
		{"delete 500", http.MethodDelete, "/api/v1/notification-channels/" + strconv.FormatInt(channelID, 10), "", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body []byte
			if tc.body != "" {
				body = []byte(tc.body)
			}
			req := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req = withUser(req, userID)
			req = withUsername(req, "channel_admin")
			rec := httptest.NewRecorder()
			if tc.path == "/api/v1/notification-channels" {
				handler.handleChannels(rec, req)
			} else {
				handler.handleChannelSubpath(rec, req)
			}
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	// POST against the closed pool to exercise the create error path.
	t.Run("create 500", func(t *testing.T) {
		body := `{"channel_type":"slack","name":"will-fail"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/notification-channels",
			bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		req = withUser(req, userID)
		req = withUsername(req, "channel_admin")
		rec := httptest.NewRecorder()
		handler.handleChannels(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
		}
	})

	// PUT against the closed pool: the existing-channel fetch will fail
	// before merge, returning 500.
	t.Run("update 500", func(t *testing.T) {
		body := `{"name":"updated"}`
		req := httptest.NewRequest(http.MethodPut,
			"/api/v1/notification-channels/"+strconv.FormatInt(channelID, 10),
			bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		req = withUser(req, userID)
		req = withUsername(req, "channel_admin")
		rec := httptest.NewRecorder()
		handler.handleChannelSubpath(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
		}
	})
}

// =============================================================================
// Local helpers
// =============================================================================

// newAuthStoreForChannelTests builds an AuthStore on a temp directory
// and lowers bcrypt cost. The returned cleanup must be deferred to
// avoid goroutine leaks.
func newAuthStoreForChannelTests(t *testing.T) (*auth.AuthStore, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "channel-handler-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	store, err := auth.NewAuthStore(tmpDir, 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("NewAuthStore: %v", err)
	}
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)
	return store, func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}
}

// =============================================================================
// Telegram channels (issue #475)
// =============================================================================

// telegramHandlerToken is a syntactically valid but fictitious Bot API
// token. Every assertion below treats it as a live credential.
const telegramHandlerToken = "123456789:AAErq-leak-me-not-TEST-TOKEN"

// telegramUndecryptableToken stands in for what
// decryptNotificationSecret hands back when a stored token cannot be
// decrypted because the server secret was rotated or lost: the raw
// column value, which is base64 and can never match
// telegramBotTokenPattern.
const telegramUndecryptableToken = "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXo="

// createTelegramChannel inserts a telegram channel through the datastore
// so the handler paths see a fully-populated, encrypted row.
func createTelegramChannel(t *testing.T, ds *database.Datastore, name string,
	botToken, chatID *string) int64 {
	t.Helper()
	owner := "channel_admin"
	channel := &database.NotificationChannel{
		OwnerUsername:         &owner,
		Enabled:               true,
		ChannelType:           database.ChannelTypeTelegram,
		Name:                  name,
		HTTPMethod:            "POST",
		SMTPPort:              587,
		TelegramBotToken:      botToken,
		TelegramChatID:        chatID,
		ReminderIntervalHours: 4,
	}
	if err := ds.CreateNotificationChannel(context.Background(), channel); err != nil {
		t.Fatalf("CreateNotificationChannel: %v", err)
	}
	return channel.ID
}

// TestCreateChannel_TelegramHappyPath creates a telegram channel and
// asserts the response redacts the bot token, advertises it through
// telegram_bot_token_set, and returns the chat ID in clear.
func TestCreateChannel_TelegramHappyPath(t *testing.T) {
	ds, pool, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	body := fmt.Sprintf(`{
		"channel_type": "telegram",
		"name": "tg-create",
		"telegram_bot_token": %q,
		"telegram_chat_id": "-1001234567890"
	}`, telegramHandlerToken)

	rec := postChannel(t, handler, userID, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	// Assert on the marshaled response body, not on the struct: the
	// redaction contract is a JSON-serialization contract.
	raw := rec.Body.String()
	if strings.Contains(raw, telegramHandlerToken) {
		t.Errorf("POST response leaked the bot token; body=%s", raw)
	}
	if strings.Contains(raw, `"telegram_bot_token"`) {
		t.Errorf("POST response carries a telegram_bot_token key; body=%s", raw)
	}

	got := decodeRaw(t, rec.Body.Bytes())
	if v, _ := got["telegram_bot_token_set"].(bool); !v {
		t.Errorf("telegram_bot_token_set = %v, want true", got["telegram_bot_token_set"])
	}
	if v, _ := got["telegram_chat_id"].(string); v != "-1001234567890" {
		t.Errorf("telegram_chat_id = %v, want -1001234567890", got["telegram_chat_id"])
	}
	if v, _ := got["channel_type"].(string); v != "telegram" {
		t.Errorf("channel_type = %v, want telegram", got["channel_type"])
	}

	channelID := int64(got["id"].(float64))
	stored, ok := readEncryptedColumn(t, pool, channelID, "telegram_bot_token_encrypted")
	if !ok {
		t.Fatal("telegram_bot_token_encrypted is NULL after create")
	}
	if stored == telegramHandlerToken {
		t.Error("bot token was stored in plaintext")
	}
}

// TestCreateChannel_TelegramValidationErrors covers the 400 branches
// specific to telegram: each required field missing, and each supplied
// with an unusable shape.
func TestCreateChannel_TelegramValidationErrors(t *testing.T) {
	authStore, cleanupStore := newAuthStoreForChannelTests(t)
	defer cleanupStore()
	userID := setupUserWithPermission(t, authStore, "tg_validator",
		auth.PermManageNotificationChannels)
	checker := auth.NewRBACChecker(authStore)
	handler := NewNotificationChannelHandlerWithSecurity(nil, authStore, checker, false, nil, nil)

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing bot token",
			body: `{"channel_type":"telegram","name":"t","telegram_chat_id":"-100123"}`,
			want: "telegram_bot_token is required for telegram channels",
		},
		{
			name: "empty bot token",
			body: `{"channel_type":"telegram","name":"t","telegram_bot_token":"","telegram_chat_id":"-100123"}`,
			want: "telegram_bot_token is required for telegram channels",
		},
		{
			name: "bot token with no body",
			body: `{"channel_type":"telegram","name":"t","telegram_bot_token":"123456789:","telegram_chat_id":"-100123"}`,
			want: "telegram_bot_token must have the form <bot id>:<token>",
		},
		{
			name: "bot token with no colon",
			body: `{"channel_type":"telegram","name":"t","telegram_bot_token":"nonsense","telegram_chat_id":"-100123"}`,
			want: "telegram_bot_token must have the form <bot id>:<token>",
		},
		{
			name: "bot token containing a path separator",
			body: `{"channel_type":"telegram","name":"t","telegram_bot_token":"1:a/b","telegram_chat_id":"-100123"}`,
			want: "telegram_bot_token must have the form <bot id>:<token>",
		},
		{
			name: "missing chat id",
			body: `{"channel_type":"telegram","name":"t","telegram_bot_token":"123456789:AAA"}`,
			want: "telegram_chat_id is required for telegram channels",
		},
		{
			name: "empty chat id",
			body: `{"channel_type":"telegram","name":"t","telegram_bot_token":"123456789:AAA","telegram_chat_id":""}`,
			want: "telegram_chat_id is required for telegram channels",
		},
		{
			name: "chat id is a pasted URL",
			body: `{"channel_type":"telegram","name":"t","telegram_bot_token":"123456789:AAA","telegram_chat_id":"https://t.me/alerts"}`,
			want: "telegram_chat_id must be a numeric chat ID or an @channelusername",
		},
		{
			name: "chat id is a bare at sign",
			body: `{"channel_type":"telegram","name":"t","telegram_bot_token":"123456789:AAA","telegram_chat_id":"@"}`,
			want: "telegram_chat_id must be a numeric chat ID or an @channelusername",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/notification-channels",
				bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			req = withUser(req, userID)
			req = withUsername(req, "tg_validator")
			rec := httptest.NewRecorder()
			handler.handleChannels(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			var resp ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Error != tc.want {
				t.Errorf("Error = %q, want %q", resp.Error, tc.want)
			}
			if strings.Contains(rec.Body.String(), "leak-me-not") {
				t.Errorf("validation error echoed the submitted token: %s", rec.Body.String())
			}
		})
	}
}

// TestCreateChannel_TelegramAcceptsChatIDShapes confirms the permissive
// chat-ID check accepts every documented form rather than only the one
// the UI happens to produce.
func TestCreateChannel_TelegramAcceptsChatIDShapes(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	shapes := []string{"-1001234567890", "123456789", "@workbench_alerts", "@a_1"}
	for i, chatID := range shapes {
		t.Run(chatID, func(t *testing.T) {
			body := fmt.Sprintf(`{
				"channel_type": "telegram",
				"name": "tg-shape-%d",
				"telegram_bot_token": %q,
				"telegram_chat_id": %q
			}`, i, telegramHandlerToken, chatID)
			rec := postChannel(t, handler, userID, body)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
			}
			got := decodeRaw(t, rec.Body.Bytes())
			if v, _ := got["telegram_chat_id"].(string); v != chatID {
				t.Errorf("telegram_chat_id = %v, want %q", got["telegram_chat_id"], chatID)
			}
		})
	}
}

// TestUpdateChannel_TelegramOmittedTokenPreserved is the regression test
// for the fetch-then-edit round trip. The GET response never carries the
// bot token, so a UI that PUTs back what it read omits the field; that
// must keep the stored token rather than clearing it. The chat ID, which
// the UI does see, changes in the same request.
func TestUpdateChannel_TelegramOmittedTokenPreserved(t *testing.T) {
	ds, pool, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	token := telegramHandlerToken
	chatID := "-1001234567890"
	channelID := createTelegramChannel(t, ds, "tg-update", &token, &chatID)
	before, _ := readEncryptedColumn(t, pool, channelID, "telegram_bot_token_encrypted")

	rec := putChannel(t, handler, userID, channelID,
		`{"name":"tg-update-renamed","telegram_chat_id":"@workbench_alerts"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	got := decodeRaw(t, rec.Body.Bytes())
	if v, _ := got["telegram_bot_token_set"].(bool); !v {
		t.Errorf("telegram_bot_token_set = %v, want true (omitted token must be preserved)",
			got["telegram_bot_token_set"])
	}
	if v, _ := got["telegram_chat_id"].(string); v != "@workbench_alerts" {
		t.Errorf("telegram_chat_id = %v, want @workbench_alerts", got["telegram_chat_id"])
	}
	if strings.Contains(rec.Body.String(), telegramHandlerToken) {
		t.Errorf("PUT response leaked the bot token; body=%s", rec.Body.String())
	}

	// The stored ciphertext is re-encrypted on every write, so compare
	// the decrypted value rather than the column bytes.
	after, ok := readEncryptedColumn(t, pool, channelID, "telegram_bot_token_encrypted")
	if !ok || after == "" {
		t.Fatal("bot token column was cleared by an update that omitted it")
	}
	if after == telegramHandlerToken {
		t.Error("bot token was rewritten in plaintext")
	}
	if before == "" {
		t.Fatal("bot token column was empty before the update")
	}

	reloaded, err := ds.GetNotificationChannel(context.Background(), channelID)
	if err != nil {
		t.Fatalf("GetNotificationChannel: %v", err)
	}
	assertSecretPointer(t, "TelegramBotToken", reloaded.TelegramBotToken, telegramHandlerToken)
}

// TestUpdateChannel_TelegramTokenReplaced covers the other two arms of
// the three-way pointer semantics: a non-empty value replaces the stored
// token, and an empty string clears it (which the validator then
// rejects, because a telegram channel without a token cannot deliver).
func TestUpdateChannel_TelegramTokenReplaced(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	token := telegramHandlerToken
	chatID := "-1001234567890"
	channelID := createTelegramChannel(t, ds, "tg-replace", &token, &chatID)

	replacement := "987654321:BBBreplacement-token"
	rec := putChannel(t, handler, userID, channelID,
		fmt.Sprintf(`{"telegram_bot_token":%q}`, replacement))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), replacement) {
		t.Errorf("PUT response leaked the replacement token; body=%s", rec.Body.String())
	}

	reloaded, err := ds.GetNotificationChannel(context.Background(), channelID)
	if err != nil {
		t.Fatalf("GetNotificationChannel: %v", err)
	}
	assertSecretPointer(t, "TelegramBotToken", reloaded.TelegramBotToken, replacement)
}

// TestUpdateChannel_TelegramValidationErrors covers the post-merge 400s:
// clearing either required field, and supplying a malformed one.
func TestUpdateChannel_TelegramValidationErrors(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	token := telegramHandlerToken
	chatID := "-1001234567890"
	channelID := createTelegramChannel(t, ds, "tg-validate", &token, &chatID)

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "clear bot token",
			body: `{"telegram_bot_token":""}`,
			want: "telegram_bot_token is required for telegram channels",
		},
		{
			name: "clear chat id",
			body: `{"telegram_chat_id":""}`,
			want: "telegram_chat_id is required for telegram channels",
		},
		{
			name: "malformed replacement token",
			body: `{"telegram_bot_token":"not-a-token"}`,
			want: "telegram_bot_token must have the form <bot id>:<token>",
		},
		{
			name: "malformed replacement chat id",
			body: `{"telegram_chat_id":"My Alerts Channel"}`,
			want: "telegram_chat_id must be a numeric chat ID or an @channelusername",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := putChannel(t, handler, userID, channelID, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			var resp ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Error != tc.want {
				t.Errorf("Error = %q, want %q", resp.Error, tc.want)
			}
		})
	}
}

// TestUpdateChannel_TelegramUndecryptableStoredToken is the regression
// test for the lock-out described on validateTelegramFields. When the
// server secret has been rotated, decryptNotificationSecret returns the
// stored ciphertext rather than a token, and shape-checking that value
// would reject every PUT on the channel - including the UI's
// enable/disable toggle, whose whole body is {"enabled": false}. An
// operator who has lost the secret has to be able to switch the broken
// channel off.
func TestUpdateChannel_TelegramUndecryptableStoredToken(t *testing.T) {
	ds, pool, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()

	token := telegramHandlerToken
	chatID := "-1001234567890"
	channelID := createTelegramChannel(t, ds, "tg-rotated-secret", &token, &chatID)

	// Rotate the server secret out from under the stored row. Every
	// later read of this channel now yields undecryptable ciphertext.
	rotated := database.NewTestDatastoreWithSecret(pool, channelTestServerSecret+"-rotated")
	handler, userID, cleanupAuth := setupChannelHandler(t, rotated)
	defer cleanupAuth()

	stored, err := rotated.GetNotificationChannel(context.Background(), channelID)
	if err != nil {
		t.Fatalf("GetNotificationChannel: %v", err)
	}
	if stored.TelegramBotToken == nil {
		t.Fatal("stored bot token is nil; the fixture no longer exercises the lock-out")
	}
	if telegramBotTokenPattern.MatchString(*stored.TelegramBotToken) {
		t.Fatal("stored value still looks like a token; the fixture no longer " +
			"exercises the lock-out")
	}

	// The body the UI's disable toggle sends, and nothing else.
	rec := putChannel(t, handler, userID, channelID, `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	got := decodeRaw(t, rec.Body.Bytes())
	if v, ok := got["enabled"].(bool); !ok || v {
		t.Errorf("enabled = %v, want false", got["enabled"])
	}

	// A rename must work too: nothing about the request touches the
	// token, so nothing about the token may block it.
	rec = putChannel(t, handler, userID, channelID, `{"name":"tg-rotated-renamed"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("rename status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// Supplying a bad token is still rejected: only the stored value is
	// exempt from the shape check.
	rec = putChannel(t, handler, userID, channelID, `{"telegram_bot_token":"not-a-token"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad replacement status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}

	// And a good one repairs the channel.
	rec = putChannel(t, handler, userID, channelID,
		`{"telegram_bot_token":"987654321:BBBrepaired-token"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("repair status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	repaired, err := rotated.GetNotificationChannel(context.Background(), channelID)
	if err != nil {
		t.Fatalf("GetNotificationChannel after repair: %v", err)
	}
	assertSecretPointer(t, "TelegramBotToken", repaired.TelegramBotToken,
		"987654321:BBBrepaired-token")
}

// TestUpdateChannel_TelegramValidatesOnTypeSwitch confirms the telegram
// requirements are enforced when an existing channel of another type is
// converted to telegram, not only when it was created as one.
func TestUpdateChannel_TelegramValidatesOnTypeSwitch(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	emailID := createTestChannel(t, ds, "tg-switch", nil, nil, nil, nil)

	rec := putChannel(t, handler, userID, emailID, `{"channel_type":"telegram"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != "telegram_bot_token is required for telegram channels" {
		t.Errorf("Error = %q, want the bot token requirement", resp.Error)
	}
}

// createChannelWithTelegramFields inserts a channel of an arbitrary type
// that nonetheless carries telegram columns, bypassing the handler. It
// stands in for a row written before the create path started dropping
// telegram fields on non-telegram channels, which is the state the
// type-switch shape check has to cope with.
func createChannelWithTelegramFields(t *testing.T, ds *database.Datastore, name string,
	channelType database.NotificationChannelType, botToken, chatID *string) int64 {
	t.Helper()
	owner := "channel_admin"
	channel := &database.NotificationChannel{
		OwnerUsername:         &owner,
		Enabled:               true,
		ChannelType:           channelType,
		Name:                  name,
		HTTPMethod:            "POST",
		SMTPPort:              587,
		EndpointURL:           ptr("https://hooks.example.com/endpoint"),
		TelegramBotToken:      botToken,
		TelegramChatID:        chatID,
		ReminderIntervalHours: 4,
	}
	if err := ds.CreateNotificationChannel(context.Background(), channel); err != nil {
		t.Fatalf("CreateNotificationChannel: %v", err)
	}
	return channel.ID
}

// TestCreateChannel_TelegramFieldsIgnoredForOtherTypes pins the gate on
// the create path. Telegram validation runs only in the telegram branch,
// so a POST for any other type must not persist telegram fields at all;
// otherwise the row would carry a bot token that never passed the shape
// check, and a later type switch could adopt it.
func TestCreateChannel_TelegramFieldsIgnoredForOtherTypes(t *testing.T) {
	ds, pool, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	cases := []struct {
		name string
		body string
	}{
		{
			name: "webhook",
			body: `{
				"channel_type": "webhook",
				"name": "tg-smuggle-webhook",
				"endpoint_url": "https://hooks.example.com/endpoint",
				"telegram_bot_token": "not a token/with?delimiters#inside",
				"telegram_chat_id": "not a chat id"
			}`,
		},
		{
			name: "slack",
			body: `{
				"channel_type": "slack",
				"name": "tg-smuggle-slack",
				"webhook_url": "https://hooks.example.com/slack",
				"telegram_bot_token": "123456789:AAsmuggled-but-well-shaped",
				"telegram_chat_id": "-100999"
			}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := postChannel(t, handler, userID, tc.body)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
			}
			got := decodeRaw(t, rec.Body.Bytes())
			if v, _ := got["telegram_bot_token_set"].(bool); v {
				t.Error("telegram_bot_token_set = true, want false for a non-telegram channel")
			}
			if v, ok := got["telegram_chat_id"]; ok && v != nil {
				t.Errorf("telegram_chat_id = %v, want absent", v)
			}

			id := int64(got["id"].(float64))
			if stored, ok := readEncryptedColumn(t, pool, id,
				"telegram_bot_token_encrypted"); ok {
				t.Errorf("telegram_bot_token_encrypted = %q, want NULL", stored)
			}
			if stored, ok := readEncryptedColumn(t, pool, id, "telegram_chat_id"); ok {
				t.Errorf("telegram_chat_id column = %q, want NULL", stored)
			}

			reloaded, err := ds.GetNotificationChannel(context.Background(), id)
			if err != nil {
				t.Fatalf("GetNotificationChannel: %v", err)
			}
			if reloaded.TelegramBotToken != nil {
				t.Errorf("TelegramBotToken = %q, want nil", *reloaded.TelegramBotToken)
			}
			if reloaded.TelegramChatID != nil {
				t.Errorf("TelegramChatID = %q, want nil", *reloaded.TelegramChatID)
			}
		})
	}
}

// TestUpdateChannel_TelegramTypeSwitchChecksStoredToken covers the other
// half of the same invariant. Converting a channel to telegram is a
// deliberate act, so the token it will run with is shape-checked even
// when the request does not supply one - which is the only way a legacy
// row carrying an unchecked token can be caught.
func TestUpdateChannel_TelegramTypeSwitchChecksStoredToken(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	chatID := "-1001234567890"
	malformed := "not a token/with?delimiters#inside"
	badID := createChannelWithTelegramFields(t, ds, "tg-switch-bad",
		database.ChannelTypeWebhook, &malformed, &chatID)

	rec := putChannel(t, handler, userID, badID, `{"channel_type":"telegram"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != "telegram_bot_token must have the form <bot id>:<token>" {
		t.Errorf("Error = %q, want the shape requirement", resp.Error)
	}

	// The rejected switch must not have been written.
	unchanged, err := ds.GetNotificationChannel(context.Background(), badID)
	if err != nil {
		t.Fatalf("GetNotificationChannel: %v", err)
	}
	if unchanged.ChannelType != database.ChannelTypeWebhook {
		t.Errorf("channel_type = %q, want webhook", unchanged.ChannelType)
	}

	// Supplying a well-shaped token with the switch repairs it.
	rec = putChannel(t, handler, userID, badID,
		`{"channel_type":"telegram","telegram_bot_token":"987654321:BBBswitched-token"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("repair status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	repaired, err := ds.GetNotificationChannel(context.Background(), badID)
	if err != nil {
		t.Fatalf("GetNotificationChannel after repair: %v", err)
	}
	if repaired.ChannelType != database.ChannelTypeTelegram {
		t.Errorf("channel_type = %q, want telegram", repaired.ChannelType)
	}
	assertSecretPointer(t, "TelegramBotToken", repaired.TelegramBotToken,
		"987654321:BBBswitched-token")
}

// TestUpdateChannel_TelegramTypeSwitchAcceptsValidStoredToken is the
// positive case: a stored token that does satisfy the shape check lets
// the switch through without the request having to resend the
// credential, which the GET response never reveals anyway.
func TestUpdateChannel_TelegramTypeSwitchAcceptsValidStoredToken(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	token := telegramHandlerToken
	chatID := "-1001234567890"
	id := createChannelWithTelegramFields(t, ds, "tg-switch-good",
		database.ChannelTypeWebhook, &token, &chatID)

	rec := putChannel(t, handler, userID, id, `{"channel_type":"telegram"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), telegramHandlerToken) {
		t.Errorf("PUT response leaked the bot token; body=%s", rec.Body.String())
	}
	got := decodeRaw(t, rec.Body.Bytes())
	if v, _ := got["channel_type"].(string); v != "telegram" {
		t.Errorf("channel_type = %v, want telegram", v)
	}
	if v, _ := got["telegram_bot_token_set"].(bool); !v {
		t.Error("telegram_bot_token_set = false, want true")
	}

	switched, err := ds.GetNotificationChannel(context.Background(), id)
	if err != nil {
		t.Fatalf("GetNotificationChannel: %v", err)
	}
	assertSecretPointer(t, "TelegramBotToken", switched.TelegramBotToken, telegramHandlerToken)

	// A later PUT on the now-telegram channel that omits the token must
	// still pass: the storage exemption applies once the channel IS
	// telegram, which is what keeps an undecryptable token from locking
	// the channel down.
	rec = putChannel(t, handler, userID, id, `{"enabled":false}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// TestGetChannel_TelegramRedaction exercises the read path: the token is
// gone from the body, the indicator is present, and the chat ID is
// readable.
func TestGetChannel_TelegramRedaction(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	token := telegramHandlerToken
	chatID := "@workbench_alerts"
	channelID := createTelegramChannel(t, ds, "tg-get", &token, &chatID)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/notification-channels/"+strconv.FormatInt(channelID, 10), nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()
	handler.handleChannelSubpath(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, telegramHandlerToken) {
		t.Errorf("GET response leaked the bot token; body=%s", body)
	}
	if strings.Contains(body, `"telegram_bot_token"`) {
		t.Errorf("GET response carries a telegram_bot_token key; body=%s", body)
	}
	got := decodeRaw(t, rec.Body.Bytes())
	if v, _ := got["telegram_bot_token_set"].(bool); !v {
		t.Errorf("telegram_bot_token_set = %v, want true", got["telegram_bot_token_set"])
	}
	if v, _ := got["telegram_chat_id"].(string); v != chatID {
		t.Errorf("telegram_chat_id = %v, want %q", got["telegram_chat_id"], chatID)
	}
}

// TestGetChannel_TelegramFlagFalseWhenUnset is the inverse: a channel
// with no bot token reports the indicator as false and omits the chat
// ID entirely.
func TestGetChannel_TelegramFlagFalseWhenUnset(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	channelID := createTestChannel(t, ds, "tg-unset", nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/notification-channels/"+strconv.FormatInt(channelID, 10), nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()
	handler.handleChannelSubpath(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	got := decodeRaw(t, rec.Body.Bytes())
	v, ok := got["telegram_bot_token_set"]
	if !ok {
		t.Fatalf("telegram_bot_token_set missing; body=%s", rec.Body.String())
	}
	if b, _ := v.(bool); b {
		t.Errorf("telegram_bot_token_set = %v, want false", v)
	}
	if _, present := got["telegram_chat_id"]; present {
		t.Errorf("telegram_chat_id should be omitted when unset; body=%s", rec.Body.String())
	}
}

// TestTestChannel_TelegramSuccess drives POST
// /notification-channels/{id}/test end to end against an httptest stand-in
// for the Bot API.
func TestTestChannel_TelegramSuccess(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":1}}`)
	}))
	defer server.Close()
	withTelegramBaseURL(t, server.URL)

	token := telegramHandlerToken
	chatID := "-1001234567890"
	channelID := createTelegramChannel(t, ds, "tg-test-ok", &token, &chatID)

	rec := postChannelTest(t, handler, userID, channelID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if want := "/bot" + telegramHandlerToken + "/sendMessage"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

// TestTestChannel_TelegramAPIFailure covers the ok:false path: the
// handler must answer 502 and must not echo the API detail (or the
// token) to the client.
func TestTestChannel_TelegramAPIFailure(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`)
	}))
	defer server.Close()
	withTelegramBaseURL(t, server.URL)

	token := telegramHandlerToken
	chatID := "-1001234567890"
	channelID := createTelegramChannel(t, ds, "tg-test-fail", &token, &chatID)

	rec := postChannelTest(t, handler, userID, channelID)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), telegramHandlerToken) {
		t.Errorf("test response leaked the bot token; body=%s", rec.Body.String())
	}
}

// TestTestChannel_TelegramMissingConfiguration covers the two 400
// branches that guard the send.
func TestTestChannel_TelegramMissingConfiguration(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	token := telegramHandlerToken
	chatID := "-1001234567890"

	cases := []struct {
		name     string
		botToken *string
		chatID   *string
		want     string
	}{
		{
			name:   "no bot token",
			chatID: &chatID,
			want:   "Telegram bot token is not configured for this channel",
		},
		{
			name:     "no chat id",
			botToken: &token,
			want:     "Telegram chat ID is not configured for this channel",
		},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			channelID := createTelegramChannel(t, ds,
				fmt.Sprintf("tg-test-missing-%d", i), tc.botToken, tc.chatID)
			rec := postChannelTest(t, handler, userID, channelID)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			var resp ErrorResponse
			if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Error != tc.want {
				t.Errorf("Error = %q, want %q", resp.Error, tc.want)
			}
		})
	}
}

// postChannelTest issues POST /api/v1/notification-channels/{id}/test.
func postChannelTest(t *testing.T, h *NotificationChannelHandler,
	userID int64, channelID int64) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/notification-channels/"+strconv.FormatInt(channelID, 10)+"/test", nil)
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	req = withUsername(req, "channel_admin")
	rec := httptest.NewRecorder()
	h.handleChannelSubpath(rec, req)
	return rec
}

// TestValidateTelegramFields exercises the validator directly so the
// permissive-shape decisions are pinned by a test rather than only by a
// comment.
func TestValidateTelegramFields(t *testing.T) {
	str := func(s string) *string { return &s }

	cases := []struct {
		name     string
		botToken *string
		chatID   *string
		// fromStorage marks the cases where the token came from the
		// database rather than from the request, which is the only
		// difference the shape check makes.
		fromStorage bool
		want        string
	}{
		{name: "both valid numeric", botToken: str("1:AA"), chatID: str("-1001234567890")},
		{name: "both valid username", botToken: str("123456789:AA-_bb"), chatID: str("@alerts_bot")},
		{name: "positive chat id", botToken: str("1:AA"), chatID: str("42")},
		{name: "nil token", chatID: str("42"),
			want: "telegram_bot_token is required for telegram channels"},
		{name: "empty token", botToken: str(""), chatID: str("42"),
			want: "telegram_bot_token is required for telegram channels"},
		{name: "nil chat id", botToken: str("1:AA"),
			want: "telegram_chat_id is required for telegram channels"},
		{name: "token with space", botToken: str("1:A A"), chatID: str("42"),
			want: "telegram_bot_token must have the form <bot id>:<token>"},
		{name: "token with query char", botToken: str("1:A?b"), chatID: str("42"),
			want: "telegram_bot_token must have the form <bot id>:<token>"},
		{name: "token with fragment char", botToken: str("1:A#b"), chatID: str("42"),
			want: "telegram_bot_token must have the form <bot id>:<token>"},
		{name: "token with path separator", botToken: str("1:A/b"), chatID: str("42"),
			want: "telegram_bot_token must have the form <bot id>:<token>"},
		{name: "non-numeric bot id", botToken: str("abc:AA"), chatID: str("42"),
			want: "telegram_bot_token must have the form <bot id>:<token>"},
		{name: "chat id with spaces", botToken: str("1:AA"), chatID: str("my chat"),
			want: "telegram_chat_id must be a numeric chat ID or an @channelusername"},
		{name: "chat id with trailing junk", botToken: str("1:AA"), chatID: str("42abc"),
			want: "telegram_chat_id must be a numeric chat ID or an @channelusername"},
		{name: "username with a dash", botToken: str("1:AA"), chatID: str("@alerts-bot"),
			want: "telegram_chat_id must be a numeric chat ID or an @channelusername"},
		{name: "stored ciphertext is accepted",
			botToken: str(telegramUndecryptableToken), chatID: str("42"), fromStorage: true},
		{name: "stored token is still required",
			botToken: str(""), chatID: str("42"), fromStorage: true,
			want: "telegram_bot_token is required for telegram channels"},
		{name: "supplied ciphertext is rejected",
			botToken: str(telegramUndecryptableToken), chatID: str("42"),
			want: "telegram_bot_token must have the form <bot id>:<token>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := validateTelegramFields(tc.botToken, tc.chatID, !tc.fromStorage)
			if got != tc.want {
				t.Errorf("validateTelegramFields() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestTelegramBotTokenPatternExcludesRedactionTerminators pins the
// invariant the redactor depends on: every character that ends a token
// run in isTelegramTokenTerminator, and every character an error or a
// log line wraps a URL in, must be rejected by the validator. If a
// token could legally contain one of these, redaction would stop
// part-way through the credential and print the rest of it.
func TestTelegramBotTokenPatternExcludesRedactionTerminators(t *testing.T) {
	excluded := []struct {
		name string
		c    byte
	}{
		{"space", ' '},
		{"tab", '\t'},
		{"newline", '\n'},
		{"carriage return", '\r'},
		{"nul", 0x00},
		{"escape", 0x1b},
		{"delete", 0x7f},
		{"slash", '/'},
		{"question mark", '?'},
		{"hash", '#'},
		{"double quote", '"'},
		{"apostrophe", '\''},
		{"backquote", '`'},
		{"less than", '<'},
		{"greater than", '>'},
		{"closing paren", ')'},
		{"closing bracket", ']'},
		{"comma", ','},
		{"semicolon", ';'},
	}
	for _, e := range excluded {
		t.Run(e.name, func(t *testing.T) {
			token := "123456789:AAFake" + string(e.c) + "TokenBody"
			if telegramBotTokenPattern.MatchString(token) {
				t.Errorf("telegramBotTokenPattern accepted a token containing %q; "+
					"the redactor would leak everything after it", string(e.c))
			}
		})
	}

	// The alphabet real BotFather tokens use must still pass.
	for _, ok := range []string{
		"123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw",
		"1:A",
		"999999999999:aZ0-_",
	} {
		if !telegramBotTokenPattern.MatchString(ok) {
			t.Errorf("telegramBotTokenPattern rejected the legitimate token %q", ok)
		}
	}
}

// =============================================================================
// POST /notification-channels/{id}/test for the non-telegram channel types
//
// Adding the telegram case to testChannel made the whole switch a
// modified unit, so the remaining arms are covered here rather than left
// at zero.
// =============================================================================

// setupChannelHandlerAllowingInternal is setupChannelHandler with the
// host validator configured to permit loopback addresses, which is what
// an httptest server binds to. Without it every send below would stop at
// the SSRF guard instead of reaching the sender.
func setupChannelHandlerAllowingInternal(t *testing.T, ds *database.Datastore) (*NotificationChannelHandler, int64, func()) {
	t.Helper()
	authStore, cleanup := newAuthStoreForChannelTests(t)
	userID := setupUserWithPermission(t, authStore, "channel_admin",
		auth.PermManageNotificationChannels)
	checker := auth.NewRBACChecker(authStore)
	handler := NewNotificationChannelHandlerWithSecurity(ds, authStore, checker, true, nil, nil)
	return handler, userID, cleanup
}

// createTypedTestChannel inserts a channel of an arbitrary type with the
// URL-bearing fields the test needs, bypassing the request validation so
// the handler-side guards can be exercised on stored state.
func createTypedTestChannel(t *testing.T, ds *database.Datastore, name string,
	channelType database.NotificationChannelType, webhookURL, endpointURL *string) int64 {
	t.Helper()
	owner := "channel_admin"
	channel := &database.NotificationChannel{
		OwnerUsername:         &owner,
		Enabled:               true,
		ChannelType:           channelType,
		Name:                  name,
		HTTPMethod:            "POST",
		SMTPPort:              587,
		WebhookURL:            webhookURL,
		EndpointURL:           endpointURL,
		ReminderIntervalHours: 4,
	}
	if err := ds.CreateNotificationChannel(context.Background(), channel); err != nil {
		t.Fatalf("CreateNotificationChannel: %v", err)
	}
	return channel.ID
}

// assertTestChannelError runs the test endpoint and checks the status and
// message.
func assertTestChannelError(t *testing.T, h *NotificationChannelHandler,
	userID, channelID int64, wantStatus int, wantMsg string) {
	t.Helper()
	rec := postChannelTest(t, h, userID, channelID)
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, wantStatus, rec.Body.String())
	}
	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != wantMsg {
		t.Errorf("Error = %q, want %q", resp.Error, wantMsg)
	}
}

// TestTestChannel_PermissionAndNotFound covers the two guards ahead of
// the switch.
func TestTestChannel_PermissionAndNotFound(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	// No user on the context: the permission gate answers 403 before
	// anything else runs.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/notification-channels/1/test", nil)
	rec := httptest.NewRecorder()
	handler.handleChannelSubpath(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("without permission: status = %d, want 403", rec.Code)
	}

	assertTestChannelError(t, handler, userID, 999999,
		http.StatusNotFound, "Notification channel not found")
}

// TestTestChannel_EmailValidationBranches covers every 400 the email arm
// can produce.
func TestTestChannel_EmailValidationBranches(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	// No SMTP host.
	owner := "channel_admin"
	bare := &database.NotificationChannel{
		OwnerUsername: &owner, Enabled: true, ChannelType: database.ChannelTypeEmail,
		Name: "email-no-host", HTTPMethod: "POST", SMTPPort: 587,
	}
	if err := ds.CreateNotificationChannel(context.Background(), bare); err != nil {
		t.Fatalf("create: %v", err)
	}
	assertTestChannelError(t, handler, userID, bare.ID,
		http.StatusBadRequest, "SMTP host is not configured for this channel")

	// SMTP host but no from address.
	noFrom := &database.NotificationChannel{
		OwnerUsername: &owner, Enabled: true, ChannelType: database.ChannelTypeEmail,
		Name: "email-no-from", HTTPMethod: "POST", SMTPPort: 587,
		SMTPHost: ptr("smtp.example.com"),
	}
	if err := ds.CreateNotificationChannel(context.Background(), noFrom); err != nil {
		t.Fatalf("create: %v", err)
	}
	assertTestChannelError(t, handler, userID, noFrom.ID,
		http.StatusBadRequest, "From address is not configured for this channel")

	// Fully configured but with no recipients at all.
	full := createTestChannel(t, ds, "email-no-recipients", nil, nil, nil, nil)
	assertTestChannelError(t, handler, userID, full,
		http.StatusBadRequest,
		"No recipients available. Provide a recipient_email or add enabled recipients to the channel.")

	// A malformed request body is rejected before anything is sent.
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/notification-channels/"+strconv.FormatInt(full, 10)+"/test",
		bytes.NewReader([]byte(`{not json`)))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	req = withUsername(req, "channel_admin")
	rec := httptest.NewRecorder()
	handler.handleChannelSubpath(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// TestTestChannel_EmailRecipientSources covers the two ways the arm
// builds its recipient list, and the SSRF guard on the SMTP host.
func TestTestChannel_EmailRecipientSources(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	// createTestChannel stores smtp.example.com, which is a public name
	// the validator lets through only if it resolves; use a loopback
	// literal so the reject branch is deterministic.
	owner := "channel_admin"
	ch := &database.NotificationChannel{
		OwnerUsername: &owner, Enabled: true, ChannelType: database.ChannelTypeEmail,
		Name: "email-loopback", HTTPMethod: "POST", SMTPPort: 587,
		SMTPHost: ptr("127.0.0.1"), FromAddress: ptr("alerts@example.com"),
	}
	if err := ds.CreateNotificationChannel(context.Background(), ch); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Recipient supplied in the body: the handler must not need a
	// stored recipient, and the loopback SMTP host must be refused.
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/notification-channels/"+strconv.FormatInt(ch.ID, 10)+"/test",
		bytes.NewReader([]byte(`{"recipient_email":"probe@example.com"}`)))
	req.Header.Set("Content-Type", "application/json")
	req = withUser(req, userID)
	req = withUsername(req, "channel_admin")
	rec := httptest.NewRecorder()
	handler.handleChannelSubpath(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != "Invalid SMTP host" {
		t.Errorf("Error = %q, want %q", resp.Error, "Invalid SMTP host")
	}

	// Stored, enabled recipients are used when the body names none.
	// A disabled recipient must be ignored, so add one of each.
	for _, r := range []*database.EmailRecipient{
		{ChannelID: ch.ID, EmailAddress: "off@example.com", Enabled: false},
		{ChannelID: ch.ID, EmailAddress: "on@example.com", Enabled: true},
	} {
		if err := ds.CreateEmailRecipient(context.Background(), r); err != nil {
			t.Fatalf("CreateEmailRecipient: %v", err)
		}
	}
	assertTestChannelError(t, handler, userID, ch.ID,
		http.StatusBadRequest, "Invalid SMTP host")
}

// TestTestChannel_EmailSendFailure lets the SMTP host through the
// validator and then fails to connect, covering the 502 arm.
func TestTestChannel_EmailSendFailure(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	handler, userID, cleanupAuth := setupChannelHandlerAllowingInternal(t, ds)
	defer cleanupAuth()

	// Bind and release a port so the connection is refused immediately
	// rather than hanging on a firewall drop.
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL, err := url.Parse(closed.URL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	closed.Close()
	port, err := strconv.Atoi(closedURL.Port())
	if err != nil {
		t.Fatalf("port: %v", err)
	}

	owner := "channel_admin"
	ch := &database.NotificationChannel{
		OwnerUsername: &owner, Enabled: true, ChannelType: database.ChannelTypeEmail,
		Name: "email-send-fail", HTTPMethod: "POST", SMTPPort: port,
		SMTPHost: ptr(closedURL.Hostname()), FromAddress: ptr("alerts@example.com"),
		SMTPUsername: ptr("u"), SMTPPassword: ptr("p"), FromName: ptr("Alerts"),
	}
	if err := ds.CreateNotificationChannel(context.Background(), ch); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := ds.CreateEmailRecipient(context.Background(),
		&database.EmailRecipient{ChannelID: ch.ID, EmailAddress: "on@example.com", Enabled: true}); err != nil {
		t.Fatalf("CreateEmailRecipient: %v", err)
	}

	assertTestChannelError(t, handler, userID, ch.ID,
		http.StatusBadGateway, "Failed to send test email")
}

// TestTestChannel_SlackAndMattermost covers the shared webhook arm for
// both display types, plus its guards.
func TestTestChannel_SlackAndMattermost(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	strict, strictUser, cleanupStrict := setupChannelHandler(t, ds)
	defer cleanupStrict()
	permissive, permissiveUser, cleanupPermissive := setupChannelHandlerAllowingInternal(t, ds)
	defer cleanupPermissive()

	// No webhook URL configured.
	noURL := createTypedTestChannel(t, ds, "slack-no-url", database.ChannelTypeSlack, nil, nil)
	assertTestChannelError(t, strict, strictUser, noURL,
		http.StatusBadRequest, "Webhook URL is not configured for this channel")

	// A stored URL that url.Parse rejects outright.
	badURL := createTypedTestChannel(t, ds, "slack-bad-url", database.ChannelTypeSlack,
		ptr("http://hooks.example.com:not-a-port/x"), nil)
	assertTestChannelError(t, strict, strictUser, badURL,
		http.StatusBadRequest, "Invalid webhook URL")

	// A loopback URL is refused by the SSRF guard.
	loopback := createTypedTestChannel(t, ds, "slack-loopback", database.ChannelTypeSlack,
		ptr("http://127.0.0.1:9/hook"), nil)
	assertTestChannelError(t, strict, strictUser, loopback,
		http.StatusBadRequest, "Invalid webhook host")

	// Happy path, and the Mattermost display-type branch.
	var gotBodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBodies = append(gotBodies, string(body))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	for _, tc := range []struct {
		name        string
		channelType database.NotificationChannelType
		wantWord    string
	}{
		{"slack-ok", database.ChannelTypeSlack, "Slack"},
		{"mattermost-ok", database.ChannelTypeMattermost, "Mattermost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := createTypedTestChannel(t, ds, tc.name, tc.channelType, ptr(server.URL), nil)
			rec := postChannelTest(t, permissive, permissiveUser, id)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			last := gotBodies[len(gotBodies)-1]
			if !strings.Contains(last, tc.wantWord) {
				t.Errorf("delivered body = %q, want it to name %q", last, tc.wantWord)
			}
		})
	}

	// A send that fails answers 502.
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	failID := createTypedTestChannel(t, ds, "slack-fail", database.ChannelTypeSlack,
		ptr(failing.URL), nil)
	assertTestChannelError(t, permissive, permissiveUser, failID,
		http.StatusBadGateway, "Failed to send test webhook")
}

// TestTestChannel_GenericWebhook covers the generic webhook arm and its
// guards. The success case also exercises derefStr, which the handler
// uses only here.
func TestTestChannel_GenericWebhook(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()
	strict, strictUser, cleanupStrict := setupChannelHandler(t, ds)
	defer cleanupStrict()
	permissive, permissiveUser, cleanupPermissive := setupChannelHandlerAllowingInternal(t, ds)
	defer cleanupPermissive()

	noURL := createTypedTestChannel(t, ds, "wh-no-url", database.ChannelTypeWebhook, nil, nil)
	assertTestChannelError(t, strict, strictUser, noURL,
		http.StatusBadRequest, "Endpoint URL is not configured for this channel")

	badURL := createTypedTestChannel(t, ds, "wh-bad-url", database.ChannelTypeWebhook,
		nil, ptr("http://endpoint.example.com:not-a-port/x"))
	assertTestChannelError(t, strict, strictUser, badURL,
		http.StatusBadRequest, "Invalid endpoint URL")

	loopback := createTypedTestChannel(t, ds, "wh-loopback", database.ChannelTypeWebhook,
		nil, ptr("http://127.0.0.1:9/hook"))
	assertTestChannelError(t, strict, strictUser, loopback,
		http.StatusBadRequest, "Invalid endpoint host")

	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	owner := "channel_admin"
	ok := &database.NotificationChannel{
		OwnerUsername: &owner, Enabled: true, ChannelType: database.ChannelTypeWebhook,
		Name: "wh-ok", HTTPMethod: "POST", SMTPPort: 587,
		EndpointURL: ptr(server.URL), AuthType: ptr("bearer"),
		AuthCredentials: ptr("hunter2"), Headers: map[string]string{"X-Probe": "1"},
	}
	if err := ds.CreateNotificationChannel(context.Background(), ok); err != nil {
		t.Fatalf("create: %v", err)
	}
	rec := postChannelTest(t, permissive, permissiveUser, ok.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if gotAuth != "Bearer hunter2" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer hunter2")
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	failID := createTypedTestChannel(t, ds, "wh-fail", database.ChannelTypeWebhook,
		nil, ptr(failing.URL))
	assertTestChannelError(t, permissive, permissiveUser, failID,
		http.StatusBadGateway, "Failed to send test webhook")
}
