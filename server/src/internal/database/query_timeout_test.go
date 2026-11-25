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
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/config"
)

// TestResolveQueryTimeout covers parsing database.query_timeout.
func TestResolveQueryTimeout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		value   string
		want    time.Duration
		wantErr string
	}{
		{name: "empty selects the per-datastore default", value: "", want: 0},
		{name: "a duration is accepted", value: "90s", want: 90 * time.Second},
		{name: "the maximum is accepted", value: "4m", want: MaxQueryTimeout},
		{name: "an unparseable value is rejected", value: "soon", wantErr: "invalid query_timeout"},
		{name: "zero is rejected", value: "0", wantErr: "is not positive"},
		{name: "a negative value is rejected", value: "-1s", wantErr: "is not positive"},
		{name: "a value over the maximum is rejected", value: "5m", wantErr: "exceeds the maximum"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveQueryTimeout(tt.value)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveQueryTimeout(%q) = %v, %v; want error containing %q",
						tt.value, got, err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("resolveQueryTimeout(%q) = %v, %v; want %v", tt.value, got, err, tt.want)
			}
		})
	}
}

// TestQueryTimeout covers how a request's own budget combines with the
// configured query_timeout and the datastore kind (issue #891).
func TestQueryTimeout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		configured time.Duration
		coldFront  bool
		base       time.Duration
		want       time.Duration
	}{
		{"plain PostgreSQL keeps a short budget", 0, false, ShortQueryTimeout, ShortQueryTimeout},
		{"plain PostgreSQL keeps a long budget", 0, false, LongQueryTimeout, LongQueryTimeout},
		{"ColdFront raises a short budget", 0, true, ShortQueryTimeout, ColdFrontQueryTimeout},
		{"ColdFront raises a long budget", 0, true, LongQueryTimeout, ColdFrontQueryTimeout},
		{"ColdFront keeps a budget above its floor", 0, true, 3 * time.Minute, 3 * time.Minute},
		{"a configured value raises the budget", 45 * time.Second, false, ShortQueryTimeout, 45 * time.Second},
		{"a configured value never lowers the budget", 20 * time.Second, false, LongQueryTimeout, LongQueryTimeout},
		{"a configured value replaces the ColdFront floor", 45 * time.Second, true, ShortQueryTimeout, 45 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := &Datastore{queryTimeoutFloor: effectiveQueryTimeoutFloor(tt.configured, tt.coldFront)}
			if got := d.QueryTimeout(tt.base); got != tt.want {
				t.Errorf("QueryTimeout(%v) = %v, want %v", tt.base, got, tt.want)
			}
		})
	}
}

// TestQueryTimeoutNilDatastore checks that a handler built without a
// datastore keeps its own budget rather than panicking.
func TestQueryTimeoutNilDatastore(t *testing.T) {
	t.Parallel()
	var d *Datastore
	if got := d.QueryTimeout(ShortQueryTimeout); got != ShortQueryTimeout {
		t.Errorf("nil QueryTimeout = %v, want %v", got, ShortQueryTimeout)
	}
}

// TestWithQueryTimeout checks that the derived context carries the
// raised deadline.
func TestWithQueryTimeout(t *testing.T) {
	t.Parallel()
	d := &Datastore{queryTimeoutFloor: ColdFrontQueryTimeout}
	start := time.Now()
	ctx, cancel := d.WithQueryTimeout(context.Background(), ShortQueryTimeout)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("WithQueryTimeout returned a context without a deadline")
	}
	if remaining := deadline.Sub(start); remaining < ColdFrontQueryTimeout ||
		remaining > ColdFrontQueryTimeout+time.Minute {
		t.Errorf("deadline is %v away, want about %v", remaining, ColdFrontQueryTimeout)
	}
}

// TestDefaultStatementTimeout checks that the default statement timeout
// never cancels a statement before the request's own deadline would.
func TestDefaultStatementTimeout(t *testing.T) {
	t.Parallel()
	if got := defaultStatementTimeout(0); got != DefaultDatastoreStatementTimeout {
		t.Errorf("defaultStatementTimeout(0) = %v, want %v", got, DefaultDatastoreStatementTimeout)
	}
	if got := defaultStatementTimeout(10 * time.Second); got != DefaultDatastoreStatementTimeout {
		t.Errorf("defaultStatementTimeout(10s) = %v, want %v", got, DefaultDatastoreStatementTimeout)
	}
	if got := defaultStatementTimeout(ColdFrontQueryTimeout); got != ColdFrontQueryTimeout {
		t.Errorf("defaultStatementTimeout(%v) = %v, want it raised to match",
			ColdFrontQueryTimeout, got)
	}
}

func TestSettleStatementTimeout(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		configured time.Duration
		explicit   bool
		floor      time.Duration
		want       time.Duration
		wantRaised bool
	}{
		{"unset without floor", DefaultDatastoreStatementTimeout, false, 0, DefaultDatastoreStatementTimeout, false},
		{"unset with ColdFront floor", DefaultDatastoreStatementTimeout, false, ColdFrontQueryTimeout, ColdFrontQueryTimeout, false},
		{"explicit without floor", 5 * time.Second, true, 0, 5 * time.Second, false},
		{"explicit below floor is raised", 30 * time.Second, true, ColdFrontQueryTimeout, ColdFrontQueryTimeout, true},
		{"explicit equal to floor", ColdFrontQueryTimeout, true, ColdFrontQueryTimeout, ColdFrontQueryTimeout, false},
		{"explicit above floor", 3 * time.Minute, true, ColdFrontQueryTimeout, 3 * time.Minute, false},
		{"explicit zero stays disabled", 0, true, ColdFrontQueryTimeout, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, raised := settleStatementTimeout(tt.configured, tt.explicit, tt.floor)
			if got != tt.want || raised != tt.wantRaised {
				t.Errorf("settleStatementTimeout(%v, %v, %v) = (%v, %v), want (%v, %v)",
					tt.configured, tt.explicit, tt.floor, got, raised, tt.want, tt.wantRaised)
			}
		})
	}
}

// TestNewDatastoreRejectsInvalidQueryTimeout checks that a bad value
// fails before any connection is attempted, naming the key.
func TestNewDatastoreRejectsInvalidQueryTimeout(t *testing.T) {
	t.Parallel()
	ds, err := NewDatastore(&config.DatabaseConfig{
		Host:         "127.0.0.1",
		Port:         5432,
		Database:     "postgres",
		User:         "postgres",
		QueryTimeout: "forever",
	}, "test-secret")
	if err == nil {
		ds.Close()
		t.Fatal("NewDatastore accepted an invalid query_timeout")
	}
	if !strings.Contains(err.Error(), "invalid query_timeout") {
		t.Fatalf("NewDatastore error = %v, want it to mention the query_timeout key", err)
	}
}

// TestDatastoreAppliesConfiguredQueryTimeout connects a real pool with
// query_timeout set: request deadlines rise to the configured value, and
// so does the pool's statement timeout, whether statement_timeout is
// unset or set explicitly to a shorter value (the "30s" that older
// example configurations carried).
func TestDatastoreAppliesConfiguredQueryTimeout(t *testing.T) {
	for _, statementTimeout := range []string{"", "30s"} {
		t.Run("statement_timeout="+statementTimeout, func(t *testing.T) {
			cfg := testDatastoreConfig(t)
			cfg.QueryTimeout = "45s"
			cfg.StatementTimeout = statementTimeout

			ds, err := NewDatastore(cfg, "test-secret")
			if err != nil {
				t.Skipf("Could not connect to test database: %v", err)
			}
			defer ds.Close()

			if got := ds.QueryTimeout(ShortQueryTimeout); got != 45*time.Second {
				t.Errorf("QueryTimeout(%v) = %v, want 45s", ShortQueryTimeout, got)
			}
			if got := showStatementTimeout(t, ds); got != "45s" {
				t.Errorf("statement_timeout = %q, want \"45s\"", got)
			}
		})
	}
}
