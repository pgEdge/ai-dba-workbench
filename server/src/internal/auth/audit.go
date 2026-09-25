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
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/pkg/crypto"
)

// auditTableDDL creates the append-only audit_events table, its
// lookup indexes and the trigger that rejects updates. It is executed
// by the fresh-install schema in initSchema, by the migrations that
// introduced the table, and again by ensureAuditSchema on every open,
// so every statement is idempotent. The unique index that keeps the
// chain linear lives in auditChainIndexDDL and is applied separately.
const auditTableDDL = auditTableOnlyDDL + auditNoUpdateTriggerDDL

// auditTableOnlyDDL is the table and its lookup indexes, without the
// append-only trigger.
const auditTableOnlyDDL = `
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
`

// auditNoUpdateTriggerDDL creates the trigger that makes audit_events
// append-only. It is a constant of its own because rechainAuditLogTx
// drops it for the length of one transaction and must then re-create
// exactly the trigger it removed, rather than an approximation of it.
const auditNoUpdateTriggerDDL = `
    CREATE TRIGGER IF NOT EXISTS audit_events_no_update
    BEFORE UPDATE ON audit_events
    BEGIN
        SELECT RAISE(ABORT, 'audit_events is append-only');
    END;
`

// auditSchemaDDL is the whole audit schema, table, trigger and chain
// index together, for callers that have no reason to tell them apart.
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
//
// Version 2 replaced the unkeyed SHA-256 of version 1 with an HMAC over
// the same fields, keyed by a key derived from the server secret, so
// that rewriting the log needs the secret as well as write access to
// auth.db. Version 1 has no live rows anywhere: a database that still
// holds any refuses to open until an operator re-hashes the whole log
// under the key with RechainAuditLog, so the unkeyed rendering survives
// only as something the re-chain recomputes on its way past, and
// auditHash refuses it outright.
const auditHashVersion = 2

const (
	// auditHashV1Label is the first field of the version 1 rendering.
	auditHashV1Label = "v1"

	// auditHashV2Label is the first field of the version 2 rendering.
	// It differs from auditHashV1Label so that no two versions can
	// produce the same digest over the same fields even where the
	// digest function is otherwise the same primitive.
	auditHashV2Label = "v2"
)

// auditHashKeySalt is the fixed PBKDF2 salt separating the audit chain
// key from every other key derived from the same server secret, most
// importantly the one that encrypts stored database passwords and the
// one that seals OIDC login state. It is a constant rather than a
// random value because the key has to be reproducible across restarts,
// across the server and the command line, and across two servers
// sharing one secret; the salt is not a secret, the server secret is.
// Never reuse a salt from another subsystem here, and never change this
// string: doing so makes every row already written unverifiable.
const auditHashKeySalt = "pgedge-ai-workbench/audit-hash-chain/v1"

// errUnknownAuditHashVersion is returned when a row names a hash
// version this build has no rendering for.
var errUnknownAuditHashVersion = errors.New("unknown audit hash version")

// errNoAuditKey is returned when a keyed rendering is asked for without
// a key. It is a failure rather than a fallback to an unkeyed digest:
// a hash computed under an empty key would verify for anyone, which is
// precisely the property version 2 exists to remove.
var errNoAuditKey = errors.New(
	"no audit hash key: the store was opened without one, so the audit " +
		"chain cannot be extended or verified")

// The sentinels below separate the ways verification can fail, because
// an operator's next move differs completely between them: a broken
// chain is an incident, whilst a key mismatch is usually a mislaid or
// rotated secret file. Reporting the second as the first both sends
// people hunting for an attack that has not happened and, worse, trains
// them to dismiss the real thing as "probably the key again".
var (
	// ErrAuditChainBroken reports a row whose hash or link does not
	// match what the chain says it should be.
	ErrAuditChainBroken = errors.New("audit chain broken")

	// ErrAuditChainDowngraded reports a row claiming a lower hash
	// version than a row before it, which no honest writer produces:
	// the version only ever rises.
	ErrAuditChainDowngraded = errors.New("audit chain downgraded")

	// ErrAuditUnkeyedRow reports a row claiming the unkeyed version 1
	// hash. The re-chain leaves none behind, and nothing this build
	// writes produces one, so a row carrying it is always either a
	// database that has not been re-chained yet or a row someone wrote
	// without the key. It is returned both by the refusal to open such
	// a database and by auditHash, which will not compute the unkeyed
	// rendering for a live row at all.
	ErrAuditUnkeyedRow = errors.New("unkeyed audit row")

	// ErrAuditKeyMismatch reports a log nothing in which verifies under
	// this key, which is far more often a secret that has been rotated,
	// moved or never created than a log rewritten from its first row.
	ErrAuditKeyMismatch = errors.New("audit chain key mismatch")
)

// DeriveAuditKey derives the audit chain key from the server secret.
// It is exported because both the server and the command line open the
// same auth.db and must arrive at the same key; a command that wrote a
// row under a different key would break the chain for the server.
//
// An empty secret yields nil, which every caller must treat as a
// failure rather than as a usable key.
func DeriveAuditKey(serverSecret string) []byte {
	return crypto.DeriveKey(serverSecret, []byte(auditHashKeySalt))
}

// AuditKeyForTesting returns a fixed, deliberately non-secret audit
// chain key for tests, which have no server secret to derive one from.
// It skips the PBKDF2 work DeriveAuditKey does, because a test suite
// that opened a hundred stores would otherwise pay for a hundred
// key derivations to no purpose. It must never be used outside tests,
// and the value is a constant precisely so that a key leaking into a
// real deployment is obvious in the source rather than plausible.
//
// It cannot live in an in-package export_test.go, because its callers
// are spread across the api, tools, llmproxy and cmd/mcp-server test
// binaries, so it links into ai-dba-server like any other exported
// function. The testing.Testing guard is what keeps it from being
// reachable there: outside a test binary it panics rather than handing
// a published constant to something that would then key a real audit
// chain with it.
func AuditKeyForTesting() []byte {
	if !testing.Testing() {
		panic("auth.AuditKeyForTesting was called outside a test binary: " +
			"it returns a published constant, and an audit chain keyed " +
			"with it could be rewritten by anyone")
	}

	return []byte("pgedge-ai-workbench test audit key, not a secret!")
}

// SeedAuditEventForTesting appends one keyed audit event stamped at, as
// the store would have written it then, so that a test outside this
// package can exercise the retention purge on rows old enough to go.
// It is exported, and guarded, for the reasons given for
// SeedUnkeyedAuditLogForTesting below.
func SeedAuditEventForTesting(s *AuthStore, at time.Time) error {
	if !testing.Testing() {
		panic("auth.SeedAuditEventForTesting was called outside a test " +
			"binary: it writes audit rows with a chosen timestamp")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ev := newEvent(systemActor, "test.seed", "", nil, "", nil)
	ev.OccurredAt = at.UTC()

	return s.recordAuditInOwnTx(ev)
}

// SeedUnkeyedAuditLogForTesting appends rows audit events hashed under
// the unkeyed version 1 rendering, stamped one minute apart from at,
// and then deletes the row with id breakAt when breakAt is not zero, so
// that the row after it no longer links to the row before it.
//
// It exists because nothing in this build writes a version 1 row, by
// design, and the tests for the re-chain need a database that looks
// like one an older release left behind. It is exported for the same
// reason AuditKeyForTesting is: its callers are in the cmd/mcp-server
// test binary, which cannot reach an in-package export_test.go. The
// same testing.Testing guard keeps it unreachable from the server,
// where a call would write rows the verifier refuses.
func SeedUnkeyedAuditLogForTesting(s *AuthStore, rows int, at time.Time,
	breakAt int64) error {

	if !testing.Testing() {
		panic("auth.SeedUnkeyedAuditLogForTesting was called outside a " +
			"test binary: it writes audit rows that cannot be verified")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := 0; i < rows; i++ {
		ev := newEvent(systemActor, "user.create", "user", nil, "old", nil)
		ev.OccurredAt = at.Add(time.Duration(i) * time.Minute)
		ev.Outcome = OutcomeSuccess

		var prevHash string
		err := s.db.QueryRow(
			"SELECT hash FROM audit_events ORDER BY id DESC LIMIT 1").
			Scan(&prevHash)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to read the previous hash: %w", err)
		}
		ev.PrevHash = prevHash
		ev.HashVersion = 1
		ev.Hash = auditHashV1(ev)

		if _, err := s.db.Exec(`
            INSERT INTO audit_events (
                occurred_at, actor_type, actor_id, actor_name, actor_ip,
                action, target_type, target_id, target_name, outcome,
                error, details, prev_hash, hash, hash_version
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			ev.OccurredAt.UTC().Format(auditTimeLayout),
			string(ev.ActorType), ev.ActorID, ev.ActorName,
			nullableText(ev.ActorIP), ev.Action, nullableText(ev.TargetType),
			ev.TargetID, nullableText(ev.TargetName), string(ev.Outcome),
			nullableText(ev.Error), nullableText(string(ev.Details)),
			ev.PrevHash, ev.Hash, ev.HashVersion); err != nil {
			return fmt.Errorf("failed to insert an unkeyed row: %w", err)
		}
	}

	if breakAt != 0 {
		if _, err := s.db.Exec("DELETE FROM audit_events WHERE id = ?",
			breakAt); err != nil {
			return fmt.Errorf("failed to delete row %d: %w", breakAt, err)
		}
	}

	return nil
}

// auditIDString renders a nullable identifier for the hash input:
// the decimal value, or the empty string when unset.
func auditIDString(id *int64) string {
	if id == nil {
		return ""
	}
	return strconv.FormatInt(*id, 10)
}

// auditHash computes the hex digest over the canonical rendering of the
// event under the version named by ev.HashVersion, which begins with
// the previous row's hash and so chains each row to its predecessor.
// Version 2 is an HMAC-SHA256 under key, which must be non-empty.
//
// Version 1, the unkeyed SHA-256, is refused. Anyone who can write
// auth.db can compute an unkeyed digest, so a version 1 row proves
// nothing about who wrote it; the re-chain leaves none behind and a
// database still holding one will not open, which together mean a
// version 1 row reaching here is evidence rather than history.
// verifyLegacyAuditChain computes the version 1 rendering directly, for
// the one caller with a reason to: the re-chain's report on the log it
// is about to replace.
//
// A version with no rendering here is an error, so that a verifier
// tells "written under a format this build predates" apart from "the
// hash does not match".
func auditHash(ev *AuditEvent, key []byte) (string, error) {
	switch ev.HashVersion {
	case 1:
		return "", fmt.Errorf("%w: audit row %d claims the unkeyed hash "+
			"version 1, which anyone able to write this file can compute",
			ErrAuditUnkeyedRow, ev.ID)
	case 2:
		return auditHashV2(ev, key)
	default:
		return "", fmt.Errorf("%w %d", errUnknownAuditHashVersion,
			ev.HashVersion)
	}
}

// canonicalFields renders a list of fields unambiguously for hashing.
//
// Each field is rendered as its byte length, a colon and then the field
// itself, rather than being joined by a separator. Any separator that
// can occur inside a field makes the rendering ambiguous: joined with a
// bare "|", an actor_name of "alice|198.51.100.9" alongside an empty
// actor_ip renders identically to those two values in their proper
// columns, so one event could be made to stand for another without
// disturbing its hash. A length prefix cannot collide that way, because
// the reader never has to guess where a field ends.
func canonicalFields(fields ...string) []byte {
	var canonical strings.Builder
	for _, field := range fields {
		canonical.WriteString(strconv.Itoa(len(field)))
		canonical.WriteByte(':')
		canonical.WriteString(field)
	}

	return []byte(canonical.String())
}

// auditCanonical renders the event for hashing, opening with the
// version label and then the version number the row itself carries.
//
// Both are covered deliberately. The label alone would separate the
// renderings only for a verifier that already knew which version to
// use, and this one does not: auditHash reads hash_version straight off
// the row, so the row chooses the algorithm it is checked with. Unless
// the number is inside the digest, anyone who can write auth.db can
// relabel a keyed version 2 row as version 1, recompute the unkeyed
// SHA-256 that version 1 uses, and have it verify; that is the JWT
// "alg: none" downgrade with the columns renamed. Binding the number
// means a row hashed as version 2 cannot be re-presented as version 1
// without invalidating its own digest.
func auditCanonical(label string, ev *AuditEvent) []byte {
	return canonicalFields(
		label,
		strconv.Itoa(ev.HashVersion),
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
	)
}

// auditHashV1 is the version 1 rendering: an unkeyed SHA-256. It is
// kept only so that the re-chain can report on the chain it is about to
// replace, and so that a database written by an older build can be
// recognized. No row is ever written under it, and auditHash refuses
// it, so nothing reaches this function on a verification path.
func auditHashV1(ev *AuditEvent) string {
	sum := sha256.Sum256(auditCanonical(auditHashV1Label, ev))
	return hex.EncodeToString(sum[:])
}

// auditHashV2 is the version 2 rendering: HMAC-SHA256 over the same
// canonical form, keyed by the key derived from the server secret. The
// key is what makes the chain worth anything against a deliberate
// attacker: with version 1, write access to auth.db was enough to
// rewrite a row and recompute every hash after it, whereas an HMAC
// cannot be recomputed without the secret as well.
func auditHashV2(ev *AuditEvent, key []byte) (string, error) {
	if len(key) == 0 {
		return "", errNoAuditKey
	}

	mac := hmac.New(sha256.New, key)
	// hash.Hash.Write is documented never to return an error.
	mac.Write(auditCanonical(auditHashV2Label, ev))

	return hex.EncodeToString(mac.Sum(nil)), nil
}

// auditPageSize is how many rows the whole-log walks read at a time.
// They page rather than holding one result set open because the
// re-chain updates rows as it goes, and a statement executed on the
// connection a query is still streaming from would deadlock against it.
//
// It is a variable so that the tests can lower it and walk a log of a
// handful of rows across several pages; nothing outside the tests
// assigns to it.
var auditPageSize = 500

// auditQuerier is the part of *sql.DB and *sql.Tx the paged walk needs,
// so that a walk can run either inside a transaction or outside one.
type auditQuerier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// forEachAuditEvent calls fn for every audit row in id order, reading
// them a page at a time. fn may execute further statements on q,
// because each page's rows are fully read and closed before fn sees
// them.
//
// The first page has no lower bound on id. Ids are values in the file,
// and an INSERT may name zero or a negative one, so a walk that began
// above any fixed value would skip rows the verifier and the re-chain
// must both see.
func forEachAuditEvent(q auditQuerier, fn func(AuditEvent) error) error {
	var after *int64
	for {
		page, err := auditEventPage(q, after)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			return nil
		}
		for i := range page {
			if err := fn(page[i]); err != nil {
				return err
			}
			id := page[i].ID
			after = &id
		}
	}
}

// auditEventPage reads up to auditPageSize rows in id order: the first
// rows of the log when after is nil, otherwise those with an id above
// it.
func auditEventPage(q auditQuerier, after *int64) ([]AuditEvent, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if after == nil {
		rows, err = q.Query(auditSelectFirstPage, auditPageSize)
	} else {
		rows, err = q.Query(auditSelectPage, *after, auditPageSize)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query audit events: %w", err)
	}
	defer rows.Close()

	page := make([]AuditEvent, 0, auditPageSize)
	for rows.Next() {
		ev, err := scanAuditEvent(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("failed to scan audit event: %w", err)
		}
		page = append(page, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read audit events: %w", err)
	}

	return page, nil
}

// countUnkeyedAuditRows returns the number of rows still carrying the
// unkeyed version 1 hash, and the lowest and highest id among them.
func (s *AuthStore) countUnkeyedAuditRows() (count, lowest, highest int64,
	err error) {

	var low, high sql.NullInt64
	if err := s.db.QueryRow(
		`SELECT COUNT(*), MIN(id), MAX(id) FROM audit_events
         WHERE hash_version = 1`).Scan(&count, &low, &high); err != nil {
		return 0, 0, 0, fmt.Errorf(
			"failed to count unkeyed audit events: %w", err)
	}

	return count, low.Int64, high.Int64, nil
}

// checkNoKeyedAuditRows returns an error naming how many keyed rows the
// log holds, and is called only where unkeyed rows are already known to
// be present. Any keyed row at all alongside an unkeyed one means
// unkeyed rows reached a log that was already keyed.
//
// Ordering is deliberately not consulted. An earlier version asked
// whether the unkeyed rows formed an id-prefix of the log, which an
// attacker satisfies by writing one unkeyed row at an explicit id below
// the genuine first row: AUTOINCREMENT constrains the ids SQLite
// assigns and not the ones an INSERT may name, so a row at id 0 or -1
// is accepted and sorts first. The property that holds without regard
// to ordering is simpler and is the one worth testing: no code path
// writes an audit row while the store is being opened, so a log that
// mixes the two renderings at all was not produced by this server.
//
// The distinction decides what an operator is told to do. A log that is
// wholly unkeyed is what an upgrade leaves behind, and re-chaining it is
// the intended remedy. A mixed log is not: re-chaining would sign the
// unkeyed rows under the server secret and make them indistinguishable
// from events this server recorded. The only remedy for that shape is to
// restore auth.db from a known-good copy.
func (s *AuthStore) checkNoKeyedAuditRows() error {
	var keyed int64
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM audit_events
         WHERE hash_version <> 1`).Scan(&keyed); err != nil {
		return fmt.Errorf("failed to count keyed audit events: %w", err)
	}
	if keyed == 0 {
		return nil
	}

	return fmt.Errorf("%w: this database holds %d keyed audit event(s) "+
		"alongside events still hashed under the unkeyed version 1 "+
		"rendering. A database upgraded from a release that predates the "+
		"keyed audit log holds unkeyed events and nothing else, so a log "+
		"holding both means unkeyed rows were written into a log that was "+
		"already keyed. No server path writes such a row, and re-chaining "+
		"would sign them under the server secret, so "+
		"'ai-dba-server -rechain-audit-log' is refused here. Treat this "+
		"database as suspect and restore auth.db from a known-good copy. "+
		"Deleting auth.db is NOT the remedy: it destroys every user, "+
		"token, group and permission in this installation",
		ErrAuditUnkeyedRow, keyed)
}

// ensureNoUnkeyedAuditRows refuses to open a database that still holds
// audit rows hashed under the unkeyed version 1 rendering, and runs on
// every open.
//
// The unkeyed rendering can be computed by anyone who can write
// auth.db, so an unkeyed row carries no evidence of who wrote it, and a
// run of them is indistinguishable from a forged history. Earlier
// designs tried to keep a boundary between the unkeyed rows a database
// inherited and the keyed rows written since; every mechanism for
// managing that boundary, including moving it down when retention
// deleted the row it rested on, turned out to be another way to have
// the server sign a forgery under the real key. Removing the rows
// removes the problem, so the upgrade re-hashes the whole log under the
// key once and nothing unkeyed is admissible afterwards.
//
// The gate reads the rows rather than schema_version, which is a value
// inside the file and so writable by whoever can write the log.
func (s *AuthStore) ensureNoUnkeyedAuditRows() error {
	count, lowest, highest, err := s.countUnkeyedAuditRows()
	if err != nil {
		return err
	}
	if count == 0 {
		return nil
	}

	// A mixed log is refused in its own terms, so that the operator is
	// not pointed at a command that will refuse them in turn, and would
	// sign the intruding rows if it did not.
	if err := s.checkNoKeyedAuditRows(); err != nil {
		return err
	}

	return fmt.Errorf("%w: this database holds %d audit event(s) hashed "+
		"under the unkeyed version 1 rendering (rows %d to %d), which "+
		"anyone able to write auth.db can compute, so their presence is "+
		"equally consistent with a forged log. Treat this database as "+
		"suspect and restore auth.db from a known-good copy. "+
		"Only if this is the first start after upgrading from a release "+
		"that predates the keyed audit log, and you are satisfied the "+
		"file has not been altered, re-hash the existing events under "+
		"the server secret with 'ai-dba-server -rechain-audit-log'; that "+
		"command attests whatever the log says at the moment it runs. "+
		"Deleting auth.db is NOT the remedy: it destroys every user, "+
		"token, group and permission in this installation",
		ErrAuditUnkeyedRow, count, lowest, highest)
}

// AuditRechainPlan describes the log a re-chain is about to rewrite. It
// is handed to the confirmation callback before anything is written, so
// that the operator decides on the figures rather than on the command's
// name.
type AuditRechainPlan struct {
	// Events is the number of rows in the log.
	Events int64

	// UnkeyedEvents is how many of them still carry the unkeyed
	// version 1 hash.
	UnkeyedEvents int64

	// Oldest and Newest are the timestamps of the first and last rows,
	// both zero when the log is empty.
	Oldest time.Time
	Newest time.Time

	// LowestID and HighestID are MIN(id) and MAX(id) at the moment the
	// plan was taken, both zero when the log is empty. Together with
	// Events they identify the rows the operator agreed to, and the
	// rewrite re-reads all three under the write lock and refuses to
	// proceed if any has moved.
	LowestID  int64
	HighestID int64

	// LegacyChainOK reports whether the log, as it stands, recomputes
	// under the rules each row was written with.
	//
	// It is worth reporting and worth nothing as a guarantee. The
	// unkeyed rendering is computable by anyone who can write the file,
	// so an attacker who rewrote the log will have left a chain that
	// recomputes perfectly; all a clean result rules out is someone who
	// edited a row and did not bother to fix the hashes after it.
	LegacyChainOK bool

	// LegacyFirstBad is the id of the first row that did not recompute,
	// or 0 when LegacyChainOK is true.
	LegacyFirstBad int64

	// LegacyChainErr is why the recomputation failed, nil when it did
	// not.
	LegacyChainErr error

	// Mode is which of the two re-chains the plan is for: a re-hash of
	// a log inherited from a release before the keyed chain, or a
	// re-anchor of a keyed log that no longer verifies. The fields
	// below describe a re-anchor and are empty for a re-hash.
	Mode AuditRechainMode

	// Problem is why the log fails verification, or nil when it
	// verifies and there is nothing to re-anchor. KeyMismatch reports
	// that the failure has the shape a changed server secret leaves.
	Problem     error
	KeyMismatch bool

	// HeadID and HeadHash identify the oldest row, which the re-anchor
	// records as where the log now begins.
	HeadID   int64
	HeadHash string

	// PreviousHead is where the newest purge or re-chain event said
	// the log began, or nil when no such event records it.
	PreviousHead *AuditRechainHead

	// HistoryEvents is how many rows, from the oldest through
	// HistoryThroughID, the re-anchor would accept as history: every
	// row up to the last one that does not verify under the key in use
	// or does not link to the row before it. Zero when there are none.
	HistoryEvents    int64
	HistoryThroughID int64

	// reanchor is the scan the figures above came from, which the
	// transaction repeats and compares under the write lock.
	reanchor auditReanchorScan
}

// AuditRechainResult reports what a re-chain did.
type AuditRechainResult struct {
	// Confirmed is false when the confirmation callback declined, in
	// which case nothing was written.
	Confirmed bool

	// Events is the number of rows re-hashed, not counting the
	// audit.rechain event the re-chain appends. A re-anchor re-hashes
	// nothing and leaves it zero.
	Events int64

	// Mode is which re-chain ran, or would have.
	Mode AuditRechainMode

	// UpToDate reports that the log already verified, so the operator
	// was not asked and nothing was written.
	UpToDate bool
}

// AuditRechainConfirm is shown the plan and returns whether to proceed.
// Returning false leaves the log untouched.
type AuditRechainConfirm func(AuditRechainPlan) (bool, error)

// auditActionRechain is the action recorded by the re-chain itself, so
// that the log accounts for the one event that rewrote all of it.
const auditActionRechain = "audit.rechain"

// RechainAuditLog re-hashes every row of the audit log as a keyed
// version 2 row under auditKey, in id order and in a single
// transaction, and appends an audit.rechain event recording that it
// did. It is the one-time upgrade step for a database written by a
// release that predates the keyed chain, and the only way to open such
// a database at all: every ordinary open refuses while an unkeyed row
// remains.
//
// Because prev_hash is part of the hash input, re-hashing cascades:
// each row's prev_hash becomes the newly computed hash of the row
// before it. The first row's prev_hash is carried over exactly as
// given, which is what VerifyAuditChain already accepts, since the
// retention purge may long since have deleted the row it points at.
//
// confirm is called with the figures before anything is written and
// must return true for the rewrite to happen. The re-chain attests
// whatever the database says at the moment it runs: it proves that the
// log has not been altered since, and nothing whatever about what
// happened before. That is why it is an operator's deliberate act and
// not something a start-up does.
//
// On a log with no unkeyed rows it re-anchors instead (see
// audit_reanchor.go): a log that verifies returns UpToDate without
// calling confirm, and one that does not is left unchanged apart from
// one appended audit.rechain event, which records where the log now
// begins and which of its oldest rows are accepted as history.
func RechainAuditLog(dataDir string, auditKey []byte, actor Actor,
	confirm AuditRechainConfirm) (AuditRechainResult, error) {

	if confirm == nil {
		return AuditRechainResult{}, errors.New(
			"a re-chain confirmation callback is required")
	}

	store, err := newAuthStore(dataDir, 0, 0, auditKey, true)
	if err != nil {
		return AuditRechainResult{}, err
	}
	defer store.Close()

	return store.rechainAuditLog(actor, confirm)
}

// rechainAuditLog is RechainAuditLog on an already-open store.
//
// s.mu is held across the confirmation as well as the rewrite. The
// prompt can sit there indefinitely, which would be intolerable in the
// server; it is not, because the only caller is a one-shot command
// whose store is doing nothing else. Holding it means the figures the
// operator agreed to and the rows the transaction rewrites are the same
// log as far as this process is concerned.
func (s *AuthStore) rechainAuditLog(actor Actor,
	confirm AuditRechainConfirm) (AuditRechainResult, error) {

	s.mu.Lock()
	defer s.mu.Unlock()

	var result AuditRechainResult

	plan, err := s.auditRechainPlan()
	if err != nil {
		return result, err
	}
	if plan.Mode == AuditRechainReanchor {
		return s.reanchorAuditLog(actor, plan, confirm)
	}
	result.Mode = plan.Mode

	proceed, err := confirm(plan)
	if err != nil {
		return result, err
	}
	if !proceed {
		return result, nil
	}

	events, err := s.rechainAuditLogTx(actor, plan)
	if err != nil {
		return result, err
	}

	result.Confirmed = true
	result.Events = events

	return result, nil
}

// auditRechainPlan gathers what the operator is shown before the
// rewrite: the size and span of the log, how much of it is unkeyed, and
// whether it recomputes under the rules it was written with.
func (s *AuthStore) auditRechainPlan() (AuditRechainPlan, error) {
	var plan AuditRechainPlan

	unkeyed, _, _, err := s.countUnkeyedAuditRows()
	if err != nil {
		return plan, err
	}
	plan.UnkeyedEvents = unkeyed

	// The re-chain exists for one database shape: a log written wholly
	// under the old unkeyed rendering and inherited at upgrade. Any
	// other shape is refused before the operator is shown figures they
	// might approve, because approving them would have the server sign
	// rows it never wrote.
	if unkeyed > 0 {
		if err := s.checkNoKeyedAuditRows(); err != nil {
			return plan, err
		}
	}

	var oldest, newest sql.NullString
	var lowestID, highestID sql.NullInt64
	if err := s.db.QueryRow(
		`SELECT COUNT(*), MIN(occurred_at), MAX(occurred_at),
                MIN(id), MAX(id)
         FROM audit_events`).
		Scan(&plan.Events, &oldest, &newest, &lowestID,
			&highestID); err != nil {
		return plan, fmt.Errorf("failed to measure the audit log: %w", err)
	}
	plan.LowestID = lowestID.Int64
	plan.HighestID = highestID.Int64
	if plan.Oldest, err = parseAuditTime(oldest); err != nil {
		return plan, err
	}
	if plan.Newest, err = parseAuditTime(newest); err != nil {
		return plan, err
	}

	firstBad, legacyErr := s.verifyLegacyAuditChain()
	plan.LegacyChainOK = legacyErr == nil
	plan.LegacyFirstBad = firstBad
	plan.LegacyChainErr = legacyErr

	plan.Mode = AuditRechainRehash
	if unkeyed == 0 {
		plan.Mode = AuditRechainReanchor
		if err := s.auditReanchorPlan(&plan); err != nil {
			return plan, err
		}
	}

	return plan, nil
}

// parseAuditTime reads an occurred_at value that may be NULL, which is
// what MIN and MAX return over an empty table.
func parseAuditTime(v sql.NullString) (time.Time, error) {
	if !v.Valid {
		return time.Time{}, nil
	}

	parsed, err := time.Parse(time.RFC3339Nano, v.String)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid occurred_at %q in the audit "+
			"log: %w", v.String, err)
	}

	return parsed, nil
}

// verifyLegacyAuditChain recomputes the chain as it currently stands,
// checking each row under the rendering that row claims, and returns
// the id of the first row that fails. It is the one place that computes
// the unkeyed version 1 digest for an existing row, because it is the
// one place with a reason to: the re-chain reports on the log it is
// about to replace, and a log that does not even recompute under its
// own rules is worth knowing about before blessing it.
//
// What it shows is bounded. Version 1 is unkeyed, so an attacker who
// rewrote the log recomputed those hashes as easily as this does; only
// carelessness shows up here. Any keyed row it meets is checked under
// the key, which does mean something, but a database being re-chained
// is one whose keyed rows are the newest few at most.
func (s *AuthStore) verifyLegacyAuditChain() (int64, error) {
	firstBad := int64(0)
	prevHash := ""
	first := true
	highestVersion := 0

	err := forEachAuditEvent(s.db, func(ev AuditEvent) error {
		if !first && ev.PrevHash != prevHash {
			firstBad = ev.ID
			return fmt.Errorf("%w at row %d: it does not link to row %d",
				ErrAuditChainBroken, ev.ID, ev.ID-1)
		}
		// A row claiming an older rendering than one before it is the
		// same fact VerifyAuditChain refuses, and for the same reason:
		// the unkeyed rendering is computable by anyone who can write
		// the file, so a version 1 row above a keyed one is a row put
		// there by hand. Without this check a log of keyed rows with a
		// single unkeyed row appended recomputes under the rules each
		// row claims, and the plan would report it as recomputing
		// cleanly.
		if ev.HashVersion < highestVersion {
			firstBad = ev.ID
			return fmt.Errorf(
				"%w at row %d: version %d after version %d",
				ErrAuditChainDowngraded, ev.ID, ev.HashVersion,
				highestVersion)
		}

		var want string
		switch ev.HashVersion {
		case 1:
			want = auditHashV1(&ev)
		case 2:
			computed, err := auditHashV2(&ev, s.auditKey)
			if err != nil {
				firstBad = ev.ID
				return err
			}
			want = computed
		default:
			firstBad = ev.ID
			return fmt.Errorf("%w: audit row %d claims hash version %d",
				errUnknownAuditHashVersion, ev.ID, ev.HashVersion)
		}
		if want != ev.Hash {
			firstBad = ev.ID
			return fmt.Errorf("%w at row %d", ErrAuditChainBroken, ev.ID)
		}

		highestVersion = ev.HashVersion
		prevHash = ev.Hash
		first = false

		return nil
	})
	if err != nil {
		return firstBad, err
	}

	return 0, nil
}

// ErrAuditRechainChanged reports that the log moved between the plan
// the operator approved and the transaction that would have rewritten
// it, so the rewrite was abandoned with nothing written.
var ErrAuditRechainChanged = errors.New("audit log changed since the plan")

// checkAuditRechainPlanStillHolds re-reads the row count and id range
// under the write lock and refuses the rewrite when any of the three
// differs from the approved plan.
//
// It deliberately does not re-run the legacy verification: that walks
// the whole log a second time, and what is wanted here is only that the
// rows about to be signed are the rows that were counted, which the id
// range and the count settle between them. A row appended, removed or
// renumbered moves at least one of the three.
func (s *AuthStore) checkAuditRechainPlanStillHolds(tx *sql.Tx,
	plan AuditRechainPlan) error {

	var events int64
	var lowest, highest sql.NullInt64
	if err := tx.QueryRow(
		`SELECT COUNT(*), MIN(id), MAX(id) FROM audit_events`).
		Scan(&events, &lowest, &highest); err != nil {
		return fmt.Errorf("failed to re-measure the audit log: %w", err)
	}

	if events == plan.Events && lowest.Int64 == plan.LowestID &&
		highest.Int64 == plan.HighestID {

		return nil
	}

	return fmt.Errorf("%w: the log now holds %d event(s) in rows %d to %d, "+
		"but the plan you approved described %d event(s) in rows %d to %d. "+
		"Nothing has been signed and nothing has been written. Something "+
		"wrote to auth.db between the plan and this rewrite; find out what "+
		"before running the re-chain again",
		ErrAuditRechainChanged, events, lowest.Int64, highest.Int64,
		plan.Events, plan.LowestID, plan.HighestID)
}

// rechainAuditLogTx performs the rewrite, and returns the number of
// rows re-hashed.
//
// The append-only trigger refuses an UPDATE, so it is dropped and
// re-created inside the transaction that does the rewriting. That is
// safe on three counts, and all three are needed:
//
//   - SQLite makes DDL transactional, so the DROP is undone by a
//     rollback exactly as the row updates are. A failure part-way
//     therefore leaves both the rows and the trigger as they were,
//     rather than a half-rewritten log with no trigger on it.
//   - The store opens SQLite with _txlock=immediate, so this
//     transaction takes the database's write lock at BEGIN. No other
//     connection can write between the DROP and the CREATE.
//   - In WAL mode a reader on another connection sees the last
//     committed snapshot, so no other connection ever observes the
//     database without the trigger; it appears again, having never been
//     absent, at commit.
//
// The alternative, deleting every row and re-inserting it with the new
// hash, needs no DDL but rewrites sqlite_sequence as a side effect of
// emptying an AUTOINCREMENT table, and verifyAuditTail reads exactly
// that value to detect a truncated log. Dropping the trigger touches
// less.
func (s *AuthStore) rechainAuditLogTx(actor Actor, plan AuditRechainPlan) (
	int64, error) {

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin the re-chain transaction: %w",
			err)
	}
	committed := false
	defer func() {
		if !committed {
			//nolint:errcheck // Rollback error is not critical; the
			// outer error is already being returned.
			tx.Rollback()
		}
	}()

	// The plan was taken before the confirmation, and the prompt can
	// sit in front of an operator for as long as they take to answer.
	// s.mu keeps this process out in the meantime and nothing else:
	// auth.db is a file, and anyone able to write it is another writer
	// on it. Re-reading the row range now, inside the transaction that
	// holds the database's write lock, is what ties the figures the
	// operator approved to the rows about to be signed.
	if err := s.checkAuditRechainPlanStillHolds(tx, plan); err != nil {
		return 0, err
	}

	if _, err := tx.Exec("DROP TRIGGER IF EXISTS " +
		auditNoUpdateTrigger); err != nil {
		return 0, fmt.Errorf("failed to drop the append-only trigger %s: %w",
			auditNoUpdateTrigger, err)
	}

	rehashed := int64(0)
	prevHash := ""
	first := true
	walkErr := forEachAuditEvent(tx, func(ev AuditEvent) error {
		if !first {
			// Every row but the first is re-linked to the new hash of
			// the row before it. The first keeps the prev_hash it was
			// written with, because the row it points at may have been
			// purged years ago and there is nothing else to point at.
			ev.PrevHash = prevHash
		}
		ev.HashVersion = auditHashVersion

		hash, err := auditHash(&ev, s.auditKey)
		if err != nil {
			return fmt.Errorf("failed to re-hash audit row %d: %w", ev.ID, err)
		}
		ev.Hash = hash

		if _, err := tx.Exec(`UPDATE audit_events
             SET prev_hash = ?, hash = ?, hash_version = ? WHERE id = ?`,
			ev.PrevHash, ev.Hash, ev.HashVersion, ev.ID); err != nil {
			return fmt.Errorf("failed to re-chain audit row %d: %w",
				ev.ID, err)
		}

		rehashed++
		prevHash = ev.Hash
		first = false

		return nil
	})
	if walkErr != nil {
		return 0, walkErr
	}

	// The plan counted the rows with COUNT(*), and the walk reached
	// them one page at a time. Were the two ever to disagree, some row
	// the operator approved would be left unsigned beside keyed ones,
	// a mixed log the store then refuses to open, so the rewrite is
	// abandoned rather than committed.
	if rehashed != plan.Events {
		return 0, fmt.Errorf("%w: the plan counted %d event(s) but the "+
			"rewrite reached %d; nothing has been written",
			ErrAuditRechainChanged, plan.Events, rehashed)
	}

	if _, err := tx.Exec(auditNoUpdateTriggerDDL); err != nil {
		return 0, fmt.Errorf("failed to restore the append-only trigger "+
			"%s: %w", auditNoUpdateTrigger, err)
	}

	// The re-chain records itself, in the same transaction and as the
	// newest link of the chain it has just rebuilt, so that the log
	// carries the one event that explains why every hash in it changed.
	ev := newEvent(actor, auditActionRechain, "", nil, "", map[string]any{
		"events":          rehashed,
		"unkeyed_events":  plan.UnkeyedEvents,
		"legacy_chain_ok": plan.LegacyChainOK,
	})
	if err := s.recordAudit(tx, ev); err != nil {
		return 0, fmt.Errorf("failed to record the re-chain event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit the re-chain: %w", err)
	}
	committed = true

	return rehashed, nil
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
	hash, err := auditHash(ev, s.auditKey)
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
	// A trigger planted in auth.db can make the INSERT succeed without
	// writing a row (RAISE(IGNORE)), which would commit the change the
	// event describes with no record of it; verifyAuditSchema refuses
	// such a trigger, and this refuses the write it would cause.
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to confirm audit event insert: %w", err)
	}
	if n != 1 {
		return fmt.Errorf("failed to insert audit event: %d rows written, "+
			"not 1; a trigger on audit_events may be discarding events",
			n)
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

// The audit_events reads whose text is fixed are assembled here, once,
// rather than concatenated at each call site: every one of them is a
// compile-time constant, so folding them keeps the call sites reading
// as the plain parameterised queries they are.
const (
	auditSelectAll = "SELECT " + auditColumns + " FROM audit_events"

	auditSelectPage = auditSelectAll +
		" WHERE id > ? ORDER BY id LIMIT ?"

	auditSelectFirstPage = auditSelectAll + " ORDER BY id LIMIT ?"

	auditSelectByID = auditSelectAll + " WHERE id = ?"

	auditSelectInIDOrder = auditSelectAll + " ORDER BY id"
)

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
	// constants (auditSelectAll) and a WHERE clause built solely from
	// fixed literals by auditWhere; every filter value is bound.
	rows, err := s.db.Query(auditSelectAll+where+
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
// prev_hash cannot be checked against its predecessor, because the
// retention purge may have removed the row it points at; the head is
// instead checked against the record the newest purge or re-chain
// event keeps of it, as audit_head.go describes, and a head with a
// predecessor and no purge event at all is reported as a deletion. Rows
// a re-chain accepted as history are checked against its digest rather
// than the key; VerifyAuditLog reports how many there are. Callers must
// check the error first: a firstBad of 0 means the chain is intact only when err is nil, because
// a scan or iteration failure also reports firstBad 0.
//
// Every row must carry the keyed version 2 hash. A row claiming the
// unkeyed version 1 rendering is refused by auditHash, because the
// re-chain that an upgrade runs leaves none behind and nothing this
// build writes produces one. The check that the version never falls
// along the chain is redundant while 2 is the only admissible value,
// and is kept because it costs nothing and will matter on the day a
// version 3 exists.
//
// Errors wrap one of the sentinels declared at the top of this file, so
// a caller can tell tampering, a downgrade and a mislaid key apart
// without reading the message.
//
// What a clean result is worth is set out at verifyAuditTail, and it is
// less than it looks: read that before relying on this.
func (s *AuthStore) VerifyAuditChain() (int, int64, error) {
	report, err := s.VerifyAuditLog()
	return report.Events, report.FirstBad, err
}

// verifyAuditSchema checks that the two schema objects the chain's
// guarantees depend on are present and do what their names say: the
// unique index on prev_hash, without which two rows may claim the same
// predecessor and fork the log, and the BEFORE UPDATE trigger, without
// which a row can be rewritten in place. ensureAuditSchema re-creates
// both on every open, so their absence from a live database means they
// were dropped since, and a chain that recomputes cleanly under those
// conditions has proved nothing.
//
// The definitions are checked as well as the names, because
// ensureAuditSchema creates both with IF NOT EXISTS and so never
// replaces an object that merely has the right name. Anyone able to
// write auth.db could otherwise swap in a unique index on some other
// column, or a same-named trigger that does nothing, once, and fork or
// rewrite the log thereafter with this check still passing. That is
// no protection against such a writer, who can do a great deal else,
// but it keeps the check meaning what it says.
func (s *AuthStore) verifyAuditSchema() error {
	if err := s.verifyAuditChainIndex(); err != nil {
		return err
	}

	return s.verifyAuditNoUpdateTrigger()
}

// verifyAuditChainIndex checks that idx_audit_prev_hash is a unique,
// non-partial index on audit_events whose only key column is prev_hash.
// A partial index enforces uniqueness only over the rows its WHERE
// clause selects, which may be none, and an index over any other column
// or columns allows two rows to name the same predecessor.
func (s *AuthStore) verifyAuditChainIndex() error {
	var unique, partial int
	err := s.db.QueryRow(`SELECT "unique", partial
         FROM pragma_index_list('audit_events') WHERE name = ?`,
		auditChainIndexName).Scan(&unique, &partial)
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
	case partial != 0:
		return fmt.Errorf("audit chain unprotected: index %s on "+
			"audit_events is partial, so it does not cover every row and "+
			"the chain may have forked", auditChainIndexName)
	}

	// An expression in the key reports a NULL name, which COALESCE turns
	// into an empty string so that it counts as a column that is not
	// prev_hash rather than failing the scan.
	var columns, onPrevHash int
	if err := s.db.QueryRow(`SELECT COUNT(*),
                COALESCE(SUM(COALESCE(name, '') = 'prev_hash'), 0)
         FROM pragma_index_info(?)`, auditChainIndexName).
		Scan(&columns, &onPrevHash); err != nil {
		return fmt.Errorf("failed to read the columns of index %s: %w",
			auditChainIndexName, err)
	}
	if columns != 1 || onPrevHash != 1 {
		return fmt.Errorf("audit chain unprotected: index %s on "+
			"audit_events is not an index on prev_hash alone (%d key "+
			"column(s)), so the chain may have forked",
			auditChainIndexName, columns)
	}

	return nil
}

// verifyAuditNoUpdateTrigger checks that audit_events_no_update exists
// on audit_events, is exactly the trigger auditNoUpdateTriggerDDL
// creates, and is the only trigger that touches audit_events at all.
// Nothing short of the whole definition would do: a trigger with a WHEN
// clause, or one that fires only on UPDATE OF some columns, still
// contains BEFORE UPDATE and RAISE(ABORT, yet lets rows be rewritten.
// And any other trigger is refused, whichever table it is on, because
// one that discards chosen events with RAISE(IGNORE), or deletes them
// as they arrive, leaves a chain that verifies with those events never
// in it.
func (s *AuthStore) verifyAuditNoUpdateTrigger() error {
	rows, err := s.db.Query(`SELECT name, sql FROM sqlite_master
         WHERE type = 'trigger'
           AND (tbl_name = 'audit_events' COLLATE NOCASE
                OR sql LIKE '%audit_events%')
         ORDER BY name`)
	if err != nil {
		return fmt.Errorf("failed to look for trigger %s: %w",
			auditNoUpdateTrigger, err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var name string
		var definition sql.NullString
		if err := rows.Scan(&name, &definition); err != nil {
			return fmt.Errorf("failed to look for trigger %s: %w",
				auditNoUpdateTrigger, err)
		}
		if name != auditNoUpdateTrigger {
			return fmt.Errorf("audit chain unprotected: the trigger %q "+
				"is not one this server creates and acts on audit_events, "+
				"so events may have been discarded or rewritten", name)
		}
		if normalizeSchemaSQL(definition.String) != expectedAuditTriggerSQL() {
			return fmt.Errorf("audit chain unprotected: the trigger %s on "+
				"audit_events does not match the definition this server "+
				"creates, so rows may have been rewritten in place",
				auditNoUpdateTrigger)
		}
		found = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("failed to look for trigger %s: %w",
			auditNoUpdateTrigger, err)
	}
	if !found {
		return fmt.Errorf("audit chain unprotected: the trigger %s is "+
			"missing from audit_events, so rows may have been rewritten "+
			"in place", auditNoUpdateTrigger)
	}

	return nil
}

// expectedAuditTriggerSQL is auditNoUpdateTriggerDDL as SQLite records
// it in sqlite_master, normalised for comparison. SQLite stores the
// statement text as written, less the IF NOT EXISTS clause and the
// terminating semicolon.
func expectedAuditTriggerSQL() string {
	return strings.Replace(normalizeSchemaSQL(auditNoUpdateTriggerDDL),
		"CREATE TRIGGER IF NOT EXISTS ", "CREATE TRIGGER ", 1)
}

// normalizeSchemaSQL collapses every run of whitespace to one space and
// drops a trailing semicolon, so that two renderings of one statement
// that differ only in layout compare equal.
func normalizeSchemaSQL(text string) string {
	return strings.TrimSuffix(strings.Join(strings.Fields(text), " "), ";")
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
// operator or a script makes without meaning to hide anything.
//
// Against a deliberate attacker it rests on the chain, and since hash
// version 2 the chain is an HMAC keyed by a key derived from the server
// secret. Rewriting a row and recomputing every hash after it therefore
// needs write access to auth.db and read access to the server secret
// file, rather than write access alone as it did under version 1.
// Splitting those two is the whole of the protection, so it is worth
// keeping them apart: the secret belongs in a file the account running
// the server can read and nothing else can.
//
// The unkeyed version 1 rendering is no longer admissible anywhere. A
// database written by a build that predates the key holds rows computed
// under it, and an attacker with write access can compute them just as
// easily, so there is no way to tell the two apart; rather than try,
// this build refuses to open such a database until an operator has
// re-hashed the whole log under the key with RechainAuditLog, and
// auditHash then refuses version 1 outright.
//
// What that still does not buy:
//
//   - An attacker who can read the server secret and write auth.db can
//     rewrite the whole chain freely, so this check sees nothing.
//     Keeping those two apart is the entire protection against
//     rewriting: the secret belongs in a file the account running the
//     server can read and nothing else can.
//   - Deleting the newest rows needs no secret at all. The rows left
//     behind still verify, and nothing protects sqlite_sequence the way
//     the no-update trigger protects audit_events, so an attacker with
//     write access alone can delete the tail and write the sequence
//     down to the new MAX(id) in one more statement, after which this
//     check agrees. It catches a deletion that leaves the sequence
//     alone, and nothing more.
//   - Deleting the oldest rows is caught by the head check in
//     audit_head.go only once a purge run by a build that records the
//     head has removed something. Until then the newest audit.purge
//     event, if there is one, says nothing about where the head should
//     be, and the check can report a head with a predecessor only when
//     no purge event survives at all.
//   - The re-chain attests whatever the database said at the moment it
//     ran. It is a one-time trust event, and everything before it rests
//     on the operator having had good reason to believe the file.
//   - Nothing anchors the log outside the file, so a whole database
//     replaced with a consistent forgery, key and all, is
//     indistinguishable from the real one.
//   - Nothing verifies the chain at start-up. It is checked when an
//     operator runs 'ai-dba-server -verify-audit-log', and not
//     otherwise, so a rewrite goes unnoticed until someone looks.
//
// Treat a failed verification as evidence of tampering, never a
// successful one as proof of its absence.
func (s *AuthStore) verifyAuditTail() error {
	present, err := s.sequenceTablePresent()
	if err != nil {
		return err
	}
	if !present {
		return fmt.Errorf("%w: audit chain tail unverifiable: the "+
			"sqlite_sequence table is missing; every database this server "+
			"creates holds one, because its tables use AUTOINCREMENT, so it "+
			"has been removed", ErrAuditChainBroken)
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
				"%w: audit chain tail missing: the sqlite_sequence row for "+
					"audit_events has been removed or emptied whilst %d "+
					"event(s) remain", ErrAuditChainBroken, newest)
		}
		return nil
	}

	switch {
	case seq.Int64 > newest:
		return fmt.Errorf("%w: audit chain tail missing: newest row %d, "+
			"sequence %d", ErrAuditChainBroken, newest, seq.Int64)
	case seq.Int64 < newest:
		return fmt.Errorf(
			"%w: audit sequence inconsistent: newest row %d, sequence %d; "+
				"SQLite raises the sequence before it writes the row, so "+
				"it cannot fall behind on its own",
			ErrAuditChainBroken, newest, seq.Int64)
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

// PurgeAuditEvents deletes the run of oldest events that sits entirely
// before the given time and returns the number of rows removed. On a
// log whose ids and timestamps both rise together, which is every log
// this server wrote itself, that is the same set of rows as deleting by
// timestamp alone; the difference, and why it is written this way, is
// set out beside the statement below. Because each retained row
// still carries its own prev_hash, the chain stays verifiable from the
// oldest retained row onwards.
//
// A purge that removed anything records an audit.purge event of its
// own, in the same transaction as the DELETE, so that the log always
// explains its own missing prefix. The event names the row it left as
// the oldest, by id and hash, which is what lets VerifyAuditChain tell
// a purge from a deletion at the start of the log. The event is written
// by the system actor and carries no target, because retention acts on
// the log as a whole.
//
// Before it deletes anything the purge verifies the rows it is about to
// remove, and it refuses, deleting nothing, when they do not begin
// where the previous purge left the head or do not chain through to the
// row that will become the new one. audit_head.go explains why the
// record needs that.
//
// It never re-signs a row. An earlier design had the purge re-sign a
// boundary record when it removed the row that record rested on, which
// turned retention, something that runs unattended every few minutes,
// into a path that re-signed part of the log under the real key;
// backdating one row was then enough to steer what got signed. Nothing
// here writes a hash over a row it did not itself create, and the head
// it records is a row that must still verify on its own.
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

	// The purge removes a contiguous prefix of ids and can express
	// nothing else. It finds the oldest row inside the retention window
	// and deletes everything below it, rather than deleting by
	// occurred_at directly.
	//
	// occurred_at is a value in the file, so anyone able to write
	// auth.db can backdate whichever rows they choose. A plain
	// "DELETE ... WHERE occurred_at < ?" would then remove the newest
	// events instead of the oldest: the purge would relink the chain to
	// whatever survived, and the audit.purge event it appends would
	// raise MAX(id) back into agreement with sqlite_sequence, which is
	// exactly the disagreement verifyAuditTail reads to detect a
	// truncated log. Retention runs unattended every few minutes, so
	// that would be a deletion oracle an attacker need only wait for.
	// id is insertion order and is assigned by SQLite on every server
	// insert. The append-only trigger stops an UPDATE changing it, but a
	// writer of the file can delete a row and insert it again at another
	// id, and since the id is not covered by the hash the row still
	// verifies there; so a prefix of ids is not by itself a prefix of
	// the log. What makes it one is verifyAuditPurgePrefix, which
	// requires the rows to be deleted to link, in id order, from the
	// recorded head through to the new one. A row inserted at a chosen
	// id can also only move the boundary earlier, never past the oldest
	// row inside the window.
	//
	// When no row is inside the window MIN(id) is NULL and nothing is
	// deleted, which is a
	// deliberate departure from deleting the whole log. NULL rather
	// than a fixed value such as 0, because an INSERT may name a
	// negative id and a fixed cut-off would delete below it. Over-
	// retaining is harmless and lasts only until the next scheduled
	// purge after an event falls inside the window again, whereas
	// emptying the table hands the
	// same oracle back: backdating every row would erase the log
	// entirely and leave the appended audit.purge event as a fresh
	// genesis row.
	var cut sql.NullInt64
	if err := tx.QueryRow(`SELECT MIN(id) FROM audit_events
        WHERE occurred_at >= ?`,
		olderThan.UTC().Format(auditTimeLayout)).Scan(&cut); err != nil {
		return 0, fmt.Errorf("failed to find the audit retention "+
			"boundary: %w", err)
	}
	if !cut.Valid {
		return 0, nil
	}

	// Most ticks remove nothing, and they should not pay for reading
	// the head of the log to find that out.
	var below int64
	if err := tx.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE id < ?`,
		cut.Int64).Scan(&below); err != nil {
		return 0, fmt.Errorf("failed to count audit events to purge: %w", err)
	}
	if below == 0 {
		return 0, nil
	}

	// The rows about to go are verified first, and the purge refuses
	// rather than delete a prefix that does not begin where the last
	// purge left the head or does not chain through to the row that
	// becomes the new one. audit_head.go sets out why: the head record
	// this purge writes would otherwise launder a deletion.
	kept, err := s.verifyAuditPurgePrefix(tx, cut.Int64)
	if err != nil {
		return 0, err
	}

	result, err := tx.Exec(`DELETE FROM audit_events WHERE id < ?`,
		cut.Int64)
	if err != nil {
		return 0, fmt.Errorf("failed to purge audit events: %w", err)
	}

	removed, err = result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to count purged audit events: %w", err)
	}

	if removed > 0 {
		details := map[string]any{
			"older_than":           olderThan.UTC().Format(time.RFC3339),
			"removed":              removed,
			"oldest_retained_id":   kept.newHead.ID,
			"oldest_retained_hash": kept.newHead.Hash,
		}
		// Rows a re-chain accepted as history that survive this purge
		// stay accepted, under a digest of those that remain; the purge
		// event is now the newest anchor, so it must carry them on.
		if h := kept.history; h != nil {
			details["history_through_id"] = *h.HistoryThroughID
			details["history_events"] = h.HistoryEvents
			details["history_digest"] = h.HistoryDigest
		}
		ev := newEvent(systemActor, auditActionPurge, "", nil, "", details)
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
