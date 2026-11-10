/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package overview

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/testutil/scratchdb"
)

// TestRateLimit_PartialScopeCacheHit checks that a user who can see
// only part of a cluster is answered from the summary cached for their
// visible members under the cluster's name, without spending a
// generation. The generator caches that summary under a key that
// includes the sanitized scope name, so a limiter that looked it up
// without the name would miss it and refuse the user with a 429.
func TestRateLimit_PartialScopeCacheHit(t *testing.T) {
	pool := scratchdb.NewCollectorSchema(t, "_overview_partial_scope", 4)
	ctx := context.Background()

	var groupID, clusterID, visibleID, hiddenID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO cluster_groups (name) VALUES ('Test Group') RETURNING id`).Scan(&groupID); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO clusters (group_id, name) VALUES ($1, 'Test Cluster') RETURNING id`,
		groupID).Scan(&clusterID); err != nil {
		t.Fatalf("seed cluster: %v", err)
	}
	for _, seed := range []struct {
		name string
		id   *int
	}{{"test-visible", &visibleID}, {"test-hidden", &hiddenID}} {
		if err := pool.QueryRow(ctx,
			`INSERT INTO connections (cluster_id, name, host, database_name, username, owner_username)
             VALUES ($1, $2, 'db.example.com', 'postgres', 'postgres', 'test-owner') RETURNING id`,
			clusterID, seed.name).Scan(seed.id); err != nil {
			t.Fatalf("seed connection %s: %v", seed.name, err)
		}
	}

	key := connectionsKey([]int{visibleID}, sanitizeScopeName("Test Cluster"))
	g := cachedGenerator(t, nil, true, key)

	store, cleanup := newRBACTestStore(t)
	t.Cleanup(cleanup)
	userCtx := grantedUserContext(t, store, visibleID)
	h := NewHandlerWithRBAC(g, NewHub(), auth.NewRBACChecker(store), database.NewTestDatastore(pool))
	h.SetRateLimits(RateLimits{GenerationsPerHour: 1, Burst: 1})
	h.limits.generate.Allow("granted")

	target := fmt.Sprintf("?scope_type=cluster&scope_id=%d", clusterID)

	rr := doRequestWithContext(t, h, userCtx, http.MethodGet, "/api/v1/overview"+target)
	if got := decodeSummary(t, rr); got != "Cached "+key {
		t.Fatalf("expected the summary cached under %q, got %q", key, got)
	}

	rr = serveSSE(t, h, userCtx, "/api/v1/overview/stream"+target, 500*time.Millisecond)
	if rr.Code != http.StatusOK {
		t.Fatalf("stream: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "Cached "+key) {
		t.Fatalf("stream: expected the summary cached under %q, got %q", key, body)
	}
	if strings.Contains(body, statusGenerating) {
		t.Fatalf("stream: expected no generating notice for a cache hit, got %q", body)
	}
}
