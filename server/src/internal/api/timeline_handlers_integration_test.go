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
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// timelineTestSchema mirrors the minimum subset of tables that the
// alert_cleared timeline subquery and the connection visibility lister
// touch. Restricting the request to the alert_cleared event type keeps
// the schema small, because buildUnionQuery only emits subqueries for
// the requested types.
const timelineTestSchema = `
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS connections CASCADE;

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    is_monitored BOOLEAN NOT NULL DEFAULT FALSE,
    name VARCHAR(255) NOT NULL,
    description TEXT DEFAULT '',
    host VARCHAR(255) NOT NULL DEFAULT 'localhost',
    port INTEGER NOT NULL DEFAULT 5432,
    database_name VARCHAR(255) NOT NULL DEFAULT 'postgres',
    cluster_id INTEGER,
    membership_source VARCHAR(20) NOT NULL DEFAULT 'auto',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE alert_rules (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    metric_unit VARCHAR(64)
);

CREATE TABLE alerts (
    id SERIAL PRIMARY KEY,
    connection_id INTEGER NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    rule_id INTEGER,
    alert_type VARCHAR(64) NOT NULL DEFAULT 'threshold',
    severity VARCHAR(32) NOT NULL DEFAULT 'warning',
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    metric_name VARCHAR(255),
    metric_value DOUBLE PRECISION,
    threshold_value DOUBLE PRECISION,
    operator VARCHAR(8),
    database_name VARCHAR(255),
    probe_name VARCHAR(255),
    triggered_at TIMESTAMPTZ NOT NULL,
    cleared_at TIMESTAMPTZ,
    status VARCHAR(32) NOT NULL DEFAULT 'active'
);
`

const timelineTestTeardown = `
DROP TABLE IF EXISTS alerts CASCADE;
DROP TABLE IF EXISTS alert_rules CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
`

// timelineTestEnv bundles the Postgres-backed datastore and the auth
// store that a timeline handler test needs.
type timelineTestEnv struct {
	pool      *pgxpool.Pool
	datastore *database.Datastore
	authStore *auth.AuthStore
	// connA and connB are two seeded connections, each carrying one
	// cleared alert inside the request window used below.
	connA int
	connB int
}

// newTimelineTestEnv brings up the schema, seeds two connections with a
// cleared alert apiece, and returns an auth store for RBAC wiring. It
// skips when no test database is configured so unit-only runs still
// pass.
func newTimelineTestEnv(t *testing.T) (*timelineTestEnv, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping timeline integration test")
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
	if _, err := pool.Exec(ctx, timelineTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create timeline test schema: %v", err)
	}

	env := &timelineTestEnv{
		pool:      pool,
		datastore: database.NewTestDatastore(pool),
	}
	env.connA = insertTimelineConnection(t, pool, "timeline-a", "conn_owner")
	env.connB = insertTimelineConnection(t, pool, "timeline-b", "someone_else")
	insertTimelineClearedAlert(t, pool, env.connA, "Alert on A")
	insertTimelineClearedAlert(t, pool, env.connB, "Alert on B")

	tmpDir, err := os.MkdirTemp("", "timeline-handler-test-*")
	if err != nil {
		pool.Close()
		t.Fatalf("MkdirTemp: %v", err)
	}
	store, err := auth.NewAuthStore(tmpDir, 0, 0)
	if err != nil {
		pool.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("NewAuthStore: %v", err)
	}
	store.SetBcryptCostForTesting(t, bcrypt.MinCost)
	env.authStore = store

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
		if _, err := pool.Exec(context.Background(), timelineTestTeardown); err != nil {
			t.Logf("teardown: %v", err)
		}
		pool.Close()
	}
	return env, cleanup
}

// insertTimelineConnection seeds one unshared connection owned by the
// given username and returns its ID.
func insertTimelineConnection(t *testing.T, pool *pgxpool.Pool, name, owner string) int {
	t.Helper()
	var id int
	err := pool.QueryRow(context.Background(),
		`INSERT INTO connections (name, owner_username, is_shared)
         VALUES ($1, $2, FALSE) RETURNING id`, name, owner).Scan(&id)
	if err != nil {
		t.Fatalf("insert connection %s: %v", name, err)
	}
	return id
}

// insertTimelineClearedAlert seeds a fired-then-cleared alert half an
// hour in the past, which falls inside every window the tests request.
func insertTimelineClearedAlert(t *testing.T, pool *pgxpool.Pool, connID int, title string) {
	t.Helper()
	cleared := time.Now().UTC().Add(-30 * time.Minute)
	triggered := cleared.Add(-5 * time.Minute)
	_, err := pool.Exec(context.Background(),
		`INSERT INTO alerts (
            connection_id, severity, title, description, triggered_at,
            cleared_at, status
        ) VALUES ($1, 'warning', $2, 'seeded', $3, $4, 'cleared')`,
		connID, title, triggered, cleared)
	if err != nil {
		t.Fatalf("insert alert for connection %d: %v", connID, err)
	}
}

// timelineRequest builds a GET request for the seeded window. The extra
// query fragment lets a caller add connection filters.
func timelineRequest(extra string) *http.Request {
	now := time.Now().UTC()
	url := "/api/v1/timeline/events?start_time=" +
		now.Add(-2*time.Hour).Format(time.RFC3339) +
		"&end_time=" + now.Format(time.RFC3339) +
		"&event_types=alert_cleared&limit=100"
	if extra != "" {
		url += "&" + extra
	}
	return httptest.NewRequest(http.MethodGet, url, nil)
}

// grantTimelineUser creates a user, puts it in a fresh group, and grants
// read access to each of the supplied connection IDs. It returns the
// user ID for use with withUser.
func grantTimelineUser(t *testing.T, store *auth.AuthStore, username string, connIDs ...int) int64 {
	t.Helper()
	if err := store.CreateUser(username, "Password1234", "", "", ""); err != nil {
		t.Fatalf("CreateUser %s: %v", username, err)
	}
	userID, err := store.GetUserID(username)
	if err != nil {
		t.Fatalf("GetUserID %s: %v", username, err)
	}
	groupID, err := store.CreateGroup(username+"_group", "")
	if err != nil {
		t.Fatalf("CreateGroup for %s: %v", username, err)
	}
	if err := store.AddUserToGroup(groupID, userID); err != nil {
		t.Fatalf("AddUserToGroup for %s: %v", username, err)
	}
	for _, id := range connIDs {
		if err := store.GrantConnectionPrivilege(groupID, id, auth.AccessLevelRead); err != nil {
			t.Fatalf("GrantConnectionPrivilege %d: %v", id, err)
		}
	}
	return userID
}

// decodeTimelineResult reads a 200 response body as a TimelineResult.
func decodeTimelineResult(t *testing.T, rec *httptest.ResponseRecorder) *database.TimelineResult {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected status %d, got %d (body %s)",
			http.StatusOK, rec.Code, rec.Body.String())
	}
	var result database.TimelineResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode timeline result: %v", err)
	}
	return &result
}

// connectionIDsOf collects the connection IDs present in a result so
// assertions do not depend on event ordering.
func connectionIDsOf(result *database.TimelineResult) map[int]bool {
	ids := make(map[int]bool, len(result.Events))
	for i := range result.Events {
		ids[result.Events[i].ConnectionID] = true
	}
	return ids
}

// TestTimelineHandler_Integration_SuperuserSeesAllConnections drives the
// full handler against Postgres. A superuser bypasses the visibility
// filter entirely, so both seeded connections appear.
func TestTimelineHandler_Integration_SuperuserSeesAllConnections(t *testing.T) {
	env, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	handler := NewTimelineHandler(env.datastore, env.authStore,
		auth.NewRBACChecker(env.authStore))

	rec := httptest.NewRecorder()
	handler.handleTimelineEvents(rec, withSuperuser(timelineRequest("")))

	result := decodeTimelineResult(t, rec)
	if result.TotalCount != 2 {
		t.Fatalf("Expected 2 events, got %d", result.TotalCount)
	}
	ids := connectionIDsOf(result)
	if !ids[env.connA] || !ids[env.connB] {
		t.Errorf("Expected both connections in result, got %v", ids)
	}
}

// TestTimelineHandler_Integration_NilRBACChecker confirms that a handler
// built without an RBAC checker skips visibility filtering and queries
// the datastore directly.
func TestTimelineHandler_Integration_NilRBACChecker(t *testing.T) {
	env, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	handler := NewTimelineHandler(env.datastore, env.authStore, nil)

	rec := httptest.NewRecorder()
	handler.handleTimelineEvents(rec, timelineRequest(""))

	result := decodeTimelineResult(t, rec)
	if result.TotalCount != 2 {
		t.Errorf("Expected 2 events with no RBAC checker, got %d", result.TotalCount)
	}
}

// TestTimelineHandler_Integration_VisibilityFilters covers the RBAC
// branches: an unfiltered request restricted to the visible set, an
// explicit connection_id inside and outside that set, and a
// connection_ids list intersected against it.
func TestTimelineHandler_Integration_VisibilityFilters(t *testing.T) {
	env, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	// The user is granted connA only, and owns nothing, so connB stays
	// invisible however it is requested.
	userID := grantTimelineUser(t, env.authStore, "timeline_reader", env.connA)
	handler := NewTimelineHandler(env.datastore, env.authStore,
		auth.NewRBACChecker(env.authStore))

	tests := []struct {
		name        string
		extra       string
		expectCount int
		expectConn  int
	}{
		{
			name:        "no filter falls back to the visible set",
			extra:       "",
			expectCount: 1,
			expectConn:  env.connA,
		},
		{
			name:        "visible connection_id is respected",
			extra:       "connection_id=" + strconv.Itoa(env.connA),
			expectCount: 1,
			expectConn:  env.connA,
		},
		{
			name:        "invisible connection_id yields an empty result",
			extra:       "connection_id=" + strconv.Itoa(env.connB),
			expectCount: 0,
		},
		{
			name:        "connection_ids are intersected with the visible set",
			extra:       "connection_ids=" + strconv.Itoa(env.connA) + "," + strconv.Itoa(env.connB),
			expectCount: 1,
			expectConn:  env.connA,
		},
		{
			name:        "wholly invisible connection_ids yield an empty result",
			extra:       "connection_ids=" + strconv.Itoa(env.connB),
			expectCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := withUsername(withUser(timelineRequest(tt.extra), userID),
				"timeline_reader")
			handler.handleTimelineEvents(rec, req)

			result := decodeTimelineResult(t, rec)
			if result.TotalCount != tt.expectCount {
				t.Fatalf("Expected %d events, got %d", tt.expectCount, result.TotalCount)
			}
			if tt.expectCount > 0 && !connectionIDsOf(result)[tt.expectConn] {
				t.Errorf("Expected connection %d in result, got %v",
					tt.expectConn, connectionIDsOf(result))
			}
		})
	}
}

// TestTimelineHandler_Integration_NoVisibleConnections confirms that a
// caller with an empty visible set gets an empty result without the
// datastore being queried at all.
func TestTimelineHandler_Integration_NoVisibleConnections(t *testing.T) {
	env, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	userID := grantTimelineUser(t, env.authStore, "timeline_nobody")
	handler := NewTimelineHandler(env.datastore, env.authStore,
		auth.NewRBACChecker(env.authStore))

	rec := httptest.NewRecorder()
	req := withUsername(withUser(timelineRequest(""), userID), "timeline_nobody")
	handler.handleTimelineEvents(rec, req)

	result := decodeTimelineResult(t, rec)
	if result.TotalCount != 0 || len(result.Events) != 0 {
		t.Errorf("Expected an empty result, got %d events (total %d)",
			len(result.Events), result.TotalCount)
	}
}

// TestTimelineHandler_Integration_VisibilityLookupFails covers the 500
// path taken when the visibility lister cannot enumerate connections.
// Dropping the connections table makes GetAllConnections fail.
func TestTimelineHandler_Integration_VisibilityLookupFails(t *testing.T) {
	env, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	userID := grantTimelineUser(t, env.authStore, "timeline_broken_lister")
	if _, err := env.pool.Exec(context.Background(),
		"DROP TABLE IF EXISTS alerts CASCADE; DROP TABLE IF EXISTS connections CASCADE"); err != nil {
		t.Fatalf("drop connections: %v", err)
	}
	handler := NewTimelineHandler(env.datastore, env.authStore,
		auth.NewRBACChecker(env.authStore))

	rec := httptest.NewRecorder()
	req := withUsername(withUser(timelineRequest(""), userID),
		"timeline_broken_lister")
	handler.handleTimelineEvents(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
	var response ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if response.Error != "Failed to fetch timeline events" {
		t.Errorf("Unexpected error message: %q", response.Error)
	}
}

// TestTimelineHandler_Integration_RegisterRoutes covers the configured
// branch of RegisterRoutes, where the route reaches the real handler
// rather than the not-configured stub.
func TestTimelineHandler_Integration_RegisterRoutes(t *testing.T) {
	env, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	handler := NewTimelineHandler(env.datastore, env.authStore,
		auth.NewRBACChecker(env.authStore))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, func(h http.HandlerFunc) http.HandlerFunc { return h })

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, withSuperuser(timelineRequest("")))

	result := decodeTimelineResult(t, rec)
	if result.TotalCount != 2 {
		t.Errorf("Expected 2 events through the registered route, got %d",
			result.TotalCount)
	}
}

// TestTimelineHandler_Integration_QueryFails covers the 500 path taken
// when the timeline query itself errors. The superuser skips the
// visibility lookup, so dropping the alerts table fails the query.
func TestTimelineHandler_Integration_QueryFails(t *testing.T) {
	env, cleanup := newTimelineTestEnv(t)
	defer cleanup()

	if _, err := env.pool.Exec(context.Background(),
		"DROP TABLE IF EXISTS alerts CASCADE"); err != nil {
		t.Fatalf("drop alerts: %v", err)
	}
	handler := NewTimelineHandler(env.datastore, env.authStore,
		auth.NewRBACChecker(env.authStore))

	rec := httptest.NewRecorder()
	handler.handleTimelineEvents(rec, withSuperuser(timelineRequest("")))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}
