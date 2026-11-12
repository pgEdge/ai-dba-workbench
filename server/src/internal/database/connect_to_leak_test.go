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
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// captureStdLog redirects the standard logger for the rest of the test.
func captureStdLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

// TestConnectToErrorsNameNoSecrets checks that a connection string pgx
// cannot parse comes back from ConnectTo as ErrInvalidConnectionString,
// with neither the password nor an SSL file path in the error, because
// the MCP providers return that error's text to the caller. The log
// keeps the reason and the path, but not the password.
func TestConnectToErrorsNameNoSecrets(t *testing.T) {
	const password = "Sup3rSecretPw"
	missing := filepath.Join(t.TempDir(), "missing-ca.pem")

	cases := []struct {
		name      string
		connStr   string
		logWants  string
		logBanned []string
	}{
		{"missing CA file",
			"postgres://user:" + password + "@db.example.com:5432/mydb?sslmode=disable&sslrootcert=" + missing,
			missing, []string{password}},
		{"bad sslmode",
			"postgres://user:" + password + "@db.example.com:5432/mydb?sslmode=bogus",
			"sslmode", []string{password}},
		{"bad URL",
			"postgres://user:" + password + "@db.example.com:5432/%zz",
			"not a valid URL", []string{password, "%zz"}},
		// The decoded sslrootcert path reaches pgx's reason, so the log
		// line must quote it rather than let the newline forge a second line.
		{"newline in path",
			"postgres://user:" + password + "@db.example.com:5432/mydb?sslmode=disable&sslrootcert=" + missing + "%0A%5BINFO%5D%20forged",
			"forged", []string{password, "\n[INFO]"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureStdLog(t)
			err := NewClient(nil).ConnectTo(tc.connStr)
			if !errors.Is(err, ErrInvalidConnectionString) {
				t.Fatalf("err = %v, want ErrInvalidConnectionString", err)
			}
			for _, secret := range []string{password, missing, "db.example.com"} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("error %q contains %q", err, secret)
				}
			}
			if !strings.Contains(logs.String(), tc.logWants) {
				t.Errorf("log %q does not contain %q", logs.String(), tc.logWants)
			}
			for _, banned := range tc.logBanned {
				if strings.Contains(logs.String(), banned) {
					t.Errorf("log %q contains %q", logs.String(), banned)
				}
			}
		})
	}
}

func TestDescribeParseConfigError(t *testing.T) {
	_, err := pgconn.ParseConfig("postgres://user:pw-not-logged@db.example.com/x?sslmode=bogus")
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
