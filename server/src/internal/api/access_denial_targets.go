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
	"context"
	"net/http"
	"strconv"

	"github.com/pgedge/ai-workbench/server/internal/auth"
)

// The reasons recorded for a refused request on an object the caller
// cannot reach. The reason is part of the denial coalescer's key, so it
// must come from this fixed vocabulary and never name the object: a
// caller probing many connection ids would otherwise open a fresh key,
// and write a fresh audit row, for every id. The object goes in the
// row's target instead (issue #675).
const (
	connectionAccessDenied = "Permission denied: you do not have access to this connection"
	connectionWriteDenied  = "Permission denied: you do not have write access to this connection"
	alertAccessDenied      = "Permission denied: you do not have access to this alert"
	// systemAlertManageDenied is recorded when a caller may not act on
	// a system alert, which needs manage_alert_rules rather than access
	// to a connection. It matches the reason requirePermission records.
	systemAlertManageDenied = "Permission denied: requires " +
		auth.PermManageAlertRules + " permission"
)

// The audit actions recorded for refusals made through refuseTarget.
// Reads below a connection, such as its databases or its cluster, are
// recorded as reads of the connection.
const (
	deniedActionConnectionRead          = "connection.read"
	deniedActionConnectionClusterUpdate = "connection.cluster.update"
	deniedActionConnectionCurrentUpdate = "connection.current.update"
	deniedActionConnectionQuery         = "connection.query"
	deniedActionConnectionQueryWrite    = "connection.query.write"
	deniedActionConnectionQueryValidate = "connection.query.validate"
	deniedActionMetricsRead             = "metrics.read"
	deniedActionServerInfoRead          = "server_info.read"
	deniedActionAlertAcknowledge        = "alert.acknowledge"
	deniedActionAlertUnacknowledge      = "alert.unacknowledge"
	deniedActionAlertAnalysisUpdate     = "alert.analysis.update"
)

// deniedTargetKey is the context key under which refuseTarget passes
// the action and target of a refusal to the denial recorder.
type deniedTargetKey struct{}

// deniedTarget is the action and target a refusing handler names for
// the denial recorder, for a route whose object deniedRoute cannot
// read from the path, such as one named in a query parameter or a
// request body.
type deniedTarget struct {
	action string
	target string
}

// withDeniedTarget returns r carrying the action and target to record
// for its refusal.
func withDeniedTarget(r *http.Request, action, target string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), deniedTargetKey{},
		deniedTarget{action: action, target: target}))
}

// deniedTargetFrom returns the action and target a refusing handler
// attached to r with withDeniedTarget, if any.
func deniedTargetFrom(r *http.Request) (deniedTarget, bool) {
	d, ok := r.Context().Value(deniedTargetKey{}).(deniedTarget)
	return d, ok
}

// connectionDenialTarget names a connection as a denial target.
func connectionDenialTarget(connectionID int) string {
	return denialResourceTarget("connections", strconv.Itoa(connectionID))
}

// alertDenialTarget names an alert as a denial target.
func alertDenialTarget(alertID int64) string {
	return denialResourceTarget("alerts", strconv.FormatInt(alertID, 10))
}

// refuseTarget audits the refusal of action on target with reason,
// when a recorder is set, and answers 403 with message. reason must be
// one of the fixed reasons above, since it keys the coalescer; message
// is what the client sees and may name the object.
func (record denialRecorder) refuseTarget(w http.ResponseWriter,
	r *http.Request, action, target, reason, message string) {

	if record != nil {
		record(withDeniedTarget(r, action, target), reason)
	}
	RespondError(w, http.StatusForbidden, message)
}

// refuseConnection audits the refusal of action on a connection the
// caller cannot access, and answers 403 with message.
func (record denialRecorder) refuseConnection(w http.ResponseWriter,
	r *http.Request, action string, connectionID int, message string) {

	record.refuseTarget(w, r, action, connectionDenialTarget(connectionID),
		connectionAccessDenied, message)
}
