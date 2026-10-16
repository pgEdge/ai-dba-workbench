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
	"encoding/json"
	"math"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/resources"
)

// refusedConnectionID is a connection ID the callers in these tests
// cannot access.
const refusedConnectionID = 4242

// unreachablePool returns a pool that is never connected: pgxpool dials
// lazily, and the refusals under test return before any query, so the
// tools see a configured datastore without a database behind it. Any
// query that does run fails at once against the closed port.
func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(),
		"postgres://nobody@127.0.0.1:1/none?connect_timeout=2")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// deniedRows returns the denied audit rows for action.
func deniedRows(t *testing.T, store *auth.AuthStore, action string) []auth.AuditEvent {
	t.Helper()
	events, _, err := store.ListAuditEvents(auth.AuditFilter{
		Action: action, Outcome: string(auth.OutcomeDenied)})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	return events
}

// denialTarget returns the target an audit row records.
func denialTarget(t *testing.T, event auth.AuditEvent) string {
	t.Helper()
	var details struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal(event.Details, &details); err != nil {
		t.Fatalf("Failed to decode details %s: %v", event.Details, err)
	}
	return details.Target
}

// TestConnectionDenialTarget pins the connections/<id> form the REST
// routes record.
func TestConnectionDenialTarget(t *testing.T) {
	if got := connectionDenialTarget(7); got != "connections/7" {
		t.Errorf("connectionDenialTarget(7) = %q, want connections/7", got)
	}
	for _, id := range []int{0, -3} {
		if got := connectionDenialTarget(id); got != "connections" {
			t.Errorf("connectionDenialTarget(%d) = %q, want connections",
				id, got)
		}
	}
}

// TestParseConnectionArgsRejectsInvalidIDs checks that a connection_id
// that is not a positive integer is marked invalid rather than
// truncated, and that Resolve refuses it before any lookup, recording
// nothing (it names no connection).
func TestParseConnectionArgsRejectsInvalidIDs(t *testing.T) {
	invalid := map[string]any{
		"zero":          float64(0),
		"negative":      float64(-2),
		"fractional":    3.7,
		"NaN":           math.NaN(),
		"infinite":      math.Inf(1),
		"too large":     float64(math.MaxInt32) + 1,
		"int zero":      0,
		"int negative":  -1,
		"tiny fraction": 0.5,
	}
	capture := &toolDenialCapture{}
	resolver := NewConnectionResolver(nil, nil, nil, capture.record)
	for name, raw := range invalid {
		t.Run(name, func(t *testing.T) {
			ca := parseConnectionArgs(map[string]any{"connection_id": raw})
			if !ca.HasConnID || !ca.InvalidConnID || ca.ConnectionID != 0 {
				t.Fatalf("Expected an invalid connection_id, got %+v", ca)
			}
			for _, r := range []*ConnectionResolver{resolver, nil} {
				_, resp := r.Resolve(context.Background(),
					deniedActionConnectionQuery,
					map[string]any{"connection_id": raw}, nil)
				if resp == nil || !resp.IsError ||
					resp.Content[0].Text != invalidConnectionIDMessage {
					t.Errorf("Expected %q, got %+v",
						invalidConnectionIDMessage, resp)
				}
			}
		})
	}
	if len(capture.denials) != 0 {
		t.Errorf("Expected nothing recorded, got %v", capture.denials)
	}

	for _, raw := range []any{float64(1), float64(math.MaxInt32), 9} {
		ca := parseConnectionArgs(map[string]any{"connection_id": raw})
		if ca.InvalidConnID || ca.ConnectionID < 1 {
			t.Errorf("Expected %v accepted, got %+v", raw, ca)
		}
	}
}

// TestDenyWithoutRecorder checks that a nil recorder records nothing and
// does not panic.
func TestDenyWithoutRecorder(t *testing.T) {
	var none DenialRecorder
	none.deny(context.Background(), deniedActionConnectionRead,
		connectionDenialTarget(1), connectionAccessDenied)
}

// TestSessionConnectionDenied checks the client resolver's hook: none
// without a recorder, and otherwise a connection.read refusal of the
// selected connection.
func TestSessionConnectionDenied(t *testing.T) {
	if sessionConnectionDenied(nil) != nil {
		t.Error("Expected no hook without a recorder")
	}

	capture := &toolDenialCapture{}
	sessionConnectionDenied(capture.record)(context.Background(), 7)
	want := toolDenial{deniedActionConnectionRead, "connections/7",
		connectionAccessDenied}
	if len(capture.denials) != 1 || capture.denials[0] != want {
		t.Errorf("Expected %v recorded, got %v", want, capture.denials)
	}
}

// TestResolverRecordsRefusedConnection checks that the resolver records
// a connection_id the caller may not access under the action its tool
// passes, and that an invalid database name, which is refused before
// the access check, records nothing.
func TestResolverRecordsRefusedConnection(t *testing.T) {
	capture := &toolDenialCapture{}
	resolver := NewConnectionResolver(nil, nil, nil, capture.record)

	_, resp := resolver.Resolve(context.Background(),
		deniedActionConnectionQueryValidate,
		map[string]any{"connection_id": float64(refusedConnectionID)}, nil)
	if resp == nil || !resp.IsError ||
		resp.Content[0].Text != connectionNotAccessibleMsg {
		t.Fatalf("Expected the unchanged refusal, got %+v", resp)
	}
	want := toolDenial{deniedActionConnectionQueryValidate,
		"connections/4242", connectionAccessDenied}
	if len(capture.denials) != 1 || capture.denials[0] != want {
		t.Errorf("Expected %v recorded, got %v", want, capture.denials)
	}

	capture.denials = nil
	_, resp = resolver.Resolve(context.Background(),
		deniedActionConnectionQuery, map[string]any{
			"connection_id": float64(refusedConnectionID),
			"database_name": "bad\x00name",
		}, nil)
	if resp == nil || !resp.IsError {
		t.Fatalf("Expected the database name refused, got %+v", resp)
	}
	if len(capture.denials) != 0 {
		t.Errorf("Expected nothing recorded, got %v", capture.denials)
	}
}

// TestResolveAccessibleConnectionRecordsDenial checks that a denied
// check is recorded under the caller's action and that a granted one is
// not.
func TestResolveAccessibleConnectionRecordsDenial(t *testing.T) {
	capture := &toolDenialCapture{}
	_, resp := resolveAccessibleConnection(context.Background(), nil, nil,
		nil, capture.record, deniedActionMetricsRead, refusedConnectionID)
	if resp == nil {
		t.Fatal("Expected a refusal")
	}
	want := toolDenial{deniedActionMetricsRead, "connections/4242",
		connectionAccessDenied}
	if len(capture.denials) != 1 || capture.denials[0] != want {
		t.Errorf("Expected %v recorded, got %v", want, capture.denials)
	}

	capture.denials = nil
	store := newExecuteTestAuthStore(t)
	superCtx := context.WithValue(context.Background(),
		auth.IsSuperuserContextKey, true)
	if _, resp := resolveAccessibleConnection(superCtx, nil,
		auth.NewRBACChecker(store), nil, capture.record,
		deniedActionMetricsRead, refusedConnectionID); resp != nil {
		t.Fatalf("Expected access granted, got %+v", resp)
	}
	if len(capture.denials) != 0 {
		t.Errorf("Expected nothing recorded, got %v", capture.denials)
	}
}

// TestDatastoreToolsRecordRefusedConnection drives each datastore tool
// that names a connection with a checker that denies it, and checks the
// action each records, the connection it names and the fixed reason,
// and that the caller's message is unchanged.
func TestDatastoreToolsRecordRefusedConnection(t *testing.T) {
	pool := unreachablePool(t)
	tests := []struct {
		name   string
		build  func(DenialRecorder) Tool
		args   map[string]any
		action string
	}{
		{"query_metrics", func(r DenialRecorder) Tool {
			return QueryMetricsTool(pool, nil, r)
		}, map[string]any{"probe_name": "pg_stat_database"},
			deniedActionMetricsRead},
		{"get_metric_baselines", func(r DenialRecorder) Tool {
			return GetMetricBaselinesTool(pool, nil, nil, r)
		}, map[string]any{}, deniedActionMetricsRead},
		{"get_alert_rules", func(r DenialRecorder) Tool {
			return GetAlertRulesTool(pool, nil, r)
		}, map[string]any{}, deniedActionConnectionRead},
		{"get_alert_history", func(r DenialRecorder) Tool {
			return GetAlertHistoryTool(pool, nil, nil, r)
		}, map[string]any{}, deniedActionConnectionRead},
		{"get_blackouts", func(r DenialRecorder) Tool {
			return GetBlackoutsTool(pool, nil, nil, r)
		}, map[string]any{}, deniedActionConnectionRead},
		{"get_timeline_events", func(r DenialRecorder) Tool {
			return GetTimelineEventsTool(database.NewTestDatastore(pool),
				nil, nil, r)
		}, map[string]any{}, deniedActionConnectionRead},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := &toolDenialCapture{}
			tt.args["connection_id"] = float64(refusedConnectionID)
			tt.args["__context"] = context.Background()

			resp, err := tt.build(capture.record).Handler(tt.args)
			if err != nil {
				t.Fatalf("Handler returned a Go error: %v", err)
			}
			if !resp.IsError || len(resp.Content) == 0 ||
				len(resp.Content[0].Text) < len(connectionNotAccessibleMsg) ||
				resp.Content[0].Text[:len(connectionNotAccessibleMsg)] !=
					connectionNotAccessibleMsg {
				t.Fatalf("Expected the unchanged refusal, got %+v", resp)
			}
			want := toolDenial{tt.action, "connections/4242",
				connectionAccessDenied}
			if len(capture.denials) != 1 || capture.denials[0] != want {
				t.Errorf("Expected %v recorded, got %v", want,
					capture.denials)
			}
		})
	}
}

// TestProviderRecordsToolAccessRefusal checks that a call the tool-level
// RBAC gate refuses is written to the audit log as mcp.tool.call, naming
// a registered tool by the registry's own name and any other name as
// unmatched, with the fixed reason, and that the caller's message is
// unchanged. A call the gate admits, and an exempt tool, record nothing.
func TestProviderRecordsToolAccessRefusal(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		public  bool
		target  string
		refused bool
	}{
		{"registered tool", "query_metrics", false, "tools/query_metrics", true},
		{"unknown name", "no_such_tool", false, unmatchedToolTarget, true},
		{"admitted tool", "list_probes", true, "", false},
		{"exempt tool", "test_query", false, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			provider, _ := newExecuteTestProvider(t, cfg)
			store := provider.authStore
			if tt.public {
				if _, err := store.RegisterMCPPrivilege(tt.tool,
					auth.MCPPrivilegeTypeTool, "Public tool", true); err != nil {
					t.Fatalf("RegisterMCPPrivilege: %v", err)
				}
			}
			if err := store.CreateUser("plain_user", "Password-123!", "", "", ""); err != nil {
				t.Fatalf("CreateUser: %v", err)
			}
			userID, err := store.GetUserID("plain_user")
			if err != nil {
				t.Fatalf("GetUserID: %v", err)
			}
			ctx := context.WithValue(userContext("plain_user", userID),
				auth.TokenHashContextKey, "session-token-hash")

			resp, err := provider.Execute(ctx, tt.tool, map[string]any{
				"query": "SELECT 1"})
			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			wantText := "Access denied: you do not have permission to use tool '" +
				tt.tool + "'"
			refused := resp.IsError && len(resp.Content) > 0 &&
				resp.Content[0].Text == wantText
			if refused != tt.refused {
				t.Fatalf("Expected refused=%v, got %+v", tt.refused, resp)
			}

			events := deniedRows(t, store, deniedActionToolCall)
			if !tt.refused {
				if len(events) != 0 {
					t.Errorf("Expected no denial, got %d", len(events))
				}
				return
			}
			if len(events) != 1 {
				t.Fatalf("Expected 1 mcp.tool.call denial, got %d",
					len(events))
			}
			if events[0].Error != toolAccessDenied {
				t.Errorf("Expected reason %q, got %q", toolAccessDenied,
					events[0].Error)
			}
			if events[0].ActorName != "plain_user" {
				t.Errorf("Expected the denial attributed to plain_user, got %q",
					events[0].ActorName)
			}
			if got := denialTarget(t, events[0]); got != tt.target {
				t.Errorf("Expected target %q, got %q", tt.target, got)
			}
		})
	}
}

// newWiredTestProvider returns a provider over a real auth store and a
// datastore whose pool is never reached, so that the datastore tools and
// the connection resolver are registered as in production. Its RBAC
// checker's connection lookups fail, which denies every connection to a
// caller who is not a superuser.
func newWiredTestProvider(t *testing.T) (*ContextAwareProvider, *auth.AuthStore) {
	t.Helper()
	store := newExecuteTestAuthStore(t)
	clientManager := database.NewClientManager(nil)
	t.Cleanup(func() {
		if err := clientManager.CloseAll(); err != nil {
			t.Logf("CloseAll: %v", err)
		}
	})
	cfg := &config.Config{}
	provider := NewContextAwareProvider(clientManager,
		resources.NewContextAwareRegistry(clientManager, cfg, nil, nil),
		nil, cfg, store, nil, database.NewTestDatastore(unreachablePool(t)))
	return provider, store
}

// TestProviderWiresConnectionDenialRecorder drives each tool that names
// a connection through ContextAwareProvider.Execute, as a session user
// the tool-level gate admits but who has no access to the connection,
// and checks the denied row each leaves. Each case runs as its own user,
// so that the coalescer does not fold one tool's refusal into another's.
// A provider that builds a tool or the resolver without the recorder
// fails its case.
func TestProviderWiresConnectionDenialRecorder(t *testing.T) {
	provider, store := newWiredTestProvider(t)

	tests := []struct {
		tool   string
		args   map[string]any
		action string
	}{
		{"query_metrics", map[string]any{"probe_name": "pg_stat_database"},
			deniedActionMetricsRead},
		{"get_metric_baselines", map[string]any{}, deniedActionMetricsRead},
		{"get_alert_rules", map[string]any{}, deniedActionConnectionRead},
		{"get_alert_history", map[string]any{}, deniedActionConnectionRead},
		{"get_blackouts", map[string]any{}, deniedActionConnectionRead},
		{"get_timeline_events", map[string]any{}, deniedActionConnectionRead},
		{"query_database", map[string]any{"query": "SELECT 1"},
			deniedActionConnectionQuery},
		{"execute_explain", map[string]any{"query": "SELECT 1"},
			deniedActionConnectionQuery},
		{"count_rows", map[string]any{"table": "t"},
			deniedActionConnectionQuery},
		{"similarity_search", map[string]any{"table_name": "t",
			"query_text": "x"}, deniedActionConnectionQuery},
		{"get_schema_info", map[string]any{}, deniedActionConnectionRead},
		{"test_query", map[string]any{"query": "SELECT 1"},
			deniedActionConnectionQueryValidate},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			if _, err := store.RegisterMCPPrivilege(tt.tool,
				auth.MCPPrivilegeTypeTool, "Public tool", true); err != nil {
				t.Fatalf("RegisterMCPPrivilege: %v", err)
			}
			username := "user_" + tt.tool
			if err := store.CreateUser(username, "Password-123!", "", "", ""); err != nil {
				t.Fatalf("CreateUser: %v", err)
			}
			userID, err := store.GetUserID(username)
			if err != nil {
				t.Fatalf("GetUserID: %v", err)
			}
			ctx := context.WithValue(userContext(username, userID),
				auth.TokenHashContextKey, "hash-"+tt.tool)

			tt.args["connection_id"] = float64(refusedConnectionID)
			resp, err := provider.Execute(ctx, tt.tool, tt.args)
			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if !resp.IsError {
				t.Fatalf("Expected a refusal, got %+v", resp)
			}

			var found bool
			for _, event := range deniedRows(t, store, tt.action) {
				if event.ActorName != username {
					continue
				}
				found = true
				if event.Error != connectionAccessDenied {
					t.Errorf("Expected reason %q, got %q",
						connectionAccessDenied, event.Error)
				}
				if got := denialTarget(t, event); got != "connections/4242" {
					t.Errorf("Expected target connections/4242, got %q", got)
				}
			}
			if !found {
				t.Errorf("Expected a denied %s row for %s", tt.action,
					username)
			}
		})
	}
}

// TestProviderRecordsLostSessionConnection checks that a selected
// connection the caller can no longer access is recorded as a
// connection.read refusal of that connection when a tool falls back to
// it, as REST records a read of such a current connection.
func TestProviderRecordsLostSessionConnection(t *testing.T) {
	provider, store := newWiredTestProvider(t)
	if _, err := store.RegisterMCPPrivilege("query_database",
		auth.MCPPrivilegeTypeTool, "Public tool", true); err != nil {
		t.Fatalf("RegisterMCPPrivilege: %v", err)
	}
	if err := store.CreateUser("plain_user", "Password-123!", "", "", ""); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	userID, err := store.GetUserID("plain_user")
	if err != nil {
		t.Fatalf("GetUserID: %v", err)
	}
	const tokenHash = "lost-session-hash"
	if err := store.SetConnectionSession(tokenHash, refusedConnectionID,
		nil); err != nil {
		t.Fatalf("SetConnectionSession: %v", err)
	}
	ctx := context.WithValue(userContext("plain_user", userID),
		auth.TokenHashContextKey, tokenHash)

	resp, err := provider.Execute(ctx, "query_database",
		map[string]any{"query": "SELECT 1"})
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if !resp.IsError {
		t.Fatalf("Expected a refusal, got %+v", resp)
	}

	events := deniedRows(t, store, deniedActionConnectionRead)
	if len(events) != 1 {
		t.Fatalf("Expected 1 connection.read denial, got %d", len(events))
	}
	if events[0].ActorName != "plain_user" ||
		events[0].Error != connectionAccessDenied ||
		denialTarget(t, events[0]) != "connections/4242" {
		t.Errorf("Unexpected denial %+v", events[0])
	}
}
