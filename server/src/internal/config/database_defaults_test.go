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
	"strings"
	"testing"
)

// TestLoadConfigDatabaseSectionDefaults checks that a database section
// in the configuration file which omits host, port, database or sslmode
// gets the documented default for each omitted field (issue #860),
// rather than the zero value.
func TestLoadConfigDatabaseSectionDefaults(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want DatabaseConfig
	}{
		{
			name: "omits host",
			yaml: "database:\n  port: 5433\n  database: wb\n  user: u\n  sslmode: require\n",
			want: DatabaseConfig{Host: "localhost", Port: 5433, Database: "wb", User: "u", SSLMode: "require"},
		},
		{
			name: "omits port",
			yaml: "database:\n  host: db.example.com\n  database: wb\n  user: u\n  sslmode: require\n",
			want: DatabaseConfig{Host: "db.example.com", Port: 5432, Database: "wb", User: "u", SSLMode: "require"},
		},
		{
			name: "omits database",
			yaml: "database:\n  host: db.example.com\n  port: 5433\n  user: u\n  sslmode: require\n",
			want: DatabaseConfig{Host: "db.example.com", Port: 5433, Database: "postgres", User: "u", SSLMode: "require"},
		},
		{
			name: "omits sslmode",
			yaml: "database:\n  host: db.example.com\n  port: 5433\n  database: wb\n  user: u\n",
			want: DatabaseConfig{Host: "db.example.com", Port: 5433, Database: "wb", User: "u", SSLMode: "prefer"},
		},
		{
			name: "omits all four",
			yaml: "database:\n  user: u\n",
			want: DatabaseConfig{Host: "localhost", Port: 5432, Database: "postgres", User: "u", SSLMode: "prefer"},
		},
		{
			name: "multi-host list is kept",
			yaml: "database:\n  host: h1,h2:5434\n  user: u\n",
			want: DatabaseConfig{Host: "h1,h2:5434", Port: 5432, Database: "postgres", User: "u", SSLMode: "prefer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempConfig(t, tt.yaml)
			cfg, err := LoadConfig(path, CLIFlags{ConfigFileSet: true, ConfigFile: path})
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if cfg.Database == nil {
				t.Fatal("expected database config")
			}
			got := *cfg.Database
			if got.Host != tt.want.Host || got.Port != tt.want.Port ||
				got.Database != tt.want.Database || got.User != tt.want.User ||
				got.SSLMode != tt.want.SSLMode {
				t.Errorf("database = {Host:%q Port:%d Database:%q User:%q SSLMode:%q}, want {Host:%q Port:%d Database:%q User:%q SSLMode:%q}",
					got.Host, got.Port, got.Database, got.User, got.SSLMode,
					tt.want.Host, tt.want.Port, tt.want.Database, tt.want.User, tt.want.SSLMode)
			}
		})
	}
}

// TestLoadConfigDatabaseSectionDefaultsConnectionString checks the
// symptom reported in issue #860: a database section that omits port
// and database must not produce "host:0" or an empty database path.
func TestLoadConfigDatabaseSectionDefaultsConnectionString(t *testing.T) {
	path := writeTempConfig(t, "database:\n  host: db.example.com\n  user: workbench\n")
	cfg, err := LoadConfig(path, CLIFlags{ConfigFileSet: true, ConfigFile: path})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	got := cfg.Database.BuildConnectionString()
	want := "postgres://workbench@db.example.com:5432/postgres?sslmode=prefer"
	if got != want {
		t.Errorf("connection string = %q, want %q", got, want)
	}
	if strings.Contains(got, ":0/") {
		t.Errorf("connection string %q carries port 0", got)
	}
}

// TestLoadConfigDatabaseFlagsOverrideFileAndDefaults checks that the
// -db-* flags still take precedence over both the file's values and the
// defaults applied to fields the file omits.
func TestLoadConfigDatabaseFlagsOverrideFileAndDefaults(t *testing.T) {
	path := writeTempConfig(t, "database:\n  host: filehost\n  port: 5433\n  user: fileuser\n")
	cfg, err := LoadConfig(path, CLIFlags{
		ConfigFileSet: true, ConfigFile: path,
		DBHostSet: true, DBHost: "flaghost",
		DBPortSet: true, DBPort: 6543,
		DBNameSet: true, DBName: "flagdb",
		DBSSLSet: true, DBSSLMode: "verify-full",
	})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	d := cfg.Database
	if d.Host != "flaghost" || d.Port != 6543 || d.Database != "flagdb" ||
		d.SSLMode != "verify-full" || d.User != "fileuser" {
		t.Errorf("database = %+v, want flag values with file user", *d)
	}
}

// TestLoadConfigNoDatabaseSectionStaysNil checks that the defaults do not
// invent a database section when neither the file nor the flags set one.
func TestLoadConfigNoDatabaseSectionStaysNil(t *testing.T) {
	path := writeTempConfig(t, "http:\n  address: \":9000\"\n")
	cfg, err := LoadConfig(path, CLIFlags{ConfigFileSet: true, ConfigFile: path})
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Database != nil {
		t.Errorf("database = %+v, want nil", *cfg.Database)
	}
}
