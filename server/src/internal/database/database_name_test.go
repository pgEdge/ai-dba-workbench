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
	"maps"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestValidateDatabaseName covers the narrow checks applied to a
// caller-supplied database override: it turns away what cannot name a
// real database without rejecting the unusual names PostgreSQL allows.
func TestValidateDatabaseName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"plain name", "mydb", false},
		{"mixed case and digits", "MyDB_2", false},
		{"punctuation is allowed", "my-db.prod", false},
		{"spaces inside are allowed", "my db", false},
		{"unicode is allowed", "données", false},
		{"injection payload is still a valid name",
			"mydb?host=evil.example.com", false},
		{"63 bytes", strings.Repeat("a", 63), false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"tab only", "\t", true},
		{"64 bytes", strings.Repeat("a", 64), true},
		{"64 bytes of multibyte runes", strings.Repeat("é", 32), true},
		{"NUL byte", "my\x00db", true},
		{"newline", "my\ndb", true},
		{"carriage return", "my\rdb", true},
		{"escape", "my\x1bdb", true},
		{"delete", "my\x7fdb", true},
		{"C1 control", "my\u0085db", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDatabaseName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDatabaseName(%q) = %v, wantErr %v",
					tt.input, err, tt.wantErr)
			}
		})
	}
}

// TestBuildConnectionStringDatabaseNameRoundTrip is the regression guard
// for the connection-parameter injection: a database name carrying URL
// metacharacters must reach pgx as that exact name, leaving the host,
// port, user and password of the stored connection untouched. Asserting
// on the parsed configuration rather than on the string is the point,
// since it is pgx's reading of the DSN that decides where the password
// is sent. This is a parse-only assertion; it never opens a connection.
func TestBuildConnectionStringDatabaseNameRoundTrip(t *testing.T) {
	ds := &Datastore{}

	conn := &MonitoredConnection{
		Host:         "db.example.com",
		Port:         5433,
		DatabaseName: "mydb",
		Username:     "user",
		SSLMode:      sql.NullString{String: "verify-full", Valid: true},
	}
	const password = "s3cr3t"

	// The same connection with a benign name is the baseline: a hostile
	// name must leave every other setting pgx parses exactly as it is
	// here. verify-full gives the baseline no plaintext fallback, so a
	// smuggled sslmode shows up in the TLS settings and the fallbacks.
	baseline, err := pgconn.ParseConfig(
		ds.BuildConnectionString(conn, password, "mydb"))
	if err != nil {
		t.Fatalf("pgconn.ParseConfig(baseline) failed: %v", err)
	}
	if baseline.TLSConfig == nil || len(baseline.Fallbacks) != 0 {
		t.Fatalf("baseline TLS = %v, fallbacks = %d; want TLS and none",
			baseline.TLSConfig, len(baseline.Fallbacks))
	}

	names := []string{
		"mydb",
		"mydb?host=evil.example.com&sslmode=disable",
		"mydb?options=-c%20search_path=public",
		"mydb&user=postgres",
		"my/db",
		"/reports",
		"//reports",
		"/",
		"/mydb?host=evil.example.com&sslmode=disable",
		"/mydb&sslmode=disable&options=-c%20role=postgres",
		"/mydb&dbname=other&host=evil.example.com,db.example.com",
		"mydb?dbname=other&target_session_attrs=any",
		"my#db",
		"my db",
		"100%db",
		"mydb@other",
		"données",
	}

	for _, database := range names {
		t.Run(database, func(t *testing.T) {
			dsn := ds.BuildConnectionString(conn, password, database)

			cfg, err := pgconn.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("pgconn.ParseConfig(%q) failed: %v", dsn, err)
			}
			if cfg.Host != conn.Host {
				t.Errorf("host = %q, want %q (dsn %q)",
					cfg.Host, conn.Host, dsn)
			}
			if cfg.Port != uint16(conn.Port) {
				t.Errorf("port = %d, want %d (dsn %q)",
					cfg.Port, conn.Port, dsn)
			}
			if cfg.User != conn.Username {
				t.Errorf("user = %q, want %q (dsn %q)",
					cfg.User, conn.Username, dsn)
			}
			if cfg.Password != password {
				t.Errorf("password = %q, want %q (dsn %q)",
					cfg.Password, password, dsn)
			}
			if cfg.Database != database {
				t.Errorf("database = %q, want %q (dsn %q)",
					cfg.Database, database, dsn)
			}
			if len(cfg.Fallbacks) != 0 {
				t.Errorf("%d fallbacks, want none (dsn %q)",
					len(cfg.Fallbacks), dsn)
			}
			if !maps.Equal(cfg.RuntimeParams, baseline.RuntimeParams) {
				t.Errorf("runtime params = %v, want %v (dsn %q)",
					cfg.RuntimeParams, baseline.RuntimeParams, dsn)
			}
			if cfg.TLSConfig == nil ||
				cfg.TLSConfig.ServerName != baseline.TLSConfig.ServerName ||
				cfg.TLSConfig.InsecureSkipVerify != baseline.TLSConfig.InsecureSkipVerify {
				t.Errorf("TLS config = %+v, want one matching %+v (dsn %q)",
					cfg.TLSConfig, baseline.TLSConfig, dsn)
			}
		})
	}
}

// TestBuildConnectionStringBracketedIPv6Host confirms that an IPv6
// literal, with or without the brackets a stored value may carry,
// reaches pgx as the same host rather than as a nested bracket run.
func TestBuildConnectionStringBracketedIPv6Host(t *testing.T) {
	ds := &Datastore{}

	for _, host := range []string{"::1", "[::1]"} {
		t.Run(host, func(t *testing.T) {
			conn := &MonitoredConnection{
				Host:         host,
				Port:         5432,
				DatabaseName: "mydb",
				Username:     "user",
			}

			dsn := ds.BuildConnectionString(conn, "", "")

			cfg, err := pgconn.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("pgconn.ParseConfig(%q) failed: %v", dsn, err)
			}
			if cfg.Host != "::1" {
				t.Errorf("host = %q, want %q (dsn %q)", cfg.Host, "::1", dsn)
			}
		})
	}
}
