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
// Without a sharing lookup the RBAC checker treats every connection as
// shared, so another user can see it; withSharingLookup makes the
// checker respect the seeded row's is_shared = FALSE.
type deleteConnectionHarness struct {
	handler *ConnectionHandler
	ds      *database.Datastore
	pool    *pgxpool.Pool
	store   *auth.AuthStore
	checker *auth.RBACChecker
}

func newDeleteConnectionHarness(t *testing.T, id int, owner string) *deleteConnectionHarness {
	t.Helper()

	ds, pool, cleanupDS := newIssue269ConnectionDatastore(t)
	t.Cleanup(cleanupDS)
	rbac, store, cleanupStore := createTestRBACHandler(t)
	t.Cleanup(cleanupStore)

	seedIssue269Connection(t, pool, id, owner, "delete-target")

	checker := auth.NewRBACChecker(store)
	handler := NewConnectionHandlerWithSecurity(ds, store, checker, false, nil, nil)
	handler.SetDenialRecorder(rbac.RecordDenial)
	return &deleteConnectionHarness{
		handler: handler,
		ds:      ds,
		pool:    pool,
		store:   store,
		checker: checker,
	}
}

// withSharingLookup makes the checker read sharing and ownership from
// the datastore, as the server does.
func (h *deleteConnectionHarness) withSharingLookup() *deleteConnectionHarness {
	h.checker.SetConnectionSharingLookup(h.ds.GetConnectionSharingInfo)
	return h
}

// request builds an authenticated DELETE for the named user, creating
// the user with the given admin permissions.
func (h *deleteConnectionHarness) request(t *testing.T, ctx context.Context,
	username string, permissions []string) *http.Request {
	t.Helper()

	setupUserWithPermissions(t, h.store, username, permissions)
	token, _, err := h.store.AuthenticateUser(username, "Password1234")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/connections/1", nil).
		WithContext(ctx)
	return withAuth(t, h.store, req, token)
}

// marked reports whether connection id has been marked deleted. The row
// itself stays until the background purge removes it (issue #669).
func (h *deleteConnectionHarness) marked(t *testing.T, id int) bool {
	t.Helper()
	var marked bool
	if err := h.pool.QueryRow(context.Background(),
		`SELECT deleted_at IS NOT NULL FROM connections WHERE id = $1`, id,
	).Scan(&marked); err != nil {
		t.Fatalf("reading connection %d: %v", id, err)
	}
	return marked
}

// deleteDenials counts the audited connection.delete refusals.
func (h *deleteConnectionHarness) deleteDenials(t *testing.T) int {
	t.Helper()
	events, _, err := h.store.ListAuditEvents(auth.AuditFilter{
		Action: "connection.delete", Outcome: string(auth.OutcomeDenied)})
	if err != nil {
		t.Fatalf("ListAuditEvents failed: %v", err)
	}
	return len(events)
}

// Trigger functions for installMarkTrigger: one slows the UPDATE that
// marks a connection deleted, one makes it fail, and one skips it, as a
// concurrent delete that won the race would.
const (
	issue480SlowMarkFunction = `
        CREATE OR REPLACE FUNCTION issue480_connection_mark()
        RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN
            PERFORM pg_sleep(0.6);
            RETURN NEW;
        END;
        $$;`
	issue480FailingMarkFunction = `
        CREATE OR REPLACE FUNCTION issue480_connection_mark()
        RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN
            RAISE EXCEPTION 'blocked by test';
        END;
        $$;`
	issue480SkippingMarkFunction = `
        CREATE OR REPLACE FUNCTION issue480_connection_mark()
        RETURNS trigger LANGUAGE plpgsql AS $$
        BEGIN
            RETURN NULL;
        END;
        $$;`
)

// installMarkTrigger runs function (one of the constants above) and
// attaches it as a BEFORE UPDATE trigger on connections.
func (h *deleteConnectionHarness) installMarkTrigger(t *testing.T, function string) {
	t.Helper()
	ctx := context.Background()
	if _, err := h.pool.Exec(ctx, function); err != nil {
		t.Fatalf("creating mark trigger function: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
        CREATE TRIGGER issue480_conn_mark
        BEFORE UPDATE ON connections
        FOR EACH ROW EXECUTE FUNCTION issue480_connection_mark();
    `); err != nil {
		t.Fatalf("installing mark trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = h.pool.Exec(context.Background(),
			`DROP FUNCTION IF EXISTS issue480_connection_mark() CASCADE`)
	})
}

// TestDeleteConnection_SurvivesRequestCancellation is the handler-level
// regression test for issue #480. The delete is slowed by a trigger, and
// the request context is canceled whilst it runs, as a reverse proxy
// giving up would do. The delete must still commit.
func TestDeleteConnection_SurvivesRequestCancellation(t *testing.T) {
	const id, owner = 7480, "issue480_owner"
	h := newDeleteConnectionHarness(t, id, owner)
	h.installMarkTrigger(t, issue480SlowMarkFunction)

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
	if !h.marked(t, id) {
		t.Fatalf("connection %d not marked deleted", id)
	}
}

// TestDeleteConnection_HandlerResponses covers the handler's remaining
// branches against a real datastore.
func TestDeleteConnection_HandlerResponses(t *testing.T) {
	const id, owner = 7481, "issue480_resp_owner"

	t.Run("owner delete marks the row and nudges the purge", func(t *testing.T) {
		h := newDeleteConnectionHarness(t, id, owner)
		nudges := 0
		h.handler.SetPurgeNudge(func() { nudges++ })
		req := h.request(t, context.Background(), owner, nil)
		rec := httptest.NewRecorder()
		h.handler.deleteConnection(rec, req, id)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204. Body: %s", rec.Code, rec.Body.String())
		}
		if !h.marked(t, id) {
			t.Fatal("connection not marked deleted")
		}
		if nudges != 1 {
			t.Fatalf("purge nudged %d times, want 1", nudges)
		}

		// The connection is now gone to callers, so a second delete
		// answers as for any missing connection.
		rec = httptest.NewRecorder()
		h.handler.deleteConnection(rec, req, id)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("repeat delete status = %d, want 404. Body: %s",
				rec.Code, rec.Body.String())
		}
	})

	t.Run("manage_connections deletes another user's visible connection", func(t *testing.T) {
		h := newDeleteConnectionHarness(t, id, owner)
		req := h.request(t, context.Background(), "issue480_admin",
			[]string{auth.PermManageConnections})
		rec := httptest.NewRecorder()
		h.handler.deleteConnection(rec, req, id)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204. Body: %s", rec.Code, rec.Body.String())
		}
		if !h.marked(t, id) {
			t.Fatal("connection not marked deleted")
		}
	})

	// manage_connections does not reach a connection the caller cannot
	// see, which answers as a missing one does (issue #710).
	t.Run("manage_connections cannot delete another user's private connection", func(t *testing.T) {
		h := newDeleteConnectionHarness(t, id, owner).withSharingLookup()
		req := h.request(t, context.Background(), "issue480_hidden_admin",
			[]string{auth.PermManageConnections})
		rec := httptest.NewRecorder()
		h.handler.deleteConnection(rec, req, id)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404. Body: %s", rec.Code, rec.Body.String())
		}
		if h.marked(t, id) {
			t.Fatal("hidden connection was marked deleted")
		}
		if n := h.deleteDenials(t); n != 0 {
			t.Fatalf("hidden connection recorded %d refusals, want 0", n)
		}
	})

	t.Run("visible connection without permission is an audited 403", func(t *testing.T) {
		h := newDeleteConnectionHarness(t, id, owner)
		req := h.request(t, context.Background(), "issue480_other", nil)
		rec := httptest.NewRecorder()
		h.handler.deleteConnection(rec, req, id)
		assertError(t, rec, http.StatusForbidden, connectionDeleteDenied)
		if h.marked(t, id) {
			t.Fatal("connection marked deleted despite 403")
		}
		assertDenialRecorded(t, h.store, "connection.delete", connectionDeleteDenied)
	})

	t.Run("unseen connection is an unaudited 404", func(t *testing.T) {
		h := newDeleteConnectionHarness(t, id, owner).withSharingLookup()
		req := h.request(t, context.Background(), "issue480_stranger", nil)
		rec := httptest.NewRecorder()
		h.handler.deleteConnection(rec, req, id)
		assertError(t, rec, http.StatusNotFound, "Connection not found")
		if h.marked(t, id) {
			t.Fatal("connection marked deleted despite 404")
		}
		if n := h.deleteDenials(t); n != 0 {
			t.Fatalf("recorded %d denials for an unseen connection, want 0", n)
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

	t.Run("a delete that loses the race is not found", func(t *testing.T) {
		h := newDeleteConnectionHarness(t, id, owner)
		nudges := 0
		h.handler.SetPurgeNudge(func() { nudges++ })
		h.installMarkTrigger(t, issue480SkippingMarkFunction)
		req := h.request(t, context.Background(), owner, nil)
		rec := httptest.NewRecorder()
		h.handler.deleteConnection(rec, req, id)
		assertError(t, rec, http.StatusNotFound, "Connection not found")
		if nudges != 0 {
			t.Fatalf("purge nudged %d times after a 404, want 0", nudges)
		}
	})

	t.Run("datastore failure is a 500", func(t *testing.T) {
		h := newDeleteConnectionHarness(t, id, owner)
		h.installMarkTrigger(t, issue480FailingMarkFunction)
		req := h.request(t, context.Background(), owner, nil)
		rec := httptest.NewRecorder()
		h.handler.deleteConnection(rec, req, id)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500. Body: %s", rec.Code, rec.Body.String())
		}
		if h.marked(t, id) {
			t.Fatal("connection marked deleted despite failure")
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
