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
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pgedge/ai-workbench/pkg/hostvalidation"
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
// hostaddr, bracketed or not, is carried as one bare address beside the
// host name, which stays the host pgx verifies TLS against, and that
// with no host the address stands in for one.
func TestBuildMonitoredConnectionParams_HostAddrBareIP(t *testing.T) {
	cases := map[string]string{
		"192.0.2.10":    "192.0.2.10",
		"2001:db8::1":   "2001:db8::1",
		"[2001:db8::1]": "2001:db8::1",
	}
	for stored, want := range cases {
		for _, host := range []string{"db.example.com", ""} {
			t.Run(stored+"/"+host, func(t *testing.T) {
				conn := MonitoredConnection{
					ID:           3,
					Host:         host,
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
				wantHost := host
				if wantHost == "" {
					wantHost = want
				}
				if cfg.Host != wantHost || len(cfg.Fallbacks) != 0 {
					t.Errorf("host = %q with %d fallbacks, want %q alone",
						cfg.Host, len(cfg.Fallbacks), wantHost)
				}
				if got := cfg.RuntimeParams["hostaddr"]; got != want {
					t.Errorf("hostaddr = %q, want %q", got, want)
				}
			})
		}
	}
}

// TestBuildMonitoredConnectionParams_BracketedHost checks that an IPv6
// literal host stored with one bracket pair reaches the keyword/value
// connection string bare, as pgx needs it.
func TestBuildMonitoredConnectionParams_BracketedHost(t *testing.T) {
	conn := MonitoredConnection{
		ID:           3,
		Host:         "[2001:db8::1]",
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
	if cfg.Host != "2001:db8::1" {
		t.Errorf("host = %q, want 2001:db8::1", cfg.Host)
	}
}

// TestBuildMonitoredConnectionParams_RefusesEmptyTarget checks that a
// stored connection with neither a host nor a hostaddr is refused rather
// than left to pgx's default host.
func TestBuildMonitoredConnectionParams_RefusesEmptyTarget(t *testing.T) {
	for _, conn := range []MonitoredConnection{
		{ID: 3, Port: 5432, DatabaseName: "d", Username: "u"},
		{ID: 3, Host: "[]", Port: 5432, DatabaseName: "d", Username: "u"},
		{ID: 3, Port: 5432, DatabaseName: "d", Username: "u",
			HostAddr: sql.NullString{String: "", Valid: true}},
	} {
		if params, err := buildMonitoredConnectionParams(conn, "", "", ""); err == nil {
			t.Errorf("host %q: expected a refusal, got params %v", conn.Host, params)
		}
	}
}

// TestMonitoredPoolConfig_HostAddr checks that a hostaddr is taken out
// of the startup parameters and becomes the address pgx dials, whilst
// the host name stays the TLS server name verify-full checks, and that
// the always-blocked floor still applies to that address.
func TestMonitoredPoolConfig_HostAddr(t *testing.T) {
	connStr := "host=db.example.com hostaddr=192.0.2.10 port=5432 user=u dbname=d sslmode=verify-full"
	cfg, err := monitoredPoolConfig(connStr, 1, 0)
	if err != nil {
		t.Fatalf("monitoredPoolConfig: %v", err)
	}
	cc := cfg.ConnConfig
	if _, ok := cc.RuntimeParams["hostaddr"]; ok {
		t.Error("hostaddr left in the startup parameters")
	}
	if cc.Host != "db.example.com" {
		t.Errorf("host = %q, want db.example.com", cc.Host)
	}
	if cc.TLSConfig == nil || cc.TLSConfig.ServerName != "db.example.com" {
		t.Errorf("TLS config = %+v, want ServerName db.example.com", cc.TLSConfig)
	}
	addrs, err := cc.LookupFunc(context.Background(), "db.example.com")
	if err != nil || len(addrs) != 1 || addrs[0] != "192.0.2.10" {
		t.Errorf("lookup = %v, %v, want [192.0.2.10]", addrs, err)
	}

	cfg, err = monitoredPoolConfig(
		"host=db.example.com hostaddr=169.254.169.254 user=u dbname=d sslmode=disable", 1, 0)
	if err != nil {
		t.Fatalf("monitoredPoolConfig: %v", err)
	}
	if _, err := cfg.ConnConfig.LookupFunc(context.Background(), "db.example.com"); !errors.Is(err, hostvalidation.ErrNoPermittedAddress) {
		t.Errorf("metadata hostaddr: err = %v, want ErrNoPermittedAddress", err)
	}

	if _, err := monitoredPoolConfig(
		"host=db.example.com hostaddr=evil.example.com user=u dbname=d sslmode=disable", 1, 0); !errors.Is(err, ErrInvalidMonitoredConnString) {
		t.Errorf("name hostaddr: err = %v, want ErrInvalidMonitoredConnString", err)
	}
}

// TestCreateMonitoredPool_DialsHostAddr checks that the pool dials the
// hostaddr rather than resolving the host name: the name is in the
// reserved .invalid domain, so a lookup would fail, and the error must
// instead be the refused connection on loopback port 1.
func TestCreateMonitoredPool_DialsHostAddr(t *testing.T) {
	connStr := "host=db.invalid hostaddr=127.0.0.1 port=1 user=u dbname=d sslmode=disable connect_timeout=1"
	_, err := createMonitoredPool(context.Background(), connStr, 1, 0)
	if err == nil {
		t.Fatal("expected a dial failure on port 1")
	}
	if !strings.Contains(err.Error(), "127.0.0.1") {
		t.Errorf("err = %v, want a dial failure on 127.0.0.1", err)
	}
}
