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

const testAppName = "pgEdge AI DBA Workbench"

// TestAddApplicationNameKeepsRawQuery checks that adding
// application_name leaves the existing query text byte-for-byte as it
// was. Decoding and re-encoding it through url.Values would turn a
// literal '+' into a space, which libpq and pgx read differently.
func TestAddApplicationNameKeepsRawQuery(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "literal plus in a path",
			in:   "postgres://user@db.example.com:5432/mydb?sslrootcert=/a+b.crt",
			want: "postgres://user@db.example.com:5432/mydb?sslrootcert=/a+b.crt" +
				"&application_name=pgEdge%20AI%20DBA%20Workbench",
		},
		{
			name: "no query",
			in:   "postgres://user@db.example.com:5432/mydb",
			want: "postgres://user@db.example.com:5432/mydb" +
				"?application_name=pgEdge%20AI%20DBA%20Workbench",
		},
		{
			name: "empty query",
			in:   "postgres://user@db.example.com:5432/mydb?",
			want: "postgres://user@db.example.com:5432/mydb" +
				"?application_name=pgEdge%20AI%20DBA%20Workbench",
		},
		{
			name: "escaped values and order kept",
			in:   "postgres://user@db.example.com:5432/mydb?sslmode=require&options=-c%20x%3Dy",
			want: "postgres://user@db.example.com:5432/mydb?sslmode=require&options=-c%20x%3Dy" +
				"&application_name=pgEdge%20AI%20DBA%20Workbench",
		},
		{
			name: "existing application_name left alone",
			in:   "postgres://user@db.example.com:5432/mydb?application_name=a+b&sslmode=disable",
			want: "postgres://user@db.example.com:5432/mydb?application_name=a+b&sslmode=disable",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := addApplicationName(tc.in, testAppName)
			if err != nil {
				t.Fatalf("addApplicationName: %v", err)
			}
			if got != tc.want {
				t.Errorf("addApplicationName =\n %q, want\n %q", got, tc.want)
			}
		})
	}

	t.Run("pgx reads the plus literally", func(t *testing.T) {
		got, err := addApplicationName(
			"postgres://user@db.example.com:5432/mydb?sslmode=disable&sslrootcert=/a+b.crt",
			testAppName)
		if err != nil {
			t.Fatalf("addApplicationName: %v", err)
		}
		// pgx opens the root certificate whilst parsing, even with
		// sslmode=disable, so the error names the decoded path.
		_, err = pgconn.ParseConfig(got)
		if err == nil || !strings.Contains(err.Error(), "open /a+b.crt:") {
			t.Errorf("ParseConfig error = %v, want it to open /a+b.crt", err)
		}
	})

	t.Run("invalid URL", func(t *testing.T) {
		if _, err := addApplicationName("postgres://user@db.example.com:bad/mydb", testAppName); err == nil {
			t.Error("expected an error for an invalid port")
		}
	})
}

// TestAddApplicationNameHostileValues takes hostile user names,
// passwords and database names through BuildConnectionString,
// addApplicationName and pgconn.ParseConfig, the path ConnectTo
// follows, and checks that every value arrives unchanged, that none
// moves the host, port or fallbacks, and that application_name is set.
func TestAddApplicationNameHostileValues(t *testing.T) {
	values := []string{
		"p@ss:w/rd?#%+&= x",
		" leading",
		"trailing ",
		"  both  ",
		"x?host=evil.example.com",
		"x&application_name=evil",
		"x@evil.example.com:6379",
		"x,evil.example.com",
		"[::1]",
		"%00",
		"%2C",
		"a+b",
		"pässwörd",
		"密码",
		"emoji 🐘",
	}
	for _, v := range values {
		t.Run(v, func(t *testing.T) {
			conn := &MonitoredConnection{
				Host:         "db.example.com",
				Port:         5433,
				DatabaseName: v,
				Username:     v,
				SSLMode:      sql.NullString{String: "verify-full", Valid: true},
			}
			dsn, err := addApplicationName(
				mustBuildConnString(t, &Datastore{}, conn, v, ""), testAppName)
			if err != nil {
				t.Fatalf("addApplicationName: %v", err)
			}
			cfg, err := pgconn.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("ParseConfig failed: %v", err)
			}
			if cfg.User != v || cfg.Password != v || cfg.Database != v {
				t.Errorf("user/password/database = %q/%q/%q, want %q",
					cfg.User, cfg.Password, cfg.Database, v)
			}
			if cfg.Host != "db.example.com" || cfg.Port != 5433 || len(cfg.Fallbacks) != 0 {
				t.Errorf("target = %s:%d with %d fallbacks, want "+
					"db.example.com:5433 alone", cfg.Host, cfg.Port, len(cfg.Fallbacks))
			}
			if got := cfg.RuntimeParams["application_name"]; got != testAppName {
				t.Errorf("application_name = %q, want %q", got, testAppName)
			}
		})
	}
}
