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
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgedge/ai-workbench/pkg/connstring"
	"github.com/pgedge/ai-workbench/pkg/datastoreconfig"
)

// hostileConnValues are values that break out of a quoted libpq value
// when quotes or backslashes are escaped incorrectly. None is empty,
// because pgconn substitutes a default for an empty user or host.
var hostileConnValues = []string{
	`x' host='evil.example.com`,
	`x\' host=evil.example.com`,
	`x'' host='evil.example.com`,
	`x' hostaddr='192.0.2.66' sslmode='disable`,
	`trailing\`,
	`'`,
	`\`,
	`''`,
	`\'`,
	"a b\tc\nd",
	"a=b c=d",
	` sslcert=/tmp/evil.crt `,
}

// clearPGEnv unsets the libpq environment variables pgconn reads, so the
// parsed configuration reflects only the connection string. Each is
// restored when the test ends. They are unset rather than set empty
// because pgconn rejects an empty PGSSLCERTMODE.
func clearPGEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"PGHOST", "PGPORT", "PGDATABASE", "PGUSER", "PGPASSWORD",
		"PGPASSFILE", "PGAPPNAME", "PGCONNECT_TIMEOUT", "PGSSLMODE",
		"PGSSLKEY", "PGSSLCERT", "PGSSLROOTCERT", "PGSSLCERTMODE",
		"PGSSLPASSWORD", "PGSSLSNI", "PGSERVICE", "PGSERVICEFILE",
		"PGTARGETSESSIONATTRS", "PGOPTIONS", "PGHOSTADDR",
		"PGMINPROTOCOLVERSION", "PGMAXPROTOCOLVERSION",
		"PGREQUIREAUTH", "PGCHANNELBINDING",
	} {
		// t.Setenv records the current value for restoration.
		t.Setenv(name, os.Getenv(name))
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}
}

// writeClientCert writes a self-signed certificate and its key to the
// given paths and returns the certificate's DER bytes.
func writeClientCert(t *testing.T, certPath, keyPath string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "roundtrip"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return der
}

// TestConnstringBuildRoundTripPgconn builds connection strings with a
// hostile value in each field, parses them with pgconn.ParseConfig as
// the collector's pools do, and checks that every field comes back
// exactly, that the host is the intended one with no fallbacks, and that
// no runtime parameter beyond application_name appears.
//
// sslcert and sslkey are not exposed by pgconn.Config, so they name real
// files: pgconn reads both while parsing, and loads the expected
// certificate only if each path survived intact.
func TestConnstringBuildRoundTripPgconn(t *testing.T) {
	clearPGEnv(t)
	dir := t.TempDir()
	defaultCert := filepath.Join(dir, "client.crt")
	defaultKey := filepath.Join(dir, "client.key")
	defaultDER := writeClientCert(t, defaultCert, defaultKey)

	targets := []string{"dbname", "user", "password", "host", "sslcert", "sslkey", "application_name"}
	for vi, v := range hostileConnValues {
		for _, target := range targets {
			params := map[string]string{
				"dbname":           "db",
				"user":             "monitor",
				"password":         "pw",
				"host":             "db.example.com",
				"port":             "5432",
				"sslmode":          "require",
				"sslcert":          defaultCert,
				"sslkey":           defaultKey,
				"application_name": "app",
			}
			wantDER := defaultDER
			switch target {
			case "sslcert", "sslkey":
				// The hostile value is the file name, inside its own
				// directory so the cert and key do not collide.
				sub := filepath.Join(dir, "v"+string(rune('a'+vi))+target)
				if err := os.Mkdir(sub, 0o700); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				certPath := filepath.Join(sub, "cert")
				keyPath := filepath.Join(sub, "key")
				// Concatenate rather than Join, which would clean the
				// value; a slash in it becomes a subdirectory.
				hostilePath := sub + "/" + v
				if err := os.MkdirAll(filepath.Dir(hostilePath), 0o700); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if target == "sslcert" {
					certPath = hostilePath
				} else {
					keyPath = hostilePath
				}
				wantDER = writeClientCert(t, certPath, keyPath)
				params["sslcert"] = certPath
				params["sslkey"] = keyPath
			default:
				params[target] = v
			}

			s := connstring.Build(params)
			cfg, err := pgconn.ParseConfig(s)
			if err != nil {
				t.Errorf("%s=%q: ParseConfig(%q): %v", target, v, s, err)
				continue
			}
			if cfg.Database != params["dbname"] {
				t.Errorf("%s=%q: Database = %q, want %q", target, v, cfg.Database, params["dbname"])
			}
			if cfg.User != params["user"] {
				t.Errorf("%s=%q: User = %q, want %q", target, v, cfg.User, params["user"])
			}
			if cfg.Password != params["password"] {
				t.Errorf("%s=%q: Password = %q, want %q", target, v, cfg.Password, params["password"])
			}
			if cfg.Host != params["host"] {
				t.Errorf("%s=%q: Host = %q, want %q", target, v, cfg.Host, params["host"])
			}
			if cfg.Port != 5432 {
				t.Errorf("%s=%q: Port = %d, want 5432", target, v, cfg.Port)
			}
			if len(cfg.Fallbacks) != 0 {
				t.Errorf("%s=%q: %d fallbacks, want none", target, v, len(cfg.Fallbacks))
			}
			if len(cfg.RuntimeParams) != 1 || cfg.RuntimeParams["application_name"] != params["application_name"] {
				t.Errorf("%s=%q: RuntimeParams = %q, want only application_name=%q",
					target, v, cfg.RuntimeParams, params["application_name"])
			}
			if cfg.TLSConfig == nil || len(cfg.TLSConfig.Certificates) != 1 ||
				!bytes.Equal(cfg.TLSConfig.Certificates[0].Certificate[0], wantDER) {
				t.Errorf("%s=%q: TLS client certificate is not the one at the given path", target, v)
			}
		}
	}
}

// TestConnstringBuildFromConfigRoundTripPgconn checks the datastore path
// used by the collector and alerter with hostile credentials.
func TestConnstringBuildFromConfigRoundTripPgconn(t *testing.T) {
	clearPGEnv(t)
	for _, v := range hostileConnValues {
		cfg := datastoreconfig.DatastoreConfig{
			Host:     "datastore.example.com",
			Port:     5432,
			Database: v,
			Username: v,
			Password: v,
			SSLMode:  "disable",
		}
		s := connstring.BuildFromConfig(cfg, "app "+v)
		parsed, err := pgconn.ParseConfig(s)
		if err != nil {
			t.Errorf("%q: ParseConfig(%q): %v", v, s, err)
			continue
		}
		if parsed.Database != v || parsed.User != v || parsed.Password != v {
			t.Errorf("%q: parsed database %q, user %q, password %q",
				v, parsed.Database, parsed.User, parsed.Password)
		}
		if parsed.Host != cfg.Host || len(parsed.Fallbacks) != 0 || parsed.TLSConfig != nil {
			t.Errorf("%q: parsed host %q with %d fallbacks, TLS %v",
				v, parsed.Host, len(parsed.Fallbacks), parsed.TLSConfig != nil)
		}
		if len(parsed.RuntimeParams) != 1 || parsed.RuntimeParams["application_name"] != "app "+v {
			t.Errorf("%q: RuntimeParams = %q", v, parsed.RuntimeParams)
		}
	}
}

// TestConnstringBuildNULRejected records that pgconn refuses a value
// holding a NUL byte, so a stored NUL makes the connection fail rather
// than truncate or split a value.
func TestConnstringBuildNULRejected(t *testing.T) {
	clearPGEnv(t)
	s := connstring.Build(map[string]string{
		"host":   "db.example.com",
		"dbname": "a\x00 host=evil.example.com",
	})
	_, err := pgconn.ParseConfig(s)
	if err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("ParseConfig(%q) error = %v, want a NUL byte error", s, err)
	}
}
