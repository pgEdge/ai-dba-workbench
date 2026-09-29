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
	"strconv"
	"testing"
)

func TestNotificationChannelHandler_RegisterRoutes(t *testing.T) {
	mux := http.NewServeMux()
	NewNotificationChannelHandlerWithSecurity(nil, nil, nil, false, nil, nil).RegisterRoutes(mux, covIdentityWrapper)
	for _, p := range []string{"/api/v1/notification-channels", "/api/v1/notification-channels/1"} {
		covExpect(t, covDo(mux.ServeHTTP, http.MethodGet, p, ""), http.StatusServiceUnavailable)
	}

	f := newCovFixture(t)
	mux = http.NewServeMux()
	NewNotificationChannelHandlerWithSecurity(f.ds, nil, newTestRBACChecker(t), false, nil, nil).
		RegisterRoutes(mux, covIdentityWrapper)
	covExpect(t, covDo(mux.ServeHTTP, http.MethodGet, "/api/v1/notification-channels", "", withSuperuser),
		http.StatusOK)
	covExpect(t, covDo(mux.ServeHTTP, http.MethodGet,
		"/api/v1/notification-channels/"+strconv.FormatInt(f.channelID, 10), "", withSuperuser), http.StatusOK)
}

func TestNotificationChannelHandler_Recipients(t *testing.T) {
	f := newCovFixture(t)
	h := NewNotificationChannelHandlerWithSecurity(f.ds, nil, newTestRBACChecker(t), false, nil, nil)
	broken := NewNotificationChannelHandlerWithSecurity(newBrokenDatastore(t), nil, newTestRBACChecker(t), false, nil, nil)
	serve := h.handleChannelSubpath
	base := "/api/v1/notification-channels/" + strconv.FormatInt(f.channelID, 10) + "/recipients"
	missing := "/api/v1/notification-channels/999999/recipients"
	noPerm := func(r *http.Request) *http.Request { return withUser(r, 424242) }

	// Routing.
	covExpect(t, covDo(serve, http.MethodDelete, base, "", withSuperuser), http.StatusMethodNotAllowed)
	covExpect(t, covDo(serve, http.MethodGet, base+"/x", "", withSuperuser), http.StatusBadRequest)
	covExpect(t, covDo(serve, http.MethodGet, base+"/1", "", withSuperuser), http.StatusMethodNotAllowed)
	covExpect(t, covDo(serve, http.MethodGet, base+"/1/extra", "", withSuperuser), http.StatusNotFound)

	// Create.
	rec := covDo(serve, http.MethodPost, base, `{"email_address": "jane.doe@example.com", "display_name": "Jane"}`,
		withSuperuser)
	covExpect(t, rec, http.StatusCreated)
	recipientID := covCreatedID(t, rec.Body.Bytes())
	covExpect(t, covDo(serve, http.MethodPost, base, `{"email_address": "ops@example.com", "enabled": false}`,
		withSuperuser), http.StatusCreated)
	covExpect(t, covDo(serve, http.MethodPost, base, `{"email_address": "not-an-address"}`, withSuperuser),
		http.StatusBadRequest)
	covExpect(t, covDo(serve, http.MethodPost, base, `{`, withSuperuser), http.StatusBadRequest)
	covExpect(t, covDo(serve, http.MethodPost, missing, `{"email_address": "a@example.com"}`, withSuperuser),
		http.StatusNotFound)
	covExpect(t, covDo(serve, http.MethodPost, base, `{"email_address": "a@example.com"}`, noPerm),
		http.StatusForbidden)

	// List.
	rec = covDo(serve, http.MethodGet, base, "", withSuperuser)
	covExpect(t, rec, http.StatusOK)
	covExpect(t, covDo(serve, http.MethodGet, missing, "", withSuperuser), http.StatusNotFound)
	covExpect(t, covDo(serve, http.MethodGet, base, "", noPerm), http.StatusForbidden)

	// Update.
	item := base + "/" + recipientID
	covExpect(t, covDo(serve, http.MethodPut, item, `{"email_address": "jane.doe@example.org", "enabled": false}`,
		withSuperuser), http.StatusOK)
	covExpect(t, covDo(serve, http.MethodPut, item, `{"email_address": "jane.doe@example.org"}`, withSuperuser),
		http.StatusOK)
	covExpect(t, covDo(serve, http.MethodPut, item, `{"email_address": ""}`, withSuperuser), http.StatusBadRequest)
	covExpect(t, covDo(serve, http.MethodPut, item, `{`, withSuperuser), http.StatusBadRequest)
	covExpect(t, covDo(serve, http.MethodPut, base+"/999999", `{"email_address": "a@example.com"}`, withSuperuser),
		http.StatusNotFound)
	covExpect(t, covDo(serve, http.MethodPut, item, `{"email_address": "a@example.com"}`, noPerm),
		http.StatusForbidden)

	// Delete.
	covExpect(t, covDo(serve, http.MethodDelete, item, "", withSuperuser), http.StatusOK)
	covExpect(t, covDo(serve, http.MethodDelete, item, "", withSuperuser), http.StatusNotFound)
	covExpect(t, covDo(serve, http.MethodDelete, item, "", noPerm), http.StatusForbidden)

	// Datastore failures.
	b := broken.handleChannelSubpath
	covExpect(t, covDo(b, http.MethodGet, base, "", withSuperuser), http.StatusInternalServerError)
	covExpect(t, covDo(b, http.MethodPost, base, `{"email_address": "a@example.com"}`, withSuperuser),
		http.StatusInternalServerError)
	covExpect(t, covDo(b, http.MethodPut, item, `{"email_address": "a@example.com"}`, withSuperuser),
		http.StatusInternalServerError)
	covExpect(t, covDo(b, http.MethodDelete, item, "", withSuperuser), http.StatusInternalServerError)
	covExpect(t, covDo(b, http.MethodPost, "/api/v1/notification-channels/1/test", "", withSuperuser),
		http.StatusInternalServerError)
	covExpect(t, covDo(b, http.MethodDelete, "/api/v1/notification-channels/1", "", withSuperuser),
		http.StatusInternalServerError)
	covExpect(t, covDo(serve, http.MethodDelete, "/api/v1/notification-channels/999999", "", withSuperuser),
		http.StatusNotFound)

	// The channel exists but the recipients table is gone.
	if _, err := f.pool.Exec(context.Background(), "DROP TABLE email_recipients"); err != nil {
		t.Fatalf("drop email_recipients: %v", err)
	}
	covExpect(t, covDo(serve, http.MethodGet, base, "", withSuperuser), http.StatusInternalServerError)
	covExpect(t, covDo(serve, http.MethodPost, base, `{"email_address": "a@example.com"}`, withSuperuser),
		http.StatusInternalServerError)
}

func TestNotificationChannelHandler_UpdateMergesAllFields(t *testing.T) {
	f := newCovFixture(t)
	h := NewNotificationChannelHandlerWithSecurity(f.ds, nil, newTestRBACChecker(t), false, nil, nil)
	url := "/api/v1/notification-channels/" + strconv.FormatInt(f.channelID, 10)
	body := `{
        "from_address": "alerts@example.com",
        "from_name": "Alerts",
        "telegram_chat_id": "12345",
        "template_alert_fire": "fire",
        "template_alert_clear": "clear",
        "template_reminder": "remind",
        "reminder_enabled": true,
        "reminder_interval_hours": 6,
        "is_estate_default": true
    }`
	rec := covDo(h.handleChannelSubpath, http.MethodPut, url, body, withSuperuser)
	covExpect(t, rec, http.StatusOK)
	m := decodeRaw(t, rec.Body.Bytes())
	if m["from_name"] != "Alerts" || m["reminder_enabled"] != true || m["is_estate_default"] != true {
		t.Fatalf("merged fields not reflected in response: %v", m)
	}
}

func TestNotificationChannelHandler_TestUnsupportedType(t *testing.T) {
	f := newCovFixture(t)
	// The fixture table carries no channel_type CHECK, so a type the
	// handler does not know about can be stored directly.
	var id int64
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO notification_channels (channel_type, name) VALUES ('pager', 'legacy') RETURNING id`).
		Scan(&id); err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	h := NewNotificationChannelHandlerWithSecurity(f.ds, nil, newTestRBACChecker(t), false, nil, nil)
	covExpect(t, covDo(h.handleChannelSubpath, http.MethodPost,
		"/api/v1/notification-channels/"+strconv.FormatInt(id, 10)+"/test", "", withSuperuser), http.StatusBadRequest)
}
