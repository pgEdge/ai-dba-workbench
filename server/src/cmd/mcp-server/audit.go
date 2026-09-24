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
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/logging"
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
//
// Every field the table prints that a caller could have influenced, the
// actor name, the target and the error text, goes through
// logging.SanitizeForLog first. An audit event can be written by an
// unauthenticated principal (a denial names whatever the client claimed
// to be), so without escaping a crafted name could carry terminal
// escape sequences or newlines into an operator's console and rewrite
// what the rest of the table appears to say. Sanitizing before
// truncation also keeps the escaped text inside its column. The -json
// output is left untouched: JSON encoding escapes control characters
// itself, and altering the values would corrupt the machine-readable
// form.
func printAuditTable(events []auth.AuditEvent, total int) {
	fmt.Println("\nAudit events:")
	fmt.Println(strings.Repeat("=", auditRuleWidth))
	fmt.Printf("%-8s %-20s %-20s %-24s %-24s %-8s %s\n",
		"ID", "Time", "Actor", "Action", "Target", "Outcome", "Error")
	fmt.Println(strings.Repeat("-", auditRuleWidth))

	for i := range events {
		ev := &events[i]
		actor := fmt.Sprintf("%s (%s)",
			logging.SanitizeForLog(ev.ActorName), ev.ActorType)
		fmt.Printf("%-8d %-20s %-20s %-24s %-24s %-8s %s\n",
			ev.ID,
			ev.OccurredAt.UTC().Format("2006-01-02 15:04:05"),
			truncateField(actor, 20),
			truncateField(ev.Action, 24),
			truncateField(logging.SanitizeForLog(auditTarget(ev)), 24),
			ev.Outcome,
			truncateField(logging.SanitizeForLog(ev.Error), 24),
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

	// With -json the output is consumed by a machine, so an empty
	// result set is an empty stream rather than a human sentence that
	// would have to be filtered back out.
	if f.JSONOutput {
		return printAuditJSON(events)
	}

	if len(events) == 0 {
		fmt.Println("No audit events found.")
		return nil
	}

	printAuditTable(events, total)
	return nil
}

// Exit statuses for verify-audit-log. They are distinct so that a
// monitoring script can act on the difference without parsing English:
// a mislaid or rotated secret needs someone to find the right file,
// whilst a log that verified as far as row N and then stopped needs an
// incident. Everything else keeps the general-purpose 1 the other
// commands use.
const (
	// auditExitTampered reports a log whose contents contradict the
	// chain, the version ordering or the rule that no row may carry the
	// unkeyed version 1 hash.
	auditExitTampered = 2

	// auditExitKeyMismatch reports a log in which nothing verified,
	// which is far more often the wrong server secret than a rewrite
	// that began at the first row.
	auditExitKeyMismatch = 3
)

// auditVerifyError carries the exit status a failed verification should
// leave behind, so that the decision is made where the failure is
// understood rather than in the dispatcher.
type auditVerifyError struct {
	code int
	err  error
}

func (e *auditVerifyError) Error() string { return e.err.Error() }

func (e *auditVerifyError) Unwrap() error { return e.err }

// auditVerifyExitCode returns the status verify-audit-log should exit
// with for err, defaulting to 1 for anything that carries no opinion.
func auditVerifyExitCode(err error) int {
	var verifyErr *auditVerifyError
	if errors.As(err, &verifyErr) {
		return verifyErr.code
	}

	return 1
}

// verifyAuditLogCommand handles the verify-audit-log command,
// recomputing the hash chain over the whole audit log and reporting the
// first row that fails.
//
// A key mismatch is reported as such rather than as tampering. The two
// look similar from the outside, since under the wrong key no keyed row
// recomputes, but they call for opposite responses, and an operator
// told their log had been tampered with every time a secret was rotated
// would soon stop believing the message when it mattered.
func verifyAuditLogCommand(dataDir string) error {
	store, err := openAuthStoreCLI(dataDir)
	if err != nil {
		return fmt.Errorf("failed to open auth store: %w", err)
	}
	defer store.Close()

	rows, firstBad, err := store.VerifyAuditChain()
	if err != nil {
		return describeAuditVerifyFailure(rows, firstBad, err)
	}

	fmt.Printf("Audit log verified: %d event(s), chain intact\n", rows)
	return nil
}

// rechainAuditLogCommand handles the rechain-audit-log command, which
// re-hashes an existing audit log as keyed version 2 rows under the
// server secret. It is the one-time upgrade step for a database written
// by a release that predates the keyed chain, and such a database
// refuses to open until it has been run.
//
// It prints what it has found and asks before it writes anything. The
// re-chain attests whatever the log says at the moment it runs, so it
// must be a deliberate act taken on the figures, rather than the reflex
// an operator reaches for to clear a start-up error. assumeYes, from
// -confirm-rechain, is the answer for a non-interactive run.
func rechainAuditLogCommand(dataDir string, assumeYes bool, in io.Reader,
	out io.Writer) error {

	auditKey, err := cliAuditKey()
	if err != nil {
		return err
	}

	result, err := auth.RechainAuditLog(dataDir, auditKey, cliActor(),
		func(plan auth.AuditRechainPlan) (bool, error) {
			printAuditRechainPlan(out, dataDir, plan)
			if assumeYes {
				// An unattended run has nobody to weigh the warning
				// the plan has just printed, so a log that does not
				// even agree with its own hashes is refused rather
				// than signed. An operator who has read it and still
				// judges the log sound can say so interactively.
				if !plan.LegacyChainOK {
					return false, errors.New(
						"the audit log does not verify under its own " +
							"unkeyed rules, so -confirm-rechain will not " +
							"sign it. Inspect the log, restore auth.db " +
							"from a known-good copy if you cannot account " +
							"for the difference, and re-chain " +
							"interactively if you judge it sound")
				}

				fmt.Fprintln(out,
					"Proceeding: -confirm-rechain was given.")
				return true, nil
			}

			return confirmAuditRechain(in, out)
		})
	if err != nil {
		return fmt.Errorf("failed to re-chain the audit log: %w", err)
	}
	if !result.Confirmed {
		fmt.Fprintln(out, "Aborted. Nothing has been changed.")
		return nil
	}

	fmt.Fprintf(out, "Audit log re-chained: %d event(s) re-hashed under the "+
		"server secret, and the re-chain itself recorded in the log.\n",
		result.Events)
	fmt.Fprintln(out, "Verify the log now with -verify-audit-log.")

	return nil
}

// auditRechainConfirmWord is what an interactive operator must type. It
// is a word rather than a single letter because the answer should cost
// a moment's thought; "y" is what one presses to make a prompt go away.
const auditRechainConfirmWord = "rechain"

// printAuditRechainPlan reports what the log holds and what the
// re-chain would do to it.
func printAuditRechainPlan(out io.Writer, dataDir string,
	plan auth.AuditRechainPlan) {

	fmt.Fprintf(out, "Audit log in %s:\n", dataDir)
	fmt.Fprintf(out, "  Events:              %d\n", plan.Events)
	fmt.Fprintf(out, "  Unkeyed (version 1): %d\n", plan.UnkeyedEvents)
	fmt.Fprintf(out, "  Oldest event:        %s\n", auditPlanTime(plan.Oldest))
	fmt.Fprintf(out, "  Newest event:        %s\n", auditPlanTime(plan.Newest))

	if plan.LegacyChainOK {
		fmt.Fprintln(out, "  Existing chain:      recomputes cleanly")
		fmt.Fprintln(out, "\nThat the existing chain recomputes is worth "+
			"little on its own: the version 1\nhash is unkeyed, so anyone "+
			"able to write auth.db could have produced a chain\nthat "+
			"recomputes just as cleanly. It rules out a careless edit, and "+
			"nothing more.")
	} else {
		fmt.Fprintf(out, "  Existing chain:      DOES NOT recompute "+
			"(first bad row %d)\n", plan.LegacyFirstBad)
		if plan.LegacyChainErr != nil {
			fmt.Fprintf(out, "                       %v\n",
				plan.LegacyChainErr)
		}
		fmt.Fprintln(out, "\nThe log does not agree with its own hashes. "+
			"Re-chaining would sign that log\nunder the server secret. "+
			"Restore auth.db from a known-good copy instead unless\nyou "+
			"know why it differs.")
	}

	fmt.Fprintf(out, "\nThis will re-hash all %d event(s) as keyed "+
		"version 2 rows, in one transaction,\nand record the re-chain in "+
		"the log. It attests the log exactly as it now stands:\nwhatever "+
		"this database currently says becomes what the keyed chain "+
		"vouches for.\n", plan.Events)
}

// auditPlanTime renders a timestamp for the plan, naming an empty log
// rather than printing a zero time at it.
func auditPlanTime(t time.Time) string {
	if t.IsZero() {
		return "(none)"
	}

	return t.UTC().Format(time.RFC3339)
}

// confirmAuditRechain asks the operator to type the confirmation word.
// Anything else, end of input included, declines; a non-interactive run
// with no -confirm-rechain therefore changes nothing rather than
// proceeding on silence.
func confirmAuditRechain(in io.Reader, out io.Writer) (bool, error) {
	fmt.Fprintf(out, "\nType %q to proceed, or anything else to abort: ",
		auditRechainConfirmWord)

	reader := bufio.NewReader(in)
	answer, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("failed to read the confirmation: %w", err)
	}

	return strings.TrimSpace(answer) == auditRechainConfirmWord, nil
}

// describeAuditVerifyFailure turns a VerifyAuditChain error into the
// message and exit status the operator should see.
func describeAuditVerifyFailure(rows int, firstBad int64, err error) error {
	if errors.Is(err, auth.ErrAuditKeyMismatch) {
		return &auditVerifyError{
			code: auditExitKeyMismatch,
			err: fmt.Errorf("audit log could not be verified with the key in "+
				"use, after %d event(s): %w\n"+
				"       Nothing in this log verified, which usually means "+
				"the server secret file is not the one the log was written "+
				"under; confirm secret_file before treating this as "+
				"tampering", rows, err),
		}
	}

	tampered := errors.Is(err, auth.ErrAuditChainBroken) ||
		errors.Is(err, auth.ErrAuditChainDowngraded) ||
		errors.Is(err, auth.ErrAuditUnkeyedRow)

	if firstBad != 0 {
		wrapped := fmt.Errorf(
			"audit log verification failed at row %d after %d event(s): %w",
			firstBad, rows, err)
		if tampered {
			return &auditVerifyError{code: auditExitTampered, err: wrapped}
		}
		return wrapped
	}

	wrapped := fmt.Errorf("failed to verify audit log: %w", err)
	if tampered {
		return &auditVerifyError{code: auditExitTampered, err: wrapped}
	}

	return wrapped
}
