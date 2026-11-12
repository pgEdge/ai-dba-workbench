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
)

// sslRootCertOf returns the stored sslrootcert of a connection, or ""
// when it is NULL.
func sslRootCertOf(t *testing.T, env *connCRUDEnv, id int) string {
	t.Helper()
	var v sql.NullString
	if err := env.pool.QueryRow(context.Background(),
		"SELECT sslrootcert FROM connections WHERE id = $1", id).
		Scan(&v); err != nil {
		t.Fatalf("read sslrootcert: %v", err)
	}
	return v.String
}

// TestConnectionHandler_SSLPathsWithoutDirectory checks that, with no
// connection_security.ssl_cert_dir configured, create and update refuse
// any SSL file path with a message telling the operator what to set, and
// still accept the platform trust store and cleared fields.
func TestConnectionHandler_SSLPathsWithoutDirectory(t *testing.T) {
	env := newTargetCRUDEnv(t)
	id := env.seed(t, env.user, "db.example.com", 5432, "app", "alice", nil)

	for _, field := range []string{"ssl_cert_path", "ssl_key_path", "ssl_root_cert_path"} {
		extra := fmt.Sprintf(`%q:"/etc/ssl/certs/ca.pem"`, field)
		for _, req := range []*http.Request{
			env.request(http.MethodPost, "/api/v1/connections", createBody(extra), true),
			env.request(http.MethodPut, connPath(id, ""), "{"+extra+"}", true),
		} {
			rec := env.serve(req)
			expectStatus(t, rec, http.StatusBadRequest)
			if !strings.Contains(rec.Body.String(), "connection_security.ssl_cert_dir") {
				t.Errorf("%s %s: body %q does not name the option",
					req.Method, field, rec.Body.String())
			}
		}
	}

	rec := env.serve(env.request(http.MethodPost, "/api/v1/connections",
		createBody(`"ssl_root_cert_path":"system","ssl_cert_path":""`), true))
	expectStatus(t, rec, http.StatusCreated)

	rec = env.serve(env.request(http.MethodPut, connPath(id, ""),
		`{"ssl_root_cert_path":"system"}`, true))
	expectStatus(t, rec, http.StatusOK)
	if got := sslRootCertOf(t, env, id); got != "system" {
		t.Errorf("sslrootcert = %q, want system", got)
	}
}

// TestConnectionHandler_SSLPathsWithDirectory checks create and update
// against a configured directory: paths inside are stored, and anything
// relative, escaping with "..", or outside is refused without the path
// being echoed back.
func TestConnectionHandler_SSLPathsWithDirectory(t *testing.T) {
	env := newTargetCRUDEnv(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env.ds.SetSSLCertDir(dir)
	id := env.seed(t, env.user, "db.example.com", 5432, "app", "alice", nil)

	refused := []struct {
		name string
		path string
		want string
	}{
		{"relative", "certs/ca.pem", "ssl_root_cert_path must be an absolute path"},
		{"dot-dot", dir + "/../ca.pem", "ssl_root_cert_path must not contain '..'"},
		{"outside", "/etc/passwd", "ssl_root_cert_path must be inside"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			extra := fmt.Sprintf(`"ssl_root_cert_path":%q`, tc.path)
			for _, req := range []*http.Request{
				env.request(http.MethodPost, "/api/v1/connections", createBody(extra), true),
				env.request(http.MethodPut, connPath(id, ""), "{"+extra+"}", true),
			} {
				rec := env.serve(req)
				expectStatus(t, rec, http.StatusBadRequest)
				body := rec.Body.String()
				if !strings.Contains(body, tc.want) {
					t.Errorf("%s: body %q does not contain %q", req.Method, body, tc.want)
				}
				if strings.Contains(body, tc.path) {
					t.Errorf("%s: body %q echoes the path", req.Method, body)
				}
			}
			if got := sslRootCertOf(t, env, id); got != "" {
				t.Errorf("sslrootcert stored as %q by a refused update", got)
			}
		})
	}

	inside := filepath.Join(dir, "ca.pem")
	rec := env.serve(env.request(http.MethodPost, "/api/v1/connections",
		createBody(fmt.Sprintf(`"ssl_root_cert_path":%q`, inside)), true))
	expectStatus(t, rec, http.StatusCreated)

	rec = env.serve(env.request(http.MethodPut, connPath(id, ""),
		fmt.Sprintf(`{"ssl_root_cert_path":%q}`, inside), true))
	expectStatus(t, rec, http.StatusOK)
	if got := sslRootCertOf(t, env, id); got != inside {
		t.Errorf("sslrootcert = %q, want %q", got, inside)
	}

	rec = env.serve(env.request(http.MethodPut, connPath(id, ""),
		`{"ssl_root_cert_path":""}`, true))
	expectStatus(t, rec, http.StatusOK)
	if got := sslRootCertOf(t, env, id); got != "" {
		t.Errorf("sslrootcert = %q, want it cleared", got)
	}
}
