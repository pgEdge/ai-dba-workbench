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
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pgedge/ai-workbench/pkg/connstring"
	"github.com/pgedge/ai-workbench/pkg/crypto"
)

// TestMonitoredConnStringKeysMatchBuilder builds a monitored connection
// with every option set and checks that the keys it emits are exactly
// monitoredConnStringKeys, so that a parameter added to the builder
// without being allowed (or allowed but never emitted) fails here, and
// that monitoredPoolConfig accepts the resulting string.
func TestMonitoredConnStringKeysMatchBuilder(t *testing.T) {
	clearPGEnv(t)
	certDir := t.TempDir()
	certPath := filepath.Join(certDir, "client.crt")
	keyPath := filepath.Join(certDir, "client.key")
	rootPath := filepath.Join(certDir, "root.crt")
	// The client certificate is self-signed, so it serves as the root
	// certificate too.
	writeClientCert(t, certPath, keyPath)
	pemBytes, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	if err := os.WriteFile(rootPath, pemBytes, 0o600); err != nil {
		t.Fatalf("write root cert: %v", err)
	}

	enc, err := crypto.EncryptPassword("secret", "server-secret")
	if err != nil {
		t.Fatalf("EncryptPassword: %v", err)
	}
	conn := MonitoredConnection{
		ID:                1,
		Name:              "n",
		Host:              "db.example.com",
		HostAddr:          sql.NullString{String: "192.0.2.10", Valid: true},
		Port:              5433,
		DatabaseName:      "default-db",
		Username:          "monitor",
		PasswordEncrypted: sql.NullString{String: enc, Valid: true},
		SSLMode:           sql.NullString{String: "verify-full", Valid: true},
		SSLCert:           sql.NullString{String: certPath, Valid: true},
		SSLKey:            sql.NullString{String: keyPath, Valid: true},
		SSLRootCert:       sql.NullString{String: rootPath, Valid: true},
	}
	params, err := buildMonitoredConnectionParams(conn, "override-db", "server-secret", certDir)
	if err != nil {
		t.Fatalf("buildMonitoredConnectionParams: %v", err)
	}

	emitted := make([]string, 0, len(params))
	for key := range params {
		emitted = append(emitted, key)
	}
	slices.Sort(emitted)
	allowed := slices.Clone(monitoredConnStringKeys)
	slices.Sort(allowed)
	if !slices.Equal(emitted, allowed) {
		t.Fatalf("builder emits %q, monitoredConnStringKeys allows %q", emitted, allowed)
	}

	config, err := monitoredPoolConfig(connstring.Build(params), 1, 0)
	if err != nil {
		t.Fatalf("monitoredPoolConfig rejected the builder's own output: %v", err)
	}
	if config.ConnConfig.Host != "db.example.com" || config.ConnConfig.Database != "override-db" {
		t.Errorf("parsed host/database = %q/%q", config.ConnConfig.Host, config.ConnConfig.Database)
	}
}

// TestMonitoredPoolConfigRejectsUnlistedKeys checks that a monitored
// connection string carrying servicefile, service or passfile is refused
// with ErrInvalidMonitoredConnString, in keyword/value and URL form,
// without the error repeating the string.
func TestMonitoredPoolConfigRejectsUnlistedKeys(t *testing.T) {
	clearPGEnv(t)
	dir := t.TempDir()
	serviceFile := filepath.Join(dir, "pg_service.conf")
	if err := os.WriteFile(serviceFile,
		[]byte("[redirect]\nhost=192.0.2.66\nsslmode=disable\n"), 0o600); err != nil {
		t.Fatalf("write service file: %v", err)
	}
	passFile := filepath.Join(dir, "pgpass")
	if err := os.WriteFile(passFile,
		[]byte("*:*:*:*:pw-from-passfile\n"), 0o600); err != nil {
		t.Fatalf("write passfile: %v", err)
	}

	base := "host=db.example.com port=5432 dbname=d user=u password=Sup3rSecretPw sslmode=require"
	// pgx lets keys in the string win over the service file, so the
	// redirect shows only when the string leaves host out.
	serviceStr := "dbname=d user=u servicefile='" + serviceFile + "' service=redirect"

	// Without the allow-list pgx honors the service file and connects
	// somewhere else entirely, which is what the check exists to stop.
	unguarded, err := pgconn.ParseConfig(serviceStr)
	if err != nil {
		t.Fatalf("pgconn.ParseConfig: %v", err)
	}
	if unguarded.Host != "192.0.2.66" {
		t.Fatalf("service file did not redirect the host (got %q); the test no longer shows the risk", unguarded.Host)
	}

	for name, connStr := range map[string]string{
		"servicefile and service": serviceStr,
		"with host":               base + " servicefile='" + serviceFile + "' service=redirect",
		"servicefile":             base + " servicefile='" + serviceFile + "'",
		"service":                 base + " service=redirect",
		"passfile":                "host=db.example.com dbname=d user=u sslmode=require passfile='" + passFile + "'",
		"url servicefile":         "postgresql://u:Sup3rSecretPw@db.example.com/d?servicefile=" + serviceFile + "&service=redirect",
		"url passfile":            "postgresql://u@db.example.com/d?passfile=" + passFile,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := monitoredPoolConfig(connStr, 1, 0)
			if !errors.Is(err, ErrInvalidMonitoredConnString) {
				t.Fatalf("err = %v, want ErrInvalidMonitoredConnString", err)
			}
			for _, leak := range []string{"Sup3rSecretPw", serviceFile, passFile, "db.example.com"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("error %q contains %q", err, leak)
				}
			}
		})
	}
}
