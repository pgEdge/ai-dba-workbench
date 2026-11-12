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
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/pkg/hostvalidation"
	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// TestHostValidator_ValidateHost_Syntax checks that a host carrying
// connection-string structure is refused before any allowlist or DNS
// check, even when the allowlist would otherwise admit it. Every hostile
// host is itself on the allowlist, so only the syntax check can refuse
// it, and the error must be ErrHostSyntax, so that a refusal for some
// other reason, such as a failed DNS lookup, does not count.
func TestHostValidator_ValidateHost_Syntax(t *testing.T) {
	hostile := []string{
		"db.example.com,169.254.169.254",
		"[[::1]]",
		"[db.example.com]",
		"[127.0.0.1]",
		"db.example.com/x",
		"db.example.com?sslmode=disable",
		"user@db.example.com",
		"db.example.com#frag",
		"db.example.com%2C127.0.0.1",
		"db.example.com 127.0.0.1",
		"db.example.com\t",
		"db.example.com\n",
	}
	v := NewHostValidator(true, append([]string{"db.example.com"}, hostile...), nil)
	for _, host := range hostile {
		if err := v.ValidateHost(host); !errors.Is(err, hostvalidation.ErrHostSyntax) {
			t.Errorf("ValidateHost(%q) = %v, want ErrHostSyntax", host, err)
		}
	}
	if err := v.ValidateHost("db.example.com"); err != nil {
		t.Errorf("ValidateHost(allowlisted) = %v, want nil", err)
	}
}

// TestHostValidator_ValidateHostAddr covers the literal-IP requirement
// and the reuse of the host checks for the parsed address.
func TestHostValidator_ValidateHostAddr(t *testing.T) {
	strict := NewHostValidator(false, []string{"192.0.2.10"},
		[]string{"198.51.100.0/24"})
	permissive := NewHostValidator(true, nil, []string{"198.51.100.0/24"})

	cases := []struct {
		name     string
		v        *HostValidator
		hostaddr string
		wantErr  bool
	}{
		{"metadata address", strict, "169.254.169.254", true},
		{"loopback", strict, "127.0.0.1", true},
		{"private range", strict, "10.0.0.5", true},
		{"allowlisted", strict, "192.0.2.10", false},
		{"public address", strict, "8.8.8.8", false},
		{"blocked range", permissive, "198.51.100.7", true},
		{"internal allowed", permissive, "127.0.0.1", false},
		{"bracketed IPv6", permissive, "[::1]", false},
		{"bracketed IPv6 internal refused", strict, "[::1]", true},
		{"host name", permissive, "db.example.com", true},
		{"list injection", permissive, "127.0.0.1]:6379,[127.0.0.1", true},
		{"comma list", permissive, "127.0.0.1,127.0.0.2", true},
		{"with port", permissive, "127.0.0.1:6379", true},
		{"empty", permissive, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.v.ValidateHostAddr(tc.hostaddr)
			if (err != nil) != tc.wantErr {
				t.Errorf("ValidateHostAddr(%q) error = %v, wantErr %v",
					tc.hostaddr, err, tc.wantErr)
			}
		})
	}
}

// newTargetCRUDEnv is a connCRUDEnv whose handler also allows the
// hostname db.example.com by name, so that the tests below never depend
// on DNS resolution.
func newTargetCRUDEnv(t *testing.T) *connCRUDEnv {
	t.Helper()
	env := newConnCRUDEnv(t)
	env.handler = NewConnectionHandlerWithSecurity(env.ds, env.store,
		auth.NewRBACChecker(env.store), false,
		[]string{"192.0.2.10", "db.example.com"},
		[]string{"blocked.example.com"})
	return env
}

// createBody is a valid create request for db.example.com with extra
// raw JSON fields spliced in, so the hostile payloads are sent exactly as
// an attacker would write them.
func createBody(extra string) string {
	body := `{"name":"target-test","host":"db.example.com","port":5432,` +
		`"database_name":"postgres","username":"alice","password":"secret"`
	if extra != "" {
		body += "," + extra
	}
	return body + "}"
}

// hostaddrOf returns the stored hostaddr of a connection, or "" when it
// is NULL.
func hostaddrOf(t *testing.T, env *connCRUDEnv, id int) string {
	t.Helper()
	var hostaddr sql.NullString
	if err := env.pool.QueryRow(context.Background(),
		"SELECT hostaddr FROM connections WHERE id = $1", id).
		Scan(&hostaddr); err != nil {
		t.Fatalf("read hostaddr: %v", err)
	}
	return hostaddr.String
}

// TestConnectionHandler_CreateConnection_HostAddrSSRF is the regression
// test for the hostaddr server-side request forgery: a hostaddr naming
// an internal address, or smuggling a second target into the URL, must
// be refused even though the host itself is allowed.
func TestConnectionHandler_CreateConnection_HostAddrSSRF(t *testing.T) {
	env := newTargetCRUDEnv(t)

	refused := []struct {
		name  string
		extra string
		want  string
	}{
		{"metadata hostaddr behind an allowed host",
			`"hostaddr":"169.254.169.254"`, "Invalid hostaddr"},
		{"bracket list injection",
			`"hostaddr":"127.0.0.1]:6379,[127.0.0.1"`, "Invalid hostaddr"},
		{"host name as hostaddr",
			`"hostaddr":"db.example.com"`, "Invalid hostaddr"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.serve(env.request(http.MethodPost,
				"/api/v1/connections", createBody(tc.extra), true))
			expectStatus(t, rec, http.StatusBadRequest)
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("body %q does not mention %q",
					rec.Body.String(), tc.want)
			}
		})
	}

	t.Run("host list injection", func(t *testing.T) {
		body := strings.Replace(createBody(""), `"db.example.com"`,
			`"db.example.com,169.254.169.254"`, 1)
		rec := env.serve(env.request(http.MethodPost,
			"/api/v1/connections", body, true))
		expectStatus(t, rec, http.StatusBadRequest)
		if !strings.Contains(rec.Body.String(), "Invalid host") {
			t.Errorf("body %q does not mention Invalid host",
				rec.Body.String())
		}
	})

	var count int
	if err := env.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM connections").Scan(&count); err != nil {
		t.Fatalf("count connections: %v", err)
	}
	if count != 0 {
		t.Fatalf("%d connections stored by refused requests", count)
	}

	t.Run("allowed hostaddr", func(t *testing.T) {
		rec := env.serve(env.request(http.MethodPost, "/api/v1/connections",
			createBody(`"hostaddr":"192.0.2.10"`), true))
		expectStatus(t, rec, http.StatusCreated)
	})
}

// TestConnectionHandler_UpdateConnection_HostAddrSSRF covers the same
// payloads on update, where the stored host is already allowed.
func TestConnectionHandler_UpdateConnection_HostAddrSSRF(t *testing.T) {
	env := newTargetCRUDEnv(t)
	id := env.seed(t, env.user, "db.example.com", 5432, "app", "alice", nil)

	refused := []struct {
		name string
		body string
		want string
	}{
		{"metadata hostaddr behind an allowed host",
			`{"host":"db.example.com","hostaddr":"169.254.169.254"}`,
			"Invalid hostaddr"},
		{"metadata hostaddr alone",
			`{"hostaddr":"169.254.169.254"}`, "Invalid hostaddr"},
		{"bracket list injection",
			`{"hostaddr":"127.0.0.1]:6379,[127.0.0.1"}`, "Invalid hostaddr"},
		{"host list injection",
			`{"host":"db.example.com,169.254.169.254"}`, "Invalid host"},
		{"bracketed host",
			`{"host":"[127.0.0.1]:6379,[127.0.0.1]"}`, "Invalid host"},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.serve(env.request(http.MethodPut, connPath(id, ""),
				tc.body, true))
			expectStatus(t, rec, http.StatusBadRequest)
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("body %q does not mention %q",
					rec.Body.String(), tc.want)
			}
			if got := hostaddrOf(t, env, id); got != "" {
				t.Errorf("hostaddr stored as %q by a refused update", got)
			}
		})
	}

	t.Run("allowed hostaddr", func(t *testing.T) {
		rec := env.serve(env.request(http.MethodPut, connPath(id, ""),
			`{"hostaddr":"192.0.2.10"}`, true))
		expectStatus(t, rec, http.StatusOK)
		if got := hostaddrOf(t, env, id); got != "192.0.2.10" {
			t.Errorf("hostaddr = %q, want 192.0.2.10", got)
		}
	})

	t.Run("empty hostaddr clears it", func(t *testing.T) {
		rec := env.serve(env.request(http.MethodPut, connPath(id, ""),
			`{"hostaddr":""}`, true))
		expectStatus(t, rec, http.StatusOK)
		if got := hostaddrOf(t, env, id); got != "" {
			t.Errorf("hostaddr = %q, want it cleared", got)
		}
	})
}
