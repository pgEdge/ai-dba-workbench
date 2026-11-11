/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package config

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestBuildConnectionStringEncodesEveryComponent checks, through
// pgconn.ParseConfig, that database names with URI syntax in them, IPv6
// hosts and hostile sslmode values each reach pgx as given, without
// changing the host, port or any other parameter.
func TestBuildConnectionStringEncodesEveryComponent(t *testing.T) {
	databases := []string{
		"production", "a?b", "a#b", "a%b", "a%2Fb", "my db", "a+b",
		"a/b", "/leading", "x?host=evil.example.com", "x&sslmode=disable",
		"[::1]", "pässwörd",
	}
	for _, db := range databases {
		t.Run("database "+db, func(t *testing.T) {
			cfg := DatabaseConfig{
				User: "admin", Password: "secret", Host: "db.example.com",
				Port: 5433, Database: db, SSLMode: "verify-full",
			}
			dsn := cfg.BuildConnectionString()
			parsed, err := pgconn.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("ParseConfig(%q): %v", dsn, err)
			}
			if parsed.Database != db {
				t.Errorf("database = %q, want %q (dsn %q)", parsed.Database, db, dsn)
			}
			if parsed.Host != "db.example.com" || parsed.Port != 5433 ||
				len(parsed.Fallbacks) != 0 || parsed.TLSConfig == nil {
				t.Errorf("target = %s:%d with %d fallbacks (TLS %v), want "+
					"db.example.com:5433 alone over TLS (dsn %q)", parsed.Host,
					parsed.Port, len(parsed.Fallbacks), parsed.TLSConfig != nil, dsn)
			}
		})
	}

	hosts := []struct{ in, want string }{
		{"::1", "::1"},
		{"[::1]", "::1"},
		{"2001:db8::10", "2001:db8::10"},
		{"[2001:db8::10]", "2001:db8::10"},
		{"192.0.2.1", "192.0.2.1"},
	}
	for _, h := range hosts {
		t.Run("host "+h.in, func(t *testing.T) {
			cfg := DatabaseConfig{User: "admin", Host: h.in, Port: 5433, Database: "mydb"}
			dsn := cfg.BuildConnectionString()
			parsed, err := pgconn.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("ParseConfig(%q): %v", dsn, err)
			}
			if parsed.Host != h.want || parsed.Port != 5433 || parsed.Database != "mydb" {
				t.Errorf("target = %s:%d/%s, want %s:5433/mydb (dsn %q)",
					parsed.Host, parsed.Port, parsed.Database, h.want, dsn)
			}
		})
	}

	t.Run("hostile sslmode stays one value", func(t *testing.T) {
		cfg := DatabaseConfig{
			User: "admin", Host: "db.example.com", Port: 5433, Database: "mydb",
			SSLMode: "disable&host=evil.example.com",
		}
		dsn := cfg.BuildConnectionString()
		const want = "postgres://admin@db.example.com:5433/mydb" +
			"?sslmode=disable%26host%3Devil.example.com"
		if dsn != want {
			t.Errorf("dsn = %q, want %q", dsn, want)
		}
		// pgx refuses the unknown sslmode rather than reading a host
		// out of it.
		if _, err := pgconn.ParseConfig(dsn); err == nil {
			t.Error("expected ParseConfig to refuse the sslmode")
		}
	})
}
