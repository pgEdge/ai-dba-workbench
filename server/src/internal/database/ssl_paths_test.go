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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"errors"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pgedge/ai-workbench/pkg/sslpaths"
)

// sslTestDir returns a fresh certificate directory with every symbolic
// link in its path resolved, so expectations do not depend on whether
// the system temporary directory is itself a link.
func sslTestDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDatastoreSSLCertDir(t *testing.T) {
	var nilStore *Datastore
	if got := nilStore.SSLCertDir(); got != "" {
		t.Errorf("nil Datastore SSLCertDir = %q", got)
	}
	d := &Datastore{}
	d.SetSSLCertDir("/etc/pgedge/certs")
	if got := d.SSLCertDir(); got != "/etc/pgedge/certs" {
		t.Errorf("SSLCertDir = %q", got)
	}
}

// TestBuildConnectionStringRefusesStoredSSLPath checks the connect-time
// guard on stored SSL file paths, and that its error names no path.
func TestBuildConnectionStringRefusesStoredSSLPath(t *testing.T) {
	dir := sslTestDir(t)
	cases := []struct {
		name string
		dir  string
		conn MonitoredConnection
	}{
		{"root cert outside", dir, MonitoredConnection{SSLRootCert: sql.NullString{String: "/etc/passwd", Valid: true}}},
		{"cert outside", dir, MonitoredConnection{SSLCert: sql.NullString{String: "/etc/passwd", Valid: true}}},
		{"key escapes", dir, MonitoredConnection{SSLKey: sql.NullString{String: dir + "/../x.key", Valid: true}}},
		{"directory unset", "", MonitoredConnection{SSLRootCert: sql.NullString{String: dir + "/ca.crt", Valid: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := tc.conn
			conn.ID, conn.Host, conn.Port = 3, "db.example.com", 5432
			conn.DatabaseName, conn.Username = "mydb", "user"
			d := &Datastore{}
			d.SetSSLCertDir(tc.dir)
			dsn, err := d.BuildConnectionString(&conn, "pw", "")
			if !errors.Is(err, ErrUnsafeSSLPath) || dsn != "" {
				t.Fatalf("got (%q, %v), want ErrUnsafeSSLPath", dsn, err)
			}
			if strings.Contains(err.Error(), "/") {
				t.Errorf("error %q names a path", err)
			}
		})
	}

	// sslrootcert=system needs no directory.
	conn := MonitoredConnection{
		Host: "db.example.com", Port: 5432, DatabaseName: "mydb", Username: "user",
		SSLRootCert: sql.NullString{String: sslpaths.SSLRootCertSystem, Valid: true},
	}
	if _, err := (&Datastore{}).BuildConnectionString(&conn, "", ""); err != nil {
		t.Errorf("sslrootcert=system: %v", err)
	}
}

// writeTestCertPair writes a self-signed certificate and its private key
// in PEM form, which serves pgx as a CA file and as a client pair alike.
func writeTestCertPair(t *testing.T, certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	writeTestFile(t, keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
}

// TestBuildConnectionStringHostileSSLValues checks that SSL file paths
// and an sslmode carrying query syntax such as "&host=" or "=" stay
// inside their own parameter: pgx reads back each value unchanged, and
// the host, port and fallbacks are those of the stored connection.
func TestBuildConnectionStringHostileSSLValues(t *testing.T) {
	dir := sslTestDir(t)
	hostile := []string{
		"&host=evil.example.com",
		"=",
		"a=b",
		"x&sslmode=disable",
		"?host=evil.example.com",
		"#frag",
		"a+b",
		"with space",
		"%26host%3Devil.example.com",
	}
	for _, h := range hostile {
		t.Run(h, func(t *testing.T) {
			certPath := filepath.Join(dir, "cert"+h+".crt")
			keyPath := filepath.Join(dir, "key"+h+".key")
			writeTestCertPair(t, certPath, keyPath)
			conn := &MonitoredConnection{
				Host:         "db.example.com",
				Port:         5433,
				DatabaseName: "mydb",
				Username:     "user",
				SSLMode:      sql.NullString{String: "verify-full", Valid: true},
				SSLCert:      sql.NullString{String: certPath, Valid: true},
				SSLKey:       sql.NullString{String: keyPath, Valid: true},
				SSLRootCert:  sql.NullString{String: certPath, Valid: true},
			}
			d := &Datastore{}
			d.SetSSLCertDir(dir)
			dsn := mustBuildConnString(t, d, conn, "pw", "")

			u, err := url.Parse(dsn)
			if err != nil {
				t.Fatalf("url.Parse: %v", err)
			}
			q := u.Query()
			want := url.Values{
				"sslmode":     {"verify-full"},
				"sslcert":     {certPath},
				"sslkey":      {keyPath},
				"sslrootcert": {certPath},
			}
			if len(q) != len(want) {
				t.Fatalf("query = %v, want exactly %v", q, want)
			}
			for k, v := range want {
				if got := q[k]; len(got) != 1 || got[0] != v[0] {
					t.Errorf("%s = %q, want %q", k, got, v[0])
				}
			}

			cfg, err := pgconn.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("ParseConfig failed: %v", err)
			}
			if cfg.Host != "db.example.com" || cfg.Port != 5433 ||
				len(cfg.Fallbacks) != 0 || cfg.Database != "mydb" {
				t.Errorf("target = %s:%d/%s with %d fallbacks, want "+
					"db.example.com:5433/mydb alone", cfg.Host, cfg.Port,
					cfg.Database, len(cfg.Fallbacks))
			}
			if cfg.TLSConfig == nil || len(cfg.TLSConfig.Certificates) != 1 {
				t.Errorf("pgx did not load the client certificate from the stored path")
			}
		})
	}

	// A hostile sslmode stays one sslmode value, which pgx refuses
	// outright rather than reading as a host or a second sslmode.
	for _, h := range hostile {
		conn := &MonitoredConnection{
			Host: "db.example.com", Port: 5433, DatabaseName: "mydb", Username: "user",
			SSLMode: sql.NullString{String: "verify-full" + h, Valid: true},
		}
		dsn := mustBuildConnString(t, &Datastore{}, conn, "", "")
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatalf("url.Parse: %v", err)
		}
		if q := u.Query(); len(q) != 1 || q.Get("sslmode") != "verify-full"+h {
			t.Errorf("sslmode %q: query = %v", h, q)
		}
		if _, err := pgconn.ParseConfig(dsn); err == nil ||
			!strings.Contains(err.Error(), "sslmode") {
			t.Errorf("sslmode %q: ParseConfig error = %v, want an sslmode refusal", h, err)
		}
	}
}
