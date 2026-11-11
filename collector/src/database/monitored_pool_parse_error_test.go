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
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestCreateMonitoredPoolParseErrorIsGeneric checks that a connection
// string pgx refuses comes back as ErrInvalidMonitoredConnString, whose
// text names neither the password nor an SSL file path, because the
// scheduler stores it in connection_error for the API to show.
func TestCreateMonitoredPoolParseErrorIsGeneric(t *testing.T) {
	const password = "Sup3rSecretPw"
	missing := filepath.Join(t.TempDir(), "missing-ca.pem")
	for _, connStr := range []string{
		"host=db.example.com user=u password=" + password + " sslmode=disable sslrootcert=" + missing,
		"host=db.example.com user=u password=" + password + " sslmode=bogus",
		"::not::a::valid::connstr",
	} {
		_, err := createMonitoredPool(context.Background(), connStr, 1, 0)
		if !errors.Is(err, ErrInvalidMonitoredConnString) {
			t.Fatalf("err = %v, want ErrInvalidMonitoredConnString", err)
		}
		for _, secret := range []string{password, missing, "db.example.com"} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error %q contains %q", err, secret)
			}
		}
	}
}

func TestDescribeParseConfigError(t *testing.T) {
	_, err := pgconn.ParseConfig("host=db.example.com password=pw-not-logged sslmode=bogus")
	if err == nil {
		t.Fatal("ParseConfig accepted sslmode=bogus")
	}
	got := describeParseConfigError(err)
	if strings.Contains(got, "pw-not-logged") || strings.Contains(got, "db.example.com") {
		t.Errorf("description %q repeats the connection string", got)
	}
	if !strings.Contains(got, "<connection string>") || !strings.Contains(got, "sslmode") {
		t.Errorf("description %q lost the reason", got)
	}
	if got := describeParseConfigError(errors.New("plain")); got != "plain" {
		t.Errorf("plain error described as %q", got)
	}
}
