/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/logging"
)

// handleAudit serves GET /api/v1/rbac/audit, the read side of the RBAC
// audit log. The log records who changed what across the whole
// installation, so the endpoint is restricted to superusers rather than
// to any of the finer-grained admin permissions.
//
// The body is a bare JSON array of auth.AuditEvent, newest first, and
// X-Total-Count carries the number of rows matching the filters before
// limit and offset are applied so that a client can size its pager.
func (h *RBACHandler) handleAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !h.requireSuperuser(w, r) {
		return
	}

	if !h.requireUnscopedTokenForAudit(w, r) {
		return
	}

	filter, err := parseAuditFilter(r.URL.Query())
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request: "+err.Error())
		return
	}

	events, total, err := h.authStore.ListAuditEvents(filter)
	if err != nil {
		// The underlying error can name internal SQL, so it is logged
		// rather than returned to the caller.
		log.Printf("[ERROR] Failed to list audit events: %v", err)
		RespondError(w, http.StatusInternalServerError,
			"Failed to list audit events")
		return
	}

	// A nil slice would encode as null; the web client and the OpenAPI
	// spec both expect an array.
	if events == nil {
		events = []auth.AuditEvent{}
	}

	// A successful read is deliberately not written to audit_events
	// itself: reading the log is not a change, and recording every
	// read in the same append-only table would let anyone with the
	// superuser right grow it without bound. The server log is where
	// the read is accounted for instead, so that "who has been
	// reading the audit log, and for what" is answerable after the
	// fact. Every value here comes from the request, so it goes
	// through the log sanitiser; none of them can carry a secret,
	// since the filter is a set of names, types and timestamps.
	log.Printf("[AUDIT] Audit log read by %s: filter %s, %d row(s) returned of %d matching",
		logging.SanitizeForLog(auth.ActorFromContext(r.Context()).Name),
		logging.SanitizeForLog(describeAuditFilter(filter)),
		len(events), total)

	w.Header().Set(headerTotalCount, strconv.Itoa(total))
	RespondJSON(w, http.StatusOK, events)
}

// describeAuditFilter renders the filters in effect for the log line
// above, in a stable order and naming only the ones actually set, so
// that an unfiltered read is visibly a read of everything rather than a
// row of empty values. Timestamps are rendered in RFC 3339 UTC, which
// is how they arrived.
//
// Values are quoted with strconv.Quote, because the pairs are separated
// by spaces and an actor name may contain one: ?actor=alice+action=x
// would otherwise render as two filters where only one was applied, and
// anything reading the line back would believe the second. Quoting also
// escapes the quote character itself, so a value cannot close its own
// quoting to fabricate a second filter. Newlines are already handled
// upstream by logging.SanitizeForLog, which keeps the whole of this to
// one line; this is confusion between fields within that line, not
// injection of another.
func describeAuditFilter(f auth.AuditFilter) string {
	var parts []string

	add := func(name, value string) {
		if value != "" {
			parts = append(parts,
				name+"="+strconv.Quote(capAuditFilterValue(value)))
		}
	}

	add("actor", f.ActorName)
	add("actor_type", f.ActorType)
	add("action", f.Action)
	add("target_type", f.TargetType)
	if f.TargetID != nil {
		add("target_id", strconv.FormatInt(*f.TargetID, 10))
	}
	add("outcome", f.Outcome)
	if f.Since != nil {
		add("since", f.Since.UTC().Format(time.RFC3339Nano))
	}
	if f.Until != nil {
		add("until", f.Until.UTC().Format(time.RFC3339Nano))
	}
	if f.Limit > 0 {
		add("limit", strconv.Itoa(f.Limit))
	}
	if f.Offset > 0 {
		add("offset", strconv.Itoa(f.Offset))
	}

	if len(parts) == 0 {
		return "none"
	}

	return strings.Join(parts, " ")
}

// auditFilterValueMax is the longest filter value, in runes, that the
// audit-read log line reproduces. The filters arrive as query
// parameters, so without a cap one request could write a line of any
// length to the server log; the names, types and actions they match
// are all far shorter than this.
const auditFilterValueMax = 64

// capAuditFilterValue shortens a filter value to auditFilterValueMax
// runes for the log line, marking the cut with an ellipsis so that a
// shortened value is never mistaken for the whole of it. It counts
// runes rather than bytes so that a cut never splits a character.
func capAuditFilterValue(value string) string {
	runes := []rune(value)
	if len(runes) <= auditFilterValueMax {
		return value
	}
	return string(runes[:auditFilterValueMax-3]) + "..."
}

// auditScopeDenied is the message returned to a token whose admin
// scope does not reach the audit log.
const auditScopeDenied = "Permission denied: token admin scope does not " +
	"include audit access"

// auditTokenUnidentified is the message returned to a request that
// authenticated as an API token but reached the handler without the
// token's id, so that its admin scope could not be checked.
const auditTokenUnidentified = "Permission denied: the acting token could " +
	"not be identified, so its admin scope could not be checked"

// requireUnscopedTokenForAudit refuses a request made with an API token
// whose admin scope has been narrowed. A superuser's token inherits
// superuser rights, so requireSuperuser alone would let a token created
// for one narrow job read the whole installation's audit log; a scope
// that names specific permissions is an explicit statement that the
// token is not a general-purpose stand-in for its owner, and the audit
// log is not one of the permissions it can name. A token with no admin
// scope at all, or one holding the "*" wildcard, is unrestricted by
// design and passes.
//
// A scope lookup that fails is treated as a refusal rather than a pass,
// so that a database error cannot widen access, and so is a context
// that claims API-token authentication but carries no token id: a scope
// that cannot be looked up cannot be shown to include the audit log.
// Every authenticated path sets the id today, so that branch is
// unreachable; it exists for the path a later change mounts without
// createAuthWrapper, which must be refused rather than blessed.
func (h *RBACHandler) requireUnscopedTokenForAudit(w http.ResponseWriter,
	r *http.Request) bool {

	if !auth.IsAPITokenFromContext(r.Context()) {
		return true
	}

	tokenID := auth.GetTokenIDFromContext(r.Context())
	if tokenID == 0 {
		h.recordDenial(r, auditTokenUnidentified)
		RespondError(w, http.StatusForbidden, auditTokenUnidentified)
		return false
	}

	scope, err := h.authStore.GetTokenAdminScope(tokenID)
	if err != nil {
		// The underlying error can name internal SQL, so it is logged
		// rather than returned to the caller.
		log.Printf("[ERROR] Failed to read admin scope for token %d: %v",
			tokenID, err)
		h.recordDenial(r, auditScopeDenied)
		RespondError(w, http.StatusForbidden, auditScopeDenied)
		return false
	}

	if len(scope) == 0 {
		return true
	}
	for _, permission := range scope {
		if permission == auth.AdminPermissionWildcard {
			return true
		}
	}

	h.recordDenial(r, auditScopeDenied)
	RespondError(w, http.StatusForbidden, auditScopeDenied)

	return false
}

// parseAuditFilter converts the query string into an auth.AuditFilter.
// String filters are passed through untouched, because the store binds
// every value as a parameter; the numeric and timestamp filters are
// parsed here so that a malformed value is a 400 rather than a silently
// ignored filter.
func parseAuditFilter(q url.Values) (auth.AuditFilter, error) {
	filter := auth.AuditFilter{
		ActorName:  q.Get("actor"),
		ActorType:  q.Get("actor_type"),
		Action:     q.Get("action"),
		TargetType: q.Get("target_type"),
		Outcome:    q.Get("outcome"),
	}

	targetID, err := parseOptionalInt64(q, "target_id")
	if err != nil {
		return auth.AuditFilter{}, err
	}
	filter.TargetID = targetID

	if filter.Since, err = parseOptionalTime(q, "since"); err != nil {
		return auth.AuditFilter{}, err
	}
	if filter.Until, err = parseOptionalTime(q, "until"); err != nil {
		return auth.AuditFilter{}, err
	}

	if filter.Limit, err = parseNonNegativeInt(q, "limit"); err != nil {
		return auth.AuditFilter{}, err
	}
	if filter.Offset, err = parseNonNegativeInt(q, "offset"); err != nil {
		return auth.AuditFilter{}, err
	}

	return filter, nil
}

// parseOptionalInt64 reads an optional integer parameter, returning nil
// when it is absent or empty.
func parseOptionalInt64(q url.Values, name string) (*int64, error) {
	raw := q.Get(name)
	if raw == "" {
		return nil, nil
	}

	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s must be an integer", name)
	}
	return &value, nil
}

// parseNonNegativeInt reads an optional count parameter, returning 0
// when it is absent or empty. A negative value is rejected rather than
// clamped, so that a client sending nonsense finds out about it.
func parseNonNegativeInt(q url.Values, name string) (int, error) {
	raw := q.Get(name)
	if raw == "" {
		return 0, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must not be negative", name)
	}
	return value, nil
}

// parseOptionalTime reads an optional RFC 3339 timestamp parameter,
// returning nil when it is absent or empty. Only RFC 3339 is accepted,
// so a bare date or a local-format timestamp is an error rather than a
// filter that quietly matches the wrong rows.
func parseOptionalTime(q url.Values, name string) (*time.Time, error) {
	raw := q.Get(name)
	if raw == "" {
		return nil, nil
	}

	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("%s must be an RFC 3339 timestamp", name)
	}
	return &value, nil
}
