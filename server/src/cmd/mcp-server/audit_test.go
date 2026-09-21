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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"

	_ "modernc.org/sqlite"
)

// lastAuditEvent returns the most recent audit event recorded in the
// store at dataDir.
func lastAuditEvent(t *testing.T, dataDir string) auth.AuditEvent {
	t.Helper()

	store, err := auth.NewAuthStore(dataDir, 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("failed to reopen auth store: %v", err)
	}
	defer store.Close()

	events, total, err := store.ListAuditEvents(auth.AuditFilter{Limit: 1})
	if err != nil {
		t.Fatalf("failed to list audit events: %v", err)
	}
	if total == 0 || len(events) == 0 {
		t.Fatal("expected at least one audit event")
	}
	return events[0]
}

// seedAuditEvents populates the store at dataDir with a couple of
// events by making real changes through a CLI-attributed store.
func seedAuditEvents(t *testing.T, dataDir string) {
	t.Helper()

	store, err := auth.NewAuthStore(dataDir, 0, 0, auth.AuditKeyForTesting())
	if err != nil {
		t.Fatalf("failed to create auth store: %v", err)
	}
	cli := cliStore(store)
	if _, err := cli.CreateGroup("auditgroup", "seeded"); err != nil {
		t.Fatalf("failed to create group: %v", err)
	}
	if err := cli.CreateUser("audituser", "Sup3rSecret!pass", "", "", ""); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("failed to close store: %v", err)
	}
}

func TestCLIActor(t *testing.T) {
	actor := cliActor()

	if actor.Type != auth.ActorCLI {
		t.Errorf("expected actor type %q, got %q", auth.ActorCLI, actor.Type)
	}
	if actor.Name == "" {
		t.Error("expected a non-empty actor name")
	}
	if actor.IP != "" {
		t.Errorf("expected no actor IP, got %q", actor.IP)
	}
	if actor.ID != nil {
		t.Error("expected a nil actor ID")
	}
}

func TestCLIActorName(t *testing.T) {
	failing := func() (*user.User, error) {
		return nil, errors.New("no current user")
	}

	t.Run("uses the current OS user", func(t *testing.T) {
		got := cliActorName(func() (*user.User, error) {
			return &user.User{Username: "osuser"}, nil
		})
		if got != "osuser" {
			t.Errorf("expected %q, got %q", "osuser", got)
		}
	})

	t.Run("falls back to USER", func(t *testing.T) {
		t.Setenv("USER", "envuser")
		if got := cliActorName(failing); got != "envuser" {
			t.Errorf("expected %q, got %q", "envuser", got)
		}
	})

	t.Run("falls back to unknown", func(t *testing.T) {
		t.Setenv("USER", "")
		if got := cliActorName(failing); got != "unknown" {
			t.Errorf("expected %q, got %q", "unknown", got)
		}
	})
}

func TestAuditTarget(t *testing.T) {
	id := int64(7)
	tests := []struct {
		name     string
		event    auth.AuditEvent
		expected string
	}{
		{"no target", auth.AuditEvent{}, "-"},
		{"named target", auth.AuditEvent{
			TargetType: "group", TargetName: "admins", TargetID: &id,
		}, "group:admins"},
		{"identified target", auth.AuditEvent{
			TargetType: "token", TargetID: &id,
		}, "token#7"},
		{"type only", auth.AuditEvent{TargetType: "user"}, "user"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := auditTarget(&tt.event); got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestTruncateField(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		width    int
		expected string
	}{
		{"short enough", "abc", 5, "abc"},
		{"exact width", "abcde", 5, "abcde"},
		{"truncated with ellipsis", "abcdefgh", 6, "abc..."},
		{"narrow width is cut hard", "abcdef", 2, "ab"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateField(tt.value, tt.width); got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestDeleteGroupCommandRecordsCLIActor(t *testing.T) {
	dataDir, store := newCLITestStore(t)
	if _, err := store.CreateGroup("doomed", "to be deleted"); err != nil {
		t.Fatalf("failed to create group: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("failed to close store: %v", err)
	}

	captureStdout(t, func() {
		if err := deleteGroupCommand(dataDir, "doomed"); err != nil {
			t.Errorf("deleteGroupCommand failed: %v", err)
		}
	})

	ev := lastAuditEvent(t, dataDir)
	if ev.ActorType != auth.ActorCLI {
		t.Errorf("expected actor type %q, got %q", auth.ActorCLI, ev.ActorType)
	}
	if ev.ActorName != cliActor().Name {
		t.Errorf("expected actor name %q, got %q", cliActor().Name, ev.ActorName)
	}
}

func TestListAuditCommandTable(t *testing.T) {
	dataDir := t.TempDir()
	seedAuditEvents(t, dataDir)

	out := captureStdout(t, func() {
		if err := listAuditCommand(dataDir, &Flags{}); err != nil {
			t.Errorf("listAuditCommand failed: %v", err)
		}
	})

	for _, want := range []string{"ID", "Time", "Actor", "Action", "Target",
		"Outcome", "Error", "group.create", "user.create"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestListAuditCommandJSON(t *testing.T) {
	dataDir := t.TempDir()
	seedAuditEvents(t, dataDir)

	out := captureStdout(t, func() {
		if err := listAuditCommand(dataDir, &Flags{JSONOutput: true}); err != nil {
			t.Errorf("listAuditCommand failed: %v", err)
		}
	})

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least two JSON lines, got:\n%s", out)
	}
	for _, line := range lines {
		var ev auth.AuditEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line %q is not valid JSON: %v", line, err)
		}
		if ev.ID == 0 {
			t.Errorf("expected a non-zero id in %q", line)
		}
	}
}

func TestListAuditCommandFilters(t *testing.T) {
	dataDir := t.TempDir()
	seedAuditEvents(t, dataDir)

	t.Run("no matching events", func(t *testing.T) {
		out := captureStdout(t, func() {
			f := &Flags{AuditAction: "nothing.happens"}
			if err := listAuditCommand(dataDir, f); err != nil {
				t.Errorf("listAuditCommand failed: %v", err)
			}
		})
		if !strings.Contains(out, "No audit events found.") {
			t.Errorf("expected the empty-result message, got:\n%s", out)
		}
	})

	t.Run("filters are applied", func(t *testing.T) {
		out := captureStdout(t, func() {
			f := &Flags{
				AuditAction:     "group.create",
				AuditActor:      cliActor().Name,
				AuditTargetType: "group",
				AuditOutcome:    "success",
				AuditSince:      "2000-01-01",
				AuditUntil:      time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
				AuditLimit:      10,
			}
			if err := listAuditCommand(dataDir, f); err != nil {
				t.Errorf("listAuditCommand failed: %v", err)
			}
		})
		if !strings.Contains(out, "group.create") {
			t.Errorf("expected group.create in output, got:\n%s", out)
		}
		if strings.Contains(out, "user.create") {
			t.Errorf("did not expect user.create in output, got:\n%s", out)
		}
	})

	t.Run("target id filter", func(t *testing.T) {
		out := captureStdout(t, func() {
			f := &Flags{AuditTargetType: "group", AuditTargetID: 999999}
			if err := listAuditCommand(dataDir, f); err != nil {
				t.Errorf("listAuditCommand failed: %v", err)
			}
		})
		if !strings.Contains(out, "No audit events found.") {
			t.Errorf("expected no matches, got:\n%s", out)
		}
	})

	t.Run("bad since is an error", func(t *testing.T) {
		err := listAuditCommand(dataDir, &Flags{AuditSince: "yesterday"})
		if err == nil {
			t.Fatal("expected an error for an unparseable -audit-since")
		}
		if !strings.Contains(err.Error(), "audit-since") {
			t.Errorf("expected the error to name the flag, got: %v", err)
		}
	})

	t.Run("bad until is an error", func(t *testing.T) {
		err := listAuditCommand(dataDir, &Flags{AuditUntil: "tomorrow"})
		if err == nil {
			t.Fatal("expected an error for an unparseable -audit-until")
		}
		if !strings.Contains(err.Error(), "audit-until") {
			t.Errorf("expected the error to name the flag, got: %v", err)
		}
	})

	t.Run("unopenable data dir returns error", func(t *testing.T) {
		if err := listAuditCommand(blockingDataDir(t), &Flags{}); err == nil {
			t.Fatal("expected an error when the auth store cannot be opened")
		}
	})
}

func TestParseAuditTime(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty is nil", "", false},
		{"date only", "2026-09-15", false},
		{"rfc 3339", "2026-09-15T10:11:12Z", false},
		{"rfc 3339 with offset", "2026-09-15T10:11:12+01:00", false},
		{"nonsense", "not a time", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAuditTime(tt.input, "audit-since")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.input == "" && got != nil {
				t.Errorf("expected nil for the empty string, got %v", got)
			}
			if tt.input != "" && got == nil {
				t.Error("expected a parsed time")
			}
		})
	}
}

func TestVerifyAuditLogCommand(t *testing.T) {
	t.Run("clean store verifies", func(t *testing.T) {
		dataDir := t.TempDir()
		seedAuditEvents(t, dataDir)

		out := captureStdout(t, func() {
			if err := verifyAuditLogCommand(dataDir); err != nil {
				t.Errorf("verifyAuditLogCommand failed: %v", err)
			}
		})
		if !strings.Contains(out, "chain intact") {
			t.Errorf("expected a success message, got:\n%s", out)
		}
		if !strings.Contains(out, "event(s)") {
			t.Errorf("expected an event count, got:\n%s", out)
		}
	})

	t.Run("tampered row is detected", func(t *testing.T) {
		dataDir := t.TempDir()
		seedAuditEvents(t, dataDir)

		id := tamperAuditRow(t, dataDir)

		err := verifyAuditLogCommand(dataDir)
		if err == nil {
			t.Fatal("expected an error after tampering")
		}
		if !strings.Contains(err.Error(), "row "+strconv.FormatInt(id, 10)) {
			t.Errorf("expected the error to name row %d, got: %v", id, err)
		}
	})

	t.Run("tampering exits with its own status", func(t *testing.T) {
		dataDir := t.TempDir()
		seedAuditEvents(t, dataDir)
		tamperAuditRow(t, dataDir)

		err := verifyAuditLogCommand(dataDir)
		if err == nil {
			t.Fatal("expected an error after tampering")
		}
		if got := auditVerifyExitCode(err); got != auditExitTampered {
			t.Errorf("expected exit status %d for tampering, got %d",
				auditExitTampered, got)
		}
	})

	t.Run("unopenable data dir returns error", func(t *testing.T) {
		err := verifyAuditLogCommand(blockingDataDir(t))
		if err == nil {
			t.Fatal("expected an error when the auth store cannot be opened")
		}
		if got := auditVerifyExitCode(err); got != 1 {
			t.Errorf("expected the general-purpose exit status 1, got %d", got)
		}
	})
}

// TestDescribeAuditVerifyFailure checks that the three outcomes an
// operator has to act on differently are reported differently. A key
// mismatch means find the right secret file; a broken chain or an
// unkeyed row means an incident; anything else keeps the
// general-purpose status the other commands use.
func TestDescribeAuditVerifyFailure(t *testing.T) {
	tests := []struct {
		name     string
		rows     int
		firstBad int64
		err      error
		wantCode int
		wantText string
	}{
		{
			name:     "key mismatch",
			rows:     4,
			firstBad: 1,
			err:      fmt.Errorf("%w: nothing verified", auth.ErrAuditKeyMismatch),
			wantCode: auditExitKeyMismatch,
			wantText: "server secret file",
		},
		{
			name:     "broken chain",
			rows:     4,
			firstBad: 3,
			err:      fmt.Errorf("%w at row 3", auth.ErrAuditChainBroken),
			wantCode: auditExitTampered,
			wantText: "failed at row 3",
		},
		{
			name:     "downgrade",
			rows:     4,
			firstBad: 3,
			err:      fmt.Errorf("%w at row 3", auth.ErrAuditChainDowngraded),
			wantCode: auditExitTampered,
			wantText: "failed at row 3",
		},
		{
			name:     "unkeyed row",
			rows:     4,
			firstBad: 3,
			err:      fmt.Errorf("%w: row 3", auth.ErrAuditUnkeyedRow),
			wantCode: auditExitTampered,
			wantText: "failed at row 3",
		},
		{
			name:     "anything else",
			rows:     2,
			firstBad: 2,
			err:      errors.New("unknown audit hash version 99"),
			wantCode: 1,
			wantText: "failed at row 2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := describeAuditVerifyFailure(tc.rows, tc.firstBad, tc.err)
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := auditVerifyExitCode(err); got != tc.wantCode {
				t.Errorf("expected exit status %d, got %d", tc.wantCode, got)
			}
			if !strings.Contains(err.Error(), tc.wantText) {
				t.Errorf("expected the message to contain %q, got: %v",
					tc.wantText, err)
			}
			if !errors.Is(err, tc.err) {
				t.Error("expected the cause to remain unwrappable")
			}
		})
	}

	if got := auditVerifyExitCode(nil); got != 1 {
		t.Errorf("expected a nil error to map to 1, got %d", got)
	}
}

// TestVerifyAuditLogCommandReportsKeyMismatch checks the whole command
// against a store whose rows were written under a different secret,
// which is what an operator sees after rotating or mislaying one.
func TestVerifyAuditLogCommandReportsKeyMismatch(t *testing.T) {
	dataDir := t.TempDir()

	store, err := auth.NewAuthStore(dataDir, 0, 0,
		auth.DeriveAuditKey("the secret these rows were written under"))
	if err != nil {
		t.Fatalf("failed to create auth store: %v", err)
	}
	if err := store.CreateUser("alice", "correct horse battery staple",
		"", "Alice", "alice@example.com"); err != nil {
		t.Fatalf("failed to create a user: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("failed to close the store: %v", err)
	}

	// openAuthStoreCLI uses the test key, which is not the one above.
	err = verifyAuditLogCommand(dataDir)
	if err == nil {
		t.Fatal("expected verification to fail under a different key")
	}
	if got := auditVerifyExitCode(err); got != auditExitKeyMismatch {
		t.Errorf("expected exit status %d for a key mismatch, got %d",
			auditExitKeyMismatch, got)
	}
	if !strings.Contains(err.Error(), "secret_file") {
		t.Errorf("expected the message to point at secret_file, got: %v", err)
	}
}

// tamperAuditRow edits the action of the newest audit row in place,
// dropping and recreating the append-only trigger so the update is
// allowed, and returns the id of the row it changed.
func tamperAuditRow(t *testing.T, dataDir string) int64 {
	t.Helper()

	db, err := sql.Open("sqlite", filepath.Join(dataDir, "auth.db"))
	if err != nil {
		t.Fatalf("failed to open auth.db directly: %v", err)
	}
	defer db.Close()

	var id int64
	if err := db.QueryRow(
		"SELECT id FROM audit_events ORDER BY id DESC LIMIT 1").Scan(&id); err != nil {
		t.Fatalf("failed to read the newest audit row: %v", err)
	}

	if _, err := db.Exec("DROP TRIGGER IF EXISTS audit_events_no_update"); err != nil {
		t.Fatalf("failed to drop the append-only trigger: %v", err)
	}
	if _, err := db.Exec(
		"UPDATE audit_events SET action = ? WHERE id = ?", "tampered", id); err != nil {
		t.Fatalf("failed to tamper with the audit row: %v", err)
	}
	if _, err := db.Exec(`
        CREATE TRIGGER IF NOT EXISTS audit_events_no_update
        BEFORE UPDATE ON audit_events
        BEGIN
            SELECT RAISE(ABORT, 'audit_events is append-only');
        END`); err != nil {
		t.Fatalf("failed to recreate the append-only trigger: %v", err)
	}

	return id
}

// TestPrintAuditTableSanitisesControlCharacters checks that an actor
// name, target name or error carrying control characters cannot inject
// escape sequences or extra lines into an operator's console, whilst
// the same values pass through -json untouched, where JSON encoding
// escapes them anyway.
func TestPrintAuditTableSanitisesControlCharacters(t *testing.T) {
	events := []auth.AuditEvent{{
		ID:         1,
		OccurredAt: time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
		ActorType:  auth.ActorUser,
		ActorName:  "eve\r\n\x1b[31mADMIN",
		Action:     "user.create",
		TargetType: "user",
		TargetName: "vic\ntim",
		Outcome:    auth.OutcomeFailure,
		Error:      "boom\x1b]0;owned\x07",
	}}

	out := captureStdout(t, func() { printAuditTable(events, 1) })

	// SanitizeForLog escapes the escape character and the carriage
	// return, which is what makes the cursor movement and the color
	// change inert. It does not escape BEL, which is harmless on its
	// own once the introducing ESC has been escaped.
	for _, forbidden := range []string{"\x1b", "\r"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("table output still carries %q: %q", forbidden, out)
		}
	}
	// The table is a leading blank line, a heading, two rules, one row,
	// a closing rule, the count and a trailing blank: nine newlines. A
	// newline smuggled through a field would add another.
	if got := strings.Count(out, "\n"); got != 9 {
		t.Errorf("expected 9 newlines in the table, got %d:\n%q", got, out)
	}
	if !strings.Contains(out, "\\r\\n") {
		t.Errorf("expected the escaped newline to be visible, got:\n%q", out)
	}
}

// TestListAuditCommandJSONEmptyPrintsNothing checks that -json with no
// matching events writes an empty stream rather than a human sentence a
// consumer would have to filter out.
func TestListAuditCommandJSONEmptyPrintsNothing(t *testing.T) {
	dataDir := t.TempDir()
	seedAuditEvents(t, dataDir)

	out := captureStdout(t, func() {
		f := &Flags{AuditAction: "nothing.happens", JSONOutput: true}
		if err := listAuditCommand(dataDir, f); err != nil {
			t.Errorf("listAuditCommand failed: %v", err)
		}
	})

	if out != "" {
		t.Errorf("expected no output for an empty JSON result, got %q", out)
	}
}

// TestPrintAuditJSONEncodingFailure checks that an event which cannot
// be encoded is reported by id rather than written as a broken line. A
// details column holding text that is not valid JSON is the only way
// this arises, because json.RawMessage is copied through verbatim.
func TestPrintAuditJSONEncodingFailure(t *testing.T) {
	events := []auth.AuditEvent{{
		ID:         7,
		OccurredAt: time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
		ActorType:  auth.ActorSystem,
		ActorName:  "system",
		Action:     "user.create",
		Outcome:    auth.OutcomeSuccess,
		Details:    json.RawMessage(`{"truncated":`),
	}}

	var err error
	out := captureStdout(t, func() { err = printAuditJSON(events) })
	if err == nil {
		t.Fatal("expected an encoding error for malformed details")
	}
	if !strings.Contains(err.Error(), "audit event 7") {
		t.Errorf("expected the failing event id in the error, got %v", err)
	}
	if out != "" {
		t.Errorf("expected no output for an unencodable event, got %q", out)
	}
}
