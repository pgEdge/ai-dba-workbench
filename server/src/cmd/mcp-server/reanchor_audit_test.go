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
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
		auth.DeriveAuditKey(olderServerSecret))
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

// confirmRechainOpts is -confirm-rechain given on its own.
var confirmRechainOpts = auditRechainOptions{assumeYes: true}

// olderServerSecret is the obviously fake secret rotatedSecretAuditStore
// writes the log under.
const olderServerSecret = "an older server secret"

// writeSecretFile writes secret to a file readable only by its owner,
// as the server's own secret file is, and returns its path.
func writeSecretFile(t *testing.T, secret string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "previous.secret")
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatalf("failed to write the secret file: %v", err)
	}

	return path
}

// addCurrentKeyEvents records events under the key the commands use.
func addCurrentKeyEvents(t *testing.T, dir string, names ...string) {
	t.Helper()

	store, err := auth.NewAuthStore(dir, 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("failed to open the auth store: %v", err)
	}
	for _, name := range names {
		if err := store.CreateUser(name, "correct horse battery staple",
			"", "Test User", name+"@example.com"); err != nil {
			t.Fatalf("failed to create a user: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("failed to close the store: %v", err)
	}
}

// rotatedWithPreviousSecret is -confirm-rechain given the secret
// rotatedSecretAuditStore wrote the log under.
func rotatedWithPreviousSecret(t *testing.T) auditRechainOptions {
	t.Helper()

	return auditRechainOptions{assumeYes: true,
		previousSecretFile: writeSecretFile(t, olderServerSecret)}
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
	if err := rechainAuditLogCommand(dir, auditRechainOptions{},
		strings.NewReader("yes\n"), &out); err != nil {
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
	addCurrentKeyEvents(t, dir, "bob")

	var out bytes.Buffer
	if err := rechainAuditLogCommand(dir, rotatedWithPreviousSecret(t),
		strings.NewReader(""), &out); err != nil {
		t.Fatalf("failed to re-anchor the log: %v", err)
	}
	assertOutputContains(t, out.String(), "-confirm-rechain was given",
		"verifies under the previous", "Audit log re-anchored",
		"-verify-audit-log")

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
	if err := rechainAuditLogCommand(dir, confirmRechainOpts,
		strings.NewReader(""), &out); err != nil {
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
	err := rechainAuditLogCommand(dir, confirmRechainOpts,
		strings.NewReader(""), &out)
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
	if err := rechainAuditLogCommand(dir, auditRechainOptions{},
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

// validHash is shaped like a genuine audit row hash.
var validHash = strings.Repeat("0123456789abcdef", 4)

// TestAuditPlanHash checks that only a value shaped like a real hash is
// printed as it stands.
func TestAuditPlanHash(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"a genuine hash", validHash, validHash},
		{"the wrong length", "abc", "(not a valid hash: 3 bytes)"},
		{"a control sequence", strings.Repeat("a", 63) + "\u009b",
			"(not a valid hash: 65 bytes)"},
		{"upper case", strings.Repeat("A", 64),
			"(not a valid hash: holds characters other than lowercase hex " +
				"digits)"},
		{"an escape at full length", strings.Repeat("a", 63) + "\x1b",
			"(not a valid hash: holds characters other than lowercase hex " +
				"digits)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := auditPlanHash(tt.in); got != tt.want {
				t.Errorf("auditPlanHash(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestPrintAuditReanchorPlanWithoutHistory checks that a plan accepting
// no history warns that the re-anchor removes the evidence of the
// failure it records.
func TestPrintAuditReanchorPlanWithoutHistory(t *testing.T) {
	var out bytes.Buffer
	printAuditReanchorPlan(&out, "/var/lib/example", auth.AuditRechainPlan{
		Events:   3,
		Problem:  fmt.Errorf("%w: audit chain tail missing", auth.ErrAuditChainBroken),
		HeadID:   1,
		HeadHash: validHash,
	})
	assertOutputContains(t, out.String(), "(none; every event verifies",
		"removes the\nevidence", "Its hash:              "+validHash)
}

// TestReanchorCommandRefusesARotationWithLaterTampering checks that a
// rotated secret does not let -confirm-rechain accept tampering further
// on in the log, which the re-anchor would otherwise take in as history.
func TestReanchorCommandRefusesARotationWithLaterTampering(t *testing.T) {
	dir := rotatedSecretAuditStore(t)
	store, err := auth.NewAuthStore(dir, 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("failed to open the auth store: %v", err)
	}
	for _, name := range []string{"bob", "carol"} {
		if err := store.CreateUser(name, "correct horse battery staple",
			"", "Test User", name+"@example.com"); err != nil {
			t.Fatalf("failed to create a user: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("failed to close the store: %v", err)
	}
	tamperAuditRow(t, dir)

	err = verifyAuditLogCommand(dir)
	if got := auditVerifyExitCode(err); got != auditExitTampered {
		t.Errorf("expected tampering, got exit status %d: %v", got, err)
	}

	var out bytes.Buffer
	err = rechainAuditLogCommand(dir, rotatedWithPreviousSecret(t),
		strings.NewReader(""), &out)
	if err == nil || !strings.Contains(err.Error(), "will not re-anchor") {
		t.Fatalf("expected an unattended refusal, got %v", err)
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
			Action: "audit.purge", HeadID: 4, HeadHash: validHash,
			Verified: true},
			[]string{"event 4, hash " + validHash, "audit.purge event 9",
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

// assertStillKeyMismatch checks that a refused re-anchor left the log
// failing as a changed secret, exactly as before.
func assertStillKeyMismatch(t *testing.T, dir string) {
	t.Helper()

	err := verifyAuditLogCommand(dir)
	if got := auditVerifyExitCode(err); got != auditExitKeyMismatch {
		t.Errorf("expected the log still to report a key mismatch, got "+
			"exit status %d: %v", got, err)
	}
}

// TestReanchorCommandNeedsThePreviousSecretUnattended checks that
// -confirm-rechain on its own does not re-anchor even a genuine change
// of secret, because the shape of the log is all it would have to go on.
func TestReanchorCommandNeedsThePreviousSecretUnattended(t *testing.T) {
	dir := rotatedSecretAuditStore(t)
	addCurrentKeyEvents(t, dir, "bob")

	var out bytes.Buffer
	err := rechainAuditLogCommand(dir, confirmRechainOpts,
		strings.NewReader(""), &out)
	if err == nil || !strings.Contains(err.Error(), "will not re-anchor") ||
		!strings.Contains(err.Error(), "-previous-secret-file") {
		t.Fatalf("expected a refusal naming -previous-secret-file, got %v",
			err)
	}
	assertStillKeyMismatch(t, dir)
}

// TestReanchorCommandRefusesTheWrongPreviousSecret checks that a
// previous secret the history was not written under is refused, with
// the reason shown in the plan as well as in the error.
func TestReanchorCommandRefusesTheWrongPreviousSecret(t *testing.T) {
	dir := rotatedSecretAuditStore(t)
	addCurrentKeyEvents(t, dir, "bob")

	var out bytes.Buffer
	err := rechainAuditLogCommand(dir, auditRechainOptions{assumeYes: true,
		previousSecretFile: writeSecretFile(t, "not the older secret")},
		strings.NewReader(""), &out)
	if err == nil || !strings.Contains(err.Error(), "does not account") ||
		!strings.Contains(err.Error(), "does not verify under it") {
		t.Fatalf("expected a refusal for the wrong secret, got %v", err)
	}
	assertOutputContains(t, out.String(), "DOES NOT account")
	assertStillKeyMismatch(t, dir)
}

// TestReanchorCommandRefusesWithNoLaterEvent checks that an unattended
// re-anchor is refused when nothing in the log verifies under the
// current secret, since the re-anchor would then be signed under a
// secret nothing shows the server runs with.
func TestReanchorCommandRefusesWithNoLaterEvent(t *testing.T) {
	dir := rotatedSecretAuditStore(t)

	var out bytes.Buffer
	err := rechainAuditLogCommand(dir, rotatedWithPreviousSecret(t),
		strings.NewReader(""), &out)
	if err == nil || !strings.Contains(err.Error(),
		"no event after the history verifies") {
		t.Fatalf("expected a refusal for want of a later event, got %v", err)
	}
	assertStillKeyMismatch(t, dir)
}

// TestReanchorCommandRefusesAnUnreadablePreviousSecret checks that a
// -previous-secret-file that cannot be read stops the command before it
// looks at the log.
func TestReanchorCommandRefusesAnUnreadablePreviousSecret(t *testing.T) {
	dir := rotatedSecretAuditStore(t)

	var out bytes.Buffer
	err := rechainAuditLogCommand(dir, auditRechainOptions{assumeYes: true,
		previousSecretFile: filepath.Join(t.TempDir(), "missing.secret")},
		strings.NewReader(""), &out)
	if err == nil || !strings.Contains(err.Error(),
		"failed to read -previous-secret-file") {
		t.Fatalf("expected the unreadable file to be reported, got %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected nothing to be printed, got:\n%s", out.String())
	}
	assertStillKeyMismatch(t, dir)
}

// forgeKeyChange deletes the log's first event and puts in its place a
// row that fails verification but whose hash is the one the next event
// links to, which is the shape a changed secret leaves. It is what
// anyone able to write auth.db can do to hide the deletion.
func forgeKeyChange(t *testing.T, dir string) {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(dir, "auth.db"))
	if err != nil {
		t.Fatalf("failed to open auth.db directly: %v", err)
	}
	defer db.Close()

	var link string
	if err := db.QueryRow("SELECT prev_hash FROM audit_events " +
		"WHERE id = 2").Scan(&link); err != nil {
		t.Fatalf("failed to read the second event's link: %v", err)
	}
	if _, err := db.Exec("DELETE FROM audit_events WHERE id = 1"); err != nil {
		t.Fatalf("failed to delete the first event: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO audit_events (id, occurred_at,
        actor_type, actor_name, action, outcome, prev_hash, hash,
        hash_version) VALUES (1, '2026-01-01T00:00:00Z', 'system',
        'forger', 'user.create', 'success', '', ?, 2)`, link); err != nil {
		t.Fatalf("failed to insert the forged event: %v", err)
	}
}

// TestReanchorCommandRefusesAForgedKeyChange checks the attack the
// previous secret exists to stop: a deletion disguised as a changed
// secret is reported as one, but -confirm-rechain re-anchors it neither
// on its own nor given a previous secret, since the forged row verifies
// under no secret the operator has.
func TestReanchorCommandRefusesAForgedKeyChange(t *testing.T) {
	dir := t.TempDir()
	addCurrentKeyEvents(t, dir, "alice", "bob", "carol")
	forgeKeyChange(t, dir)
	assertStillKeyMismatch(t, dir)

	for name, opts := range map[string]auditRechainOptions{
		"no previous secret": confirmRechainOpts,
		"a previous secret": {assumeYes: true,
			previousSecretFile: writeSecretFile(t, olderServerSecret)},
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			err := rechainAuditLogCommand(dir, opts, strings.NewReader(""),
				&out)
			if err == nil || !strings.Contains(err.Error(),
				"will not re-anchor") {
				t.Fatalf("expected an unattended refusal, got %v", err)
			}
			assertStillKeyMismatch(t, dir)
		})
	}
}

// TestReanchorCommandAsksOnlyOnATerminal checks that an interactive
// re-anchor whose input is not a terminal refuses without reading it,
// so that a confirmation word piped in by a script is not taken as an
// operator having read the plan.
func TestReanchorCommandAsksOnlyOnATerminal(t *testing.T) {
	saved := auditInputIsTerminal
	auditInputIsTerminal = func(io.Reader) bool { return false }
	t.Cleanup(func() { auditInputIsTerminal = saved })

	dir := rotatedSecretAuditStore(t)

	var out bytes.Buffer
	err := rechainAuditLogCommand(dir, auditRechainOptions{},
		strings.NewReader(auditRechainConfirmWord+"\n"), &out)
	if err == nil || !strings.Contains(err.Error(), "on a terminal") {
		t.Fatalf("expected a refusal for input that is not a terminal, "+
			"got %v", err)
	}
	assertStillKeyMismatch(t, dir)
}

// TestAuditInputIsTerminal checks the real terminal test on input that
// is not a terminal, both a string and an ordinary file.
func TestAuditInputIsTerminal(t *testing.T) {
	// TestMain replaces the variable, so this calls the function it
	// starts as.
	isTerminal := defaultAuditInputIsTerminal

	if isTerminal(strings.NewReader(auditRechainConfirmWord)) {
		t.Error("expected a string not to be a terminal")
	}

	f, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatalf("failed to create a file: %v", err)
	}
	defer f.Close()
	if isTerminal(f) {
		t.Error("expected an ordinary file not to be a terminal")
	}
}

// TestPrintAuditReanchorEvidence checks each thing the plan says about
// the previous secret, including a reason carrying control characters.
func TestPrintAuditReanchorEvidence(t *testing.T) {
	tests := []struct {
		name  string
		plan  auth.AuditRechainPlan
		wants []string
		never string
	}{
		{name: "no previous secret", plan: auth.AuditRechainPlan{}},
		{name: "proven", plan: auth.AuditRechainPlan{PreviousKeyGiven: true,
			HistoryProven: true},
			wants: []string{"verifies under the previous"}},
		{name: "unproven", plan: auth.AuditRechainPlan{
			PreviousKeyGiven: true,
			HistoryProofErr:  errors.New("event 3 \x1b[2Jdoes not verify")},
			wants: []string{"DOES NOT account", "event 3"},
			never: "\x1b"},
		{name: "unproven without a reason", plan: auth.AuditRechainPlan{
			PreviousKeyGiven: true},
			wants: []string{"(no reason given)"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			printAuditReanchorEvidence(&out, tt.plan)
			if len(tt.wants) == 0 && out.Len() != 0 {
				t.Errorf("expected nothing, got:\n%s", out.String())
			}
			assertOutputContains(t, out.String(), tt.wants...)
			if tt.never != "" && strings.Contains(out.String(), tt.never) {
				t.Errorf("expected %q to be removed, got %q", tt.never,
					out.String())
			}
		})
	}
}

// TestRunAuditCommands checks that each audit command the flags select
// runs, and that none runs when none is selected. Only commands that
// succeed are run: a failing one ends the process.
func TestRunAuditCommands(t *testing.T) {
	dir := t.TempDir()
	addCurrentKeyEvents(t, dir, "alice")

	if runAuditCommands(&Flags{}, dir) {
		t.Error("expected no audit command to run")
	}

	tests := []struct {
		name  string
		flags Flags
		want  string
	}{
		{"list", Flags{ListAuditCmd: true, AuditLimit: 50}, "user.create"},
		{"verify", Flags{VerifyAuditCmd: true}, "chain intact"},
		{"rechain", Flags{RechainAuditCmd: true, ConfirmRechain: true},
			"nothing to re-chain"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ran := false
			printed := captureStdout(t, func() {
				ran = runAuditCommands(&tt.flags, dir)
			})
			if !ran {
				t.Fatal("expected the command to run")
			}
			assertOutputContains(t, printed, tt.want)
		})
	}
}
