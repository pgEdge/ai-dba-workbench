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
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// auditDateLayout is the shorthand accepted by -audit-since and
// -audit-until in addition to RFC 3339, interpreted as midnight UTC.
const auditDateLayout = "2006-01-02"

// auditRuleWidth is the width of the separator rules printed above and
// below the audit table, matching the column widths in
// printAuditTable.
const auditRuleWidth = 132

// cliActorName resolves the name to attribute command-line changes to:
// the current OS user, falling back to $USER and then to "unknown".
// current is injected so the fallbacks can be tested on a host where
// user.Current always succeeds.
func cliActorName(current func() (*user.User, error)) string {
	name := ""
	if u, err := current(); err == nil && u != nil {
		name = u.Username
	}
	if name == "" {
		name = os.Getenv("USER")
	}
	if name == "" {
		name = "unknown"
	}

	return name
}

// cliActor identifies the operator running the server command line.
// There is no IP, because a command line has no peer address, and no
// ID, because the operator is an OS user rather than a Workbench one.
func cliActor() auth.Actor {
	return auth.Actor{Type: auth.ActorCLI, Name: cliActorName(user.Current)}
}

// cliStore returns a view of the auth store that attributes every
// audited change to the operator running the command line. Every
// mutating call made by a CLI command goes through this view so that
// the audit log records who ran the command.
func cliStore(store *auth.AuthStore) *auth.ActorStore {
	return store.AsActor(cliActor())
}

// parseAuditTime parses a -audit-since or -audit-until value, which may
// be a full RFC 3339 timestamp or a bare YYYY-MM-DD date taken as
// midnight UTC. An empty value yields a nil time, meaning unbounded.
// flagName names the flag in the error so the operator knows which
// value was rejected.
func parseAuditTime(value, flagName string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}

	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return &parsed, nil
	}
	if parsed, err := time.Parse(auditDateLayout, value); err == nil {
		return &parsed, nil
	}

	return nil, fmt.Errorf(
		"invalid -%s value %q: expected RFC 3339 (2006-01-02T15:04:05Z07:00) or a date (2006-01-02)",
		flagName, value)
}

// auditFilterFromFlags builds an auth.AuditFilter from the audit query
// flags, rejecting unparseable timestamps.
func auditFilterFromFlags(f *Flags) (auth.AuditFilter, error) {
	since, err := parseAuditTime(f.AuditSince, "audit-since")
	if err != nil {
		return auth.AuditFilter{}, err
	}
	until, err := parseAuditTime(f.AuditUntil, "audit-until")
	if err != nil {
		return auth.AuditFilter{}, err
	}

	filter := auth.AuditFilter{
		ActorName:  f.AuditActor,
		Action:     f.AuditAction,
		TargetType: f.AuditTargetType,
		Outcome:    f.AuditOutcome,
		Since:      since,
		Until:      until,
		Limit:      f.AuditLimit,
	}
	if f.AuditTargetID != 0 {
		targetID := f.AuditTargetID
		filter.TargetID = &targetID
	}

	return filter, nil
}

// auditTarget renders an event's target as "type:name", falling back to
// "type#id" when the target has no name and to the bare type when it
// has neither.
func auditTarget(ev *auth.AuditEvent) string {
	switch {
	case ev.TargetType == "":
		return "-"
	case ev.TargetName != "":
		return ev.TargetType + ":" + ev.TargetName
	case ev.TargetID != nil:
		return ev.TargetType + "#" + strconv.FormatInt(*ev.TargetID, 10)
	default:
		return ev.TargetType
	}
}

// truncateField shortens a value to width characters, marking a
// shortened value with a trailing ellipsis, so that one long field
// cannot break the table alignment.
func truncateField(value string, width int) string {
	if len(value) <= width {
		return value
	}
	if width <= 3 {
		return value[:width]
	}
	return value[:width-3] + "..."
}

// printAuditTable writes the events as a fixed-width table, newest
// first.
func printAuditTable(events []auth.AuditEvent, total int) {
	fmt.Println("\nAudit events:")
	fmt.Println(strings.Repeat("=", auditRuleWidth))
	fmt.Printf("%-8s %-20s %-20s %-24s %-24s %-8s %s\n",
		"ID", "Time", "Actor", "Action", "Target", "Outcome", "Error")
	fmt.Println(strings.Repeat("-", auditRuleWidth))

	for i := range events {
		ev := &events[i]
		actor := fmt.Sprintf("%s (%s)", ev.ActorName, ev.ActorType)
		fmt.Printf("%-8d %-20s %-20s %-24s %-24s %-8s %s\n",
			ev.ID,
			ev.OccurredAt.UTC().Format("2006-01-02 15:04:05"),
			truncateField(actor, 20),
			truncateField(ev.Action, 24),
			truncateField(auditTarget(ev), 24),
			ev.Outcome,
			truncateField(ev.Error, 24),
		)
	}

	fmt.Println(strings.Repeat("=", auditRuleWidth))
	fmt.Printf("Showing %d of %d event(s).\n\n", len(events), total)
}

// printAuditJSON writes one JSON object per line, using the AuditEvent
// JSON tags, so the output can be piped straight into jq or a log
// shipper.
func printAuditJSON(events []auth.AuditEvent) error {
	for i := range events {
		ev := &events[i]
		encoded, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("failed to encode audit event %d: %w", ev.ID, err)
		}
		fmt.Println(string(encoded))
	}

	return nil
}

// listAuditCommand handles the list-audit command, printing the
// matching audit events as a table or, with -json, as one JSON object
// per line.
func listAuditCommand(dataDir string, f *Flags) error {
	filter, err := auditFilterFromFlags(f)
	if err != nil {
		return err
	}

	store, err := openAuthStoreCLI(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	events, total, err := store.ListAuditEvents(filter)
	if err != nil {
		return fmt.Errorf("failed to list audit events: %w", err)
	}

	if len(events) == 0 {
		fmt.Println("No audit events found.")
		return nil
	}

	if f.JSONOutput {
		return printAuditJSON(events)
	}

	printAuditTable(events, total)
	return nil
}

// verifyAuditLogCommand handles the verify-audit-log command,
// recomputing the hash chain over the whole audit log and reporting the
// first row that fails.
func verifyAuditLogCommand(dataDir string) error {
	store, err := openAuthStoreCLI(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	rows, firstBad, err := store.VerifyAuditChain()
	if err != nil {
		if firstBad != 0 {
			return fmt.Errorf(
				"audit log verification failed at row %d after %d event(s): %w",
				firstBad, rows, err)
		}
		return fmt.Errorf("failed to verify audit log: %w", err)
	}

	fmt.Printf("Audit log verified: %d event(s), chain intact\n", rows)
	return nil
}
