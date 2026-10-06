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
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// TestConnectionDeleteContext checks the budget the DELETE runs under
// (issue #480): the deadline is database.ConnectionDeleteTimeout rather
// than the lookup's ten seconds, and canceling the request does not
// cancel the delete.
func TestConnectionDeleteContext(t *testing.T) {
	type ctxKey struct{}
	parent, cancelParent := context.WithCancel(
		context.WithValue(context.Background(), ctxKey{}, "kept"))

	before := time.Now()
	ctx, cancel := connectionDeleteContext(parent)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("delete context has no deadline")
	}
	if earliest := before.Add(database.ConnectionDeleteTimeout); deadline.Before(earliest) {
		t.Fatalf("deadline %v is earlier than %v", deadline, earliest)
	}
	if latest := time.Now().Add(database.ConnectionDeleteTimeout); deadline.After(latest) {
		t.Fatalf("deadline %v is later than %v", deadline, latest)
	}
	if got := ctx.Value(ctxKey{}); got != "kept" {
		t.Fatalf("request values not carried over: got %v", got)
	}

	cancelParent()
	select {
	case <-ctx.Done():
		t.Fatalf("delete context canceled with its request: %v", ctx.Err())
	default:
	}

	cancel()
	if ctx.Err() == nil {
		t.Fatal("delete context not canceled by its own cancel func")
	}
}

// deleteConnectionHarness wires a real datastore and auth store for the
// deleteConnection handler tests, seeding connection id owned by owner.
type deleteConnectionHarness struct {
	handler *ConnectionHandler
	pool    *pgxpool.Pool
	store   *auth.AuthStore
}

func newDeleteConnectionHarness(t *testing.T, id int, owner string) *deleteConnectionHarness {
	t.Helper()

	ds, pool, cleanupDS := newIssue269ConnectionDatastore(t)
	t.Cleanup(cleanupDS)
	_, store, cleanupStore := createTestRBACHandler(t)
	t.Cleanup(cleanupStore)

	seedIssue269Connection(t, pool, id, owner, "delete-target")

	return &deleteConnectionHarness{
		handler: NewConnectionHandlerWithSecurity(ds, store,
			auth.NewRBACChecker(store), false, nil, nil),
		pool:  pool,
		store: store,
	}
}

// request builds an authenticated DELETE for the named user, creating
// the user with the given admin permissions.
func (h *deleteConnectionHarness) request(t *testing.T, ctx context.Context,
	username string, permissions []string) *http.Request {
	t.Helper()

	userID := setupUserWithPermissions(t, h.store, username, permissions)
	token, _, err := h.store.AuthenticateUser(username, "Password1234")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/connections/1", nil).
		WithContext(ctx)
	return withUser(withBearer(req, token), userID)
}

func (h *deleteConnectionHarness) rowCount(t *testing.T, id int) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM connections WHERE id = $1`, id,
	).Scan(&n); err != nil {
		t.Fatalf("counting connection rows: %v", err)
	}
	return n
}

// Trigger functions for installDeleteTrigger: one slows every DELETE on
// connections, the other makes it fail.
const (
	issue480SlowDeleteFunction = `
        CREATE OR REPLACE FUNCTION issue480_connection_delete()
        RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN
            PERFORM pg_sleep(0.6);
            RETURN OLD;
        END;
        $$;`
	issue480FailingDeleteFunction = `
        CREATE OR REPLACE FUNCTION issue480_connection_delete()
        RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN
            RAISE EXCEPTION 'blocked by test';
        END;
        $$;`
)

// installDeleteTrigger runs function (one of the constants above) and
// attaches it as a BEFORE DELETE trigger on connections.
func (h *deleteConnectionHarness) installDeleteTrigger(t *testing.T, function string) {
	t.Helper()
	ctx := context.Background()
	if _, err := h.pool.Exec(ctx, function); err != nil {
		t.Fatalf("creating delete trigger function: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
        CREATE TRIGGER issue480_conn_delete
        BEFORE DELETE ON connections
        FOR EACH ROW EXECUTE FUNCTION issue480_connection_delete();
    `); err != nil {
		t.Fatalf("installing delete trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(),
			`DROP FUNCTION IF EXISTS issue480_connection_delete() CASCADE`)
	})
}

// TestDeleteConnection_SurvivesRequestCancellation is the handler-level
// regression test for issue #480. The delete is slowed by a trigger, and
// the request context is canceled whilst it runs, as a reverse proxy
// giving up would do. The delete must still commit.
func TestDeleteConnection_SurvivesRequestCancellation(t *testing.T) {
	const id, owner = 7480, "issue480_owner"
	h := newDeleteConnectionHarness(t, id, owner)
	h.installDeleteTrigger(t, issue480SlowDeleteFunction)

	reqCtx, cancelReq := context.WithCancel(context.Background())
	defer cancelReq()
	req := h.request(t, reqCtx, owner, nil)

	timer := time.AfterFunc(200*time.Millisecond, cancelReq)
	defer timer.Stop()

	rec := httptest.NewRecorder()
	h.handler.deleteConnection(rec, req, id)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d. Body: %s",
			rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if reqCtx.Err() == nil {
		t.Fatal("request context was not canceled during the delete; test proves nothing")
	}
	if n := h.rowCount(t, id); n != 0 {
		t.Fatalf("connection %d still present (%d rows)", id, n)
	}
}

// TestDeleteConnection_HandlerResponses covers the handler's remaining
// branches against a real datastore.
func TestDeleteConnection_HandlerResponses(t *testing.T) {
	const id, owner = 7481, "issue480_resp_owner"

	t.Run("manage_connections deletes another user's connection", func(t *testing.T) {
		h := newDeleteConnectionHarness(t, id, owner)
		req := h.request(t, context.Background(), "issue480_admin",
			[]string{auth.PermManageConnections})
		rec := httptest.NewRecorder()
		h.handler.deleteConnection(rec, req, id)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204. Body: %s", rec.Code, rec.Body.String())
		}
		if n := h.rowCount(t, id); n != 0 {
			t.Fatalf("connection still present (%d rows)", n)
		}
	})

	t.Run("non-owner without permission is forbidden", func(t *testing.T) {
		h := newDeleteConnectionHarness(t, id, owner)
		req := h.request(t, context.Background(), "issue480_other", nil)
		rec := httptest.NewRecorder()
		h.handler.deleteConnection(rec, req, id)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403. Body: %s", rec.Code, rec.Body.String())
		}
		if n := h.rowCount(t, id); n != 1 {
			t.Fatalf("connection deleted despite 403 (%d rows)", n)
		}
	})

	t.Run("missing connection is not found", func(t *testing.T) {
		h := newDeleteConnectionHarness(t, id, owner)
		req := h.request(t, context.Background(), owner, nil)
		rec := httptest.NewRecorder()
		h.handler.deleteConnection(rec, req, id+1000)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404. Body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("datastore failure is a 500", func(t *testing.T) {
		h := newDeleteConnectionHarness(t, id, owner)
		h.installDeleteTrigger(t, issue480FailingDeleteFunction)
		req := h.request(t, context.Background(), owner, nil)
		rec := httptest.NewRecorder()
		h.handler.deleteConnection(rec, req, id)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500. Body: %s", rec.Code, rec.Body.String())
		}
		if n := h.rowCount(t, id); n != 1 {
			t.Fatalf("connection deleted despite failure (%d rows)", n)
		}
	})

	t.Run("missing authentication is unauthorized", func(t *testing.T) {
		h := newDeleteConnectionHarness(t, id, owner)
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/connections/1", nil)
		rec := httptest.NewRecorder()
		h.handler.deleteConnection(rec, req, id)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401. Body: %s", rec.Code, rec.Body.String())
		}
	})
}
