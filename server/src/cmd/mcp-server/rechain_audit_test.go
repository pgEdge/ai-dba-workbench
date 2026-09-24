/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// unkeyedAuditStore builds a data directory whose audit log holds rows
// hashed under the unkeyed version 1 rendering, which is what a
// database written by a release predating the keyed chain contains.
// Nothing in this build produces such a row, so they are written
// straight into the file.
//
// A non-zero breakAt deletes that row once the rows are written, which
// leaves a log that no longer links to itself.
//
// It returns the directory. The store is closed before returning, since
// every caller reopens it through a command.
func unkeyedAuditStore(t *testing.T, rows int, breakAt int64) string {
	t.Helper()

	dir := t.TempDir()
	store, err := auth.NewAuthStore(dir, 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("Failed to create the auth store: %v", err)
	}
	if err := auth.SeedUnkeyedAuditLogForTesting(store, rows,
		time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC), breakAt); err != nil {
		t.Fatalf("Failed to write the unkeyed rows: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Failed to close the store: %v", err)
	}

	return dir
}

// TestRechainAuditLogCommandRewritesTheLog runs the command as an
// operator would with the non-interactive confirmation, and checks the
// log it leaves behind verifies.
func TestRechainAuditLogCommandRewritesTheLog(t *testing.T) {
	dir := unkeyedAuditStore(t, 3, 0)

	var out bytes.Buffer
	if err := rechainAuditLogCommand(dir, true, strings.NewReader(""),
		&out); err != nil {
		t.Fatalf("Failed to re-chain the log: %v", err)
	}

	for _, want := range []string{"Events:", "Unkeyed (version 1): 3",
		"recomputes cleanly", "-confirm-rechain was given",
		"3 event(s) re-hashed", "-verify-audit-log"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("Expected the output to mention %q, got:\n%s", want,
				out.String())
		}
	}

	if err := verifyAuditLogCommand(dir); err != nil {
		t.Fatalf("Expected the re-chained log to verify: %v", err)
	}
}

// TestRechainAuditLogCommandPrintsBeforeItWrites checks the figures
// reach the operator ahead of the prompt, so that the decision is taken
// on what the log holds rather than on the command's name.
func TestRechainAuditLogCommandPrintsBeforeItWrites(t *testing.T) {
	dir := unkeyedAuditStore(t, 2, 0)

	var out bytes.Buffer
	if err := rechainAuditLogCommand(dir, false,
		strings.NewReader(auditRechainConfirmWord+"\n"), &out); err != nil {
		t.Fatalf("Failed to re-chain the log: %v", err)
	}

	printed := out.String()
	prompt := strings.Index(printed, "to proceed")
	if prompt < 0 {
		t.Fatalf("Expected a confirmation prompt, got:\n%s", printed)
	}
	for _, want := range []string{"Events:", "Oldest event:",
		"Newest event:", "attests the log exactly as it now stands"} {
		at := strings.Index(printed, want)
		if at < 0 {
			t.Errorf("Expected the output to mention %q, got:\n%s", want,
				printed)
			continue
		}
		if at > prompt {
			t.Errorf("Expected %q to be printed before the prompt", want)
		}
	}
}

// TestRechainAuditLogCommandAbortsWithoutConfirmation checks that an
// answer other than the confirmation word leaves the log alone, and
// that the log is still the unkeyed one afterwards.
func TestRechainAuditLogCommandAbortsWithoutConfirmation(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"a bare newline", "\n"},
		{"yes", "yes\n"},
		{"end of input", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := unkeyedAuditStore(t, 2, 0)

			var out bytes.Buffer
			if err := rechainAuditLogCommand(dir, false,
				strings.NewReader(tt.input), &out); err != nil {
				t.Fatalf("Aborting is not an error: %v", err)
			}
			if !strings.Contains(out.String(), "Nothing has been changed") {
				t.Errorf("Expected the abort to be reported, got:\n%s",
					out.String())
			}

			// The store still refuses to open, which it would not do
			// had anything been re-chained.
			store, err := auth.NewAuthStore(dir, 0, 0,
				auth.AuditKeyForTesting())
			if err == nil {
				store.Close()
				t.Fatal("Expected the unkeyed log to be left in place")
			}
			if !errors.Is(err, auth.ErrAuditUnkeyedRow) {
				t.Errorf("Expected an unkeyed-row refusal, got %v", err)
			}
		})
	}
}

// TestRechainAuditLogCommandReportsABrokenLegacyChain checks the
// operator is warned, before deciding, that the log does not agree with
// its own hashes.
func TestRechainAuditLogCommandReportsABrokenLegacyChain(t *testing.T) {
	// Row 2 is deleted, so row 3 no longer links to the row before it.
	dir := unkeyedAuditStore(t, 4, 2)

	var out bytes.Buffer
	if err := rechainAuditLogCommand(dir, false, strings.NewReader("\n"),
		&out); err != nil {
		t.Fatalf("Aborting is not an error: %v", err)
	}

	for _, want := range []string{"DOES NOT recompute (first bad row 3)",
		"Restore auth.db from a known-good copy"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("Expected the output to mention %q, got:\n%s", want,
				out.String())
		}
	}
}

// TestRechainAuditLogCommandRefusesABrokenChainUnattended checks that
// -confirm-rechain will not sign a log the command has just reported
// does not recompute. The warning is addressed to a person, and an
// unattended run has nobody to read it, so the answer it stands in for
// cannot be yes; a human who has read it may still proceed
// interactively.
func TestRechainAuditLogCommandRefusesABrokenChainUnattended(t *testing.T) {
	dir := unkeyedAuditStore(t, 4, 2)

	var out bytes.Buffer
	err := rechainAuditLogCommand(dir, true, strings.NewReader(""), &out)
	if err == nil {
		t.Fatal("Expected the unattended re-chain to refuse a log that " +
			"does not recompute")
	}
	for _, want := range []string{
		"does not verify under its own unkeyed rules",
		"re-chain interactively if you judge it sound"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Expected the refusal to mention %q, got %v", want, err)
		}
	}
	if strings.Contains(out.String(), "-confirm-rechain was given") {
		t.Errorf("Expected no claim to be proceeding, got:\n%s", out.String())
	}

	// Nothing was signed: the log is still the unkeyed one, so an
	// ordinary open still refuses it.
	store, openErr := auth.NewAuthStore(dir, 0, 0, auth.AuditKeyForTesting())
	if openErr == nil {
		store.Close()
		t.Fatal("Expected the unkeyed log to be left in place")
	}
	if !errors.Is(openErr, auth.ErrAuditUnkeyedRow) {
		t.Errorf("Expected an unkeyed-row refusal, got %v", openErr)
	}

	// The interactive path is unchanged: the same log re-chains when an
	// operator types the confirmation word.
	out.Reset()
	if err := rechainAuditLogCommand(dir, false,
		strings.NewReader(auditRechainConfirmWord+"\n"), &out); err != nil {
		t.Fatalf("Expected the interactive re-chain to proceed: %v", err)
	}
	if !strings.Contains(out.String(), "re-hashed") {
		t.Errorf("Expected the interactive run to rewrite the log, got:\n%s",
			out.String())
	}
}

// TestRechainAuditLogCommandReportsAFailure checks a re-chain that
// cannot run is reported rather than swallowed.
func TestRechainAuditLogCommandReportsAFailure(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		t.Fatalf("Failed to write the blocking file: %v", err)
	}

	var out bytes.Buffer
	err := rechainAuditLogCommand(filepath.Join(blocker, "data"), true,
		strings.NewReader(""), &out)
	if err == nil {
		t.Fatal("Expected an unusable data directory to be reported")
	}
	if !strings.Contains(err.Error(), "failed to re-chain the audit log") {
		t.Errorf("Expected the failure to name the command, got %v", err)
	}
}

// TestRechainAuditLogCommandNeedsAKey checks the command will not run
// without the server secret, since the whole point of it is to hash the
// log under that secret.
func TestRechainAuditLogCommandNeedsAKey(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.secret")
	useRealCLIAuditKey(t, missing)

	var out bytes.Buffer
	err := rechainAuditLogCommand(t.TempDir(), true, strings.NewReader(""),
		&out)
	if err == nil {
		t.Fatal("Expected the command to refuse to run without a secret")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("Expected the error to name the secret file, got %v", err)
	}
}

// TestAuditPlanTime checks the placeholder an empty log produces, which
// is otherwise printed as a zero timestamp that reads like a real date
// in the year one.
func TestAuditPlanTime(t *testing.T) {
	if got := auditPlanTime(time.Time{}); got != "(none)" {
		t.Errorf("Expected (none) for the zero time, got %q", got)
	}

	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	if got := auditPlanTime(when); got != "2026-03-04T05:06:07Z" {
		t.Errorf("Expected an RFC 3339 timestamp, got %q", got)
	}
}

// TestConfirmAuditRechainReadsTheAnswer covers the prompt on its own,
// including the case where the reader fails outright rather than
// declining.
func TestConfirmAuditRechainReadsTheAnswer(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"the confirmation word", auditRechainConfirmWord + "\n", true},
		{"the word with surrounding space", "  rechain  \n", true},
		{"no trailing newline", auditRechainConfirmWord, true},
		{"anything else", "no\n", false},
		{"empty input", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			got, err := confirmAuditRechain(strings.NewReader(tt.input), &out)
			if err != nil {
				t.Fatalf("Failed to read the confirmation: %v", err)
			}
			if got != tt.want {
				t.Errorf("Expected %v for %q, got %v", tt.want, tt.input, got)
			}
			if !strings.Contains(out.String(), auditRechainConfirmWord) {
				t.Errorf("Expected the prompt to name the confirmation "+
					"word, got %q", out.String())
			}
		})
	}

	t.Run("a reader that fails", func(t *testing.T) {
		var out bytes.Buffer
		if _, err := confirmAuditRechain(failingReader{}, &out); err == nil {
			t.Fatal("Expected a read failure to be reported")
		}
	})
}

// failingReader fails every read, standing in for a terminal that has
// gone away mid-prompt.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("the terminal went away")
}

// TestLoadCLISecretFile checks the command line's early read of
// secret_file: absent configuration is not an error, because the
// default search order then applies, but a configuration file that
// cannot be parsed is, because guessing at the secret would have the
// command write audit rows the server cannot verify.
func TestLoadCLISecretFile(t *testing.T) {
	dir := t.TempDir()

	t.Run("no configuration file", func(t *testing.T) {
		got, err := loadCLISecretFile(filepath.Join(dir, "absent.yaml"))
		if err != nil {
			t.Fatalf("An absent configuration file is not an error: %v", err)
		}
		if got != "" {
			t.Errorf("Expected no secret file, got %q", got)
		}
	})

	t.Run("a configured secret file", func(t *testing.T) {
		path := filepath.Join(dir, "good.yaml")
		if err := os.WriteFile(path,
			[]byte("secret_file: /etc/pgedge/server.secret\n"), 0600); err != nil {
			t.Fatalf("Failed to write the configuration file: %v", err)
		}

		got, err := loadCLISecretFile(path)
		if err != nil {
			t.Fatalf("Failed to read secret_file: %v", err)
		}
		if got != "/etc/pgedge/server.secret" {
			t.Errorf("Expected the configured path, got %q", got)
		}
	})

	t.Run("a malformed configuration file", func(t *testing.T) {
		path := filepath.Join(dir, "broken.yaml")
		if err := os.WriteFile(path, []byte("secret_file: [\n"),
			0600); err != nil {
			t.Fatalf("Failed to write the configuration file: %v", err)
		}

		_, err := loadCLISecretFile(path)
		if err == nil {
			t.Fatal("Expected a malformed configuration file to be fatal")
		}
		if !strings.Contains(err.Error(), path) {
			t.Errorf("Expected the error to name the file, got %v", err)
		}
	})
}
