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
	"net/http"
	"strings"
	"testing"
)

// TestQueryEndpoints_RefuseUnsafeStoredTarget checks that a stored row
// whose host or hostaddr was written before the API validated them is
// refused at connect time by both the execute and validate endpoints,
// with a generic message that does not echo the stored value.
func TestQueryEndpoints_RefuseUnsafeStoredTarget(t *testing.T) {
	h, pool, target, cleanup := newQueryExecTestHandler(t)
	defer cleanup()

	rows := []struct {
		name     string
		host     string
		hostaddr string
	}{
		{"comma host list", "db.example.com,169.254.169.254", ""},
		{"bracket hostaddr list", target.host, "127.0.0.1]:6379,[127.0.0.1"},
		{"host name as hostaddr", target.host, "evil.example.com"},
	}
	for _, row := range rows {
		connID := seedQueryExecConnection(t, pool, target, row.host, target.port)
		if row.hostaddr != "" {
			if _, err := pool.Exec(context.Background(),
				"UPDATE connections SET hostaddr = $1 WHERE id = $2",
				row.hostaddr, connID); err != nil {
				t.Fatalf("set hostaddr: %v", err)
			}
		}

		endpoints := map[string]func() int{
			"execute": func() int {
				rec := postQuery(t, h, connID, `{"query":"SELECT 1"}`)
				assertRefusedBody(t, rec.Body.String(), row.host, row.hostaddr)
				return rec.Code
			},
			"validate": func() int {
				rec := postValidate(t, h, connID, `{"query":"SELECT 1"}`)
				assertRefusedBody(t, rec.Body.String(), row.host, row.hostaddr)
				return rec.Code
			},
		}
		for name, call := range endpoints {
			t.Run(row.name+"/"+name, func(t *testing.T) {
				if code := call(); code != http.StatusInternalServerError {
					t.Errorf("status = %d, want %d", code,
						http.StatusInternalServerError)
				}
			})
		}
	}
}

func assertRefusedBody(t *testing.T, body, host, hostaddr string) {
	t.Helper()
	if !strings.Contains(body, "Failed to connect to database") {
		t.Errorf("body %q lacks the generic message", body)
	}
	for _, leaked := range []string{host, hostaddr} {
		if leaked != "" && strings.Contains(body, leaked) {
			t.Errorf("body %q echoes the stored value %q", body, leaked)
		}
	}
}
