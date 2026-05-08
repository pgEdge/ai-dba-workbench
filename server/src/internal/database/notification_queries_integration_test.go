/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// notificationTestSchema mirrors the production schema for the
// notification family of tables: channels, recipients, and overrides.
// The cluster hierarchy parent tables are included so foreign keys
// resolve. Constraints mirror production closely enough that the
// production INSERT/UPDATE statements pass without modification.
const notificationTestSchema = `
DROP TABLE IF EXISTS notification_channel_overrides CASCADE;
DROP TABLE IF EXISTS email_recipients CASCADE;
DROP TABLE IF EXISTS notification_channels CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;

CREATE TABLE cluster_groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL,
    name VARCHAR(255) NOT NULL
);

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

CREATE TABLE notification_channel_overrides (
    id BIGSERIAL PRIMARY KEY,
    channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    scope TEXT NOT NULL CHECK (scope IN ('group', 'cluster', 'server')),
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX idx_nco_unique_server
    ON notification_channel_overrides(channel_id, connection_id)
    WHERE scope = 'server';
CREATE UNIQUE INDEX idx_nco_unique_cluster
    ON notification_channel_overrides(channel_id, cluster_id)
    WHERE scope = 'cluster';
CREATE UNIQUE INDEX idx_nco_unique_group
    ON notification_channel_overrides(channel_id, group_id)
    WHERE scope = 'group';
`

const notificationTestTeardown = `
DROP TABLE IF EXISTS notification_channel_overrides CASCADE;
DROP TABLE IF EXISTS email_recipients CASCADE;
DROP TABLE IF EXISTS notification_channels CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
`

// notifFixture stores IDs for parent rows shared across test cases.
type notifFixture struct {
	GroupID   int
	ClusterID int
	ConnID    int
}

// newNotifTestDatastore builds a *Datastore wired against the
// TEST_AI_WORKBENCH_SERVER instance with the notification schema and
// a fixed serverSecret so encrypt/decrypt paths run end-to-end.
func newNotifTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, notifFixture, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping notification integration test")
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
	if _, err := pool.Exec(ctx, notificationTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create notification test schema: %v", err)
	}

	var groupID, clusterID, connID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO cluster_groups (name) VALUES ('grp') RETURNING id`).Scan(&groupID); err != nil {
		pool.Close()
		t.Fatalf("seed group: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO clusters (group_id, name) VALUES ($1, 'cls') RETURNING id`,
		groupID).Scan(&clusterID); err != nil {
		pool.Close()
		t.Fatalf("seed cluster: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO connections (cluster_id, name) VALUES ($1, 'conn') RETURNING id`,
		clusterID).Scan(&connID); err != nil {
		pool.Close()
		t.Fatalf("seed conn: %v", err)
	}

	ds := NewTestDatastoreWithSecret(pool, "test-server-secret-32-bytes-long!")

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), notificationTestTeardown); err != nil {
			t.Logf("notification teardown failed: %v", err)
		}
		pool.Close()
	}
	return ds, pool, notifFixture{
		GroupID:   groupID,
		ClusterID: clusterID,
		ConnID:    connID,
	}, cleanup
}

func strPtr(s string) *string { return &s }

// makeSlackChannel returns a slack channel struct with the required
// fields populated. Optional secret fields are left zero so the
// caller can override them per-test.
func makeSlackChannel(name string) *NotificationChannel {
	owner := "alice"
	desc := "the description"
	hook := "https://hooks.slack.com/services/AAA/BBB/CCC"
	return &NotificationChannel{
		OwnerUsername: &owner,
		Enabled:       true,
		ChannelType:   ChannelTypeSlack,
		Name:          name,
		Description:   &desc,
		WebhookURL:    &hook,
		HTTPMethod:    "POST",
	}
}

// makeWebhookChannel returns a webhook channel with custom headers and
// auth credentials so encryption code paths run.
func makeWebhookChannel(name string) *NotificationChannel {
	owner := "bob"
	endpoint := "https://example.com/hook"
	authType := "bearer"
	authCreds := "tok-XYZ"
	return &NotificationChannel{
		OwnerUsername:   &owner,
		Enabled:         true,
		ChannelType:     ChannelTypeWebhook,
		Name:            name,
		EndpointURL:     &endpoint,
		HTTPMethod:      "POST",
		Headers:         map[string]string{"X-Trace": "1", "Authorization": "Bearer x"},
		AuthType:        &authType,
		AuthCredentials: &authCreds,
	}
}

// makeEmailChannel returns an email channel; recipients are inserted
// separately to exercise the recipient code paths.
func makeEmailChannel(name string) *NotificationChannel {
	owner := "carol"
	host := "smtp.example.com"
	user := "smtp-user"
	pass := "smtp-pass"
	from := "alerts@example.com"
	fromName := "Alerts"
	return &NotificationChannel{
		OwnerUsername: &owner,
		Enabled:       true,
		ChannelType:   ChannelTypeEmail,
		Name:          name,
		HTTPMethod:    "POST",
		SMTPHost:      &host,
		SMTPPort:      587,
		SMTPUsername:  &user,
		SMTPPassword:  &pass,
		SMTPUseTLS:    true,
		FromAddress:   &from,
		FromName:      &fromName,
	}
}

// TestNotificationChannelLifecycle exercises Create/Get/List/Update/
// Delete in one ordered scenario. The test asserts that secret
// fields round-trip through the encrypt/decrypt helpers and that
// recipients attach to email channels on read.
func TestNotificationChannelLifecycle(t *testing.T) {
	ds, _, _, cleanup := newNotifTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	slack := makeSlackChannel("slack-1")
	if err := ds.CreateNotificationChannel(ctx, slack); err != nil {
		t.Fatalf("CreateNotificationChannel slack: %v", err)
	}
	if slack.ID == 0 {
		t.Fatal("slack ID not populated")
	}

	web := makeWebhookChannel("hook-1")
	if err := ds.CreateNotificationChannel(ctx, web); err != nil {
		t.Fatalf("CreateNotificationChannel webhook: %v", err)
	}

	email := makeEmailChannel("email-1")
	if err := ds.CreateNotificationChannel(ctx, email); err != nil {
		t.Fatalf("CreateNotificationChannel email: %v", err)
	}

	// Add a recipient under the email channel; List should attach it.
	rec := &EmailRecipient{
		ChannelID:    email.ID,
		EmailAddress: "user1@example.com",
		DisplayName:  strPtr("User One"),
		Enabled:      true,
	}
	if err := ds.CreateEmailRecipient(ctx, rec); err != nil {
		t.Fatalf("CreateEmailRecipient: %v", err)
	}

	// List
	list, err := ds.ListNotificationChannels(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("List len = %d, want 3", len(list))
	}
	var foundEmail *NotificationChannel
	var foundWebhook *NotificationChannel
	var foundSlack *NotificationChannel
	for _, c := range list {
		switch c.Name {
		case "email-1":
			foundEmail = c
		case "hook-1":
			foundWebhook = c
		case "slack-1":
			foundSlack = c
		}
	}
	if foundEmail == nil {
		t.Fatal("List missing email channel")
	}
	if len(foundEmail.Recipients) != 1 {
		t.Errorf("email recipients = %d, want 1", len(foundEmail.Recipients))
	}
	if !foundEmail.SMTPPasswordSet {
		t.Error("expected SMTPPasswordSet=true on list")
	}
	if foundEmail.SMTPPassword == nil || *foundEmail.SMTPPassword != "smtp-pass" {
		t.Errorf("smtp password roundtrip = %v, want smtp-pass", foundEmail.SMTPPassword)
	}
	if foundWebhook == nil {
		t.Fatal("List missing webhook channel")
	}
	if !foundWebhook.AuthCredentialsSet {
		t.Error("expected AuthCredentialsSet=true on list")
	}
	if foundWebhook.AuthCredentials == nil || *foundWebhook.AuthCredentials != "tok-XYZ" {
		t.Errorf("auth credentials roundtrip mismatch")
	}
	if len(foundWebhook.HeaderNames) != 2 {
		t.Errorf("HeaderNames len = %d, want 2", len(foundWebhook.HeaderNames))
	}
	if foundSlack == nil {
		t.Fatal("List missing slack channel")
	}
	if !foundSlack.WebhookURLSet {
		t.Error("expected WebhookURLSet on slack")
	}

	// Get by id
	got, err := ds.GetNotificationChannel(ctx, email.ID)
	if err != nil {
		t.Fatalf("GetNotificationChannel: %v", err)
	}
	if got.Name != "email-1" {
		t.Errorf("Get name = %q, want email-1", got.Name)
	}
	if len(got.Recipients) != 1 {
		t.Errorf("Get email recipients = %d, want 1", len(got.Recipients))
	}

	// Get by id - not found
	if _, err := ds.GetNotificationChannel(ctx, 999999); !errors.Is(err, ErrNotificationChannelNotFound) {
		t.Errorf("expected ErrNotificationChannelNotFound, got %v", err)
	}

	// Update
	got.Name = "email-1-updated"
	got.Description = strPtr("updated")
	if err := ds.UpdateNotificationChannel(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	check, err := ds.GetNotificationChannel(ctx, got.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if check.Name != "email-1-updated" {
		t.Errorf("update did not propagate, name=%q", check.Name)
	}

	// Update missing -> not found
	missing := makeSlackChannel("ghost")
	missing.ID = 999999
	if err := ds.UpdateNotificationChannel(ctx, missing); !errors.Is(err, ErrNotificationChannelNotFound) {
		t.Errorf("expected ErrNotificationChannelNotFound, got %v", err)
	}

	// Delete
	if err := ds.DeleteNotificationChannel(ctx, slack.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := ds.DeleteNotificationChannel(ctx, slack.ID); !errors.Is(err, ErrNotificationChannelNotFound) {
		t.Errorf("repeat delete should return not found, got %v", err)
	}
}

// TestEmailRecipientLifecycle covers Create/List/Update/Delete and
// the not-found paths for the email recipient sub-resource.
func TestEmailRecipientLifecycle(t *testing.T) {
	ds, _, _, cleanup := newNotifTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	email := makeEmailChannel("email-x")
	if err := ds.CreateNotificationChannel(ctx, email); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	r1 := &EmailRecipient{
		ChannelID:    email.ID,
		EmailAddress: "a@example.com",
		DisplayName:  strPtr("A"),
		Enabled:      true,
	}
	if err := ds.CreateEmailRecipient(ctx, r1); err != nil {
		t.Fatalf("create recipient: %v", err)
	}
	r2 := &EmailRecipient{
		ChannelID:    email.ID,
		EmailAddress: "b@example.com",
		Enabled:      true,
	}
	if err := ds.CreateEmailRecipient(ctx, r2); err != nil {
		t.Fatalf("create recipient: %v", err)
	}

	list, err := ds.ListEmailRecipients(ctx, email.ID)
	if err != nil {
		t.Fatalf("ListEmailRecipients: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("list len = %d, want 2", len(list))
	}

	// List for an empty channel -> empty (not nil) slice.
	emptyChan := makeEmailChannel("empty")
	if err := ds.CreateNotificationChannel(ctx, emptyChan); err != nil {
		t.Fatalf("create empty channel: %v", err)
	}
	emptyList, err := ds.ListEmailRecipients(ctx, emptyChan.ID)
	if err != nil {
		t.Fatalf("ListEmailRecipients empty: %v", err)
	}
	if emptyList == nil {
		t.Error("ListEmailRecipients should return non-nil empty slice")
	}
	if len(emptyList) != 0 {
		t.Errorf("expected empty slice, got %d", len(emptyList))
	}

	// Update
	r1.EmailAddress = "a-updated@example.com"
	r1.Enabled = false
	if err := ds.UpdateEmailRecipient(ctx, r1); err != nil {
		t.Fatalf("UpdateEmailRecipient: %v", err)
	}

	// Update missing
	missing := &EmailRecipient{
		ID:           999999,
		ChannelID:    email.ID,
		EmailAddress: "x@example.com",
	}
	if err := ds.UpdateEmailRecipient(ctx, missing); !errors.Is(err, ErrEmailRecipientNotFound) {
		t.Errorf("expected ErrEmailRecipientNotFound, got %v", err)
	}

	// Delete
	if err := ds.DeleteEmailRecipient(ctx, r2.ID); err != nil {
		t.Fatalf("DeleteEmailRecipient: %v", err)
	}
	if err := ds.DeleteEmailRecipient(ctx, r2.ID); !errors.Is(err, ErrEmailRecipientNotFound) {
		t.Errorf("repeat delete should return not found, got %v", err)
	}
}

// TestChannelOverridesAllScopes covers the three GetChannelOverridesFor*
// helpers, the upsert insert + update path for each scope, and the
// delete path. The test also exercises the invalid-scope error
// branches in UpsertChannelOverride and DeleteChannelOverride.
func TestChannelOverridesAllScopes(t *testing.T) {
	ds, pool, fx, cleanup := newNotifTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	enabled := makeSlackChannel("ovr-enabled")
	if err := ds.CreateNotificationChannel(ctx, enabled); err != nil {
		t.Fatalf("create enabled: %v", err)
	}
	disabled := makeSlackChannel("ovr-disabled")
	disabled.Enabled = false
	if err := ds.CreateNotificationChannel(ctx, disabled); err != nil {
		t.Fatalf("create disabled: %v", err)
	}

	// Server scope
	if err := ds.UpsertChannelOverride(ctx, "server", fx.ConnID, enabled.ID,
		ChannelOverrideUpdate{Enabled: false}); err != nil {
		t.Fatalf("upsert server insert: %v", err)
	}
	// Re-upsert same key updates rather than insert.
	if err := ds.UpsertChannelOverride(ctx, "server", fx.ConnID, enabled.ID,
		ChannelOverrideUpdate{Enabled: true}); err != nil {
		t.Fatalf("upsert server update: %v", err)
	}
	// Cluster scope
	if err := ds.UpsertChannelOverride(ctx, "cluster", fx.ClusterID, enabled.ID,
		ChannelOverrideUpdate{Enabled: false}); err != nil {
		t.Fatalf("upsert cluster: %v", err)
	}
	// Group scope
	if err := ds.UpsertChannelOverride(ctx, "group", fx.GroupID, enabled.ID,
		ChannelOverrideUpdate{Enabled: false}); err != nil {
		t.Fatalf("upsert group: %v", err)
	}
	// Invalid scope
	if err := ds.UpsertChannelOverride(ctx, "weird", 1, enabled.ID,
		ChannelOverrideUpdate{Enabled: true}); err == nil {
		t.Error("expected invalid-scope error")
	}

	// Server overrides list
	srvList, err := ds.GetChannelOverridesForServer(ctx, fx.ConnID)
	if err != nil {
		t.Fatalf("server list: %v", err)
	}
	// only one channel is enabled (disabled is filtered by `nc.enabled = true`)
	if len(srvList) != 1 {
		t.Fatalf("server list len = %d, want 1", len(srvList))
	}
	if !srvList[0].HasOverride || srvList[0].OverrideEnabled == nil || !*srvList[0].OverrideEnabled {
		t.Errorf("server override flags wrong: %+v", srvList[0])
	}

	// Cluster overrides list
	clsList, err := ds.GetChannelOverridesForCluster(ctx, fx.ClusterID)
	if err != nil {
		t.Fatalf("cluster list: %v", err)
	}
	if len(clsList) != 1 || !clsList[0].HasOverride {
		t.Errorf("cluster list mismatch: %+v", clsList)
	}

	// Group overrides list
	grpList, err := ds.GetChannelOverridesForGroup(ctx, fx.GroupID)
	if err != nil {
		t.Fatalf("group list: %v", err)
	}
	if len(grpList) != 1 || !grpList[0].HasOverride {
		t.Errorf("group list mismatch: %+v", grpList)
	}

	// Verify a connection without an override is reflected with
	// HasOverride=false. Insert a second connection and a second
	// enabled channel with no override row.
	var connID2 int
	if err := pool.QueryRow(ctx,
		`INSERT INTO connections (cluster_id, name) VALUES ($1, 'conn-2') RETURNING id`,
		fx.ClusterID).Scan(&connID2); err != nil {
		t.Fatalf("seed conn-2: %v", err)
	}
	withoutOverride, err := ds.GetChannelOverridesForServer(ctx, connID2)
	if err != nil {
		t.Fatalf("server list (no override): %v", err)
	}
	if len(withoutOverride) != 1 {
		t.Fatalf("expected 1 row for conn-2, got %d", len(withoutOverride))
	}
	if withoutOverride[0].HasOverride {
		t.Error("expected HasOverride=false")
	}

	// Delete overrides for each scope
	if err := ds.DeleteChannelOverride(ctx, "server", fx.ConnID, enabled.ID); err != nil {
		t.Errorf("delete server: %v", err)
	}
	if err := ds.DeleteChannelOverride(ctx, "cluster", fx.ClusterID, enabled.ID); err != nil {
		t.Errorf("delete cluster: %v", err)
	}
	if err := ds.DeleteChannelOverride(ctx, "group", fx.GroupID, enabled.ID); err != nil {
		t.Errorf("delete group: %v", err)
	}
	if err := ds.DeleteChannelOverride(ctx, "weird", 1, enabled.ID); err == nil {
		t.Error("expected invalid-scope error on delete")
	}

	// Delete on missing override is a no-op (no error).
	if err := ds.DeleteChannelOverride(ctx, "server", fx.ConnID, enabled.ID); err != nil {
		t.Errorf("repeat delete should not error, got %v", err)
	}

	// After deletes, override rows should still surface the channels
	// with HasOverride=false.
	finalServer, err := ds.GetChannelOverridesForServer(ctx, fx.ConnID)
	if err != nil {
		t.Fatalf("final server list: %v", err)
	}
	if len(finalServer) != 1 || finalServer[0].HasOverride {
		t.Errorf("expected single row with HasOverride=false: %+v", finalServer)
	}
}

// TestSortedHeaderNames covers the helper directly.
func TestSortedHeaderNames(t *testing.T) {
	if got := sortedHeaderNames(nil); got != nil {
		t.Errorf("nil input -> %v, want nil", got)
	}
	if got := sortedHeaderNames(map[string]string{}); got != nil {
		t.Errorf("empty map -> %v, want nil", got)
	}
	got := sortedHeaderNames(map[string]string{"b": "1", "a": "2", "c": "3"})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("sorted result = %v, want [a b c]", got)
	}
}

// TestMarshalHeadersBranches covers the JSON helper for both empty
// and non-empty inputs. Named with a `Branches` suffix to avoid
// collision with the basic struct test in queries_test.go.
func TestMarshalHeadersBranches(t *testing.T) {
	out, err := marshalHeaders(nil)
	if err != nil || out != nil {
		t.Errorf("nil headers: err=%v out=%v", err, out)
	}
	out, err = marshalHeaders(map[string]string{})
	if err != nil || out != nil {
		t.Errorf("empty headers: err=%v out=%v", err, out)
	}
	out, err = marshalHeaders(map[string]string{"X": "Y"})
	if err != nil || string(out) != `{"X":"Y"}` {
		t.Errorf("populated headers: err=%v out=%s", err, string(out))
	}
}

// TestEncryptNotificationSecret covers the helper's three branches:
// nil/empty short-circuit, missing-secret error, and the happy path
// where encryption returns a non-nil pointer.
func TestEncryptNotificationSecret(t *testing.T) {
	// nil -> nil
	d := &Datastore{}
	if got, err := d.encryptNotificationSecret(nil); got != nil || err != nil {
		t.Errorf("nil: got=%v err=%v", got, err)
	}
	// pointer to empty string passes through unchanged.
	empty := ""
	got, err := d.encryptNotificationSecret(&empty)
	if err != nil || got != &empty {
		t.Errorf("empty: got=%v err=%v", got, err)
	}
	// missing secret -> error
	val := "secret"
	if _, err := d.encryptNotificationSecret(&val); err == nil {
		t.Error("expected error when serverSecret empty")
	}
	// happy path round-trip via decryptNotificationSecret
	d2 := &Datastore{serverSecret: "test-secret-with-enough-bytes-here-please"}
	enc, err := d2.encryptNotificationSecret(&val)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if enc == nil || *enc == "" || *enc == "secret" {
		t.Errorf("encrypt returned suspicious value: %v", enc)
	}
	dec := d2.decryptNotificationSecret(enc)
	if dec == nil || *dec != "secret" {
		t.Errorf("decrypt = %v, want secret", dec)
	}
	// decrypting a value that isn't ciphertext returns the input
	// unchanged (backwards-compatibility branch).
	plain := "plaintext"
	got2 := d2.decryptNotificationSecret(&plain)
	if got2 == nil || *got2 != "plaintext" {
		t.Errorf("non-ciphertext should pass through, got %v", got2)
	}
	// nil and empty short-circuit branches.
	if d2.decryptNotificationSecret(nil) != nil {
		t.Error("nil should pass through")
	}
	if got := d2.decryptNotificationSecret(&empty); got != &empty {
		t.Error("empty should pass through")
	}
	// no server secret short-circuits
	d3 := &Datastore{}
	if got := d3.decryptNotificationSecret(&val); got != &val {
		t.Error("no secret should pass through")
	}
}

// TestListNotificationChannelsEmpty asserts the empty-table branch
// returns a non-nil empty slice.
func TestListNotificationChannelsEmpty(t *testing.T) {
	ds, _, _, cleanup := newNotifTestDatastore(t)
	defer cleanup()

	got, err := ds.ListNotificationChannels(context.Background())
	if err != nil {
		t.Fatalf("ListNotificationChannels: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("expected non-nil empty slice, got %v", got)
	}
}

// TestNotificationErrorPaths forces SQL failures by dropping the
// underlying tables before invoking each function. This drives the
// outer error returns that the happy-path tests cannot exercise.
func TestNotificationErrorPaths(t *testing.T) {
	ds, pool, fx, cleanup := newNotifTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	// Drop the email_recipients and notification_channel_overrides
	// tables. Keep notification_channels so we can test paths that
	// only depend on missing dependents.
	if _, err := pool.Exec(ctx, `DROP TABLE notification_channel_overrides`); err != nil {
		t.Fatalf("drop overrides: %v", err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE email_recipients`); err != nil {
		t.Fatalf("drop recipients: %v", err)
	}

	if _, err := ds.GetChannelOverridesForServer(ctx, fx.ConnID); err == nil {
		t.Error("expected error: server overrides table missing")
	}
	if _, err := ds.GetChannelOverridesForCluster(ctx, fx.ClusterID); err == nil {
		t.Error("expected error: cluster overrides table missing")
	}
	if _, err := ds.GetChannelOverridesForGroup(ctx, fx.GroupID); err == nil {
		t.Error("expected error: group overrides table missing")
	}
	if err := ds.UpsertChannelOverride(ctx, "server", fx.ConnID, 1,
		ChannelOverrideUpdate{Enabled: true}); err == nil {
		t.Error("expected error from upsert with missing table")
	}
	if err := ds.DeleteChannelOverride(ctx, "server", fx.ConnID, 1); err == nil {
		t.Error("expected error from delete with missing table")
	}
	if _, err := ds.ListEmailRecipients(ctx, 1); err == nil {
		t.Error("expected error: recipients table missing")
	}
	r := &EmailRecipient{ChannelID: 1, EmailAddress: "x@example.com"}
	if err := ds.CreateEmailRecipient(ctx, r); err == nil {
		t.Error("expected error from CreateEmailRecipient with missing table")
	}
	if err := ds.UpdateEmailRecipient(ctx, r); err == nil {
		t.Error("expected error from UpdateEmailRecipient with missing table")
	}
	if err := ds.DeleteEmailRecipient(ctx, 1); err == nil {
		t.Error("expected error from DeleteEmailRecipient with missing table")
	}

	// Now drop notification_channels and check the remaining helpers.
	if _, err := pool.Exec(ctx, `DROP TABLE notification_channels CASCADE`); err != nil {
		t.Fatalf("drop channels: %v", err)
	}
	if _, err := ds.ListNotificationChannels(ctx); err == nil {
		t.Error("expected error: channels table missing")
	}
	if _, err := ds.GetNotificationChannel(ctx, 1); err == nil {
		t.Error("expected error: channels table missing on Get")
	}
	if err := ds.CreateNotificationChannel(ctx, makeSlackChannel("x")); err == nil {
		t.Error("expected error: channels table missing on Create")
	}
	c := makeSlackChannel("x")
	c.ID = 1
	if err := ds.UpdateNotificationChannel(ctx, c); err == nil {
		t.Error("expected error: channels table missing on Update")
	}
	if err := ds.DeleteNotificationChannel(ctx, 1); err == nil {
		t.Error("expected error: channels table missing on Delete")
	}
}

// TestEncryptionErrorOnCreate forces the encrypt step to fail by
// clearing the server secret after the datastore is built and
// using a channel with a non-empty webhook URL. The CreateNotification
// Channel call must propagate an error out of the encrypt helper.
func TestEncryptionErrorOnCreate(t *testing.T) {
	ds, pool, _, cleanup := newNotifTestDatastore(t)
	defer cleanup()

	// Override the secret to empty so encryption fails.
	emptySecretDS := NewTestDatastoreWithSecret(pool, "")

	ctx := context.Background()
	if err := emptySecretDS.CreateNotificationChannel(ctx, makeSlackChannel("e1")); err == nil {
		t.Error("expected encrypt error on create")
	}

	c := makeSlackChannel("e2")
	if err := ds.CreateNotificationChannel(ctx, c); err != nil {
		t.Fatalf("baseline create: %v", err)
	}
	if err := emptySecretDS.UpdateNotificationChannel(ctx, c); err == nil {
		t.Error("expected encrypt error on update")
	}
}

// TestListNotificationChannelsRecipientLoadError forces the recipient
// loader to fail after channels are listed by dropping the
// email_recipients table mid-test. This drives the
// `failed to load recipients for channel %d: %w` error branch.
func TestListNotificationChannelsRecipientLoadError(t *testing.T) {
	ds, pool, _, cleanup := newNotifTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	email := makeEmailChannel("e-rec-err")
	if err := ds.CreateNotificationChannel(ctx, email); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := pool.Exec(ctx, `DROP TABLE email_recipients`); err != nil {
		t.Fatalf("drop recipients: %v", err)
	}

	if _, err := ds.ListNotificationChannels(ctx); err == nil {
		t.Error("expected recipient-load error from List")
	}
	if _, err := ds.GetNotificationChannel(ctx, email.ID); err == nil {
		t.Error("expected recipient-load error from Get")
	}
}
