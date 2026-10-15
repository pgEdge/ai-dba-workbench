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
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/memory"
)

// memoryHandlerTestSchema is the private schema the memory handler
// tests create their chat_memories table in, so that they cannot clash
// with the memory package's own tests on the same database.
const memoryHandlerTestSchema = "api_memory_handler_test"

// memoryHandlerTestDDL creates chat_memories without the embedding
// column, which none of the handler's queries read, so the tests do not
// need pgvector. The trigger lets a test make one row fail to delete
// ("undeletable") or vanish between the lookup and the delete
// ("vanishing").
const memoryHandlerTestDDL = `
CREATE TABLE chat_memories (
    id BIGSERIAL PRIMARY KEY,
    username TEXT NOT NULL,
    scope TEXT NOT NULL,
    category TEXT NOT NULL,
    content TEXT NOT NULL,
    pinned BOOLEAN NOT NULL DEFAULT FALSE,
    model_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE FUNCTION chat_memories_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.content = 'undeletable' THEN
        RAISE EXCEPTION 'delete refused by test trigger';
    END IF;
    IF OLD.content = 'vanishing' THEN
        RETURN NULL;
    END IF;
    RETURN OLD;
END;
$$;
CREATE TRIGGER chat_memories_guard BEFORE DELETE ON chat_memories
    FOR EACH ROW EXECUTE FUNCTION chat_memories_guard();
`

// memoryHandlerFixture is a memory handler over a real chat_memories
// table, with an auth store for its permission checks.
type memoryHandlerFixture struct {
	handler *MemoryHandler
	pool    *pgxpool.Pool
	store   *auth.AuthStore
	rc      *auth.RBACChecker
	plain   scopeCaller
}

// newMemoryHandlerFixture builds the fixture against the
// TEST_AI_WORKBENCH_SERVER database, skipping when it is not set.
func newMemoryHandlerFixture(t *testing.T) *memoryHandlerFixture {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping database test")
	}

	ctx := context.Background()
	admin, err := pgx.Connect(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()

	schema := pgx.Identifier{memoryHandlerTestSchema}.Sanitize()
	if _, err := admin.Exec(ctx, "DROP SCHEMA IF EXISTS "+schema+
		" CASCADE; CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("Failed to create the test schema: %v", err)
	}
	t.Cleanup(func() {
		conn, err := pgx.Connect(context.Background(), connStr)
		if err != nil {
			t.Logf("Failed to reconnect to drop the test schema: %v", err)
			return
		}
		defer func() { _ = conn.Close(context.Background()) }()
		if _, err := conn.Exec(context.Background(),
			"DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
			t.Logf("Failed to drop the test schema: %v", err)
		}
	})

	cfg, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = memoryHandlerTestSchema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, memoryHandlerTestDDL); err != nil {
		t.Fatalf("Failed to create chat_memories: %v", err)
	}

	_, store, cleanup := createTestRBACHandler(t)
	t.Cleanup(cleanup)
	rc := auth.NewRBACChecker(store)

	return &memoryHandlerFixture{
		handler: NewMemoryHandler(memory.NewStore(pool), store, rc),
		pool:    pool,
		store:   store,
		rc:      rc,
		plain:   plainCaller(t, store, "memory_plain"),
	}
}

// seed inserts a memory and returns its id.
func (f *memoryHandlerFixture) seed(t *testing.T, username, scope,
	content string) int64 {

	t.Helper()
	var id int64
	if err := f.pool.QueryRow(context.Background(),
		`INSERT INTO chat_memories (username, scope, category, content)
         VALUES ($1, $2, 'note', $3) RETURNING id`,
		username, scope, content).Scan(&id); err != nil {
		t.Fatalf("Failed to seed a memory: %v", err)
	}
	return id
}

// exists reports whether the memory with id is still stored.
func (f *memoryHandlerFixture) exists(t *testing.T, id int64) bool {
	t.Helper()
	var found bool
	if err := f.pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM chat_memories WHERE id = $1)`,
		id).Scan(&found); err != nil {
		t.Fatalf("Failed to look up memory %d: %v", id, err)
	}
	return found
}

// serve sends a request to the handler's routes as caller.
func (f *memoryHandlerFixture) serve(caller scopeCaller, method, path,
	body string) *httptest.ResponseRecorder {

	return serveRoutes(f.handler.RegisterRoutes, caller, method, path, body)
}

// memorySuperuser is a superuser session named memory_admin.
var memorySuperuser = scopeCaller{name: "memory_admin",
	wrap: func(r *http.Request) *http.Request {
		return withUsername(withSuperuser(r), "memory_admin")
	}}

// memoryPath is the route for the memory with id.
func memoryPath(id int64) string {
	return fmt.Sprintf("/api/v1/memories/%d", id)
}

// TestMemoryHandlerNotConfigured checks that both routes answer 503
// when no memory store is configured.
func TestMemoryHandlerNotConfigured(t *testing.T) {
	h := NewMemoryHandler(nil, nil, nil)
	for _, path := range []string{"/api/v1/memories", "/api/v1/memories/1"} {
		rec := serveRoutes(h.RegisterRoutes, anonymousCaller,
			http.MethodGet, path, "")
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: expected 503, got %d", path, rec.Code)
		}
	}
}

// TestMemoryHandlerList checks the list route's method, authentication
// and result handling.
func TestMemoryHandlerList(t *testing.T) {
	f := newMemoryHandlerFixture(t)
	f.seed(t, f.plain.name, "user", "mine")
	f.seed(t, "someone_else", "user", "theirs")
	f.seed(t, "someone_else", "system", "shared")

	t.Run("wrong method", func(t *testing.T) {
		rec := f.serve(f.plain, http.MethodPost, "/api/v1/memories", "")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("Expected 405, got %d", rec.Code)
		}
	})

	t.Run("no username", func(t *testing.T) {
		rec := f.serve(anonymousCaller, http.MethodGet, "/api/v1/memories", "")
		assertError(t, rec, http.StatusUnauthorized, "Authentication required")
	})

	t.Run("own and system memories", func(t *testing.T) {
		rec := f.serve(f.plain, http.MethodGet, "/api/v1/memories", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var got struct {
			Memories []memory.Memory `json:"memories"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("Failed to decode the response: %v", err)
		}
		if len(got.Memories) != 2 {
			t.Fatalf("Expected 2 memories, got %+v", got.Memories)
		}
	})

	t.Run("none to list", func(t *testing.T) {
		rec := f.serve(f.plain, http.MethodGet,
			"/api/v1/memories?category=absent", "")
		if rec.Code != http.StatusOK ||
			rec.Body.String() != "{\"memories\":[]}\n" {
			t.Fatalf("Expected an empty list, got %d: %q", rec.Code,
				rec.Body.String())
		}
	})

	t.Run("store failure", func(t *testing.T) {
		f.pool.Close()
		rec := f.serve(f.plain, http.MethodGet, "/api/v1/memories", "")
		assertError(t, rec, http.StatusInternalServerError,
			"Failed to list memories")
	})
}

// TestMemoryHandlerByIDRouting checks the id parsing and method
// dispatch of /api/v1/memories/{id}.
func TestMemoryHandlerByIDRouting(t *testing.T) {
	f := newMemoryHandlerFixture(t)

	cases := []struct {
		method string
		path   string
		code   int
		msg    string
	}{
		{http.MethodDelete, "/api/v1/memories/", http.StatusBadRequest,
			"Memory ID is required"},
		{http.MethodDelete, "/api/v1/memories/abc", http.StatusBadRequest,
			"Invalid memory ID"},
		{http.MethodGet, "/api/v1/memories/1", http.StatusMethodNotAllowed,
			"Method not allowed"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := f.serve(f.plain, tc.method, tc.path, "")
			assertError(t, rec, tc.code, tc.msg)
		})
	}
}

// TestMemoryHandlerDelete checks deleting user and system memories,
// including that a refusal for a missing store_system_memory permission
// is audited (issue #648).
func TestMemoryHandlerDelete(t *testing.T) {
	f := newMemoryHandlerFixture(t)
	reason := permissionRefusal(auth.PermStoreSystemMemory)
	capture := &routedCapture{}
	f.handler.SetDenialRecorder(capture.record)

	t.Run("no username", func(t *testing.T) {
		rec := f.serve(anonymousCaller, http.MethodDelete, memoryPath(1), "")
		assertError(t, rec, http.StatusUnauthorized, "Authentication required")
	})

	t.Run("not found", func(t *testing.T) {
		rec := f.serve(f.plain, http.MethodDelete, memoryPath(999999), "")
		assertError(t, rec, http.StatusNotFound, "Memory not found")
	})

	t.Run("system memory refused and audited", func(t *testing.T) {
		capture.denials = nil
		id := f.seed(t, "someone_else", "system", "shared")
		rec := f.serve(f.plain, http.MethodDelete, memoryPath(id), "")
		assertError(t, rec, http.StatusForbidden, reason)
		capture.expect(t, routedDenial{"memory.delete",
			fmt.Sprintf("memories/%d", id), reason})
		if !f.exists(t, id) {
			t.Error("Expected the refused memory to remain")
		}
	})

	t.Run("system memory refusal recorded in the audit log", func(t *testing.T) {
		f.handler.SetDenialRecorder(NewRBACHandler(f.store, f.rc).RecordDenial)
		t.Cleanup(func() { f.handler.SetDenialRecorder(capture.record) })
		id := f.seed(t, "someone_else", "system", "shared")
		rec := f.serve(f.plain, http.MethodDelete, memoryPath(id), "")
		assertError(t, rec, http.StatusForbidden, reason)
		assertDenialRecorded(t, f.store, "memory.delete", reason)
	})

	okCases := []struct {
		name   string
		caller scopeCaller
		owner  string
		scope  string
	}{
		{"system memory as superuser", memorySuperuser, "someone_else", "system"},
		{"own user memory", f.plain, f.plain.name, "user"},
	}
	for _, tc := range okCases {
		t.Run(tc.name, func(t *testing.T) {
			capture.denials = nil
			id := f.seed(t, tc.owner, tc.scope, "note")
			rec := f.serve(tc.caller, http.MethodDelete, memoryPath(id), "")
			if rec.Code != http.StatusOK {
				t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
			}
			if f.exists(t, id) {
				t.Error("Expected the memory to be deleted")
			}
			capture.expect(t)
		})
	}

	failCases := []struct {
		name    string
		caller  scopeCaller
		owner   string
		scope   string
		content string
		code    int
		msg     string
	}{
		{"someone else's user memory", f.plain, "someone_else", "user",
			"note", http.StatusNotFound, "Memory not found"},
		{"system memory vanishes", memorySuperuser, "someone_else", "system",
			"vanishing", http.StatusNotFound, "Memory not found"},
		{"system memory delete fails", memorySuperuser, "someone_else", "system",
			"undeletable", http.StatusInternalServerError,
			"Failed to delete memory"},
		{"user memory delete fails", f.plain, f.plain.name, "user",
			"undeletable", http.StatusInternalServerError,
			"Failed to delete memory"},
	}
	for _, tc := range failCases {
		t.Run(tc.name, func(t *testing.T) {
			id := f.seed(t, tc.owner, tc.scope, tc.content)
			rec := f.serve(tc.caller, http.MethodDelete, memoryPath(id), "")
			assertError(t, rec, tc.code, tc.msg)
		})
	}

	t.Run("lookup fails", func(t *testing.T) {
		f.pool.Close()
		rec := f.serve(f.plain, http.MethodDelete, memoryPath(1), "")
		assertError(t, rec, http.StatusInternalServerError,
			"Failed to fetch memory")
	})
}

// TestMemoryHandlerUpdatePinned checks the pin route's authentication,
// body, ownership and failure handling.
func TestMemoryHandlerUpdatePinned(t *testing.T) {
	f := newMemoryHandlerFixture(t)
	own := f.seed(t, f.plain.name, "user", "mine")
	theirs := f.seed(t, "someone_else", "user", "theirs")

	t.Run("no username", func(t *testing.T) {
		rec := f.serve(anonymousCaller, http.MethodPatch, memoryPath(own),
			`{"pinned":true}`)
		assertError(t, rec, http.StatusUnauthorized, "Authentication required")
	})

	t.Run("bad body", func(t *testing.T) {
		rec := f.serve(f.plain, http.MethodPatch, memoryPath(own), `{`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("Expected 400, got %d", rec.Code)
		}
	})

	t.Run("own memory", func(t *testing.T) {
		rec := f.serve(f.plain, http.MethodPatch, memoryPath(own),
			`{"pinned":true}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("someone else's memory", func(t *testing.T) {
		rec := f.serve(f.plain, http.MethodPatch, memoryPath(theirs),
			`{"pinned":true}`)
		assertError(t, rec, http.StatusNotFound,
			"Memory not found or not accessible")
	})

	t.Run("store failure", func(t *testing.T) {
		f.pool.Close()
		rec := f.serve(f.plain, http.MethodPatch, memoryPath(own),
			`{"pinned":false}`)
		assertError(t, rec, http.StatusInternalServerError,
			"Failed to update memory")
	})
}
