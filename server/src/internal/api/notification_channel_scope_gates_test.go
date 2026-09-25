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
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// channelDropRecipients removes the recipients table, so that every
// recipient query fails whilst channel lookups still answer.
const channelDropRecipients = `DROP TABLE email_recipients CASCADE;`

// channelFailRecipientInsert makes every recipient insert fail whilst
// reads still answer. The teardown drops the table, and the trigger with
// it; the function goes with DROP FUNCTION in the test's cleanup.
const channelFailRecipientInsert = `
CREATE OR REPLACE FUNCTION channel_scope_fail_insert() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'recipient insert refused by test';
END;
$$;
CREATE TRIGGER channel_scope_fail_insert BEFORE INSERT ON email_recipients
    FOR EACH ROW EXECUTE FUNCTION channel_scope_fail_insert();
`

const channelFailRecipientInsertTeardown = `DROP FUNCTION IF EXISTS channel_scope_fail_insert() CASCADE;`

// channelDropChannels removes both notification tables.
const channelDropChannels = `
DROP TABLE email_recipients CASCADE;
DROP TABLE notification_channels CASCADE;
`

// channelScopeFixture is a notification channel handler over a seeded
// channel and recipient, with the shared scope callers.
type channelScopeFixture struct {
	h           *NotificationChannelHandler
	pool        *pgxpool.Pool
	channelID   int64
	recipientID int64
	callers     map[string]scopeCaller
}

func newChannelScopeFixture(t *testing.T) *channelScopeFixture {
	t.Helper()

	ds, pool, dsCleanup := newChannelTestDatastore(t)
	t.Cleanup(dsCleanup)
	_, store, storeCleanup := createTestRBACHandler(t)
	t.Cleanup(storeCleanup)

	channelID := createTestChannel(t, ds, "scope-channel", nil, nil, nil, nil)
	recipient := &database.EmailRecipient{
		ChannelID:    channelID,
		EmailAddress: "jane.doe@example.com",
		Enabled:      true,
	}
	if err := ds.CreateEmailRecipient(context.Background(), recipient); err != nil {
		t.Fatalf("CreateEmailRecipient: %v", err)
	}

	session, unscoped, wildcard, narrowed, readOnly := scopedCallers(t, store)
	return &channelScopeFixture{
		h: NewNotificationChannelHandlerWithSecurity(ds, store,
			auth.NewRBACChecker(store), false, nil, nil),
		pool:        pool,
		channelID:   channelID,
		recipientID: recipient.ID,
		callers: map[string]scopeCaller{
			"session": session, "unscoped": unscoped, "wildcard": wildcard,
			"narrowed": narrowed, "readOnly": readOnly,
			"anonymous": anonymousCaller,
		},
	}
}

// channelWrite calls one channel or recipient write handler.
type channelWrite func(f *channelScopeFixture, w http.ResponseWriter,
	r *http.Request, channelID, recipientID int64)

const (
	channelCreateBody = `{"channel_type":"email","name":"new-channel",` +
		`"smtp_host":"smtp.example.com","from_address":"alerts@example.com"}`
	channelUpdateBody   = `{"name":"renamed-channel"}`
	recipientCreateBody = `{"email_address":"john.doe@example.com","enabled":true}`
	recipientUpdateBody = `{"email_address":"test.user@example.com","enabled":false}`
)

var (
	createChannelCall channelWrite = func(f *channelScopeFixture,
		w http.ResponseWriter, r *http.Request, _, _ int64) {
		f.h.createChannel(w, r)
	}
	updateChannelCall channelWrite = func(f *channelScopeFixture,
		w http.ResponseWriter, r *http.Request, c, _ int64) {
		f.h.updateChannel(w, r, c)
	}
	deleteChannelCall channelWrite = func(f *channelScopeFixture,
		w http.ResponseWriter, r *http.Request, c, _ int64) {
		f.h.deleteChannel(w, r, c)
	}
	createRecipientCall channelWrite = func(f *channelScopeFixture,
		w http.ResponseWriter, r *http.Request, c, _ int64) {
		f.h.createRecipient(w, r, c)
	}
	updateRecipientCall channelWrite = func(f *channelScopeFixture,
		w http.ResponseWriter, r *http.Request, c, rid int64) {
		f.h.updateRecipient(w, r, c, rid)
	}
	deleteRecipientCall channelWrite = func(f *channelScopeFixture,
		w http.ResponseWriter, r *http.Request, _, rid int64) {
		f.h.deleteRecipient(w, r, rid)
	}
)

// TestNotificationChannelWritesRespectTokenScope checks that a channel
// or recipient write, which reaches alerts from every connection,
// refuses a token narrowed to some connections and admits session,
// unscoped and wildcard callers.
func TestNotificationChannelWritesRespectTokenScope(t *testing.T) {
	writes := []struct {
		name string
		fn   channelWrite
		body string
	}{
		{"create channel", createChannelCall, channelCreateBody},
		{"update channel", updateChannelCall, channelUpdateBody},
		{"delete channel", deleteChannelCall, ""},
		{"create recipient", createRecipientCall, recipientCreateBody},
		{"update recipient", updateRecipientCall, recipientUpdateBody},
		{"delete recipient", deleteRecipientCall, ""},
	}
	for _, wr := range writes {
		for _, caller := range []string{"narrowed", "readOnly"} {
			t.Run(wr.name+" refuses "+caller, func(t *testing.T) {
				f := newChannelScopeFixture(t)
				rec := httptest.NewRecorder()
				wr.fn(f, rec, newScopeRequest(f.callers[caller],
					http.MethodPost, wr.body), f.channelID, f.recipientID)
				assertOutOfTokenScope(t, rec)
			})
		}
		for _, caller := range []string{"session", "unscoped", "wildcard"} {
			t.Run(wr.name+" admits "+caller, func(t *testing.T) {
				f := newChannelScopeFixture(t)
				rec := httptest.NewRecorder()
				r := newScopeRequest(f.callers[caller], http.MethodPost, wr.body)
				assertGatePassed(t, rec, func() {
					wr.fn(f, rec, r, f.channelID, f.recipientID)
				})
				if rec.Code >= 300 {
					t.Errorf("Expected success, got %d: %s", rec.Code,
						rec.Body.String())
				}
			})
		}
		t.Run(wr.name+" needs permission", func(t *testing.T) {
			f := newChannelScopeFixture(t)
			rec := httptest.NewRecorder()
			wr.fn(f, rec, newScopeRequest(anonymousCaller, http.MethodPost,
				wr.body), f.channelID, f.recipientID)
			expectStatus(t, rec, http.StatusForbidden)
		})
	}
}

// TestNotificationRecipientAndDeleteOutcomes covers the recipient
// handlers and channel deletion past the scope gate.
func TestNotificationRecipientAndDeleteOutcomes(t *testing.T) {
	const missing = int64(999999)
	cases := []struct {
		name      string
		fn        channelWrite
		body      string
		channel   bool // use a missing channel id
		recipient bool // use a missing recipient id
		drop      string
		want      int
	}{
		{"create recipient", createRecipientCall, recipientCreateBody, false, false, "", http.StatusCreated},
		{"create recipient missing channel", createRecipientCall, recipientCreateBody, true, false, "", http.StatusNotFound},
		{"create recipient channel lookup fails", createRecipientCall, recipientCreateBody, false, false, channelDropChannels, http.StatusInternalServerError},
		{"create recipient bad body", createRecipientCall, `{`, false, false, "", http.StatusBadRequest},
		{"create recipient bad email", createRecipientCall, `{"email_address":"nobody"}`, false, false, "", http.StatusBadRequest},
		{"create recipient fails", createRecipientCall, recipientCreateBody, false, false, channelFailRecipientInsert, http.StatusInternalServerError},

		{"update recipient", updateRecipientCall, recipientUpdateBody, false, false, "", http.StatusOK},
		{"update recipient missing", updateRecipientCall, recipientUpdateBody, false, true, "", http.StatusNotFound},
		{"update recipient bad body", updateRecipientCall, `{`, false, false, "", http.StatusBadRequest},
		{"update recipient bad email", updateRecipientCall, `{"email_address":"nobody"}`, false, false, "", http.StatusBadRequest},
		{"update recipient fails", updateRecipientCall, recipientUpdateBody, false, false, channelDropRecipients, http.StatusInternalServerError},

		{"delete recipient", deleteRecipientCall, "", false, false, "", http.StatusOK},
		{"delete recipient missing", deleteRecipientCall, "", false, true, "", http.StatusNotFound},
		{"delete recipient fails", deleteRecipientCall, "", false, false, channelDropRecipients, http.StatusInternalServerError},

		{"delete channel", deleteChannelCall, "", false, false, "", http.StatusOK},
		{"delete channel missing", deleteChannelCall, "", true, false, "", http.StatusNotFound},
		{"delete channel fails", deleteChannelCall, "", false, false, channelDropChannels, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newChannelScopeFixture(t)
			if tc.drop != "" {
				t.Cleanup(func() {
					_, _ = f.pool.Exec(context.Background(),
						channelFailRecipientInsertTeardown)
				})
				mustExec(t, f.pool, tc.drop)
			}
			channelID, recipientID := f.channelID, f.recipientID
			if tc.channel {
				channelID = missing
			}
			if tc.recipient {
				recipientID = missing
			}
			rec := httptest.NewRecorder()
			tc.fn(f, rec, newScopeRequest(f.callers["session"],
				http.MethodPost, tc.body), channelID, recipientID)
			expectStatus(t, rec, tc.want)
		})
	}
}
