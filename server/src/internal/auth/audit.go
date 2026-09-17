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

// auditTableDDL creates the append-only audit_events table, its
// lookup indexes and the trigger that rejects updates. It is executed
// by the fresh-install schema in initSchema, by the migrations that
// introduced the table, and again by ensureAuditSchema on every open,
// so every statement is idempotent. The unique index that keeps the
// chain linear lives in auditChainIndexDDL and is applied separately.
const auditTableDDL = `
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
        hash TEXT NOT NULL,
        hash_version INTEGER NOT NULL DEFAULT 1
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

// auditSchemaDDL is the whole audit schema, table and chain index
// together, for callers that have no reason to tell the two apart.
const auditSchemaDDL = auditTableDDL + auditChainIndexDDL

// auditChainIndexDDL creates the unique index that keeps the chain
// linear. It is separated from the rest of the schema only so that
// ensureAuditSchema can run it on its own and name it when it fails: it
// is the one statement here that can be refused by existing data,
// because a database written by a pre-merge build of this feature may
// already hold two events claiming the same predecessor.
const auditChainIndexDDL = `
    -- One row may follow any given row, and only one row may be the
    -- genesis row whose prev_hash is empty. Without this, two events
    -- written concurrently could both chain onto the same predecessor
    -- and fork the log into two branches that each verify cleanly; the
    -- single writer makes that unlikely, whereas this makes it
    -- impossible.
    CREATE UNIQUE INDEX IF NOT EXISTS idx_audit_prev_hash
        ON audit_events(prev_hash);
`

// auditChainIndexName and auditNoUpdateTrigger name the two schema
// objects the chain's guarantees rest on. ensureAuditSchema creates them
// on every open and VerifyAuditChain refuses to pass a log they are
// missing from, because a database in which either has been dropped
// accepts a forked chain or a rewritten row without complaint.
const (
	auditChainIndexName  = "idx_audit_prev_hash"
	auditNoUpdateTrigger = "audit_events_no_update"
)

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
	// HashVersion names the rendering Hash was computed under, so that a
	// row written before a change to the format is verified under the
	// rule it was written with rather than reported as broken.
	HashVersion int `json:"hash_version"`
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

// auditHashVersion is the rendering every new row is hashed under, and
// is stored beside the hash in hash_version. Bump it alongside any change
// to what auditHash covers or how it renders it, add the new rendering
// as a case in auditHash, and keep the old case: a row is verified under
// the version it carries, so older rows keep verifying after the change
// and a row claiming a version this build does not know is reported as
// such rather than as a broken hash.
const auditHashVersion = 1

// auditHashV1Label is the first field of the version 1 rendering.
const auditHashV1Label = "v1"

// errUnknownAuditHashVersion is returned when a row names a hash
// version this build has no rendering for.
var errUnknownAuditHashVersion = errors.New("unknown audit hash version")

// auditIDString renders a nullable identifier for the hash input:
// the decimal value, or the empty string when unset.
func auditIDString(id *int64) string {
	if id == nil {
		return ""
	}
	return strconv.FormatInt(*id, 10)
}

// auditHash computes the hex SHA-256 over the canonical rendering of
// the event under the version named by ev.HashVersion, which begins
// with the previous row's hash and so chains each row to its
// predecessor. A version with no rendering here is an error, so that a
// verifier tells "written under a format this build predates" apart
// from "the hash does not match".
func auditHash(ev *AuditEvent) (string, error) {
	switch ev.HashVersion {
	case 1:
		return auditHashV1(ev), nil
	default:
		return "", fmt.Errorf("%w %d", errUnknownAuditHashVersion,
			ev.HashVersion)
	}
}

// auditHashV1 is the version 1 rendering.
//
// Each field is rendered as its byte length, a colon and then the field
// itself, rather than being joined by a separator. Any separator that
// can occur inside a field makes the rendering ambiguous: joined with a
// bare "|", an actor_name of "alice|198.51.100.9" alongside an empty
// actor_ip renders identically to those two values in their proper
// columns, so one event could be made to stand for another without
// disturbing its hash. A length prefix cannot collide that way, because
// the reader never has to guess where a field ends.
//
// The rendering opens with auditHashV1Label so that no other version
// can produce the same digest over the same fields.
func auditHashV1(ev *AuditEvent) string {
	fields := []string{
		auditHashV1Label,
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
	}

	var canonical strings.Builder
	for _, field := range fields {
		canonical.WriteString(strconv.Itoa(len(field)))
		canonical.WriteByte(':')
		canonical.WriteString(field)
	}

	sum := sha256.Sum256([]byte(canonical.String()))
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
	ev.HashVersion = auditHashVersion
	hash, err := auditHash(ev)
	if err != nil {
		return fmt.Errorf("failed to hash audit event: %w", err)
	}
	ev.Hash = hash

	result, err := tx.Exec(`
        INSERT INTO audit_events (
            occurred_at, actor_type, actor_id, actor_name, actor_ip,
            action, target_type, target_id, target_name, outcome,
            error, details, prev_hash, hash, hash_version
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
		ev.HashVersion,
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

// auditTarget carries the action and target of an in-progress mutation
// so that the deferred rollback path can record a failure event for it.
// It is nil until the target is known; a mutation that fails before
// then records nothing, because there would be nothing to attribute the
// failure to.
type auditTarget struct {
	action     string
	targetType string
	targetID   *int64
	targetName string
}

// failAudit rolls the mutation's transaction back and then records a
// failure event for it. The order matters: recordFailure opens a
// transaction of its own, and SQLite allows a single writer, so the
// rollback must happen first or the failure event is lost to the busy
// timeout. The caller must hold s.mu, which recordFailure expects.
func (s *AuthStore) failAudit(tx *sql.Tx, actor Actor, target *auditTarget,
	cause error) {

	//nolint:errcheck // Rollback error is not critical; the outer error
	// is already being returned to the caller.
	tx.Rollback()

	if target != nil {
		s.recordFailure(actor, target.action, target.targetType,
			target.targetID, target.targetName, cause)
	}
}

// RecordDenied records an authorisation denial. The event carries no
// target, because a request refused before the target is resolved has
// none, and the reason is stored in the error column.
func (s *AuthStore) RecordDenied(actor Actor, action, reason string) error {
	return s.RecordDeniedWithDetails(actor, action, reason, nil)
}

// RecordDeniedWithDetails records an authorisation denial along with
// structured details. Callers that coalesce repeated denials use it to
// report how many identical refusals a single row stands for; details
// of nil is identical to RecordDenied.
func (s *AuthStore) RecordDeniedWithDetails(actor Actor, action,
	reason string, details any) error {

	s.mu.Lock()
	defer s.mu.Unlock()

	ev := newEvent(actor, action, "", nil, "", details)
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
		&ev.Hash, &ev.HashVersion); err != nil {
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
    error, details, prev_hash, hash, hash_version`

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

	if err := s.verifyAuditSchema(); err != nil {
		return 0, 0, err
	}

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
		want, err := auditHash(&ev)
		if err != nil {
			return count, ev.ID, fmt.Errorf(
				"audit row %d was written under hash version %d, which this "+
					"build cannot verify: %w", ev.ID, ev.HashVersion, err)
		}
		if want != ev.Hash {
			return count, ev.ID, fmt.Errorf("audit chain broken at row %d", ev.ID)
		}

		prevHash = ev.Hash
		first = false
	}
	if err := rows.Err(); err != nil {
		return count, 0, fmt.Errorf("failed to read audit events: %w", err)
	}

	if err := s.verifyAuditTail(); err != nil {
		return count, 0, err
	}

	return count, 0, nil
}

// verifyAuditSchema checks that the two schema objects the chain's
// guarantees depend on are present: the unique index on prev_hash,
// without which two rows may claim the same predecessor and fork the
// log, and the BEFORE UPDATE trigger, without which a row can be
// rewritten in place. ensureAuditSchema re-creates both on every open,
// so their absence from a live database means they were dropped since,
// and a chain that recomputes cleanly under those conditions has proved
// nothing.
func (s *AuthStore) verifyAuditSchema() error {
	var unique int
	err := s.db.QueryRow(`SELECT "unique" FROM pragma_index_list('audit_events')
         WHERE name = ?`, auditChainIndexName).Scan(&unique)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("audit chain unprotected: the unique index %s is "+
			"missing from audit_events, so the chain may have forked",
			auditChainIndexName)
	case err != nil:
		return fmt.Errorf("failed to look for index %s: %w",
			auditChainIndexName, err)
	case unique == 0:
		return fmt.Errorf("audit chain unprotected: index %s on "+
			"audit_events is not unique, so the chain may have forked",
			auditChainIndexName)
	}

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master
         WHERE type = 'trigger' AND name = ? AND tbl_name = 'audit_events'`,
		auditNoUpdateTrigger).Scan(&count); err != nil {
		return fmt.Errorf("failed to look for trigger %s: %w",
			auditNoUpdateTrigger, err)
	}
	if count == 0 {
		return fmt.Errorf("audit chain unprotected: the trigger %s is "+
			"missing from audit_events, so rows may have been rewritten "+
			"in place", auditNoUpdateTrigger)
	}

	return nil
}

// verifyAuditTail detects rows removed from the newest end of the log,
// which the hash chain alone cannot see: truncating the tail leaves
// every remaining row linked to its predecessor and so verifying
// cleanly. Because audit_events.id is AUTOINCREMENT, SQLite keeps the
// highest id ever issued in sqlite_sequence and never reuses it, so the
// sequence and the largest surviving id should agree, and any
// disagreement in either direction is a fact SQLite would not have
// produced on its own:
//
//   - A sequence above the newest row means rows once existed beyond it.
//   - A sequence below the newest row is impossible from ordinary
//     inserts, since SQLite raises the sequence before it writes the
//     row, so it means the sequence was written down to disguise a
//     truncation and overshot, or was rolled back by hand.
//   - A missing or NULL sequence value whilst rows remain means the row
//     was deleted, because SQLite writes it with the first insert and
//     never removes it.
//   - No sqlite_sequence table at all is reported too. Every table this
//     store creates with an id (users, tokens, groups and the rest, not
//     only audit_events) is INTEGER PRIMARY KEY AUTOINCREMENT, so
//     SQLite has created sqlite_sequence in every database this server
//     has ever made; removing it takes PRAGMA writable_schema and
//     breaks the server's own inserts, so it is evidence, not a
//     database that predates the column.
//
// Any other query error is returned, because a check that could not run
// has not passed.
//
// The newest id and the sequence are read by one statement, so that
// they come from a single consistent snapshot. That matters because
// s.mu excludes only writers in this process: the command line opens
// its own store on the same file, and a server insert landing between
// two separate reads would leave the sequence one ahead of the newest
// id and a healthy log reported as truncated. A statement is the unit
// SQLite makes consistent, so no transaction is needed, which is as
// well, since the store opens SQLite with _txlock=immediate and a
// read-only check in a transaction would contend with the server for
// the write lock.
//
// What this is and is not. The check raises the cost of a careless
// deletion from one statement to three, and it catches the deletions an
// operator or a script makes without meaning to hide anything. It is
// no protection against someone who can write to auth.db. The chain is
// an unkeyed SHA-256, so anyone who can write the file can recompute
// every hash after a change and leave a log that verifies cleanly, and
// they can set sqlite_sequence to whatever value makes the arithmetic
// agree. Treat a failed verification as evidence of tampering, never a
// successful one as proof of its absence. Making the log resistant to a
// deliberate attacker needs a key the server does not hold, or an
// anchor outside the file, neither of which this does.
func (s *AuthStore) verifyAuditTail() error {
	present, err := s.sequenceTablePresent()
	if err != nil {
		return err
	}
	if !present {
		return errors.New("audit chain tail unverifiable: the sqlite_sequence " +
			"table is missing; every database this server creates holds one, " +
			"because its tables use AUTOINCREMENT, so it has been removed")
	}

	var newest, seq sql.NullInt64
	if err := s.db.QueryRow(`
        SELECT (SELECT MAX(id) FROM audit_events),
               (SELECT seq FROM sqlite_sequence WHERE name = 'audit_events')`).
		Scan(&newest, &seq); err != nil {
		return fmt.Errorf("failed to read newest audit row and sequence: %w", err)
	}

	return checkAuditTail(newest.Int64, seq)
}

// checkAuditTail is the comparison behind verifyAuditTail, separated so
// that each disagreement can be tested directly. newest is MAX(id), or
// zero for an empty log; seq is the sqlite_sequence value, invalid when
// the row is absent or its value NULL.
func checkAuditTail(newest int64, seq sql.NullInt64) error {
	if !seq.Valid {
		// SQLite creates this row with the first insert and never
		// removes it, so its absence alongside surviving rows means it
		// was deleted, or its value blanked, by hand.
		if newest > 0 {
			return fmt.Errorf(
				"audit chain tail missing: the sqlite_sequence row for "+
					"audit_events has been removed or emptied whilst %d "+
					"event(s) remain", newest)
		}
		return nil
	}

	switch {
	case seq.Int64 > newest:
		return fmt.Errorf("audit chain tail missing: newest row %d, sequence %d",
			newest, seq.Int64)
	case seq.Int64 < newest:
		return fmt.Errorf(
			"audit sequence inconsistent: newest row %d, sequence %d; "+
				"SQLite raises the sequence before it writes the row, so "+
				"it cannot fall behind on its own",
			newest, seq.Int64)
	}

	return nil
}

// sequenceTablePresent reports whether sqlite_sequence exists at all.
// SQLite creates it with the first AUTOINCREMENT table, which for this
// store is the users table in the fresh-install schema.
func (s *AuthStore) sequenceTablePresent() (bool, error) {
	var count int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master
         WHERE type = 'table' AND name = 'sqlite_sequence'`).
		Scan(&count); err != nil {
		return false, fmt.Errorf("failed to look for sqlite_sequence: %w", err)
	}

	return count > 0, nil
}

// auditActionPurge is the action recorded when the retention purge
// removes events, so that a shrinking log is itself accounted for.
const auditActionPurge = "audit.purge"

// PurgeAuditEvents deletes events that occurred before the given time
// and returns the number of rows removed. Because each retained row
// still carries its own prev_hash, the chain stays verifiable from the
// oldest retained row onwards.
//
// A purge that removed anything records an audit.purge event of its
// own, in the same transaction as the DELETE, so that the log always
// explains its own missing prefix: an operator comparing the oldest
// retained row against the retention window can tell a purge from a
// deletion. The event is written by the system actor and carries no
// target, because retention acts on the log as a whole.
func (s *AuthStore) PurgeAuditEvents(olderThan time.Time) (removed int64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin purge transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			//nolint:errcheck // Rollback error is not critical; the
			// outer error is already being returned.
			tx.Rollback()
		}
	}()

	result, err := tx.Exec("DELETE FROM audit_events WHERE occurred_at < ?",
		olderThan.UTC().Format(auditTimeLayout))
	if err != nil {
		return 0, fmt.Errorf("failed to purge audit events: %w", err)
	}

	removed, err = result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to count purged audit events: %w", err)
	}

	if removed > 0 {
		ev := newEvent(systemActor, auditActionPurge, "", nil, "",
			map[string]any{
				"older_than": olderThan.UTC().Format(time.RFC3339),
				"removed":    removed,
			})
		if err = s.recordAudit(tx, ev); err != nil {
			return 0, fmt.Errorf("failed to record audit purge event: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit audit purge: %w", err)
	}
	committed = true

	return removed, nil
}
