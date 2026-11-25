/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// TestHostValidator_ValidateHost_BracketedIPv6 checks that one pair of
// brackets is accepted around an IPv6 literal, and that the bare
// address then goes through the same network checks as an unbracketed
// one.
func TestHostValidator_ValidateHost_BracketedIPv6(t *testing.T) {
	strict := NewHostValidator(false, nil, []string{"2001:db8:bad::/48"})
	permissive := NewHostValidator(true, nil, nil)

	cases := []struct {
		name    string
		v       *HostValidator
		host    string
		wantErr bool
	}{
		{"public IPv6", strict, "[2001:db8::10]", false},
		{"loopback refused", strict, "[::1]", true},
		{"blocked range refused", strict, "[2001:db8:bad::1]", true},
		{"loopback with internal allowed", permissive, "[::1]", false},
		{"nested brackets", permissive, "[[::1]]", true},
		{"bracketed IPv4", permissive, "[127.0.0.1]", true},
		{"bracketed host name", permissive, "[db.example.com]", true},
		{"bracketed zone", permissive, "[fe80::1%eth0]", true},
		{"bracketed list", permissive, "[::1],[::2]", true},
		{"empty brackets", permissive, "[]", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.v.ValidateHost(tc.host)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateHost(%q) error = %v, wantErr %v",
					tc.host, err, tc.wantErr)
			}
		})
	}
}

// setStoredSSLPaths writes SSL paths straight into a connection row, as
// a server from before the ssl_cert_dir rules could have stored them.
func setStoredSSLPaths(t *testing.T, env *connCRUDEnv, id int, cert, key, rootCert string) {
	t.Helper()
	if _, err := env.pool.Exec(context.Background(),
		`UPDATE connections SET sslcert = $1, sslkey = $2, sslrootcert = $3
         WHERE id = $4`, cert, key, rootCert, id); err != nil {
		t.Fatalf("set SSL paths: %v", err)
	}
}

// descriptionOf returns the stored description of a connection.
func descriptionOf(t *testing.T, env *connCRUDEnv, id int) string {
	t.Helper()
	var v sql.NullString
	if err := env.pool.QueryRow(context.Background(),
		"SELECT description FROM connections WHERE id = $1", id).
		Scan(&v); err != nil {
		t.Fatalf("read description: %v", err)
	}
	return v.String
}

// dialogBody is an update as the server dialog sends it: every field,
// including the SSL paths, whether or not the user changed them.
func dialogBody(host, description, cert, key, rootCert string) string {
	return fmt.Sprintf(`{"name":"crud-test","description":%q,"host":%q,`+
		`"port":5432,"database_name":"app","username":"alice",`+
		`"ssl_mode":"verify-full","ssl_cert_path":%q,"ssl_key_path":%q,`+
		`"ssl_root_cert_path":%q}`, description, host, cert, key, rootCert)
}

// TestConnectionHandler_UpdateKeepsLegacySSLPaths checks that an update
// repeating SSL paths stored before the path rules existed succeeds,
// with and without connection_security.ssl_cert_dir, whilst any path the
// update changes is still refused when it breaks the rules.
func TestConnectionHandler_UpdateKeepsLegacySSLPaths(t *testing.T) {
	const (
		legacyCert = "/etc/legacy/client.crt"
		legacyKey  = "/etc/legacy/client.key"
		legacyRoot = "certs/ca.pem"
	)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	for _, certDir := range []string{"", dir} {
		t.Run(fmt.Sprintf("ssl_cert_dir=%q", certDir), func(t *testing.T) {
			env := newTargetCRUDEnv(t)
			env.ds.SetSSLCertDir(certDir)
			id := env.seed(t, env.user, "db.example.com", 5432, "app", "alice", nil)
			setStoredSSLPaths(t, env, id, legacyCert, legacyKey, legacyRoot)

			rec := env.serve(env.request(http.MethodPut, connPath(id, ""),
				dialogBody("db.example.com", "edited", legacyCert, legacyKey, legacyRoot), true))
			expectStatus(t, rec, http.StatusOK)
			if got := descriptionOf(t, env, id); got != "edited" {
				t.Errorf("description = %q, want edited", got)
			}
			if got := sslRootCertOf(t, env, id); got != legacyRoot {
				t.Errorf("sslrootcert = %q, want the stored %q kept", got, legacyRoot)
			}

			// A changed path is held to the rules, even when the other
			// two repeat their stored values.
			for _, body := range []string{
				dialogBody("db.example.com", "x", "/etc/other.crt", legacyKey, legacyRoot),
				dialogBody("db.example.com", "x", legacyCert, "relative.key", legacyRoot),
				dialogBody("db.example.com", "x", legacyCert, legacyKey, "/etc/passwd"),
			} {
				rec := env.serve(env.request(http.MethodPut, connPath(id, ""), body, true))
				expectStatus(t, rec, http.StatusBadRequest)
			}
			if got := descriptionOf(t, env, id); got != "edited" {
				t.Errorf("description = %q after refused updates, want edited", got)
			}

			// Clearing a legacy path needs no check.
			rec = env.serve(env.request(http.MethodPut, connPath(id, ""),
				dialogBody("db.example.com", "cleared", legacyCert, legacyKey, ""), true))
			expectStatus(t, rec, http.StatusOK)
			if got := sslRootCertOf(t, env, id); got != "" {
				t.Errorf("sslrootcert = %q, want it cleared", got)
			}
		})
	}
}

// TestConnectionHandler_NewSSLPathOnUnsetField checks that a path
// supplied for a field with no stored value is checked, so the
// comparison with the stored value never lets a new path through.
func TestConnectionHandler_NewSSLPathOnUnsetField(t *testing.T) {
	env := newTargetCRUDEnv(t)
	id := env.seed(t, env.user, "db.example.com", 5432, "app", "alice", nil)

	rec := env.serve(env.request(http.MethodPut, connPath(id, ""),
		`{"ssl_root_cert_path":"/etc/passwd"}`, true))
	expectStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "connection_security.ssl_cert_dir") {
		t.Errorf("body %q does not name the option", rec.Body.String())
	}
}

// TestConnectionHandler_UpdateBracketedIPv6Host checks that a connection
// whose IPv6 host is stored in brackets can be saved unchanged, and that
// a bracketed host is still refused when the bare address would be.
func TestConnectionHandler_UpdateBracketedIPv6Host(t *testing.T) {
	env := newTargetCRUDEnv(t)
	const host = "[2001:db8::10]"
	id := env.seed(t, env.user, host, 5432, "app", "alice", nil)

	rec := env.serve(env.request(http.MethodPut, connPath(id, ""),
		dialogBody(host, "edited", "", "", ""), true))
	expectStatus(t, rec, http.StatusOK)
	if got := descriptionOf(t, env, id); got != "edited" {
		t.Errorf("description = %q, want edited", got)
	}

	for _, bad := range []string{"[::1]", "[[2001:db8::10]]", "[db.example.com]"} {
		rec := env.serve(env.request(http.MethodPut, connPath(id, ""),
			dialogBody(bad, "x", "", "", ""), true))
		expectStatus(t, rec, http.StatusBadRequest)
		if !strings.Contains(rec.Body.String(), "Invalid host") {
			t.Errorf("host %q: body %q, want Invalid host", bad, rec.Body.String())
		}
	}
}

// TestConnectionHandler_UpdateKeepsStoredTarget checks that an update
// repeating a stored host and hostaddr that the save-time rules would
// now refuse (here an always-blocked address, with
// allow_internal_networks on) succeeds, as the server dialog resends the
// host on every save, whilst a changed host or hostaddr is still held to
// the rules. The connect-time checks still refuse the stored target.
func TestConnectionHandler_UpdateKeepsStoredTarget(t *testing.T) {
	const (
		storedHost     = "169.254.10.10"
		storedHostAddr = "169.254.10.20"
	)
	env := newConnCRUDEnv(t)
	env.handler = NewConnectionHandlerWithSecurity(env.ds, env.store,
		auth.NewRBACChecker(env.store), true, nil, nil)
	id := env.seed(t, env.user, storedHost, 5432, "app", "alice", nil)
	if _, err := env.pool.Exec(context.Background(),
		"UPDATE connections SET hostaddr = $1 WHERE id = $2", storedHostAddr, id); err != nil {
		t.Fatalf("set hostaddr: %v", err)
	}
	withHostAddr := func(body, hostaddr string) string {
		return strings.TrimSuffix(body, "}") + fmt.Sprintf(`,"hostaddr":%q}`, hostaddr)
	}

	rec := env.serve(env.request(http.MethodPut, connPath(id, ""),
		withHostAddr(dialogBody(storedHost, "edited", "", "", ""), storedHostAddr), true))
	expectStatus(t, rec, http.StatusOK)
	if got := descriptionOf(t, env, id); got != "edited" {
		t.Errorf("description = %q, want edited", got)
	}

	for _, tc := range []struct {
		name, body, want string
	}{
		{"changed host",
			dialogBody("169.254.10.11", "x", "", "", ""),
			"Invalid host: " + ErrAlwaysBlockedHost.Error()},
		{"changed hostaddr",
			withHostAddr(dialogBody(storedHost, "x", "", "", ""), "169.254.10.21"),
			"Invalid hostaddr: " + ErrAlwaysBlockedHost.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.serve(env.request(http.MethodPut, connPath(id, ""), tc.body, true))
			expectStatus(t, rec, http.StatusBadRequest)
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("body %q, want %q", rec.Body.String(), tc.want)
			}
		})
	}
	if got := descriptionOf(t, env, id); got != "edited" {
		t.Errorf("description = %q after refused updates, want edited", got)
	}
}
