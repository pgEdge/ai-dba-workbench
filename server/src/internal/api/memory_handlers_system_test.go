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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/memory"
)

// memoryHandlerSchema holds the chat_memories fixture in a schema of
// its own, so that these tests cannot collide with the memory package's
// tests, which create the table in public, when the packages run at
// once.
const memoryHandlerSchema = "ai_wb_memory_handler_fixture"

// systemMemoryRefusal is the reason given, and audited, when a caller
// without store_system_memory pins, unpins or deletes a system memory.
var systemMemoryRefusal = permissionRefusal(auth.PermStoreSystemMemory)

const memoryHandlerSystemFixture = `
CREATE EXTENSION IF NOT EXISTS vector;
DROP SCHEMA IF EXISTS ai_wb_memory_handler_fixture CASCADE;
CREATE SCHEMA ai_wb_memory_handler_fixture;
CREATE TABLE ai_wb_memory_handler_fixture.chat_memories (
    id BIGSERIAL PRIMARY KEY,
    username TEXT NOT NULL,
    scope TEXT NOT NULL,
    category TEXT NOT NULL,
    content TEXT NOT NULL,
    pinned BOOLEAN NOT NULL DEFAULT FALSE,
    embedding halfvec(4000),
    model_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE FUNCTION ai_wb_memory_handler_fixture.refuse_write() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'write refused by the test fixture';
END;
$$;
CREATE TRIGGER refuse_write
    BEFORE UPDATE OR DELETE ON ai_wb_memory_handler_fixture.chat_memories
    FOR EACH ROW WHEN (OLD.content = 'refuse-write')
    EXECUTE FUNCTION ai_wb_memory_handler_fixture.refuse_write();
`

// memoryHandlerEnv is a memory handler over a Postgres memory store and
// a SQLite auth store whose refusals are recorded by a real RBAC
// handler, with a mux that routes to it as the server does.
type memoryHandlerEnv struct {
	handler *MemoryHandler
	mux     *http.ServeMux
	store   *memory.Store
	pool    *pgxpool.Pool
	auth    *auth.AuthStore
	userIDs map[string]int64
}

// newMemoryHandlerEnv builds the environment, with "alice" and "bob" as
// plain users and "keeper" in a group holding store_system_memory. It
// skips when no test database is configured.
func newMemoryHandlerEnv(t *testing.T) *memoryHandlerEnv {
	t.Helper()
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping database test")
	}

	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("Could not parse test database connection string: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = memoryHandlerSchema + ", public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("Could not connect to test database: %v", err)
	}
	if _, err := pool.Exec(ctx, memoryHandlerSystemFixture); err != nil {
		pool.Close()
		t.Fatalf("Failed to create the memory fixture: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			"DROP SCHEMA IF EXISTS "+memoryHandlerSchema+" CASCADE"); err != nil {
			t.Logf("Failed to drop the memory fixture: %v", err)
		}
		pool.Close()
	})

	rbac, store, cleanup := createTestRBACHandler(t)
	t.Cleanup(cleanup)

	env := &memoryHandlerEnv{
		store:   memory.NewStore(pool),
		pool:    pool,
		auth:    store,
		userIDs: map[string]int64{},
	}
	for _, name := range []string{"alice", "bob", "keeper"} {
		if err := store.CreateUser(name, "Password1234", "", "", ""); err != nil {
			t.Fatalf("CreateUser %s: %v", name, err)
		}
		id, err := store.GetUserID(name)
		if err != nil {
			t.Fatalf("GetUserID %s: %v", name, err)
		}
		env.userIDs[name] = id
	}
	groupID, err := store.CreateGroup("memory-keepers", "")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if err := store.AddUserToGroup(groupID, env.userIDs["keeper"]); err != nil {
		t.Fatalf("AddUserToGroup: %v", err)
	}
	if err := store.GrantAdminPermission(groupID, auth.PermStoreSystemMemory); err != nil {
		t.Fatalf("GrantAdminPermission: %v", err)
	}

	env.handler = NewMemoryHandler(env.store, store, auth.NewRBACChecker(store))
	env.handler.SetDenialRecorder(rbac.RecordDenial)
	env.mux = http.NewServeMux()
	env.handler.RegisterRoutes(env.mux, func(next http.HandlerFunc) http.HandlerFunc {
		return next
	})
	return env
}

// seed stores a memory and returns its ID.
func (e *memoryHandlerEnv) seed(t *testing.T, username, scope string) int64 {
	t.Helper()
	return e.seedContent(t, username, scope, "content")
}

// seedContent stores a memory with the given content and returns its
// ID; content "refuse-write" makes every later update or delete of it
// fail.
func (e *memoryHandlerEnv) seedContent(t *testing.T, username, scope, content string) int64 {
	t.Helper()
	mem, err := e.store.Store(context.Background(), username, scope,
		"note", content, false, nil, "")
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	return mem.ID
}

// pinned reports whether the memory is pinned.
func (e *memoryHandlerEnv) pinned(t *testing.T, id int64) bool {
	t.Helper()
	mem, err := e.store.GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	return mem.Pinned
}

// serve issues a request through the mux as username, or with no
// identity when username is empty.
func (e *memoryHandlerEnv) serve(username, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if username != "" {
		ctx := context.WithValue(req.Context(), auth.IsSuperuserContextKey, false)
		ctx = context.WithValue(ctx, auth.UserIDContextKey, e.userIDs[username])
		ctx = context.WithValue(ctx, auth.UsernameContextKey, username)
		req = req.WithContext(ctx)
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	return rec
}

// assertMemoryDenial checks the single denied row for action: its
// reason, its actor and the memory it names.
func assertMemoryDenial(t *testing.T, store *auth.AuthStore, action, reason string, id int64) {
	t.Helper()
	events, _, err := store.ListAuditEvents(auth.AuditFilter{
		Action: action, Outcome: string(auth.OutcomeDenied)})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Expected 1 %s denial, got %d", action, len(events))
	}
	if events[0].Error != reason || events[0].ActorName != "alice" {
		t.Errorf("Unexpected denial: reason %q, actor %q", events[0].Error,
			events[0].ActorName)
	}
	var details struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal(events[0].Details, &details); err != nil {
		t.Fatalf("Failed to decode details %s: %v", events[0].Details, err)
	}
	if want := "memories/" + strconv.FormatInt(id, 10); details.Target != want {
		t.Errorf("Expected target %q, got %q", want, details.Target)
	}
}

// assertNoDenial checks that nothing was recorded for action.
func assertNoDenial(t *testing.T, store *auth.AuthStore, action string) {
	t.Helper()
	events, _, err := store.ListAuditEvents(auth.AuditFilter{
		Action: action, Outcome: string(auth.OutcomeDenied)})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("Expected no %s denial, got %d", action, len(events))
	}
}

// TestMemoryHandler_UpdatePinned covers PATCH /api/v1/memories/{id} for
// each kind of memory and caller (issue #704): the caller's own memory,
// another user's memory and a missing one, which answer alike, and a
// system memory with and without store_system_memory, whose refusal is
// recorded as memory.update on the memory.
func TestMemoryHandler_UpdatePinned(t *testing.T) {
	env := newMemoryHandlerEnv(t)
	own := env.seed(t, "alice", "user")
	others := env.seed(t, "bob", "user")
	system := env.seed(t, "keeper", "system")
	pin := `{"pinned": true}`

	rec := env.serve("alice", http.MethodPatch, memoryPath(own), pin)
	assertStatus(t, rec, http.StatusOK)
	if !env.pinned(t, own) {
		t.Error("Expected the caller's own memory pinned")
	}

	for _, id := range []int64{others, 999999} {
		rec = env.serve("alice", http.MethodPatch, memoryPath(id), pin)
		assertError(t, rec, http.StatusNotFound, "Memory not found or not accessible")
	}
	if env.pinned(t, others) {
		t.Error("Expected another user's memory left unpinned")
	}

	rec = env.serve("alice", http.MethodPatch, memoryPath(system), pin)
	assertError(t, rec, http.StatusForbidden, systemMemoryRefusal)
	if env.pinned(t, system) {
		t.Error("Expected the system memory left unpinned")
	}
	assertMemoryDenial(t, env.auth, "memory.update", systemMemoryRefusal, system)

	rec = env.serve("keeper", http.MethodPatch, memoryPath(system), pin)
	assertStatus(t, rec, http.StatusOK)
	if !env.pinned(t, system) {
		t.Error("Expected the system memory pinned by a permitted caller")
	}
	rec = env.serve("keeper", http.MethodPatch, memoryPath(system), `{"pinned": false}`)
	assertStatus(t, rec, http.StatusOK)
	if env.pinned(t, system) {
		t.Error("Expected the system memory unpinned by a permitted caller")
	}

	rec = env.serve("", http.MethodPatch, memoryPath(own), pin)
	assertStatus(t, rec, http.StatusUnauthorized)
	rec = env.serve("alice", http.MethodPatch, memoryPath(own), "{")
	assertStatus(t, rec, http.StatusBadRequest)
}

// TestMemoryHandler_Delete covers DELETE /api/v1/memories/{id}, whose
// refusal of a system memory is now recorded as memory.delete.
func TestMemoryHandler_Delete(t *testing.T) {
	env := newMemoryHandlerEnv(t)
	own := env.seed(t, "alice", "user")
	others := env.seed(t, "bob", "user")
	system := env.seed(t, "keeper", "system")

	rec := env.serve("alice", http.MethodDelete, memoryPath(system), "")
	assertError(t, rec, http.StatusForbidden, systemMemoryRefusal)
	assertMemoryDenial(t, env.auth, "memory.delete", systemMemoryRefusal, system)

	rec = env.serve("alice", http.MethodDelete, memoryPath(others), "")
	assertError(t, rec, http.StatusNotFound, "Memory not found")
	rec = env.serve("alice", http.MethodDelete, memoryPath(999999), "")
	assertError(t, rec, http.StatusNotFound, "Memory not found")

	rec = env.serve("alice", http.MethodDelete, memoryPath(own), "")
	assertStatus(t, rec, http.StatusOK)
	rec = env.serve("keeper", http.MethodDelete, memoryPath(system), "")
	assertStatus(t, rec, http.StatusOK)

	rec = env.serve("", http.MethodDelete, memoryPath(others), "")
	assertStatus(t, rec, http.StatusUnauthorized)
}

// TestMemoryHandler_ListAndRouting covers the list route and the
// dispatch of /api/v1/memories/{id}; none of these records a denial.
func TestMemoryHandler_ListAndRouting(t *testing.T) {
	env := newMemoryHandlerEnv(t)
	own := env.seed(t, "alice", "user")
	env.seed(t, "bob", "user")

	rec := env.serve("alice", http.MethodGet, "/api/v1/memories", "")
	assertStatus(t, rec, http.StatusOK)
	var body struct {
		Memories []memory.Memory `json:"memories"`
	}
	decodeInto(t, rec, &body)
	if len(body.Memories) != 1 || body.Memories[0].ID != own {
		t.Errorf("Expected only the caller's memory, got %+v", body.Memories)
	}

	rec = env.serve("bob", http.MethodGet, "/api/v1/memories?category=none", "")
	assertStatus(t, rec, http.StatusOK)
	decodeInto(t, rec, &body)
	if body.Memories == nil || len(body.Memories) != 0 {
		t.Errorf("Expected an empty list, got %+v", body.Memories)
	}

	tests := []struct {
		name, method, path string
		want               int
	}{
		{"list unauthenticated", http.MethodGet, "/api/v1/memories", http.StatusUnauthorized},
		{"list wrong method", http.MethodPost, "/api/v1/memories", http.StatusMethodNotAllowed},
		{"missing id", http.MethodPatch, "/api/v1/memories/", http.StatusBadRequest},
		{"invalid id", http.MethodPatch, "/api/v1/memories/abc", http.StatusBadRequest},
		{"wrong method", http.MethodGet, memoryPath(own), http.StatusMethodNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			username := "alice"
			if tt.name == "list unauthenticated" {
				username = ""
			}
			assertStatus(t, env.serve(username, tt.method, tt.path, ""), tt.want)
		})
	}
	assertNoDenial(t, env.auth, "memory.update")
	assertNoDenial(t, env.auth, "memory.delete")
}

// TestMemoryHandler_StoreErrors checks that a failing memory store
// answers 500 rather than a refusal or a not-found.
func TestMemoryHandler_StoreErrors(t *testing.T) {
	env := newMemoryHandlerEnv(t)
	id := env.seed(t, "alice", "user")

	// A write that fails after the memory was fetched.
	for _, tc := range []struct{ username, scope string }{
		{"alice", "user"}, {"keeper", "system"},
	} {
		failing := env.seedContent(t, tc.username, tc.scope, "refuse-write")
		rec := env.serve(tc.username, http.MethodPatch, memoryPath(failing), `{"pinned": true}`)
		assertError(t, rec, http.StatusInternalServerError, "Failed to update memory")
		rec = env.serve(tc.username, http.MethodDelete, memoryPath(failing), "")
		assertError(t, rec, http.StatusInternalServerError, "Failed to delete memory")
	}

	// A store that cannot be reached at all.
	env.pool.Close()

	rec := env.serve("alice", http.MethodGet, "/api/v1/memories", "")
	assertError(t, rec, http.StatusInternalServerError, "Failed to list memories")
	rec = env.serve("alice", http.MethodPatch, memoryPath(id), `{"pinned": true}`)
	assertError(t, rec, http.StatusInternalServerError, "Failed to update memory")
	rec = env.serve("alice", http.MethodDelete, memoryPath(id), "")
	assertError(t, rec, http.StatusInternalServerError, "Failed to fetch memory")
}

// TestMemoryHandler_NotConfigured checks that the routes answer 503
// when no memory store is configured.
func TestMemoryHandler_NotConfigured(t *testing.T) {
	mux := http.NewServeMux()
	NewMemoryHandler(nil, nil, nil).RegisterRoutes(mux,
		func(next http.HandlerFunc) http.HandlerFunc { return next })
	for _, path := range []string{"/api/v1/memories", "/api/v1/memories/1"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		assertStatus(t, rec, http.StatusServiceUnavailable)
	}
}
