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
	"database/sql"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestBuildMonitoredConnectionParams_RefusesUnsafeTarget checks the
// collector's defensive check: a stored host or hostaddr that would turn
// the connection into a multi-host list, or a hostaddr that is not an
// IP address, never reaches the DSN, and the error does not repeat the
// stored value (it is written to connections.connection_error).
func TestBuildMonitoredConnectionParams_RefusesUnsafeTarget(t *testing.T) {
	cases := []struct {
		name     string
		host     string
		hostaddr string
	}{
		{"comma host list", "db.example.com,169.254.169.254", ""},
		{"inner bracket host", "db.example.com]", ""},
		{"socket directory host", "/var/run/postgresql", ""},
		{"space host", "db.example.com host=evil.example.com", ""},
		{"quote host", "db.example.com' host='evil.example.com", ""},
		{"bracket hostaddr list", "db.example.com", "127.0.0.1]:6379,[127.0.0.1"},
		{"comma hostaddr list", "db.example.com", "127.0.0.1,169.254.169.254"},
		{"hostaddr host name", "db.example.com", "evil.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := MonitoredConnection{
				ID:           3,
				Host:         tc.host,
				Port:         5432,
				DatabaseName: "d",
				Username:     "u",
			}
			if tc.hostaddr != "" {
				conn.HostAddr = sql.NullString{String: tc.hostaddr, Valid: true}
			}
			params, err := buildMonitoredConnectionParams(conn, "", "", "")
			if err == nil {
				t.Fatalf("expected a refusal, got params %v", params)
			}
			for _, leaked := range []string{tc.host, tc.hostaddr} {
				if leaked != "" && strings.Contains(err.Error(), leaked) {
					t.Errorf("error %q repeats the stored value %q", err, leaked)
				}
			}
		})
	}
}

// TestBuildMonitoredConnectionParams_HostAddrBareIP checks that a valid
// hostaddr, bracketed or not, reaches pgx as one bare address.
func TestBuildMonitoredConnectionParams_HostAddrBareIP(t *testing.T) {
	cases := map[string]string{
		"192.0.2.10":    "192.0.2.10",
		"2001:db8::1":   "2001:db8::1",
		"[2001:db8::1]": "2001:db8::1",
	}
	for stored, want := range cases {
		t.Run(stored, func(t *testing.T) {
			conn := MonitoredConnection{
				ID:           3,
				Host:         "db.example.com",
				HostAddr:     sql.NullString{String: stored, Valid: true},
				Port:         5432,
				DatabaseName: "d",
				Username:     "u",
				SSLMode:      sql.NullString{String: "disable", Valid: true},
			}
			dsn, err := buildMonitoredConnectionStringForDatabase(conn, "", "", "")
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			cfg, err := pgconn.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("ParseConfig(%q) failed: %v", dsn, err)
			}
			if cfg.Host != want || len(cfg.Fallbacks) != 0 {
				t.Errorf("host = %q with %d fallbacks, want %q alone",
					cfg.Host, len(cfg.Fallbacks), want)
			}
		})
	}
}
