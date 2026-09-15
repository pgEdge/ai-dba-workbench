/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package auth

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// auditSchemaDDL creates the append-only audit_events table, its
// indexes and the trigger that rejects updates. It is executed both by
// the fresh-install schema in initSchema and by migrateV3ToV4, so every
// statement is idempotent.
const auditSchemaDDL = `
    -- Audit log of administrative changes
    CREATE TABLE IF NOT EXISTS audit_events (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        occurred_at TEXT NOT NULL,
        actor_type TEXT NOT NULL CHECK (actor_type IN ('user','token','cli','system')),
        actor_id INTEGER,
        actor_name TEXT NOT NULL,
        actor_ip TEXT,
        action TEXT NOT NULL,
        target_type TEXT,
        target_id INTEGER,
        target_name TEXT,
        outcome TEXT NOT NULL CHECK (outcome IN ('success','failure','denied')),
        error TEXT,
        details TEXT,
        prev_hash TEXT NOT NULL,
        hash TEXT NOT NULL
    );
    CREATE INDEX IF NOT EXISTS idx_audit_occurred ON audit_events(occurred_at);
    CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_events(actor_name);
    CREATE INDEX IF NOT EXISTS idx_audit_target ON audit_events(target_type, target_id);
    CREATE TRIGGER IF NOT EXISTS audit_events_no_update
    BEFORE UPDATE ON audit_events
    BEGIN
        SELECT RAISE(ABORT, 'audit_events is append-only');
    END;
`

// auditTimeLayout is the on-disk format for occurred_at. It is
// RFC 3339 with a fixed nine-digit fraction, rather than
// time.RFC3339Nano, because RFC3339Nano strips trailing zeros from the
// fraction and so produces variable-length strings: "T10:00:00Z" and
// "T10:00:00.4Z" then compare in the wrong order, since '.' sorts
// before 'Z'. Every timestamp comparison in this file is a string
// comparison against stored text, so the width must be constant.
const auditTimeLayout = "2006-01-02T15:04:05.000000000Z07:00"

const (
	// defaultAuditLimit is the page size used when a caller does not
	// ask for one.
	defaultAuditLimit = 50

	// maxAuditLimit is the largest page an audit query may return; a
	// larger request is silently capped so that a single call cannot
	// pull the whole log into memory.
	maxAuditLimit = 500
)

// ActorType identifies the kind of principal that caused an audited
// change.
type ActorType string

const (
	// ActorUser is an interactive user authenticated by session.
	ActorUser ActorType = "user"
	// ActorToken is a service-account or API token.
	ActorToken ActorType = "token"
	// ActorCLI is an operator running the server command line.
	ActorCLI ActorType = "cli"
	// ActorSystem is the server acting on its own behalf, and the
	// fallback for a store method called without an actor.
	ActorSystem ActorType = "system"
)

// Actor describes the principal responsible for an audited change. ID
// is nil, and IP empty, for the cli and system actor types.
type Actor struct {
	Type ActorType
	ID   *int64
	Name string
	IP   string
}

// systemActor attributes an event to the server itself. Store methods
// called without an explicit actor record events as this actor, so a
// missing actor degrades attribution but never loses the event.
var systemActor = Actor{Type: ActorSystem, Name: "system"}

// AuditOutcome records whether the audited operation succeeded, failed
// or was refused by an authorisation check.
type AuditOutcome string

const (
	// OutcomeSuccess marks a change that was applied.
	OutcomeSuccess AuditOutcome = "success"
	// OutcomeFailure marks a change that was attempted and errored.
	OutcomeFailure AuditOutcome = "failure"
	// OutcomeDenied marks a change refused by an authorisation check.
	OutcomeDenied AuditOutcome = "denied"
)

// AuditEvent is one row of the audit log.
type AuditEvent struct {
	ID         int64           `json:"id"`
	OccurredAt time.Time       `json:"occurred_at"`
	ActorType  ActorType       `json:"actor_type"`
	ActorID    *int64          `json:"actor_id"`
	ActorName  string          `json:"actor_name"`
	ActorIP    string          `json:"actor_ip,omitempty"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type,omitempty"`
	TargetID   *int64          `json:"target_id"`
	TargetName string          `json:"target_name,omitempty"`
	Outcome    AuditOutcome    `json:"outcome"`
	Error      string          `json:"error,omitempty"`
	Details    json.RawMessage `json:"details,omitempty"`
	PrevHash   string          `json:"prev_hash"`
	Hash       string          `json:"hash"`
}

// AuditFilter narrows an audit-log query. Every field is optional; the
// set fields are combined with AND. Limit defaults to defaultAuditLimit
// and is capped at maxAuditLimit.
type AuditFilter struct {
	ActorName  string
	ActorType  string
	Action     string
	TargetType string
	TargetID   *int64
	Outcome    string
	Since      *time.Time
	Until      *time.Time
	Limit      int
	Offset     int
}

// ActorStore is a thin view of an AuthStore that carries the actor
// responsible for the changes made through it. Mutating methods exist
// on both *AuthStore and *ActorStore with identical signatures; the
// *AuthStore variants attribute their events to systemActor.
type ActorStore struct {
	s     *AuthStore
	actor Actor
}

// AsActor returns a view of the store that attributes every audited
// change to the given actor.
func (s *AuthStore) AsActor(a Actor) *ActorStore {
	return &ActorStore{s: s, actor: a}
}

// newEvent builds a successful audit event for the given actor and
// target. Details, when not nil, is encoded as JSON; a value that
// cannot be encoded is logged and dropped rather than losing the event
// itself.
func newEvent(actor Actor, action, targetType string, targetID *int64,
	targetName string, details any) *AuditEvent {

	ev := &AuditEvent{
		ActorType:  actor.Type,
		ActorID:    actor.ID,
		ActorName:  actor.Name,
		ActorIP:    actor.IP,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		TargetName: targetName,
		Outcome:    OutcomeSuccess,
	}

	if details != nil {
		encoded, err := json.Marshal(details)
		if err != nil {
			log.Printf("[ERROR] Failed to marshal audit details for action %s: %v",
				action, err)
		} else {
			ev.Details = encoded
		}
	}

	return ev
}

// auditIDString renders a nullable identifier for the hash input:
// the decimal value, or the empty string when unset.
func auditIDString(id *int64) string {
	if id == nil {
		return ""
	}
	return strconv.FormatInt(*id, 10)
}

// auditHash computes the hex SHA-256 over the canonical rendering of
// the event, which begins with the previous row's hash and so chains
// each row to its predecessor.
func auditHash(ev *AuditEvent) string {
	canonical := strings.Join([]string{
		ev.PrevHash,
		ev.OccurredAt.UTC().Format(auditTimeLayout),
		string(ev.ActorType),
		auditIDString(ev.ActorID),
		ev.ActorName,
		ev.ActorIP,
		ev.Action,
		ev.TargetType,
		auditIDString(ev.TargetID),
		ev.TargetName,
		string(ev.Outcome),
		ev.Error,
		string(ev.Details),
	}, "|")

	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// nullableText returns nil for an empty string so that optional columns
// hold NULL rather than an empty string.
func nullableText(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// recordAudit appends an event to the audit log inside the caller's
// transaction, so that an event is neither lost nor written without the
// change it records. The caller must hold s.mu for writing: the chain
// is linear only because the read of the last hash and the insert of
// the new row happen under a single writer. A zero OccurredAt is set to
// the current UTC time; a non-zero one is kept as given. The timestamp
// is stored in auditTimeLayout, a fixed-width RFC 3339 UTC rendering
// that sorts lexically in timestamp order. On success the event's ID,
// PrevHash, OccurredAt and Hash fields are populated.
func (s *AuthStore) recordAudit(tx *sql.Tx, ev *AuditEvent) error {
	if ev == nil {
		return errors.New("audit event is nil")
	}

	var prevHash string
	err := tx.QueryRow(
		"SELECT hash FROM audit_events ORDER BY id DESC LIMIT 1").Scan(&prevHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to read previous audit hash: %w", err)
	}

	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	if ev.Outcome == "" {
		ev.Outcome = OutcomeSuccess
	}
	if ev.ActorType == "" {
		ev.ActorType = systemActor.Type
		if ev.ActorName == "" {
			ev.ActorName = systemActor.Name
		}
	}
	ev.PrevHash = prevHash
	ev.Hash = auditHash(ev)

	result, err := tx.Exec(`
        INSERT INTO audit_events (
            occurred_at, actor_type, actor_id, actor_name, actor_ip,
            action, target_type, target_id, target_name, outcome,
            error, details, prev_hash, hash
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.OccurredAt.UTC().Format(auditTimeLayout),
		string(ev.ActorType),
		ev.ActorID,
		ev.ActorName,
		nullableText(ev.ActorIP),
		ev.Action,
		nullableText(ev.TargetType),
		ev.TargetID,
		nullableText(ev.TargetName),
		string(ev.Outcome),
		nullableText(ev.Error),
		nullableText(string(ev.Details)),
		ev.PrevHash,
		ev.Hash,
	)
	if err != nil {
		return fmt.Errorf("failed to insert audit event: %w", err)
	}

	if id, err := result.LastInsertId(); err == nil {
		ev.ID = id
	}

	return nil
}

// recordFailure records a failed mutation in its own short transaction,
// because the transaction carrying the mutation has been rolled back by
// the time the failure is known. The caller must hold s.mu, so this
// helper takes no lock of its own, and must already have committed or
// rolled back its own transaction: SQLite allows one writer at a time,
// so a still-open write transaction leaves this second one blocked for
// the connection's five-second busy timeout and the event is then lost. Errors are logged rather than
// returned: an audit failure must never mask the mutation error the
// caller is about to report.
func (s *AuthStore) recordFailure(actor Actor, action, targetType string,
	targetID *int64, targetName string, cause error) {

	ev := newEvent(actor, action, targetType, targetID, targetName, nil)
	ev.Outcome = OutcomeFailure
	if cause != nil {
		ev.Error = cause.Error()
	} else {
		ev.Error = "unknown error"
	}

	if err := s.recordAuditInOwnTx(ev); err != nil {
		log.Printf("[ERROR] Failed to record audit failure event: %v", err)
	}
}

// RecordDenied records an authorisation denial. The event carries no
// target, because a request refused before the target is resolved has
// none, and the reason is stored in the error column.
func (s *AuthStore) RecordDenied(actor Actor, action, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ev := newEvent(actor, action, "", nil, "", nil)
	ev.Outcome = OutcomeDenied
	ev.Error = reason

	if err := s.recordAuditInOwnTx(ev); err != nil {
		return fmt.Errorf("failed to record denial: %w", err)
	}

	return nil
}

// recordAuditInOwnTx wraps recordAudit in a transaction of its own. The
// caller must already hold s.mu.
func (s *AuthStore) recordAuditInOwnTx(ev *AuditEvent) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin audit transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			//nolint:errcheck // Rollback error is not critical; the
			// outer error is already being returned.
			tx.Rollback()
		}
	}()

	if err := s.recordAudit(tx, ev); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit audit event: %w", err)
	}
	committed = true

	return nil
}

// effectiveAuditLimit applies the default and the cap to a requested
// page size.
func effectiveAuditLimit(limit int) int {
	if limit <= 0 {
		return defaultAuditLimit
	}
	if limit > maxAuditLimit {
		return maxAuditLimit
	}
	return limit
}

// auditWhere builds the WHERE clause and its bound arguments for a
// filter. Only bound parameters are used; no filter value ever reaches
// the SQL text.
func auditWhere(f AuditFilter) (string, []any) {
	var clauses []string
	var args []any

	if f.ActorName != "" {
		clauses = append(clauses, "actor_name = ?")
		args = append(args, f.ActorName)
	}
	if f.ActorType != "" {
		clauses = append(clauses, "actor_type = ?")
		args = append(args, f.ActorType)
	}
	if f.Action != "" {
		clauses = append(clauses, "action = ?")
		args = append(args, f.Action)
	}
	if f.TargetType != "" {
		clauses = append(clauses, "target_type = ?")
		args = append(args, f.TargetType)
	}
	if f.TargetID != nil {
		clauses = append(clauses, "target_id = ?")
		args = append(args, *f.TargetID)
	}
	if f.Outcome != "" {
		clauses = append(clauses, "outcome = ?")
		args = append(args, f.Outcome)
	}
	if f.Since != nil {
		clauses = append(clauses, "occurred_at >= ?")
		args = append(args, f.Since.UTC().Format(auditTimeLayout))
	}
	if f.Until != nil {
		clauses = append(clauses, "occurred_at <= ?")
		args = append(args, f.Until.UTC().Format(auditTimeLayout))
	}

	if len(clauses) == 0 {
		return "", args
	}

	return " WHERE " + strings.Join(clauses, " AND "), args
}

// scanAuditEvent reads one audit row from a *sql.Rows or *sql.Row.
func scanAuditEvent(scan func(dest ...any) error) (AuditEvent, error) {
	var (
		ev         AuditEvent
		occurredAt string
		actorIP    sql.NullString
		targetType sql.NullString
		targetName sql.NullString
		errText    sql.NullString
		details    sql.NullString
	)

	if err := scan(&ev.ID, &occurredAt, &ev.ActorType, &ev.ActorID,
		&ev.ActorName, &actorIP, &ev.Action, &targetType, &ev.TargetID,
		&targetName, &ev.Outcome, &errText, &details, &ev.PrevHash,
		&ev.Hash); err != nil {
		return ev, err
	}

	// RFC3339Nano parses any fraction width, so it reads back both the
	// fixed-width text this package writes and any hand-written row.
	parsed, err := time.Parse(time.RFC3339Nano, occurredAt)
	if err != nil {
		return ev, fmt.Errorf("invalid occurred_at %q on audit row %d: %w",
			occurredAt, ev.ID, err)
	}
	ev.OccurredAt = parsed
	ev.ActorIP = actorIP.String
	ev.TargetType = targetType.String
	ev.TargetName = targetName.String
	ev.Error = errText.String
	if details.Valid && details.String != "" {
		ev.Details = json.RawMessage(details.String)
	}

	return ev, nil
}

// auditColumns lists the audit_events columns in the order
// scanAuditEvent expects them.
const auditColumns = `id, occurred_at, actor_type, actor_id, actor_name,
    actor_ip, action, target_type, target_id, target_name, outcome,
    error, details, prev_hash, hash`

// ListAuditEvents returns a page of audit events, newest first, along
// with the total number of events matching the filter.
func (s *AuthStore) ListAuditEvents(f AuditFilter) ([]AuditEvent, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	where, args := auditWhere(f)

	var total int
	if err := s.db.QueryRow(
		"SELECT COUNT(*) FROM audit_events"+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count audit events: %w", err)
	}

	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	pageArgs := append(append([]any{}, args...),
		effectiveAuditLimit(f.Limit), offset)

	//nolint:gosec // G202: the concatenated fragments are compile-time
	// constants (auditColumns) and a WHERE clause built solely from
	// fixed literals by auditWhere; every filter value is bound.
	rows, err := s.db.Query("SELECT "+auditColumns+" FROM audit_events"+where+
		" ORDER BY id DESC LIMIT ? OFFSET ?", pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query audit events: %w", err)
	}
	defer rows.Close()

	events := []AuditEvent{}
	for rows.Next() {
		ev, err := scanAuditEvent(rows.Scan)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan audit event: %w", err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("failed to read audit events: %w", err)
	}

	return events, total, nil
}

// VerifyAuditChain recomputes every row's hash and checks that each row
// links to its predecessor, returning the number of rows examined and
// the id of the first row that fails. The first remaining row's
// prev_hash is accepted as given, because the retention purge may have
// removed the row it points at. Callers must check the error first: a
// firstBad of 0 means the chain is intact only when err is nil, because
// a scan or iteration failure also reports firstBad 0.
func (s *AuthStore) VerifyAuditChain() (int, int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT " + auditColumns +
		" FROM audit_events ORDER BY id")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to query audit events: %w", err)
	}
	defer rows.Close()

	count := 0
	prevHash := ""
	first := true

	for rows.Next() {
		ev, err := scanAuditEvent(rows.Scan)
		if err != nil {
			return count, 0, fmt.Errorf("failed to scan audit event: %w", err)
		}
		count++

		if !first && ev.PrevHash != prevHash {
			return count, ev.ID, fmt.Errorf("audit chain broken at row %d", ev.ID)
		}
		if auditHash(&ev) != ev.Hash {
			return count, ev.ID, fmt.Errorf("audit chain broken at row %d", ev.ID)
		}

		prevHash = ev.Hash
		first = false
	}
	if err := rows.Err(); err != nil {
		return count, 0, fmt.Errorf("failed to read audit events: %w", err)
	}

	return count, 0, nil
}

// PurgeAuditEvents deletes events that occurred before the given time
// and returns the number of rows removed. Because each retained row
// still carries its own prev_hash, the chain stays verifiable from the
// oldest retained row onwards.
func (s *AuthStore) PurgeAuditEvents(olderThan time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM audit_events WHERE occurred_at < ?",
		olderThan.UTC().Format(auditTimeLayout))
	if err != nil {
		return 0, fmt.Errorf("failed to purge audit events: %w", err)
	}

	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to count purged audit events: %w", err)
	}

	return removed, nil
}
