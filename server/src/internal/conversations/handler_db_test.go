/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package conversations

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// conversationTableDDL creates the conversations table as a temporary
// table (pg_temp). PostgreSQL searches a session's temporary schema
// first, so the unqualified name the store uses resolves to it, and
// nothing outside the fixture's one session can see or disturb it.
const conversationTableDDL = `
CREATE TABLE pg_temp.conversations (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    title TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    connection TEXT NOT NULL DEFAULT '',
    messages JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

// newConversationUpdateFixture returns a handler over a fresh
// conversations table and a session token for each named user. The
// pool holds a single connection for the life of the test, so every
// query runs in the session that owns the temporary table, which is
// dropped when that connection closes; concurrent runs against one
// database cannot disturb each other or a table another package's
// tests use.
func newConversationUpdateFixture(t *testing.T, users ...string) (*Handler, *Store, map[string]string) {
	t.Helper()

	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" || os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping conversation update test")
	}

	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.MinConns = 1
	cfg.MaxConns = 1
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = time.Hour
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to connect to the test database: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, conversationTableDDL); err != nil {
		t.Fatalf("Failed to create the conversations table: %v", err)
	}
	// The store's unqualified name must reach the temporary table, not a
	// permanent one, or the tests below would write to and drop it.
	var inTemp bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('conversations') =
        to_regclass('pg_temp.conversations')`).Scan(&inTemp); err != nil {
		t.Fatalf("Failed to resolve the conversations table: %v", err)
	}
	if !inTemp {
		t.Fatal("The conversations table does not resolve to pg_temp")
	}

	authStore, err := auth.NewAuthStore(t.TempDir(), 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to create auth store: %v", err)
	}
	t.Cleanup(func() { authStore.Close() })
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	tokens := make(map[string]string, len(users))
	for _, user := range users {
		if err := authStore.CreateUser(user, "Password1234", "", "", ""); err != nil {
			t.Fatalf("CreateUser %s: %v", user, err)
		}
		token, _, err := authStore.AuthenticateUser(user, "Password1234")
		if err != nil {
			t.Fatalf("AuthenticateUser %s: %v", user, err)
		}
		tokens[user] = token
	}

	store := NewStore(pool)
	return NewHandler(store, authStore), store, tokens
}

// TestHandleUpdate_OtherUsersConversationLooksMissing checks that PUT
// /api/v1/conversations/{id} answers another user's conversation
// exactly as it answers a missing one, as rename and delete do, and
// leaves the conversation unchanged (issue #649).
func TestHandleUpdate_OtherUsersConversationLooksMissing(t *testing.T) {
	h, store, tokens := newConversationUpdateFixture(t, "conv_owner", "conv_other")
	ctx := context.Background()

	conv, err := store.Create(ctx, "conv_owner", "provider", "model", "",
		[]Message{{Role: "user", Content: "Original question"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	put := func(token, id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/conversations/"+id,
			strings.NewReader(`{"provider":"changed","model":"changed","messages":[]}`))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.HandleUpdate(rec, req)
		return rec
	}

	missing := put(tokens["conv_other"], "conv_does_not_exist")
	other := put(tokens["conv_other"], conv.ID)
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"missing": missing, "other user's": other} {
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s conversation: status = %d, want 404. Body: %s",
				name, rec.Code, rec.Body.String())
		}
	}
	if other.Body.String() != missing.Body.String() {
		t.Errorf("Other user's conversation body %q differs from missing body %q",
			other.Body.String(), missing.Body.String())
	}

	unchanged, err := store.Get(ctx, conv.ID, "conv_owner")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if unchanged.Provider != "provider" || len(unchanged.Messages) != 1 {
		t.Errorf("Conversation changed by a refused update: %+v", unchanged)
	}

	if rec := put(tokens["conv_owner"], conv.ID); rec.Code != http.StatusOK {
		t.Fatalf("Owner update: status = %d, want 200. Body: %s",
			rec.Code, rec.Body.String())
	}
	updated, err := store.Get(ctx, conv.ID, "conv_owner")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if updated.Provider != "changed" {
		t.Errorf("Provider = %q after the owner's update, want %q",
			updated.Provider, "changed")
	}
}

// conversationRouteCase is one request through RegisterRoutes and the
// response it must receive.
type conversationRouteCase struct {
	name   string
	user   string // a key into the fixture's tokens; empty sends a bad token
	method string
	path   string
	body   string
	status int
	msg    string // a substring the body must contain; empty skips it
}

// serveConversationRoute routes one request through RegisterRoutes with
// an identity auth wrapper, as the server does after authentication.
func serveConversationRoute(h *Handler, token, method, path, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	h.RegisterRoutes(mux, func(next http.HandlerFunc) http.HandlerFunc { return next })

	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// runConversationRoutes issues each case in order; later cases may
// depend on the writes of earlier ones.
func runConversationRoutes(t *testing.T, h *Handler, tokens map[string]string,
	cases []conversationRouteCase) {

	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := "not-a-session-token"
			if tc.user != "" {
				token = tokens[tc.user]
			}
			rec := serveConversationRoute(h, token, tc.method, tc.path, tc.body)
			if rec.Code != tc.status {
				t.Fatalf("Expected status %d, got %d: %s", tc.status, rec.Code,
					rec.Body.String())
			}
			if tc.msg != "" && !strings.Contains(rec.Body.String(), tc.msg) {
				t.Errorf("Expected body to contain %q, got %s", tc.msg,
					rec.Body.String())
			}
		})
	}
}

// TestConversationRoutes covers each conversation route end to end
// against Postgres: the success paths, the validation and ownership
// refusals, and the 500s once the table is gone.
func TestConversationRoutes(t *testing.T) {
	h, store, tokens := newConversationUpdateFixture(t, "conv_owner", "conv_other")
	ctx := context.Background()

	conv, err := store.Create(ctx, "conv_owner", "provider", "model", "",
		[]Message{{Role: "user", Content: strings.Repeat("q", 120)}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	one := "/api/v1/conversations/" + conv.ID
	const list = "/api/v1/conversations"
	const created = `{"provider":"p","model":"m","messages":[{"role":"user","content":"Hello"}]}`

	runConversationRoutes(t, h, tokens, []conversationRouteCase{
		{name: "bad session", method: http.MethodGet, path: list,
			status: http.StatusUnauthorized, msg: "invalid or expired session"},
		{name: "list method not allowed", user: "conv_owner",
			method: http.MethodPut, path: list,
			status: http.StatusMethodNotAllowed},
		{name: "item method not allowed", user: "conv_owner",
			method: http.MethodPost, path: one,
			status: http.StatusMethodNotAllowed},

		{name: "create malformed body", user: "conv_owner",
			method: http.MethodPost, path: list, body: `{bad`,
			status: http.StatusBadRequest, msg: "Invalid request body"},
		{name: "create without messages", user: "conv_owner",
			method: http.MethodPost, path: list, body: `{"messages":[]}`,
			status: http.StatusBadRequest, msg: "Messages required"},
		{name: "created", user: "conv_owner", method: http.MethodPost,
			path: list, body: created, status: http.StatusCreated, msg: "Hello"},

		{name: "list with paging", user: "conv_owner", method: http.MethodGet,
			path: list + "?limit=10&offset=0", status: http.StatusOK,
			msg: strings.Repeat("q", 97) + "..."},
		{name: "list ignores bad paging", user: "conv_owner",
			method: http.MethodGet, path: list + "?limit=x&offset=-1",
			status: http.StatusOK, msg: "Hello"},
		{name: "empty list", user: "conv_other", method: http.MethodGet,
			path: list, status: http.StatusOK, msg: `"conversations":[]`},

		{name: "get without id", user: "conv_owner", method: http.MethodGet,
			path: list + "/", status: http.StatusBadRequest},
		{name: "get", user: "conv_owner", method: http.MethodGet, path: one,
			status: http.StatusOK, msg: conv.ID},
		{name: "get other user's", user: "conv_other", method: http.MethodGet,
			path: one, status: http.StatusNotFound},

		{name: "update without id", user: "conv_owner", method: http.MethodPut,
			path: list + "/", body: `{}`, status: http.StatusBadRequest},
		{name: "update malformed body", user: "conv_owner",
			method: http.MethodPut, path: one, body: `{bad`,
			status: http.StatusBadRequest},

		{name: "rename without id", user: "conv_owner", method: http.MethodPatch,
			path: list + "/", body: `{"title":"T"}`, status: http.StatusBadRequest},
		{name: "rename malformed body", user: "conv_owner",
			method: http.MethodPatch, path: one, body: `{bad`,
			status: http.StatusBadRequest},
		{name: "rename without title", user: "conv_owner",
			method: http.MethodPatch, path: one, body: `{"title":""}`,
			status: http.StatusBadRequest, msg: "Title required"},
		{name: "rename other user's", user: "conv_other",
			method: http.MethodPatch, path: one, body: `{"title":"Taken"}`,
			status: http.StatusNotFound},
		{name: "renamed", user: "conv_owner", method: http.MethodPatch,
			path: one, body: `{"title":"Renamed"}`, status: http.StatusOK},

		{name: "delete without id", user: "conv_owner",
			method: http.MethodDelete, path: list + "/",
			status: http.StatusBadRequest},
		{name: "delete other user's", user: "conv_other",
			method: http.MethodDelete, path: one, status: http.StatusNotFound},
		{name: "deleted", user: "conv_owner", method: http.MethodDelete,
			path: one, status: http.StatusOK},
		{name: "delete all needs the flag", user: "conv_owner",
			method: http.MethodDelete, path: list,
			status: http.StatusBadRequest},
		{name: "deleted all", user: "conv_owner", method: http.MethodDelete,
			path: list + "?all=true", status: http.StatusOK,
			msg: `"deleted":1`},
	})

	// With the table gone every datastore call fails. The name is
	// qualified so that only the fixture's temporary table can be
	// dropped, even if the pool's connection were ever replaced.
	if _, err := store.pool.Exec(ctx, "DROP TABLE pg_temp.conversations"); err != nil {
		t.Fatalf("Drop the conversations table: %v", err)
	}
	runConversationRoutes(t, h, tokens, []conversationRouteCase{
		{name: "list fails", user: "conv_owner", method: http.MethodGet,
			path: list, status: http.StatusInternalServerError},
		{name: "create fails", user: "conv_owner", method: http.MethodPost,
			path: list, body: created, status: http.StatusInternalServerError},
		{name: "get fails", user: "conv_owner", method: http.MethodGet,
			path: one, status: http.StatusInternalServerError},
		{name: "update fails", user: "conv_owner", method: http.MethodPut,
			path: one, body: `{"messages":[]}`,
			status: http.StatusInternalServerError},
		{name: "rename fails", user: "conv_owner", method: http.MethodPatch,
			path: one, body: `{"title":"T"}`,
			status: http.StatusInternalServerError},
		{name: "delete fails", user: "conv_owner", method: http.MethodDelete,
			path: one, status: http.StatusInternalServerError},
		{name: "delete all fails", user: "conv_owner",
			method: http.MethodDelete, path: list + "?all=true",
			status: http.StatusInternalServerError},
	})
}
