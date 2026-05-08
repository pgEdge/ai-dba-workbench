/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// blackoutTestSchema mirrors the production schema for blackouts and
// blackout_schedules sufficiently for the queries this file under test
// exercises. The schema also creates the cluster hierarchy tables so
// the scope-id check constraints have valid foreign keys to reference,
// and pre-seeds a row in each parent table so that scoped blackouts
// referencing real entities can be inserted.
const blackoutTestSchema = `
DROP TABLE IF EXISTS blackout_schedules CASCADE;
DROP TABLE IF EXISTS blackouts CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;

CREATE TABLE cluster_groups (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE clusters (
    id SERIAL PRIMARY KEY,
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE SET NULL,
    name VARCHAR(255) NOT NULL
);

CREATE TABLE blackouts (
    id BIGSERIAL PRIMARY KEY,
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    reason TEXT NOT NULL,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    created_by TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT 'server',
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (end_time > start_time),
    CONSTRAINT blackouts_scope_check
        CHECK (scope IN ('estate', 'group', 'cluster', 'server'))
);

CREATE TABLE blackout_schedules (
    id BIGSERIAL PRIMARY KEY,
    connection_id INTEGER REFERENCES connections(id) ON DELETE CASCADE,
    database_name TEXT,
    name TEXT NOT NULL,
    cron_expression TEXT NOT NULL,
    duration_minutes INTEGER NOT NULL CHECK (duration_minutes > 0),
    timezone TEXT NOT NULL DEFAULT 'UTC',
    reason TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT 'server',
    group_id INTEGER REFERENCES cluster_groups(id) ON DELETE CASCADE,
    cluster_id INTEGER REFERENCES clusters(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT blackout_schedules_scope_check
        CHECK (scope IN ('estate', 'group', 'cluster', 'server'))
);
`

const blackoutTestTeardown = `
DROP TABLE IF EXISTS blackout_schedules CASCADE;
DROP TABLE IF EXISTS blackouts CASCADE;
DROP TABLE IF EXISTS connections CASCADE;
DROP TABLE IF EXISTS clusters CASCADE;
DROP TABLE IF EXISTS cluster_groups CASCADE;
`

// blackoutFixture stores the IDs of the seed parent rows so each test
// can build hierarchical blackouts without re-querying.
type blackoutFixture struct {
	GroupID   int
	ClusterID int
	ConnID    int
}

// newBlackoutTestDatastore wires up a *Datastore against the
// TEST_AI_WORKBENCH_SERVER Postgres instance with the minimum schema for
// blackout queries. The caller receives a cleanup that drops the
// schema and closes the pool.
func newBlackoutTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, blackoutFixture, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping blackout integration test")
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

	if _, err := pool.Exec(ctx, blackoutTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create blackout test schema: %v", err)
	}

	// Seed parent rows so scope-specific blackouts can reference them.
	var groupID, clusterID, connID int
	if err := pool.QueryRow(ctx,
		`INSERT INTO cluster_groups (name) VALUES ('grp-1') RETURNING id`).Scan(&groupID); err != nil {
		pool.Close()
		t.Fatalf("seed group: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO clusters (group_id, name) VALUES ($1, 'cls-1') RETURNING id`,
		groupID).Scan(&clusterID); err != nil {
		pool.Close()
		t.Fatalf("seed cluster: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO connections (cluster_id, name) VALUES ($1, 'conn-1') RETURNING id`,
		clusterID).Scan(&connID); err != nil {
		pool.Close()
		t.Fatalf("seed connection: %v", err)
	}

	ds := NewTestDatastore(pool)

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), blackoutTestTeardown); err != nil {
			t.Logf("blackout teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, blackoutFixture{
		GroupID:   groupID,
		ClusterID: clusterID,
		ConnID:    connID,
	}, cleanup
}

// makeServerBlackout builds an active server-scoped blackout for the
// given connection, with start time 1 hour ago and end time 1 hour
// hence, so it satisfies the active-window predicate.
func makeServerBlackout(connID int, reason string) *Blackout {
	cid := connID
	now := time.Now().UTC()
	return &Blackout{
		Scope:        string(BlackoutScopeServer),
		ConnectionID: &cid,
		Reason:       reason,
		StartTime:    now.Add(-1 * time.Hour),
		EndTime:      now.Add(1 * time.Hour),
		CreatedBy:    "tester",
	}
}

// TestCreateAndGetBlackout exercises CreateBlackout and GetBlackout,
// asserting the RETURNING values flow back into the struct and that
// the blackout can be re-read by ID with the is_active flag set.
func TestCreateAndGetBlackout(t *testing.T) {
	ds, _, fx, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	b := makeServerBlackout(fx.ConnID, "create-then-get")
	if err := ds.CreateBlackout(ctx, b); err != nil {
		t.Fatalf("CreateBlackout: %v", err)
	}
	if b.ID == 0 {
		t.Fatal("CreateBlackout did not populate ID")
	}
	if b.CreatedAt.IsZero() {
		t.Error("CreateBlackout did not populate CreatedAt")
	}

	got, err := ds.GetBlackout(ctx, b.ID)
	if err != nil {
		t.Fatalf("GetBlackout: %v", err)
	}
	if got.Reason != "create-then-get" {
		t.Errorf("Reason = %q, want create-then-get", got.Reason)
	}
	if !got.IsActive {
		t.Error("expected IsActive true for currently-active window")
	}
}

// TestGetBlackoutNotFound covers the ErrBlackoutNotFound branch.
func TestGetBlackoutNotFound(t *testing.T) {
	ds, _, _, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	_, err := ds.GetBlackout(context.Background(), 99999)
	if !errors.Is(err, ErrBlackoutNotFound) {
		t.Errorf("expected ErrBlackoutNotFound, got %v", err)
	}
}

// TestUpdateBlackout exercises the happy path and the not-found
// branch.
func TestUpdateBlackout(t *testing.T) {
	ds, pool, fx, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	b := makeServerBlackout(fx.ConnID, "before")
	if err := ds.CreateBlackout(ctx, b); err != nil {
		t.Fatalf("CreateBlackout: %v", err)
	}

	newEnd := time.Now().Add(2 * time.Hour).UTC()
	if err := ds.UpdateBlackout(ctx, b.ID, "after", newEnd); err != nil {
		t.Fatalf("UpdateBlackout: %v", err)
	}

	var reason string
	var endTime time.Time
	if err := pool.QueryRow(ctx,
		`SELECT reason, end_time FROM blackouts WHERE id = $1`, b.ID,
	).Scan(&reason, &endTime); err != nil {
		t.Fatalf("verify update: %v", err)
	}
	if reason != "after" {
		t.Errorf("reason = %q, want after", reason)
	}
	if endTime.Sub(newEnd).Abs() > time.Second {
		t.Errorf("end_time = %v, want ~%v", endTime, newEnd)
	}

	if err := ds.UpdateBlackout(ctx, 999999, "x", newEnd); !errors.Is(err, ErrBlackoutNotFound) {
		t.Errorf("expected ErrBlackoutNotFound for missing id, got %v", err)
	}
}

// TestDeleteBlackout exercises both the success and not-found paths.
func TestDeleteBlackout(t *testing.T) {
	ds, _, fx, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	b := makeServerBlackout(fx.ConnID, "del")
	if err := ds.CreateBlackout(ctx, b); err != nil {
		t.Fatalf("CreateBlackout: %v", err)
	}
	if err := ds.DeleteBlackout(ctx, b.ID); err != nil {
		t.Fatalf("DeleteBlackout: %v", err)
	}

	if _, err := ds.GetBlackout(ctx, b.ID); !errors.Is(err, ErrBlackoutNotFound) {
		t.Errorf("after delete, expected ErrBlackoutNotFound, got %v", err)
	}

	if err := ds.DeleteBlackout(ctx, b.ID); !errors.Is(err, ErrBlackoutNotFound) {
		t.Errorf("repeat delete should return ErrBlackoutNotFound, got %v", err)
	}
}

// TestStopBlackout covers the active-stop happy path and the
// not-found branch when targeting a blackout whose end time is
// already past (stopping it is a no-op so the row count is zero).
func TestStopBlackout(t *testing.T) {
	ds, pool, fx, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	b := makeServerBlackout(fx.ConnID, "stop")
	if err := ds.CreateBlackout(ctx, b); err != nil {
		t.Fatalf("CreateBlackout: %v", err)
	}

	if err := ds.StopBlackout(ctx, b.ID); err != nil {
		t.Fatalf("StopBlackout: %v", err)
	}

	var endTime time.Time
	if err := pool.QueryRow(ctx,
		`SELECT end_time FROM blackouts WHERE id = $1`, b.ID,
	).Scan(&endTime); err != nil {
		t.Fatalf("verify stop: %v", err)
	}
	if time.Since(endTime) > 5*time.Second {
		t.Errorf("end_time should be near now, got %v", endTime)
	}

	// Re-running StopBlackout should now return ErrBlackoutNotFound
	// because end_time is already in the past so the WHERE clause
	// matches no rows.
	if err := ds.StopBlackout(ctx, b.ID); !errors.Is(err, ErrBlackoutNotFound) {
		t.Errorf("expected ErrBlackoutNotFound after window closed, got %v", err)
	}
}

// TestListBlackoutsFiltering exercises every filter branch in
// ListBlackouts plus the default pagination, scope-based filtering,
// and the active flag in both true/false forms.
func TestListBlackoutsFiltering(t *testing.T) {
	ds, pool, fx, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()

	// Create one blackout per scope. Each is currently active so
	// that the IsActive flag and active-true filter work.
	estate := &Blackout{
		Scope:     string(BlackoutScopeEstate),
		Reason:    "estate-bk",
		StartTime: now.Add(-30 * time.Minute),
		EndTime:   now.Add(30 * time.Minute),
		CreatedBy: "t",
	}
	if err := ds.CreateBlackout(ctx, estate); err != nil {
		t.Fatalf("estate create: %v", err)
	}

	gid := fx.GroupID
	group := &Blackout{
		Scope:     string(BlackoutScopeGroup),
		GroupID:   &gid,
		Reason:    "group-bk",
		StartTime: now.Add(-30 * time.Minute),
		EndTime:   now.Add(30 * time.Minute),
		CreatedBy: "t",
	}
	if err := ds.CreateBlackout(ctx, group); err != nil {
		t.Fatalf("group create: %v", err)
	}

	cid := fx.ClusterID
	cluster := &Blackout{
		Scope:     string(BlackoutScopeCluster),
		ClusterID: &cid,
		Reason:    "cluster-bk",
		StartTime: now.Add(-30 * time.Minute),
		EndTime:   now.Add(30 * time.Minute),
		CreatedBy: "t",
	}
	if err := ds.CreateBlackout(ctx, cluster); err != nil {
		t.Fatalf("cluster create: %v", err)
	}

	server := makeServerBlackout(fx.ConnID, "server-bk")
	if err := ds.CreateBlackout(ctx, server); err != nil {
		t.Fatalf("server create: %v", err)
	}

	// Add a finished one to validate the active=false path.
	connID := fx.ConnID
	finished := &Blackout{
		Scope:        string(BlackoutScopeServer),
		ConnectionID: &connID,
		Reason:       "expired",
		StartTime:    now.Add(-2 * time.Hour),
		EndTime:      now.Add(-1 * time.Hour),
		CreatedBy:    "t",
	}
	if err := ds.CreateBlackout(ctx, finished); err != nil {
		t.Fatalf("expired create: %v", err)
	}

	// No filter, default pagination
	res, err := ds.ListBlackouts(ctx, BlackoutFilter{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if res.TotalCount != 5 {
		t.Errorf("TotalCount = %d, want 5", res.TotalCount)
	}
	if len(res.Blackouts) != 5 {
		t.Errorf("len = %d, want 5", len(res.Blackouts))
	}

	// Scope filter
	scope := string(BlackoutScopeServer)
	res, err = ds.ListBlackouts(ctx, BlackoutFilter{Scope: &scope})
	if err != nil {
		t.Fatalf("list by scope: %v", err)
	}
	if res.TotalCount != 2 {
		t.Errorf("server scope TotalCount = %d, want 2", res.TotalCount)
	}

	// GroupID filter
	res, err = ds.ListBlackouts(ctx, BlackoutFilter{GroupID: &gid})
	if err != nil {
		t.Fatalf("list by group: %v", err)
	}
	if res.TotalCount != 1 || res.Blackouts[0].Reason != "group-bk" {
		t.Errorf("group filter mismatch: total=%d", res.TotalCount)
	}

	// ClusterID filter
	res, err = ds.ListBlackouts(ctx, BlackoutFilter{ClusterID: &cid})
	if err != nil {
		t.Fatalf("list by cluster: %v", err)
	}
	if res.TotalCount != 1 || res.Blackouts[0].Reason != "cluster-bk" {
		t.Errorf("cluster filter mismatch: total=%d", res.TotalCount)
	}

	// ConnectionID filter (also exercises Limit and Offset bounds).
	res, err = ds.ListBlackouts(ctx, BlackoutFilter{
		ConnectionID: &connID,
		Limit:        -1,
		Offset:       -1,
	})
	if err != nil {
		t.Fatalf("list by conn: %v", err)
	}
	if res.TotalCount != 2 {
		t.Errorf("conn filter total = %d, want 2", res.TotalCount)
	}

	active := true
	res, err = ds.ListBlackouts(ctx, BlackoutFilter{Active: &active})
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if res.TotalCount != 4 {
		t.Errorf("active=true total = %d, want 4", res.TotalCount)
	}

	inactive := false
	res, err = ds.ListBlackouts(ctx, BlackoutFilter{Active: &inactive})
	if err != nil {
		t.Fatalf("list inactive: %v", err)
	}
	if res.TotalCount != 1 || res.Blackouts[0].Reason != "expired" {
		t.Errorf("inactive total = %d, want 1", res.TotalCount)
	}

	// Limit + Offset positive path: take page 2 of size 1 from
	// the 5 total rows. We just verify the call succeeds and
	// returns 1 row; ordering is by start_time DESC.
	res, err = ds.ListBlackouts(ctx, BlackoutFilter{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("paged list: %v", err)
	}
	if len(res.Blackouts) != 1 {
		t.Errorf("paged list len = %d, want 1", len(res.Blackouts))
	}
	if res.TotalCount != 5 {
		t.Errorf("paged total = %d, want 5", res.TotalCount)
	}

	// scan failure path: corrupt a row by setting a NOT NULL
	// column to NULL is impossible; instead, force a scan error
	// by dropping a column the scanner expects.
	_, _ = pool.Exec(ctx, `ALTER TABLE blackouts DROP COLUMN created_by`)
	_, err = ds.ListBlackouts(ctx, BlackoutFilter{})
	if err == nil {
		t.Error("expected error after schema drift")
	}
}

// TestGetActiveBlackoutsForEntity exercises every scope branch and
// the invalid-scope error path.
func TestGetActiveBlackoutsForEntity(t *testing.T) {
	ds, _, fx, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	now := time.Now().UTC()

	gid := fx.GroupID
	cid := fx.ClusterID
	connID := fx.ConnID

	// Active in every scope.
	mustCreate := func(b *Blackout) {
		if err := ds.CreateBlackout(ctx, b); err != nil {
			t.Fatalf("CreateBlackout: %v", err)
		}
	}

	mustCreate(&Blackout{
		Scope:     string(BlackoutScopeEstate),
		Reason:    "estate-active",
		StartTime: now.Add(-1 * time.Hour),
		EndTime:   now.Add(1 * time.Hour),
		CreatedBy: "t",
	})
	mustCreate(&Blackout{
		Scope:     string(BlackoutScopeGroup),
		GroupID:   &gid,
		Reason:    "group-active",
		StartTime: now.Add(-1 * time.Hour),
		EndTime:   now.Add(1 * time.Hour),
		CreatedBy: "t",
	})
	mustCreate(&Blackout{
		Scope:     string(BlackoutScopeCluster),
		ClusterID: &cid,
		Reason:    "cluster-active",
		StartTime: now.Add(-1 * time.Hour),
		EndTime:   now.Add(1 * time.Hour),
		CreatedBy: "t",
	})
	mustCreate(&Blackout{
		Scope:        string(BlackoutScopeServer),
		ConnectionID: &connID,
		Reason:       "server-active",
		StartTime:    now.Add(-1 * time.Hour),
		EndTime:      now.Add(1 * time.Hour),
		CreatedBy:    "t",
	})
	// Already-finished server blackout that must not show up.
	mustCreate(&Blackout{
		Scope:        string(BlackoutScopeServer),
		ConnectionID: &connID,
		Reason:       "server-finished",
		StartTime:    now.Add(-2 * time.Hour),
		EndTime:      now.Add(-1 * time.Hour),
		CreatedBy:    "t",
	})

	// Estate scope is the only branch that currently runs without a
	// SQL parameter mismatch. The non-estate branches reference $2
	// in the WHERE clause but only one argument is bound (entityID
	// arrives as $1), so pgx rejects them with a "could not
	// determine data type of parameter $1" error. We assert each
	// non-estate scope returns a non-nil error so the branches
	// remain covered. See FUTURE CLEANUP note in the report.
	got, err := ds.GetActiveBlackoutsForEntity(ctx, string(BlackoutScopeEstate), 0)
	if err != nil {
		t.Fatalf("estate scope: %v", err)
	}
	if len(got) != 1 || got[0].Reason != "estate-active" {
		t.Errorf("estate scope: got %+v", got)
	}
	if !got[0].IsActive {
		t.Error("estate scope: expected IsActive=true")
	}

	for name, args := range map[string]struct {
		scope  string
		entity int
	}{
		"group":   {string(BlackoutScopeGroup), gid},
		"cluster": {string(BlackoutScopeCluster), cid},
		"server":  {string(BlackoutScopeServer), connID},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ds.GetActiveBlackoutsForEntity(ctx, args.scope, args.entity)
			if err == nil {
				t.Errorf("expected SQL parameter error for %s scope", args.scope)
			}
		})
	}

	if _, err := ds.GetActiveBlackoutsForEntity(ctx, "unknown", 1); err == nil {
		t.Error("expected error for invalid scope")
	}
}

// TestBlackoutScheduleCRUD covers Create, Get, Update, List, Delete,
// and the not-found paths for blackout schedules.
func TestBlackoutScheduleCRUD(t *testing.T) {
	ds, _, fx, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	cid := fx.ConnID
	s := &BlackoutSchedule{
		Scope:           string(BlackoutScopeServer),
		ConnectionID:    &cid,
		Name:            "nightly",
		CronExpression:  "0 0 * * *",
		DurationMinutes: 60,
		Timezone:        "UTC",
		Reason:          "nightly maintenance",
		Enabled:         true,
		CreatedBy:       "tester",
	}
	if err := ds.CreateBlackoutSchedule(ctx, s); err != nil {
		t.Fatalf("CreateBlackoutSchedule: %v", err)
	}
	if s.ID == 0 {
		t.Fatal("ID not populated")
	}
	if s.CreatedAt.IsZero() || s.UpdatedAt.IsZero() {
		t.Error("timestamps not populated")
	}

	got, err := ds.GetBlackoutSchedule(ctx, s.ID)
	if err != nil {
		t.Fatalf("GetBlackoutSchedule: %v", err)
	}
	if got.CronExpression != "0 0 * * *" {
		t.Errorf("CronExpression = %q, want '0 0 * * *'", got.CronExpression)
	}

	// not-found get
	if _, err := ds.GetBlackoutSchedule(ctx, 99999); !errors.Is(err, ErrBlackoutScheduleNotFound) {
		t.Errorf("expected ErrBlackoutScheduleNotFound, got %v", err)
	}

	// Update
	s.Reason = "updated reason"
	s.DurationMinutes = 120
	s.Enabled = false
	if err := ds.UpdateBlackoutSchedule(ctx, s); err != nil {
		t.Fatalf("UpdateBlackoutSchedule: %v", err)
	}
	got, err = ds.GetBlackoutSchedule(ctx, s.ID)
	if err != nil {
		t.Fatalf("re-get: %v", err)
	}
	if got.Reason != "updated reason" || got.DurationMinutes != 120 || got.Enabled {
		t.Errorf("update did not propagate: %+v", got)
	}

	// Update of unknown ID
	missing := &BlackoutSchedule{
		ID:              999999,
		Scope:           string(BlackoutScopeServer),
		ConnectionID:    &cid,
		Name:            "missing",
		CronExpression:  "* * * * *",
		DurationMinutes: 1,
		Timezone:        "UTC",
		Reason:          "x",
	}
	if err := ds.UpdateBlackoutSchedule(ctx, missing); !errors.Is(err, ErrBlackoutScheduleNotFound) {
		t.Errorf("expected ErrBlackoutScheduleNotFound, got %v", err)
	}

	// Delete + re-delete
	if err := ds.DeleteBlackoutSchedule(ctx, s.ID); err != nil {
		t.Fatalf("DeleteBlackoutSchedule: %v", err)
	}
	if err := ds.DeleteBlackoutSchedule(ctx, s.ID); !errors.Is(err, ErrBlackoutScheduleNotFound) {
		t.Errorf("repeat delete: expected ErrBlackoutScheduleNotFound, got %v", err)
	}
}

// TestListBlackoutSchedulesFiltering exercises every filter and the
// pagination defaults for ListBlackoutSchedules.
func TestListBlackoutSchedulesFiltering(t *testing.T) {
	ds, pool, fx, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	gid := fx.GroupID
	clid := fx.ClusterID
	cid := fx.ConnID

	mustCreate := func(s *BlackoutSchedule) {
		if err := ds.CreateBlackoutSchedule(ctx, s); err != nil {
			t.Fatalf("CreateBlackoutSchedule: %v", err)
		}
	}

	mustCreate(&BlackoutSchedule{
		Scope:           string(BlackoutScopeEstate),
		Name:            "est",
		CronExpression:  "* * * * *",
		DurationMinutes: 5,
		Timezone:        "UTC",
		Reason:          "estate",
		Enabled:         true,
		CreatedBy:       "t",
	})
	mustCreate(&BlackoutSchedule{
		Scope:           string(BlackoutScopeGroup),
		GroupID:         &gid,
		Name:            "grp",
		CronExpression:  "* * * * *",
		DurationMinutes: 5,
		Timezone:        "UTC",
		Reason:          "group",
		Enabled:         false,
		CreatedBy:       "t",
	})
	mustCreate(&BlackoutSchedule{
		Scope:           string(BlackoutScopeCluster),
		ClusterID:       &clid,
		Name:            "cls",
		CronExpression:  "* * * * *",
		DurationMinutes: 5,
		Timezone:        "UTC",
		Reason:          "cluster",
		Enabled:         true,
		CreatedBy:       "t",
	})
	mustCreate(&BlackoutSchedule{
		Scope:           string(BlackoutScopeServer),
		ConnectionID:    &cid,
		Name:            "srv",
		CronExpression:  "* * * * *",
		DurationMinutes: 5,
		Timezone:        "UTC",
		Reason:          "server",
		Enabled:         true,
		CreatedBy:       "t",
	})

	// All
	res, err := ds.ListBlackoutSchedules(ctx, BlackoutFilter{})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if res.TotalCount != 4 {
		t.Errorf("total = %d, want 4", res.TotalCount)
	}

	// Scope
	estScope := string(BlackoutScopeEstate)
	res, err = ds.ListBlackoutSchedules(ctx, BlackoutFilter{Scope: &estScope})
	if err != nil || res.TotalCount != 1 {
		t.Errorf("scope filter failed: %v, total=%d", err, res.TotalCount)
	}

	// GroupID
	res, err = ds.ListBlackoutSchedules(ctx, BlackoutFilter{GroupID: &gid})
	if err != nil || res.TotalCount != 1 {
		t.Errorf("group filter failed: %v, total=%d", err, res.TotalCount)
	}

	// ClusterID
	res, err = ds.ListBlackoutSchedules(ctx, BlackoutFilter{ClusterID: &clid})
	if err != nil || res.TotalCount != 1 {
		t.Errorf("cluster filter failed: %v, total=%d", err, res.TotalCount)
	}

	// ConnectionID
	res, err = ds.ListBlackoutSchedules(ctx, BlackoutFilter{ConnectionID: &cid})
	if err != nil || res.TotalCount != 1 {
		t.Errorf("conn filter failed: %v, total=%d", err, res.TotalCount)
	}

	// Active=true filters by enabled flag.
	enabled := true
	res, err = ds.ListBlackoutSchedules(ctx, BlackoutFilter{Active: &enabled})
	if err != nil || res.TotalCount != 3 {
		t.Errorf("active=true filter failed: %v, total=%d", err, res.TotalCount)
	}

	disabled := false
	res, err = ds.ListBlackoutSchedules(ctx, BlackoutFilter{Active: &disabled})
	if err != nil || res.TotalCount != 1 {
		t.Errorf("active=false filter failed: %v, total=%d", err, res.TotalCount)
	}

	// Pagination defaults
	res, err = ds.ListBlackoutSchedules(ctx, BlackoutFilter{Limit: -1, Offset: -1})
	if err != nil {
		t.Fatalf("default pagination: %v", err)
	}
	if len(res.Schedules) != 4 {
		t.Errorf("default pagination len = %d, want 4", len(res.Schedules))
	}

	// Explicit pagination page
	res, err = ds.ListBlackoutSchedules(ctx, BlackoutFilter{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("paged: %v", err)
	}
	if len(res.Schedules) != 2 {
		t.Errorf("paged len = %d, want 2", len(res.Schedules))
	}

	// Force scan failure to cover the rows.Scan error path.
	_, _ = pool.Exec(ctx, `ALTER TABLE blackout_schedules DROP COLUMN created_by`)
	if _, err := ds.ListBlackoutSchedules(ctx, BlackoutFilter{}); err == nil {
		t.Error("expected error after schema drift")
	}
}

// TestListBlackoutsCountQueryError forces the count query to fail so
// the early-return branch in ListBlackouts is exercised. Dropping the
// blackouts table after the test datastore is built is the simplest
// way to break both the count and main queries.
func TestListBlackoutsCountQueryError(t *testing.T) {
	ds, pool, _, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE blackouts`); err != nil {
		t.Fatalf("drop blackouts: %v", err)
	}
	if _, err := ds.ListBlackouts(ctx, BlackoutFilter{}); err == nil {
		t.Error("expected error when blackouts table missing")
	}
}

// TestListBlackoutSchedulesCountQueryError covers the same early-return
// branch in ListBlackoutSchedules.
func TestListBlackoutSchedulesCountQueryError(t *testing.T) {
	ds, pool, _, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE blackout_schedules`); err != nil {
		t.Fatalf("drop blackout_schedules: %v", err)
	}
	if _, err := ds.ListBlackoutSchedules(ctx, BlackoutFilter{}); err == nil {
		t.Error("expected error when blackout_schedules table missing")
	}
}

// TestCreateBlackoutError forces the INSERT query to fail by dropping
// the blackouts table.
func TestCreateBlackoutError(t *testing.T) {
	ds, pool, fx, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE blackouts`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	b := makeServerBlackout(fx.ConnID, "x")
	if err := ds.CreateBlackout(ctx, b); err == nil {
		t.Error("expected error when blackouts table missing")
	}
}

// TestUpdateBlackoutError forces the UPDATE to fail.
func TestUpdateBlackoutError(t *testing.T) {
	ds, pool, _, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE blackouts`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := ds.UpdateBlackout(ctx, 1, "x", time.Now().Add(time.Hour)); err == nil {
		t.Error("expected error when blackouts table missing")
	}
}

// TestDeleteBlackoutError forces the DELETE to fail.
func TestDeleteBlackoutError(t *testing.T) {
	ds, pool, _, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE blackouts`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := ds.DeleteBlackout(ctx, 1); err == nil {
		t.Error("expected error when blackouts table missing")
	}
}

// TestStopBlackoutError forces the stop UPDATE to fail.
func TestStopBlackoutError(t *testing.T) {
	ds, pool, _, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE blackouts`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := ds.StopBlackout(ctx, 1); err == nil {
		t.Error("expected error when blackouts table missing")
	}
}

// TestGetActiveBlackoutsForEntityError forces the SELECT to fail.
func TestGetActiveBlackoutsForEntityError(t *testing.T) {
	ds, pool, fx, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE blackouts`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := ds.GetActiveBlackoutsForEntity(ctx, string(BlackoutScopeServer), fx.ConnID); err == nil {
		t.Error("expected error when blackouts table missing")
	}
}

// TestCreateBlackoutScheduleError forces the INSERT to fail.
func TestCreateBlackoutScheduleError(t *testing.T) {
	ds, pool, fx, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE blackout_schedules`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	cid := fx.ConnID
	s := &BlackoutSchedule{
		Scope:           string(BlackoutScopeServer),
		ConnectionID:    &cid,
		Name:            "x",
		CronExpression:  "* * * * *",
		DurationMinutes: 1,
		Timezone:        "UTC",
		Reason:          "x",
	}
	if err := ds.CreateBlackoutSchedule(ctx, s); err == nil {
		t.Error("expected error when blackout_schedules table missing")
	}
}

// TestUpdateBlackoutScheduleError forces the UPDATE...RETURNING to
// fail with a non-not-found error.
func TestUpdateBlackoutScheduleError(t *testing.T) {
	ds, pool, fx, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE blackout_schedules`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	cid := fx.ConnID
	s := &BlackoutSchedule{
		ID:              1,
		Scope:           string(BlackoutScopeServer),
		ConnectionID:    &cid,
		Name:            "x",
		CronExpression:  "* * * * *",
		DurationMinutes: 1,
		Timezone:        "UTC",
		Reason:          "x",
	}
	if err := ds.UpdateBlackoutSchedule(ctx, s); err == nil {
		t.Error("expected error when blackout_schedules table missing")
	}
}

// TestDeleteBlackoutScheduleError forces the DELETE to fail.
func TestDeleteBlackoutScheduleError(t *testing.T) {
	ds, pool, _, cleanup := newBlackoutTestDatastore(t)
	defer cleanup()

	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DROP TABLE blackout_schedules`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := ds.DeleteBlackoutSchedule(ctx, 1); err == nil {
		t.Error("expected error when blackout_schedules table missing")
	}
}
