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

	type target struct {
		host string
		port uint16
	}
	lists := []struct {
		name string
		in   string
		want []target
	}{
		{"two hosts", "db1.example.com,db2.example.com",
			[]target{{"db1.example.com", 5433}, {"db2.example.com", 5433}}},
		{"three hosts with IPv6", "db1.example.com,::1,[2001:db8::10]",
			[]target{{"db1.example.com", 5433}, {"::1", 5433}, {"2001:db8::10", 5433}}},
		{"own ports kept", "db1.example.com:5434,db2.example.com,[2001:db8::10]:5435",
			[]target{{"db1.example.com", 5434}, {"db2.example.com", 5433}, {"2001:db8::10", 5435}}},
		{"empty own port takes the default", "db1.example.com:,db2.example.com",
			[]target{{"db1.example.com", 5433}, {"db2.example.com", 5433}}},
	}
	for _, l := range lists {
		t.Run("host list "+l.name, func(t *testing.T) {
			cfg := DatabaseConfig{User: "admin", Host: l.in, Port: 5433, Database: "mydb", SSLMode: "disable"}
			dsn := cfg.BuildConnectionString()
			parsed, err := pgconn.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("ParseConfig(%q): %v", dsn, err)
			}
			got := []target{{parsed.Host, parsed.Port}}
			for _, fb := range parsed.Fallbacks {
				got = append(got, target{fb.Host, fb.Port})
			}
			if len(got) != len(l.want) {
				t.Fatalf("targets = %v, want %v (dsn %q)", got, l.want, dsn)
			}
			for i := range got {
				if got[i] != l.want[i] {
					t.Errorf("target %d = %v, want %v (dsn %q)", i, got[i], l.want[i], dsn)
				}
			}
		})
	}

	// An entry whose text after the colon is not a port is left whole,
	// so pgx reads it as one bracketed host and it never reaches a port
	// pgx did not see as one.
	for _, bad := range []string{"db.example.com:abc", "db.example.com:0", "db.example.com:70000"} {
		t.Run("host with invalid port "+bad, func(t *testing.T) {
			cfg := DatabaseConfig{User: "admin", Host: bad, Port: 5433, Database: "mydb", SSLMode: "disable"}
			dsn := cfg.BuildConnectionString()
			parsed, err := pgconn.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("ParseConfig(%q): %v", dsn, err)
			}
			if parsed.Host != bad || parsed.Port != 5433 || len(parsed.Fallbacks) != 0 {
				t.Errorf("target = %s:%d with %d fallbacks, want %s:5433 alone (dsn %q)",
					parsed.Host, parsed.Port, len(parsed.Fallbacks), bad, dsn)
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
