/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package memory

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// memoryTestSchema mirrors the chat_memories table that store.go reads
// from and writes to. The vector dimension is intentionally small (3) so
// tests can supply tiny float slices and still exercise the cosine
// distance ordering branch.
const memoryTestSchema = `
CREATE EXTENSION IF NOT EXISTS vector;
DROP TABLE IF EXISTS chat_memories CASCADE;
CREATE TABLE chat_memories (
    id BIGSERIAL PRIMARY KEY,
    username TEXT NOT NULL,
    scope TEXT NOT NULL,
    category TEXT NOT NULL,
    content TEXT NOT NULL,
    pinned BOOLEAN NOT NULL DEFAULT FALSE,
    embedding vector(3),
    model_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`

const memoryTestTeardown = `
DROP TABLE IF EXISTS chat_memories CASCADE;
`

// newMemoryTestStore wires up a *Store backed by the
// TEST_AI_WORKBENCH_SERVER Postgres instance and creates the
// chat_memories table. Tests skip cleanly when the database is not
// configured or pgvector is unavailable.
func newMemoryTestStore(t *testing.T) (*Store, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping memory integration test")
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

	if _, err := pool.Exec(ctx, memoryTestSchema); err != nil {
		pool.Close()
		t.Skipf("Failed to create memory test schema (pgvector may be missing): %v", err)
	}

	store := NewStore(pool)

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), memoryTestTeardown); err != nil {
			t.Logf("memory teardown failed: %v", err)
		}
		pool.Close()
	}

	return store, pool, cleanup
}

// truncateMemories removes all rows and resets identity between subtests
// so order-dependent assertions can rely on a clean slate.
func truncateMemories(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`TRUNCATE chat_memories RESTART IDENTITY`); err != nil {
		t.Fatalf("truncate failed: %v", err)
	}
}

func TestStore_Store(t *testing.T) {
	store, pool, cleanup := newMemoryTestStore(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("happy path with embedding", func(t *testing.T) {
		truncateMemories(t, pool)
		mem, err := store.Store(ctx, "alice", "user", "pref",
			"likes coffee", true,
			[]float32{1, 0, 0}, "model-a")
		if err != nil {
			t.Fatalf("Store: %v", err)
		}
		if mem.ID == 0 {
			t.Errorf("expected non-zero id, got %d", mem.ID)
		}
		if mem.Username != "alice" || mem.Scope != "user" ||
			mem.Category != "pref" || mem.Content != "likes coffee" ||
			!mem.Pinned || mem.ModelName != "model-a" {
			t.Errorf("returned memory has wrong fields: %+v", mem)
		}

		// Verify it round-trips through GetByID.
		got, err := store.GetByID(ctx, mem.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.ID != mem.ID || got.Content != "likes coffee" {
			t.Errorf("GetByID returned %+v, want id=%d content=%q",
				got, mem.ID, "likes coffee")
		}
	})

	t.Run("happy path without embedding", func(t *testing.T) {
		truncateMemories(t, pool)
		mem, err := store.Store(ctx, "bob", "user", "fact",
			"plain text only", false, nil, "")
		if err != nil {
			t.Fatalf("Store nil embedding: %v", err)
		}
		if mem.ID == 0 {
			t.Errorf("expected non-zero id")
		}
	})

	t.Run("system scope allows empty username", func(t *testing.T) {
		truncateMemories(t, pool)
		mem, err := store.Store(ctx, "", "system", "policy",
			"global rule", false, []float32{0, 1, 0}, "m")
		if err != nil {
			t.Fatalf("system scope Store: %v", err)
		}
		if mem.Username != "" || mem.Scope != "system" {
			t.Errorf("unexpected fields: %+v", mem)
		}
	})

	t.Run("invalid scope is rejected", func(t *testing.T) {
		truncateMemories(t, pool)
		_, err := store.Store(ctx, "alice", "bogus", "c", "x",
			false, nil, "")
		if err == nil {
			t.Fatalf("expected error for invalid scope")
		}
	})

	t.Run("user scope requires username", func(t *testing.T) {
		truncateMemories(t, pool)
		_, err := store.Store(ctx, "", "user", "c", "x", false, nil, "")
		if err == nil {
			t.Fatalf("expected error for empty username")
		}
	})

	t.Run("blank content is rejected", func(t *testing.T) {
		truncateMemories(t, pool)
		_, err := store.Store(ctx, "alice", "user", "c", "   \t\n",
			false, nil, "")
		if err == nil {
			t.Fatalf("expected error for blank content")
		}
	})

	t.Run("insert error surfaces wrapped", func(t *testing.T) {
		// Force a DB-side failure by passing a wrong-length vector.
		truncateMemories(t, pool)
		_, err := store.Store(ctx, "alice", "user", "c", "hi",
			false, []float32{1, 2, 3, 4, 5}, "m")
		if err == nil {
			t.Fatalf("expected wrapped insert error for bad vector dim")
		}
	})
}

func TestStore_GetByID(t *testing.T) {
	store, pool, cleanup := newMemoryTestStore(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("not found returns ErrNotFound", func(t *testing.T) {
		truncateMemories(t, pool)
		_, err := store.GetByID(ctx, 9999)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("happy path returns full row", func(t *testing.T) {
		truncateMemories(t, pool)
		mem, err := store.Store(ctx, "alice", "user", "k", "v",
			false, []float32{1, 0, 0}, "m")
		if err != nil {
			t.Fatalf("seed Store: %v", err)
		}
		got, err := store.GetByID(ctx, mem.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.Username != "alice" || got.Content != "v" ||
			got.Category != "k" {
			t.Errorf("unexpected fields: %+v", got)
		}
	})
}

func TestStore_Delete(t *testing.T) {
	store, pool, cleanup := newMemoryTestStore(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("owner can delete", func(t *testing.T) {
		truncateMemories(t, pool)
		mem, err := store.Store(ctx, "alice", "user", "k", "v",
			false, nil, "")
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := store.Delete(ctx, mem.ID, "alice"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if _, err := store.GetByID(ctx, mem.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("expected row gone, got %v", err)
		}
	})

	t.Run("wrong username yields ErrNotFound", func(t *testing.T) {
		// Delete only matches on (id, username), so a non-owner sees the
		// same response as a missing row.
		truncateMemories(t, pool)
		mem, err := store.Store(ctx, "alice", "user", "k", "v",
			false, nil, "")
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		err = store.Delete(ctx, mem.ID, "bob")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound for non-owner, got %v", err)
		}
	})

	t.Run("missing id returns ErrNotFound", func(t *testing.T) {
		truncateMemories(t, pool)
		err := store.Delete(ctx, 424242, "alice")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestStore_DeleteByID(t *testing.T) {
	store, pool, cleanup := newMemoryTestStore(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("happy path", func(t *testing.T) {
		truncateMemories(t, pool)
		mem, err := store.Store(ctx, "alice", "user", "k", "v",
			false, nil, "")
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := store.DeleteByID(ctx, mem.ID); err != nil {
			t.Fatalf("DeleteByID: %v", err)
		}
		if _, err := store.GetByID(ctx, mem.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("row not deleted: %v", err)
		}
	})

	t.Run("missing id returns ErrNotFound", func(t *testing.T) {
		truncateMemories(t, pool)
		err := store.DeleteByID(ctx, 7777)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestStore_UpdatePinned(t *testing.T) {
	store, pool, cleanup := newMemoryTestStore(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("owner toggles pinned", func(t *testing.T) {
		truncateMemories(t, pool)
		mem, err := store.Store(ctx, "alice", "user", "k", "v",
			false, nil, "")
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := store.UpdatePinned(ctx, mem.ID, "alice", true); err != nil {
			t.Fatalf("UpdatePinned: %v", err)
		}
		got, err := store.GetByID(ctx, mem.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if !got.Pinned {
			t.Errorf("expected pinned=true after update")
		}
	})

	t.Run("non-owner cannot pin user-scoped memory", func(t *testing.T) {
		// UPDATE matches WHERE id=? AND (username=? OR scope='system').
		// For a user-scoped row owned by alice, bob's update affects 0
		// rows so the call returns ErrNotFound.
		truncateMemories(t, pool)
		mem, err := store.Store(ctx, "alice", "user", "k", "v",
			false, nil, "")
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		err = store.UpdatePinned(ctx, mem.ID, "bob", true)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("any user can pin a system-scoped memory", func(t *testing.T) {
		// system scope short-circuits the username check by design.
		truncateMemories(t, pool)
		mem, err := store.Store(ctx, "", "system", "policy",
			"shared", false, nil, "")
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := store.UpdatePinned(ctx, mem.ID, "anyone", true); err != nil {
			t.Fatalf("UpdatePinned system: %v", err)
		}
		got, err := store.GetByID(ctx, mem.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if !got.Pinned {
			t.Errorf("system memory should be pinned")
		}
	})

	t.Run("missing id returns ErrNotFound", func(t *testing.T) {
		truncateMemories(t, pool)
		err := store.UpdatePinned(ctx, 9999, "alice", true)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
	})
}

func TestStore_GetPinned(t *testing.T) {
	store, pool, cleanup := newMemoryTestStore(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("returns own pinned plus system pinned", func(t *testing.T) {
		truncateMemories(t, pool)

		// Alice owns one pinned + one unpinned.
		if _, err := store.Store(ctx, "alice", "user", "k",
			"a-pin", true, nil, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := store.Store(ctx, "alice", "user", "k",
			"a-unpin", false, nil, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		// Bob owns a pinned that alice should NOT see.
		if _, err := store.Store(ctx, "bob", "user", "k",
			"b-pin", true, nil, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		// System pinned visible to everyone.
		if _, err := store.Store(ctx, "", "system", "p",
			"sys-pin", true, nil, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		// System unpinned excluded by pinned filter.
		if _, err := store.Store(ctx, "", "system", "p",
			"sys-unpin", false, nil, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}

		got, err := store.GetPinned(ctx, "alice")
		if err != nil {
			t.Fatalf("GetPinned: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 pinned, got %d: %+v", len(got), got)
		}
		seen := map[string]bool{}
		for _, m := range got {
			seen[m.Content] = true
		}
		if !seen["a-pin"] || !seen["sys-pin"] {
			t.Errorf("missing expected pinned rows: %+v", got)
		}
		if seen["b-pin"] {
			t.Errorf("alice should not see bob's pinned row")
		}
	})

	t.Run("empty result for unknown user", func(t *testing.T) {
		truncateMemories(t, pool)
		got, err := store.GetPinned(ctx, "ghost")
		if err != nil {
			t.Fatalf("GetPinned: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("expected zero rows, got %d", len(got))
		}
	})
}

func TestStore_ListByUser(t *testing.T) {
	store, pool, cleanup := newMemoryTestStore(t)
	defer cleanup()
	ctx := context.Background()

	seed := func(t *testing.T) {
		t.Helper()
		truncateMemories(t, pool)
		// Alice rows in two categories.
		if _, err := store.Store(ctx, "alice", "user", "fact",
			"a-fact-1", false, nil, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := store.Store(ctx, "alice", "user", "fact",
			"a-fact-2", false, nil, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := store.Store(ctx, "alice", "user", "pref",
			"a-pref", false, nil, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		// Bob's row should not appear in alice's list.
		if _, err := store.Store(ctx, "bob", "user", "fact",
			"b-fact", false, nil, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		// System row is visible to alice.
		if _, err := store.Store(ctx, "", "system", "fact",
			"sys-fact", false, nil, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	t.Run("no category returns own + system", func(t *testing.T) {
		seed(t)
		got, err := store.ListByUser(ctx, "alice", "", 0)
		if err != nil {
			t.Fatalf("ListByUser: %v", err)
		}
		// 3 alice rows + 1 system = 4. Bob's row excluded.
		if len(got) != 4 {
			t.Errorf("expected 4 rows, got %d", len(got))
		}
		for _, m := range got {
			if m.Username == "bob" {
				t.Errorf("bob row leaked: %+v", m)
			}
		}
	})

	t.Run("category filter applies", func(t *testing.T) {
		seed(t)
		got, err := store.ListByUser(ctx, "alice", "fact", 0)
		if err != nil {
			t.Fatalf("ListByUser fact: %v", err)
		}
		// 2 alice fact rows + 1 system fact row = 3.
		if len(got) != 3 {
			t.Errorf("expected 3 fact rows, got %d: %+v", len(got), got)
		}
		for _, m := range got {
			if m.Category != "fact" {
				t.Errorf("non-fact category leaked: %+v", m)
			}
		}
	})

	t.Run("limit caps result size", func(t *testing.T) {
		seed(t)
		got, err := store.ListByUser(ctx, "alice", "", 2)
		if err != nil {
			t.Fatalf("ListByUser limit: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("expected 2 rows under limit, got %d", len(got))
		}
	})

	t.Run("limit clamps above 100", func(t *testing.T) {
		seed(t)
		got, err := store.ListByUser(ctx, "alice", "", 9999)
		if err != nil {
			t.Fatalf("ListByUser big limit: %v", err)
		}
		// We seeded only 5 rows; limit clamp must not error.
		if len(got) > 100 {
			t.Errorf("expected limit clamp, got %d rows", len(got))
		}
	})

	t.Run("category filter with limit", func(t *testing.T) {
		seed(t)
		got, err := store.ListByUser(ctx, "alice", "fact", 1)
		if err != nil {
			t.Fatalf("ListByUser fact+limit: %v", err)
		}
		if len(got) != 1 {
			t.Errorf("expected 1 row, got %d", len(got))
		}
	})
}

func TestStore_Search(t *testing.T) {
	store, pool, cleanup := newMemoryTestStore(t)
	defer cleanup()
	ctx := context.Background()

	seed := func(t *testing.T) {
		t.Helper()
		truncateMemories(t, pool)
		// Vectors aligned with x, y, z axes. Cosine distance to [1,0,0]
		// is smallest for the x-axis row, so it should appear first
		// when the search is ordered by similarity.
		if _, err := store.Store(ctx, "alice", "user", "fact",
			"x-axis vector", false,
			[]float32{1, 0, 0}, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := store.Store(ctx, "alice", "user", "fact",
			"y-axis vector", false,
			[]float32{0, 1, 0}, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := store.Store(ctx, "alice", "user", "pref",
			"alice preference", false,
			[]float32{0, 0, 1}, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := store.Store(ctx, "bob", "user", "fact",
			"bob private fact", false,
			[]float32{1, 0, 0}, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := store.Store(ctx, "", "system", "policy",
			"system note", false,
			[]float32{1, 0, 0}, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
		// One row without an embedding to confirm vector search filters
		// it out via embedding IS NOT NULL.
		if _, err := store.Store(ctx, "alice", "user", "fact",
			"no-embed text", false, nil, ""); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	t.Run("vector search orders by cosine distance", func(t *testing.T) {
		seed(t)
		got, err := store.Search(ctx, "alice", "", "", "", 0,
			[]float32{1, 0, 0})
		if err != nil {
			t.Fatalf("Search vec: %v", err)
		}
		if len(got) == 0 {
			t.Fatalf("expected non-empty result")
		}
		// Bob's row must be excluded; only alice's own + system show up.
		for _, m := range got {
			if m.Username == "bob" {
				t.Errorf("bob row leaked: %+v", m)
			}
			if m.Content == "no-embed text" {
				t.Errorf("null-embedding row leaked: %+v", m)
			}
		}
		// The closest match to [1,0,0] is the x-axis row.
		if got[0].Content != "x-axis vector" &&
			got[0].Content != "system note" {
			t.Errorf("unexpected top match: %+v", got[0])
		}
	})

	t.Run("vector search with category filter", func(t *testing.T) {
		seed(t)
		got, err := store.Search(ctx, "alice", "", "fact", "", 0,
			[]float32{1, 0, 0})
		if err != nil {
			t.Fatalf("Search vec+cat: %v", err)
		}
		for _, m := range got {
			if m.Category != "fact" {
				t.Errorf("unexpected category: %+v", m)
			}
		}
	})

	t.Run("vector search with scope filter", func(t *testing.T) {
		seed(t)
		got, err := store.Search(ctx, "alice", "", "", "system", 0,
			[]float32{1, 0, 0})
		if err != nil {
			t.Fatalf("Search vec+scope: %v", err)
		}
		if len(got) != 1 || got[0].Scope != "system" {
			t.Errorf("expected single system row, got %+v", got)
		}
	})

	t.Run("text search by content ILIKE", func(t *testing.T) {
		seed(t)
		got, err := store.Search(ctx, "alice", "axis", "", "", 0, nil)
		if err != nil {
			t.Fatalf("Search text: %v", err)
		}
		// Both x-axis and y-axis rows match; bob excluded.
		found := map[string]bool{}
		for _, m := range got {
			found[m.Content] = true
			if m.Username == "bob" {
				t.Errorf("bob leaked in text search: %+v", m)
			}
		}
		if !found["x-axis vector"] || !found["y-axis vector"] {
			t.Errorf("expected both axis rows, got %+v", got)
		}
	})

	t.Run("text search empty query returns visible rows", func(t *testing.T) {
		seed(t)
		got, err := store.Search(ctx, "alice", "", "", "", 0, nil)
		if err != nil {
			t.Fatalf("Search empty text: %v", err)
		}
		// alice's own (4) + system (1) = 5; no text filter applied.
		if len(got) != 5 {
			t.Errorf("expected 5 rows, got %d", len(got))
		}
	})

	t.Run("text search with category and scope filters", func(t *testing.T) {
		seed(t)
		got, err := store.Search(ctx, "alice", "", "fact", "user", 0, nil)
		if err != nil {
			t.Fatalf("Search text+filters: %v", err)
		}
		for _, m := range got {
			if m.Category != "fact" || m.Scope != "user" {
				t.Errorf("filter leak: %+v", m)
			}
			if m.Username == "bob" {
				t.Errorf("bob leaked: %+v", m)
			}
		}
	})

	t.Run("limit clamp lower bound", func(t *testing.T) {
		seed(t)
		got, err := store.Search(ctx, "alice", "", "", "", -5, nil)
		if err != nil {
			t.Fatalf("Search neg limit: %v", err)
		}
		// Negative limit is clamped to 20; we seeded fewer rows so just
		// confirm the call succeeded and stayed below the cap.
		if len(got) > 20 {
			t.Errorf("expected <=20 rows, got %d", len(got))
		}
	})

	t.Run("limit clamp upper bound", func(t *testing.T) {
		seed(t)
		got, err := store.Search(ctx, "alice", "", "", "", 9999, nil)
		if err != nil {
			t.Fatalf("Search huge limit: %v", err)
		}
		if len(got) > 100 {
			t.Errorf("expected <=100 rows, got %d", len(got))
		}
	})
}
