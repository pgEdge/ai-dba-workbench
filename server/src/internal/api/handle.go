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
	"errors"
	"log"
	"net/http"
)

// dbErrorMapping pairs a datastore sentinel with the 404 message that
// should be sent to the client when respondDBError encounters it. The
// helper iterates the supplied mappings in order and the first matching
// sentinel wins, so callers should list the most specific sentinel
// first.
type dbErrorMapping struct {
	Sentinel error
	Message  string
}

// notFound constructs a dbErrorMapping for the common "this sentinel
// means 404 with this message" case. It exists only to keep call sites
// terse and readable; respondDBError accepts mappings directly when a
// caller would rather build them inline.
func notFound(sentinel error, message string) dbErrorMapping {
	return dbErrorMapping{Sentinel: sentinel, Message: message}
}

// respondDBError writes a standardized HTTP response for a datastore
// error and reports whether anything was written. Callers use the
// returned bool as the loop guard:
//
//	if respondDBError(w, err, "fetch widget",
//	    notFound(database.ErrWidgetNotFound, "Widget not found")) {
//	    return
//	}
//
// When err is nil the helper returns false and writes nothing so the
// caller can continue. Otherwise it consults the supplied mappings in
// order: the first sentinel that matches errors.Is yields a 404 with
// the mapping's Message. If no mapping matches, the helper logs
// "[ERROR] Failed to <internalVerb>: <err>" and writes a 500 with the
// body "Failed to <internalVerb>".
//
// internalVerb must match the phrasing the unrefactored handler used;
// the log line and 500 body are constructed verbatim from it so the
// HTTP surface and log output stay byte-identical across the refactor.
// Pass zero mappings to skip the 404 path entirely; in that case every
// non-nil error becomes a 500.
//
// The helper emits a fixed log line shape, so call sites whose original
// log line decorated the verb with formatted arguments (for example
// `log.Printf("Failed to add server %d to cluster %d: %v", ...)`) must
// keep the manual form; folding them into respondDBError would silently
// drop the %d identifiers and weaken the log record.
func respondDBError(w http.ResponseWriter, err error, internalVerb string, mappings ...dbErrorMapping) bool {
	if err == nil {
		return false
	}
	for _, m := range mappings {
		if m.Sentinel != nil && errors.Is(err, m.Sentinel) {
			RespondError(w, http.StatusNotFound, m.Message)
			return true
		}
	}
	log.Printf("[ERROR] Failed to %s: %v", internalVerb, err)
	RespondError(w, http.StatusInternalServerError, "Failed to "+internalVerb)
	return true
}

// decodeBody is a generic wrapper around DecodeJSONBody that returns
// the decoded value rather than requiring callers to declare a
// zero-valued destination first. On a successful decode it returns the
// value and true; on a malformed body it sends a 400 response (via the
// underlying DecodeJSONBody helper) and returns the zero value and
// false. Use this where it reads cleaner than DecodeJSONBody; both
// helpers coexist deliberately so existing call sites do not need to
// churn.
func decodeBody[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var dest T
	if !DecodeJSONBody(w, r, &dest) {
		var zero T
		return zero, false
	}
	return dest, true
}
