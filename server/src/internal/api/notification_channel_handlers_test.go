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
	"net/http"
	"net/http/httptest"
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
	handler := NewNotificationChannelHandler(nil, nil, nil)
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
	handler := NewNotificationChannelHandler(nil, authStore, auth.NewRBACChecker(authStore))

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
	handler := NewNotificationChannelHandler(nil, authStore, auth.NewRBACChecker(authStore))

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
	handler := NewNotificationChannelHandler(nil, authStore, auth.NewRBACChecker(authStore))

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
	handler := NewNotificationChannelHandler(nil, authStore, auth.NewRBACChecker(authStore))

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
    channel_type TEXT NOT NULL CHECK (channel_type IN ('slack', 'mattermost', 'webhook', 'email')),
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
	handler := NewNotificationChannelHandler(ds, authStore, checker)
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
	for _, key := range []string{
		"webhook_url_set", "auth_credentials_set",
		"smtp_username_set", "smtp_password_set",
	} {
		if v, _ := got[key].(bool); v {
			t.Errorf("%s = true, want false", key)
		}
	}
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

// TestListChannels_RedactsSecrets verifies the list endpoint also
// redacts secrets and exposes the `*_set` flags for every channel.
func TestListChannels_RedactsSecrets(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()

	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	wh := "https://hooks.example.com/list-leak"
	pw := "list-leak-password"
	createTestChannel(t, ds, "list-with-secrets", &wh, nil, nil, &pw)
	createTestChannel(t, ds, "list-without-secrets", nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/notification-channels", nil)
	req = withUser(req, userID)
	rec := httptest.NewRecorder()
	handler.handleChannels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	for _, leaked := range []string{wh, pw} {
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
		for _, key := range []string{
			"webhook_url_set", "auth_credentials_set",
			"smtp_username_set", "smtp_password_set",
		} {
			if _, ok := item[key]; !ok {
				t.Errorf("list item missing %s", key)
			}
		}
		// Redacted keys must not appear at all.
		for _, redactedKey := range []string{
			"webhook_url", "auth_credentials", "smtp_username", "smtp_password",
		} {
			if _, ok := item[redactedKey]; ok {
				t.Errorf("list item leaked redacted key %q", redactedKey)
			}
		}
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
func TestUpdateChannel_OmittedSecretsArePreserved(t *testing.T) {
	ds, _, cleanupDS := newChannelTestDatastore(t)
	defer cleanupDS()

	handler, userID, cleanupAuth := setupChannelHandler(t, ds)
	defer cleanupAuth()

	origWebhook := "https://hooks.example.com/orig"
	origAuth := "Bearer original"
	origUser := "original-user"
	origPass := "original-password"
	channelID := createTestChannel(t, ds, "preserve-test",
		&origWebhook, &origAuth, &origUser, &origPass)

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

	if loaded.WebhookURL == nil || *loaded.WebhookURL != origWebhook {
		t.Errorf("WebhookURL = %v, want %q", loaded.WebhookURL, origWebhook)
	}
	if loaded.AuthCredentials == nil || *loaded.AuthCredentials != origAuth {
		t.Errorf("AuthCredentials = %v, want %q", loaded.AuthCredentials, origAuth)
	}
	if loaded.SMTPUsername == nil || *loaded.SMTPUsername != origUser {
		t.Errorf("SMTPUsername = %v, want %q", loaded.SMTPUsername, origUser)
	}
	if loaded.SMTPPassword == nil || *loaded.SMTPPassword != origPass {
		t.Errorf("SMTPPassword = %v, want %q", loaded.SMTPPassword, origPass)
	}

	// Sanity: name was actually updated and the *_set flags remain on.
	if loaded.Name != "renamed" {
		t.Errorf("Name = %q, want %q", loaded.Name, "renamed")
	}
	if !loaded.WebhookURLSet || !loaded.AuthCredentialsSet ||
		!loaded.SMTPUsernameSet || !loaded.SMTPPasswordSet {
		t.Errorf("expected all *_set flags true after preservation, got %+v", loaded)
	}
}

// TestUpdateChannel_NonEmptySecretReplaces verifies that supplying a
// new value for each secret field overwrites the stored value.
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

	body := `{
		"webhook_url": "https://hooks.example.com/new",
		"auth_credentials": "Bearer new",
		"smtp_username": "new-user",
		"smtp_password": "new-password"
	}`
	rec := putChannel(t, handler, userID, channelID, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// smtp_username is stored plaintext, so we can compare directly.
	if v, _ := readEncryptedColumn(t, pool, channelID, "smtp_username"); v != "new-user" {
		t.Errorf("smtp_username = %q, want %q", v, "new-user")
	}

	// For encrypted columns, decrypt via GetNotificationChannel.
	loaded, err := ds.GetNotificationChannel(context.Background(), channelID)
	if err != nil {
		t.Fatalf("GetNotificationChannel: %v", err)
	}
	if loaded.WebhookURL == nil || *loaded.WebhookURL != "https://hooks.example.com/new" {
		t.Errorf("WebhookURL = %v, want pointer to %q",
			loaded.WebhookURL, "https://hooks.example.com/new")
	}
	if loaded.AuthCredentials == nil || *loaded.AuthCredentials != "Bearer new" {
		t.Errorf("AuthCredentials = %v, want %q", loaded.AuthCredentials, "Bearer new")
	}
	if loaded.SMTPPassword == nil || *loaded.SMTPPassword != "new-password" {
		t.Errorf("SMTPPassword = %v, want %q", loaded.SMTPPassword, "new-password")
	}
	if !loaded.SMTPPasswordSet || !loaded.WebhookURLSet ||
		!loaded.AuthCredentialsSet || !loaded.SMTPUsernameSet {
		t.Errorf("expected all *_set flags true, got %+v", loaded)
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
	handler := NewNotificationChannelHandler(nil, authStore, checker)

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "invalid channel type",
			body: `{"channel_type":"sms","name":"bad"}`,
			want: "Invalid channel_type: must be one of email, slack, mattermost, webhook",
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
			want: "Invalid channel_type: must be one of email, slack, mattermost, webhook",
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
	store, err := auth.NewAuthStore(tmpDir, 0, 0)
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
