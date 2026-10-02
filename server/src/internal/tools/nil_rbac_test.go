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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/resources"
	"golang.org/x/crypto/bcrypt"
)

// Tests for issue #561: a tool built with no RBAC checker must deny, not
// grant unrestricted access. Every call runs as a superuser, which a real
// checker would admit, to prove that the nil checker is what denies. The
// pool is unreachable, so a tool that skipped its access check and went
// on to query would answer with a database error rather than the denial
// asserted here.

// nilRBACTools returns each connection-scoped tool built with a nil
// checker over pool.
func nilRBACTools(pool *pgxpool.Pool) map[string]Tool {
	return map[string]Tool{
		"get_alert_history":    GetAlertHistoryTool(pool, nil, nil),
		"get_alert_rules":      GetAlertRulesTool(pool, nil),
		"get_blackouts":        GetBlackoutsTool(pool, nil, nil),
		"get_metric_baselines": GetMetricBaselinesTool(pool, nil, nil),
		"query_metrics":        QueryMetricsTool(pool, nil),
		"get_timeline_events":  GetTimelineEventsTool(database.NewTestDatastore(pool), nil, nil),
	}
}

func TestTools_NilRBAC_SingleConnectionDenied(t *testing.T) {
	pool := dummyPool(t)
	defer pool.Close()

	for name, tool := range nilRBACTools(pool) {
		t.Run(name, func(t *testing.T) {
			args := map[string]any{"connection_id": float64(1)}
			if name == "query_metrics" {
				args["probe_name"] = "pg_stat_activity"
			}
			resp, err := asSuperuser(tool).Handler(args)
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if !resp.IsError || len(resp.Content) == 0 {
				t.Fatalf("expected an error response, got: %+v", resp)
			}
			// Denied and missing connections share a message so that the
			// response is not an existence oracle.
			text := resp.Content[0].Text
			if !strings.Contains(text, "not found or not accessible") {
				t.Errorf("expected a not-found denial, got: %s", text)
			}
		})
	}
}

func TestTools_NilRBAC_AllConnectionsEmpty(t *testing.T) {
	pool := dummyPool(t)
	defer pool.Close()

	tools := nilRBACTools(pool)
	for _, name := range []string{"get_alert_history", "get_blackouts", "get_metric_baselines", "get_timeline_events"} {
		t.Run(name, func(t *testing.T) {
			resp, err := asSuperuser(tools[name]).Handler(map[string]any{})
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if resp.IsError || len(resp.Content) == 0 {
				t.Fatalf("expected a success response, got: %+v", resp)
			}
			if text := resp.Content[0].Text; !strings.Contains(text, "You do not have access to any connections.") {
				t.Errorf("expected the no-access message, got: %s", text)
			}
		})
	}
}

// TestTools_NilRBAC_ExistingConnectionDenied checks every
// single-connection tool against a real connection: a nil checker denies
// it with the same message as a missing one, and a tool which skipped its
// access check would find the connection and answer something else.
func TestTools_NilRBAC_ExistingConnectionDenied(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()
	connID := seedBaselineConnection(t, pool, "denied-conn", true, "")

	for name, tool := range map[string]Tool{
		"get_alert_history":    GetAlertHistoryTool(pool, nil, nil),
		"get_blackouts":        GetBlackoutsTool(pool, nil, nil),
		"get_metric_baselines": GetMetricBaselinesTool(pool, nil, nil),
		"get_timeline_events":  GetTimelineEventsTool(database.NewTestDatastore(pool), nil, nil),
		"query_metrics":        QueryMetricsTool(pool, nil),
	} {
		t.Run(name, func(t *testing.T) {
			args := map[string]any{"connection_id": float64(connID)}
			if name == "query_metrics" {
				args["probe_name"] = "pg_stat_activity"
			}
			resp, err := asSuperuser(tool).Handler(args)
			if err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if !resp.IsError || len(resp.Content) == 0 {
				t.Fatalf("expected an error response, got: %+v", resp)
			}
			if text := resp.Content[0].Text; !strings.Contains(text, "not found or not accessible") {
				t.Errorf("expected a not-found denial, got: %s", text)
			}
		})
	}
}

// TestListConnections_NilRBAC_ShowsNothing seeds a connection and checks
// that a nil checker hides it, with the message that separates "no
// access" from "no connections exist".
func TestListConnections_NilRBAC_ShowsNothing(t *testing.T) {
	pool, _, cleanup := newToolsTestPool(t)
	defer cleanup()
	seedBaselineConnection(t, pool, "hidden-conn", true, "")

	resp, err := asSuperuser(ListConnectionsTool(pool, nil, nil)).Handler(map[string]any{})
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if resp.IsError || len(resp.Content) == 0 {
		t.Fatalf("expected a success response, got: %+v", resp)
	}
	text := resp.Content[0].Text
	if text != "You do not have access to any connections." {
		t.Errorf("expected the no-access message, got: %s", text)
	}
	if strings.Contains(text, "hidden-conn") {
		t.Errorf("connection leaked through a nil checker: %s", text)
	}
}

// TestContextAwareProvider_NilRBAC_DeniesTool checks that a provider
// whose checker is nil denies a tool call from a superuser rather than
// running it.
func TestContextAwareProvider_NilRBAC_DeniesTool(t *testing.T) {
	authStore, err := auth.NewAuthStore(t.TempDir(), 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	defer authStore.Close()
	authStore.SetBcryptCostForTesting(t, bcrypt.MinCost)

	if err := authStore.CreateUser("admin", "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, token, err := authStore.CreateToken("admin", "admin-token", nil)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if err := authStore.SetConnectionSession(token.TokenHash, 42, nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}

	clientManager := database.NewClientManager(nil)
	defer clientManager.CloseAll()
	cfg := &config.Config{}
	resourceReg := resources.NewContextAwareRegistry(clientManager, cfg, nil, nil)
	provider := NewContextAwareProvider(clientManager, resourceReg, nil, cfg, authStore, nil, nil)
	provider.rbacChecker = nil

	ctx := context.WithValue(superuserContext(), auth.TokenHashContextKey, token.TokenHash)
	args := map[string]any{"probe_name": "pg_stat_activity"}
	resp, err := provider.Execute(ctx, "query_metrics", args)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !resp.IsError || len(resp.Content) == 0 {
		t.Fatalf("expected an error response, got: %+v", resp)
	}
	if text := resp.Content[0].Text; !strings.Contains(text, "ccess denied") {
		t.Errorf("expected an access-denied message, got: %s", text)
	}
}
