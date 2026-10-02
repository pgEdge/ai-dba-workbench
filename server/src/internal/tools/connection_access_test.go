/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// Tests for issue #571: the connection-scoped tools must not reveal
// connections the caller cannot see, either through a list of suggested
// connection IDs or through a response that tells a missing connection
// apart from a forbidden one.

// connectionScopedTools returns every tool that resolves an explicit
// connection_id through resolveAccessibleConnection.
func connectionScopedTools(ds *database.Datastore, rbac *auth.RBACChecker, lister auth.ConnectionVisibilityLister) map[string]Tool {
	pool := ds.GetPool()
	return map[string]Tool{
		"get_alert_history":    GetAlertHistoryTool(pool, rbac, lister),
		"get_blackouts":        GetBlackoutsTool(pool, rbac, lister),
		"get_metric_baselines": GetMetricBaselinesTool(pool, rbac, lister),
		"get_timeline_events":  GetTimelineEventsTool(ds, rbac, lister),
	}
}

// callForText calls tool with connection_id set to connID in ctx and
// returns the response text, failing unless the response is an error.
func callForText(t *testing.T, tool Tool, ctx context.Context, connID int) string {
	t.Helper()
	resp, err := tool.Handler(map[string]any{"__context": ctx, "connection_id": float64(connID)})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !resp.IsError || len(resp.Content) == 0 {
		t.Fatalf("expected an error response, got: %+v", resp)
	}
	return resp.Content[0].Text
}

// accessFixture seeds a connection bob can see and one private to alice,
// and returns bob's context alongside the IDs.
type accessFixture struct {
	ds        *database.Datastore
	store     *auth.AuthStore
	bobCtx    context.Context
	visible   int
	forbidden int
	missing   int
}

func newAccessFixture(t *testing.T) accessFixture {
	t.Helper()
	pool, ds, cleanup := newToolsTestPool(t)
	t.Cleanup(cleanup)
	store, storeCleanup := newRBACTestStore(t)
	t.Cleanup(storeCleanup)

	if err := store.CreateUser("bob", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := store.GetUserID("bob")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}

	f := accessFixture{
		ds:        ds,
		store:     store,
		bobCtx:    nonSuperuserContextInt(userID, "bob"),
		visible:   seedBaselineConnection(t, pool, "shared-visible", true, "alice"),
		forbidden: seedBaselineConnection(t, pool, "alice-private", false, "alice"),
	}
	f.missing = f.forbidden + 1000
	return f
}

func TestConnectionScopedTools_MissingAndForbiddenIdentical(t *testing.T) {
	f := newAccessFixture(t)
	rbac := auth.NewRBACCheckerForDatastore(f.store, f.ds)
	lister := database.NewVisibilityLister(f.ds)

	for name, tool := range connectionScopedTools(f.ds, rbac, lister) {
		t.Run(name, func(t *testing.T) {
			forbidden := callForText(t, tool, f.bobCtx, f.forbidden)
			missing := callForText(t, tool, f.bobCtx, f.missing)
			if forbidden != missing {
				t.Errorf("forbidden and missing IDs differ:\nforbidden: %s\nmissing:   %s", forbidden, missing)
			}
			if !strings.Contains(forbidden, "connection not found or not accessible") {
				t.Errorf("expected the not-found wording, got: %s", forbidden)
			}
			// Suggestions list only what bob can see.
			if !strings.Contains(forbidden, "shared-visible") {
				t.Errorf("expected the visible connection in the suggestions, got: %s", forbidden)
			}
			if strings.Contains(forbidden, "alice-private") {
				t.Errorf("suggestions leaked an invisible connection: %s", forbidden)
			}
		})
	}
}

func TestConnectionScopedTools_AccessCheckErrorDenies(t *testing.T) {
	f := newAccessFixture(t)
	lister := database.NewVisibilityLister(f.ds)
	rbac := auth.NewRBACChecker(f.store)
	rbac.SetConnectionSharingLookup(func(context.Context, int) (bool, string, error) {
		return false, "", errors.New("sharing lookup unavailable")
	})

	for name, tool := range connectionScopedTools(f.ds, rbac, lister) {
		t.Run(name, func(t *testing.T) {
			// The visible connection exists, but the check cannot run.
			failed := callForText(t, tool, f.bobCtx, f.visible)
			missing := callForText(t, tool, f.bobCtx, f.missing)
			if failed != missing {
				t.Errorf("failed check and missing ID differ:\nfailed:  %s\nmissing: %s", failed, missing)
			}
			if !strings.Contains(failed, "connection not found or not accessible") {
				t.Errorf("expected a denial, got: %s", failed)
			}
		})
	}
}

func TestResolveAccessibleConnection(t *testing.T) {
	f := newAccessFixture(t)
	pool := f.ds.GetPool()
	lister := database.NewVisibilityLister(f.ds)
	super := superuserContext()
	checker := testRBACChecker(t)

	t.Run("granted returns the name", func(t *testing.T) {
		name, resp := resolveAccessibleConnection(super, pool, checker, lister, f.forbidden)
		if resp != nil || name != "alice-private" {
			t.Errorf("expected alice-private, got name=%q resp=%+v", name, resp)
		}
	})

	t.Run("superuser missing ID lists every connection", func(t *testing.T) {
		_, resp := resolveAccessibleConnection(super, pool, checker, lister, f.missing)
		if resp == nil {
			t.Fatal("expected a response")
		}
		text := resp.Content[0].Text
		for _, want := range []string{"Connections you can access include: ", "shared-visible", "alice-private"} {
			if !strings.Contains(text, want) {
				t.Errorf("expected %q in: %s", want, text)
			}
		}
	})

	t.Run("nil pool skips the lookup", func(t *testing.T) {
		name, resp := resolveAccessibleConnection(super, nil, checker, lister, f.missing)
		if resp != nil || name != "" {
			t.Errorf("expected an empty grant, got name=%q resp=%+v", name, resp)
		}
	})

	plain := "connection not found or not accessible. Use list_connections to see the connections you can access."

	t.Run("nil pool denial has no suggestions", func(t *testing.T) {
		_, resp := resolveAccessibleConnection(f.bobCtx, nil, nil, lister, f.visible)
		if resp == nil || resp.Content[0].Text != plain {
			t.Errorf("expected %q, got %+v", plain, resp)
		}
	})

	t.Run("visibility failure has no suggestions", func(t *testing.T) {
		_, resp := resolveAccessibleConnection(f.bobCtx, pool, privateChecker(t), failingLister(), f.forbidden)
		if resp == nil || resp.Content[0].Text != plain {
			t.Errorf("expected %q, got %+v", plain, resp)
		}
	})

	t.Run("no visible connections has no suggestions", func(t *testing.T) {
		_, resp := resolveAccessibleConnection(f.bobCtx, pool, privateChecker(t), sharedLister(), f.forbidden)
		if resp == nil || resp.Content[0].Text != plain {
			t.Errorf("expected %q, got %+v", plain, resp)
		}
	})

	t.Run("query failure has no suggestions", func(t *testing.T) {
		ctx, cancel := context.WithCancel(super)
		cancel()
		_, resp := resolveAccessibleConnection(ctx, pool, checker, lister, f.visible)
		if resp == nil || resp.Content[0].Text != plain {
			t.Errorf("expected %q, got %+v", plain, resp)
		}
	})

	t.Run("suggestions are capped", func(t *testing.T) {
		for i := 0; i < maxConnectionSuggestions; i++ {
			seedBaselineConnection(t, pool, "filler", true, "")
		}
		got := visibleConnectionSuggestions(super, pool, checker, lister)
		if len(got) != maxConnectionSuggestions {
			t.Errorf("expected %d suggestions, got %d", maxConnectionSuggestions, len(got))
		}
	})
}
