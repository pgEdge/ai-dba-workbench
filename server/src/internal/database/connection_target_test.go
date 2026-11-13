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
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgedge/ai-workbench/pkg/hostvalidation"
)

// TestBuildConnectionStringRefusesUnsafeTarget checks the connect-time
// guard: a stored host or hostaddr that would turn into a multi-host
// list, carry a port, or (for hostaddr) name anything but an IP address
// is refused with ErrUnsafeConnectionTarget, whatever route wrote it.
func TestBuildConnectionStringRefusesUnsafeTarget(t *testing.T) {
	cases := []struct {
		name     string
		host     string
		hostaddr string
	}{
		{"comma host list", "db.example.com,169.254.169.254", ""},
		{"encoded comma host", "db.example.com%2C169.254.169.254", ""},
		{"bracket host list", "[127.0.0.1]:6379,[127.0.0.1]", ""},
		{"inner bracket host", "db]example.com", ""},
		{"slash host", "db.example.com/other", ""},
		{"query host", "db.example.com?host=evil.example.com", ""},
		{"userinfo host", "user@db.example.com", ""},
		{"space host", "db.example.com 127.0.0.1", ""},
		{"bracket hostaddr list", "db.example.com", "127.0.0.1]:6379,[127.0.0.1"},
		{"comma hostaddr list", "db.example.com", "127.0.0.1,127.0.0.2"},
		{"hostaddr with port", "db.example.com", "127.0.0.1:6379"},
		{"hostaddr host name", "db.example.com", "evil.example.com"},
		{"neither host nor hostaddr", "", ""},
		{"empty bracketed host", "[]", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := &MonitoredConnection{
				ID:           7,
				Host:         tc.host,
				Port:         5432,
				DatabaseName: "mydb",
				Username:     "user",
			}
			if tc.hostaddr != "" {
				conn.HostAddr = sql.NullString{String: tc.hostaddr, Valid: true}
			}
			dsn, err := (&Datastore{}).BuildConnectionString(conn, "pw", "")
			if !errors.Is(err, ErrUnsafeConnectionTarget) {
				t.Fatalf("error = %v, want ErrUnsafeConnectionTarget", err)
			}
			if dsn != "" {
				t.Errorf("dsn = %q, want empty on refusal", dsn)
			}
		})
	}
}

// TestBuildConnectionStringHostAddrPreferred checks that a valid
// hostaddr, bracketed or not, is the single address pgx dials, whilst
// the host stays the name pgx connects as (its TLS ServerName), and
// that hostaddr never reaches the server as a startup parameter.
func TestBuildConnectionStringHostAddrPreferred(t *testing.T) {
	for _, hostaddr := range []string{"192.0.2.10", "2001:db8::1", "[2001:db8::1]"} {
		t.Run(hostaddr, func(t *testing.T) {
			conn := &MonitoredConnection{
				Host:         "db.example.com",
				HostAddr:     sql.NullString{String: hostaddr, Valid: true},
				SSLMode:      sql.NullString{String: "disable", Valid: true},
				Port:         5432,
				DatabaseName: "mydb",
				Username:     "user",
			}
			cfg, err := pgconn.ParseConfig(
				mustBuildConnString(t, &Datastore{}, conn, "", ""))
			if err != nil {
				t.Fatalf("ParseConfig failed: %v", err)
			}
			if cfg.Host != "db.example.com" || len(cfg.Fallbacks) != 0 {
				t.Errorf("host = %q with %d fallbacks, want db.example.com alone",
					cfg.Host, len(cfg.Fallbacks))
			}

			(&Datastore{}).GuardMonitoredConnConfig(cfg)
			if v, ok := cfg.RuntimeParams["hostaddr"]; ok {
				t.Errorf("hostaddr = %q left in the startup parameters", v)
			}
			addrs, err := cfg.LookupFunc(context.Background(), cfg.Host)
			want := trimOneBracketPairForTest(hostaddr)
			if err != nil || len(addrs) != 1 || addrs[0] != want {
				t.Errorf("lookup = %v, %v; want [%s]", addrs, err, want)
			}
		})
	}
}

// TestBuildConnectionStringHostAddrOnly checks that a connection with a
// hostaddr and no host connects to the address, as libpq does.
func TestBuildConnectionStringHostAddrOnly(t *testing.T) {
	conn := &MonitoredConnection{
		HostAddr:     sql.NullString{String: "[2001:db8::1]", Valid: true},
		SSLMode:      sql.NullString{String: "disable", Valid: true},
		Port:         5432,
		DatabaseName: "mydb",
		Username:     "user",
	}
	cfg, err := pgconn.ParseConfig(mustBuildConnString(t, &Datastore{}, conn, "", ""))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if cfg.Host != "2001:db8::1" {
		t.Errorf("host = %q, want 2001:db8::1", cfg.Host)
	}
}

// TestGuardConnConfigBadHostAddr checks that a hostaddr that is not an
// IP address, which BuildConnectionString never emits, fails the
// connection rather than resolving the host, and is still kept out of
// the startup parameters.
func TestGuardConnConfigBadHostAddr(t *testing.T) {
	cfg, err := pgconn.ParseConfig(
		"postgres://user@db.example.com:5432/mydb?sslmode=disable&hostaddr=evil.example.com")
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	GuardConnConfig(cfg, nil)
	if _, ok := cfg.RuntimeParams["hostaddr"]; ok {
		t.Error("hostaddr left in the startup parameters")
	}
	addrs, err := cfg.LookupFunc(context.Background(), cfg.Host)
	if !errors.Is(err, hostvalidation.ErrHostAddrNotIP) || addrs != nil {
		t.Errorf("lookup = %v, %v; want ErrHostAddrNotIP", addrs, err)
	}
}

// TestClientResolver_ResolveClient_BuildRefused checks that a stored
// connection BuildConnectionString refuses is never handed to the client
// manager, and that the refusal reaches the caller as the sentinel.
func TestClientResolver_ResolveClient_BuildRefused(t *testing.T) {
	resolver := &ClientResolver{
		TokenExtractor: func(ctx context.Context) string { return "test-token-hash" },
		Sessions: &mockSessionProvider{
			session: &ConnectionSession{ConnectionID: 42},
		},
		ConnInfo: &mockConnInfoProvider{
			conn:     &MonitoredConnection{ID: 42, Host: "db.example.com"},
			buildErr: ErrUnsafeConnectionTarget,
		},
		// No client manager: a refusal that fell through would answer
		// with that error rather than the sentinel.
	}

	_, err := resolver.ResolveClient(context.Background())
	if !errors.Is(err, ErrUnsafeConnectionTarget) {
		t.Fatalf("error = %v, want ErrUnsafeConnectionTarget", err)
	}
}

// TestDatabaseOverrideNUL checks both guards against a NUL in a database
// override: ValidateDatabaseName refuses it, and pgx v5.12.0 refuses the
// connection string even if the validator were skipped. A literal "%00"
// is an ordinary name and must reach pgx as those three characters.
func TestDatabaseOverrideNUL(t *testing.T) {
	conn := &MonitoredConnection{
		Host:         "db.example.com",
		Port:         5432,
		DatabaseName: "mydb",
		Username:     "user",
		SSLMode:      sql.NullString{String: "disable", Valid: true},
	}

	for _, name := range []string{"my\x00db", "\x00", "mydb\x00options=-c role=postgres"} {
		if err := ValidateDatabaseName(name); err == nil {
			t.Errorf("ValidateDatabaseName(%q) succeeded, want an error", name)
		}
		dsn := mustBuildConnString(t, &Datastore{}, conn, "", name)
		if _, err := pgconn.ParseConfig(dsn); err == nil {
			t.Errorf("pgx accepted a NUL database name (dsn %q)", dsn)
		}
	}

	for _, dsn := range []string{
		"postgres://user@db.example.com:5432/my%00db?sslmode=disable",
		"postgres://user@db.example.com:5432/?dbname=my%00db&sslmode=disable",
		"postgres://user@db.example.com:5432/my\x00db?sslmode=disable",
		"host=db.example.com dbname='my\x00db' sslmode=disable",
	} {
		if _, err := pgconn.ParseConfig(dsn); err == nil {
			t.Errorf("pgx accepted NUL in %q", dsn)
		}
	}

	if err := ValidateDatabaseName("%00"); err != nil {
		t.Fatalf("ValidateDatabaseName(%%00) = %v, want nil", err)
	}
	cfg, err := pgconn.ParseConfig(mustBuildConnString(t, &Datastore{}, conn, "", "%00"))
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	if cfg.Database != "%00" {
		t.Errorf("database = %q, want the literal %%00", cfg.Database)
	}
}

func trimOneBracketPairForTest(s string) string {
	if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
		return s[1 : len(s)-1]
	}
	return s
}

// TestBuildConnectionStringHostileUserinfo round-trips hostile user
// names and passwords through pgconn.ParseConfig, the parser pgx uses,
// and checks that none of them changes the host, port or fallbacks.
func TestBuildConnectionStringHostileUserinfo(t *testing.T) {
	values := []string{
		"p@ss:w/rd?#%+&= x",
		" leading",
		"trailing ",
		"  both  ",
		"x?host=evil.example.com",
		"x@evil.example.com:6379",
		"x,evil.example.com",
		"[::1]",
		"%00",
		"%2C",
		"pässwörd",
		"密码",
		"emoji 🐘",
	}
	for _, v := range values {
		t.Run(v, func(t *testing.T) {
			conn := &MonitoredConnection{
				Host:         "db.example.com",
				Port:         5433,
				DatabaseName: "mydb",
				Username:     v,
				SSLMode:      sql.NullString{String: "verify-full", Valid: true},
			}
			cfg, err := pgconn.ParseConfig(
				mustBuildConnString(t, &Datastore{}, conn, v, ""))
			if err != nil {
				t.Fatalf("ParseConfig failed: %v", err)
			}
			if cfg.User != v {
				t.Errorf("user = %q, want %q", cfg.User, v)
			}
			if cfg.Password != v {
				t.Errorf("password = %q, want %q", cfg.Password, v)
			}
			if cfg.Host != "db.example.com" || cfg.Port != 5433 ||
				len(cfg.Fallbacks) != 0 || cfg.Database != "mydb" {
				t.Errorf("target = %s:%d/%s with %d fallbacks, want "+
					"db.example.com:5433/mydb alone", cfg.Host, cfg.Port,
					cfg.Database, len(cfg.Fallbacks))
			}
		})
	}
}
