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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildMonitoredConnectionParams_SSLPaths checks that a stored SSL
// file path is passed to pgx only when it lies inside ssl_cert_dir, that
// every path other than sslrootcert=system is refused when no directory
// is configured, and that a refusal is ErrInvalidMonitoredConnString,
// whose text names no path.
func TestBuildMonitoredConnectionParams_SSLPaths(t *testing.T) {
	certDir := t.TempDir()
	inside := filepath.Join(certDir, "client.crt")
	if err := os.WriteFile(inside, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "secret.pem")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	escape := filepath.Join(certDir, "escape.pem")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	ns := func(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }
	tests := []struct {
		name    string
		dir     string
		cert    sql.NullString
		key     sql.NullString
		root    sql.NullString
		wantErr bool
	}{
		{name: "no files, no directory"},
		{name: "system root, no directory", root: ns("system")},
		{name: "cert, no directory", cert: ns(inside), wantErr: true},
		{name: "cert inside", dir: certDir, cert: ns(inside)},
		{name: "key outside", dir: certDir, key: ns(outside), wantErr: true},
		{name: "root via escaping link", dir: certDir, root: ns(escape), wantErr: true},
		{name: "relative cert", dir: certDir, cert: ns("client.crt"), wantErr: true},
		{name: "dot-dot cert", dir: certDir, cert: ns(certDir + "/../x"), wantErr: true},
		{name: "invalid null ignored", cert: sql.NullString{String: outside}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := MonitoredConnection{
				ID: 7, Host: "db.example.com", Port: 5432,
				DatabaseName: "postgres", Username: "u",
				SSLCert: tt.cert, SSLKey: tt.key, SSLRootCert: tt.root,
			}
			params, err := buildMonitoredConnectionParams(conn, "", "", tt.dir)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				if tt.cert.Valid && params["sslcert"] != tt.cert.String {
					t.Errorf("sslcert = %q, want %q", params["sslcert"], tt.cert.String)
				}
				if tt.root.Valid && params["sslrootcert"] != tt.root.String {
					t.Errorf("sslrootcert = %q, want %q", params["sslrootcert"], tt.root.String)
				}
				return
			}
			if !errors.Is(err, ErrInvalidMonitoredConnString) {
				t.Fatalf("err = %v, want ErrInvalidMonitoredConnString", err)
			}
			if params != nil {
				t.Errorf("params = %v, want nil", params)
			}
			if strings.Contains(err.Error(), certDir) || strings.Contains(err.Error(), outside) {
				t.Errorf("error %q names a path", err)
			}
		})
	}
}

// TestMonitoredPoolManager_SSLCertDir checks that the manager hands its
// configured directory to the parameter builder: a pool for a
// connection whose certificate lies outside it is never opened, and
// InvalidateChangedPools skips such a connection rather than hashing it.
func TestMonitoredPoolManager_SSLCertDir(t *testing.T) {
	m := NewMonitoredConnectionPoolManager(2, 60)
	if got := m.SSLCertDir(); got != "" {
		t.Fatalf("SSLCertDir() = %q, want empty", got)
	}
	dir := t.TempDir()
	m.SetSSLCertDir(dir)
	if got := m.SSLCertDir(); got != dir {
		t.Fatalf("SSLCertDir() = %q, want %q", got, dir)
	}

	conn := MonitoredConnection{
		ID: 9, Name: "n", Host: "db.example.com", Port: 5432,
		DatabaseName: "postgres", Username: "u",
		SSLCert: sql.NullString{String: "/etc/shadow", Valid: true},
	}
	_, err := m.createPool(context.Background(), conn, "", "", 9)
	if !errors.Is(err, ErrInvalidMonitoredConnString) {
		t.Fatalf("createPool err = %v, want ErrInvalidMonitoredConnString", err)
	}
	if strings.Contains(err.Error(), "/etc/shadow") {
		t.Errorf("error %q names the path", err)
	}

	// A pool recorded for the connection survives the invalidation
	// pass, because a connection whose parameters cannot be built is
	// skipped, not treated as changed.
	m.mu.Lock()
	m.poolHashes[9] = "stale"
	m.poolKeyToConnID[9] = 9
	m.mu.Unlock()
	m.InvalidateChangedPools([]MonitoredConnection{conn}, "")
	m.mu.RLock()
	_, kept := m.poolHashes[9]
	m.mu.RUnlock()
	if !kept {
		t.Error("InvalidateChangedPools removed a pool for a connection it could not hash")
	}
}
