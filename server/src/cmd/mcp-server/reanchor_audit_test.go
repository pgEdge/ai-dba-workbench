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
	"fmt"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// rotatedSecretAuditStore builds a data directory whose audit log was
// written under a server secret other than the one the commands use,
// which is what an operator has after rotating or losing the secret.
func rotatedSecretAuditStore(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	store, err := auth.NewAuthStore(dir, 0, 0,
		auth.DeriveAuditKey("an older server secret"))
	if err != nil {
		t.Fatalf("failed to create the auth store: %v", err)
	}
	if err := store.CreateUser("alice", "correct horse battery staple",
		"", "Alice", "alice@example.com"); err != nil {
		t.Fatalf("failed to create a user: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("failed to close the store: %v", err)
	}

	return dir
}

// tamperedAuditStore builds a data directory, written under the key the
// commands use, whose newest audit row has been edited in place.
func tamperedAuditStore(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	store, err := auth.NewAuthStore(dir, 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("failed to create the auth store: %v", err)
	}
	for _, name := range []string{"alice", "bob"} {
		if err := store.CreateUser(name, "correct horse battery staple",
			"", "Test User", name+"@example.com"); err != nil {
			t.Fatalf("failed to create a user: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("failed to close the store: %v", err)
	}
	tamperAuditRow(t, dir)

	return dir
}

func assertOutputContains(t *testing.T, out string, wants ...string) {
	t.Helper()

	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("expected the output to mention %q, got:\n%s", want, out)
		}
	}
}

// TestReanchorCommandDeclinedChangesNothing checks that an answer other
// than the confirmation word leaves a rotated-secret log failing exactly
// as before, having shown the operator what it found.
func TestReanchorCommandDeclinedChangesNothing(t *testing.T) {
	dir := rotatedSecretAuditStore(t)

	var out bytes.Buffer
	if err := rechainAuditLogCommand(dir, false, strings.NewReader("yes\n"),
		&out); err != nil {
		t.Fatalf("declining is not an error: %v", err)
	}
	assertOutputContains(t, out.String(), "Verification fails:",
		"do not proceed", "Oldest event accepted:", "Its hash:",
		"Previously recorded:   (none)", "Accepted as history:",
		"no longer shown to have been written", "to proceed",
		"Aborted. Nothing has been changed.")

	err := verifyAuditLogCommand(dir)
	if got := auditVerifyExitCode(err); got != auditExitKeyMismatch {
		t.Errorf("expected the log still to report a key mismatch, got "+
			"exit status %d: %v", got, err)
	}
}

// TestReanchorCommandAcceptsARotatedSecret runs the unattended
// re-anchor on a rotated-secret log, then checks the log verifies, says
// how much of it is history, and has nothing left to re-chain.
func TestReanchorCommandAcceptsARotatedSecret(t *testing.T) {
	dir := rotatedSecretAuditStore(t)

	var out bytes.Buffer
	if err := rechainAuditLogCommand(dir, true, strings.NewReader(""),
		&out); err != nil {
		t.Fatalf("failed to re-anchor the log: %v", err)
	}
	assertOutputContains(t, out.String(), "-confirm-rechain was given",
		"Audit log re-anchored", "-verify-audit-log")

	var verifyErr error
	printed := captureStdout(t, func() {
		verifyErr = verifyAuditLogCommand(dir)
	})
	if verifyErr != nil {
		t.Fatalf("expected the re-anchored log to verify: %v", verifyErr)
	}
	assertOutputContains(t, printed, "chain intact",
		"accepted as history by the re-chain", "not as written by this server")

	out.Reset()
	if err := rechainAuditLogCommand(dir, true, strings.NewReader(""),
		&out); err != nil {
		t.Fatalf("failed to run the re-chain a second time: %v", err)
	}
	assertOutputContains(t, out.String(), "nothing to re-chain")
}

// TestReanchorCommandRefusesTamperingUnattended checks that
// -confirm-rechain does not re-anchor a log whose failure a change of
// secret does not explain, and that the log is left failing.
func TestReanchorCommandRefusesTamperingUnattended(t *testing.T) {
	dir := tamperedAuditStore(t)

	var out bytes.Buffer
	err := rechainAuditLogCommand(dir, true, strings.NewReader(""), &out)
	if err == nil || !strings.Contains(err.Error(), "will not re-anchor") {
		t.Fatalf("expected an unattended refusal, got %v", err)
	}
	assertOutputContains(t, out.String(), "may be\ntampering")

	err = verifyAuditLogCommand(dir)
	if got := auditVerifyExitCode(err); got != auditExitTampered {
		t.Errorf("expected the log still to report tampering, got exit "+
			"status %d: %v", got, err)
	}
}

// TestReanchorCommandAcceptsTamperingInteractively checks that an
// operator who has read the plan can still re-anchor a tampered log.
func TestReanchorCommandAcceptsTamperingInteractively(t *testing.T) {
	dir := tamperedAuditStore(t)

	var out bytes.Buffer
	if err := rechainAuditLogCommand(dir, false,
		strings.NewReader(auditRechainConfirmWord+"\n"), &out); err != nil {
		t.Fatalf("failed to re-anchor the log: %v", err)
	}
	assertOutputContains(t, out.String(), "may be\ntampering",
		"Audit log re-anchored")

	var verifyErr error
	captureStdout(t, func() { verifyErr = verifyAuditLogCommand(dir) })
	if verifyErr != nil {
		t.Fatalf("expected the re-anchored log to verify: %v", verifyErr)
	}
}

// TestPrintAuditPreviousHead checks each form the previous head takes
// in the plan, including a hash carrying control characters.
func TestPrintAuditPreviousHead(t *testing.T) {
	tests := []struct {
		name  string
		head  *auth.AuditRechainHead
		wants []string
		never string
	}{
		{"none", nil, []string{"(none)"}, "event"},
		{"verified", &auth.AuditRechainHead{EventID: 9,
			Action: "audit.purge", HeadID: 4, HeadHash: "abc",
			Verified: true},
			[]string{"event 4, hash abc", "audit.purge event 9",
				"which verifies"}, "DOES NOT"},
		{"unverified", &auth.AuditRechainHead{EventID: 9,
			Action: "audit.rechain", HeadID: 4, HeadHash: "a\x1b[31mb"},
			[]string{"DOES NOT verify"}, "\x1b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			printAuditPreviousHead(&out, tt.head)
			assertOutputContains(t, out.String(), tt.wants...)
			if strings.Contains(out.String(), tt.never) {
				t.Errorf("expected the output not to contain %q, got:\n%s",
					tt.never, out.String())
			}
		})
	}
}

// TestAuditFailureMessagesNameTheRechain checks that both verification
// failures an operator can recover from point at -rechain-audit-log, and
// that the key mismatch is not described as tampering.
func TestAuditFailureMessagesNameTheRechain(t *testing.T) {
	mismatch := describeAuditVerifyFailure(3, 1,
		fmt.Errorf("%w: test", auth.ErrAuditKeyMismatch))
	assertOutputContains(t, mismatch.Error(), "-rechain-audit-log",
		"rather than tampering")
	if strings.Contains(mismatch.Error(), "Treat this as possible tampering") {
		t.Errorf("a key mismatch must not be reported as tampering: %v",
			mismatch)
	}

	broken := describeAuditVerifyFailure(3, 2,
		fmt.Errorf("%w at row 2", auth.ErrAuditChainBroken))
	assertOutputContains(t, broken.Error(), "-rechain-audit-log",
		"Treat this as possible tampering")
	if got := auditVerifyExitCode(broken); got != auditExitTampered {
		t.Errorf("expected exit status %d, got %d", auditExitTampered, got)
	}
}

// TestAuditPurgeFailureMessage checks the repeating purge log line names
// both commands only for a failure they can resolve.
func TestAuditPurgeFailureMessage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		hint bool
	}{
		{"key mismatch", fmt.Errorf("%w: x", auth.ErrAuditKeyMismatch), true},
		{"chain broken", fmt.Errorf("%w: x", auth.ErrAuditChainBroken), true},
		{"other", errors.New("disk I/O error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := auditPurgeFailureMessage(tt.err)
			if !strings.HasPrefix(msg,
				"[ERROR] Failed to purge audit events: ") {
				t.Errorf("unexpected prefix: %s", msg)
			}
			for _, cmd := range []string{"-verify-audit-log",
				"-rechain-audit-log"} {
				if got := strings.Contains(msg, cmd); got != tt.hint {
					t.Errorf("expected mention of %s to be %v: %s", cmd,
						tt.hint, msg)
				}
			}
		})
	}
}
