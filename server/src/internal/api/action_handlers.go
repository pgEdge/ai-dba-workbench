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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/pkg/redact"
	"github.com/pgedge/ai-workbench/server/internal/actions"
	"github.com/pgedge/ai-workbench/server/internal/apiconst"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/metrics"
)

// Limits on the fields of a proposal, matching those the propose_action
// tool applies.
const (
	actionMaxSQLBytes       = 256 * 1024
	actionMaxRationaleBytes = 16 * 1024
	actionMaxConversationID = 128
	actionMaxLinkIDs        = 100
)

// actionSources are the values a REST caller may give for "source". The
// "chat_tool" source is reserved for the propose_action tool, so a
// proposal's origin cannot be claimed by another route.
var actionSources = map[string]bool{
	"chat_block":      true,
	"alert_analysis":  true,
	"query_analysis":  true,
	"server_analysis": true,
	"chart_analysis":  true,
}

// Audit actions and reasons recorded when an action endpoint refuses a
// request, from the same fixed vocabulary as the other refusals.
const (
	deniedActionActionCreate  = "action.create"
	deniedActionActionExecute = "action.execute"
	deniedActionActionCancel  = "action.cancel"

	actionOverrideDenied = "Permission denied: overriding a veto requires a signed-in session"
)

// actionNotFound and connectionNotFound are the messages for a missing
// object. A connection the caller cannot see gets the same answer as one
// that does not exist, so ids cannot be probed.
const (
	actionNotFound       = "Action not found"
	connectionNotFound   = "Connection not found"
	conversationNotFound = "Conversation not found"
	alertNotFound        = "Alert not found"
)

// actionStore is the part of actions.Store the handler uses.
type actionStore interface {
	Get(ctx context.Context, id int64) (*actions.Action, error)
	Approve(ctx context.Context, id int64, approval, by, via string) error
	Reapprove(ctx context.Context, id int64, approval, previousBy, by, via string) error
	LinkConversation(ctx context.Context, ids []int64, conversationID, username string) error
}

// actionService is the part of actions.Service the handler uses.
type actionService interface {
	Propose(ctx context.Context, req actions.ProposeRequest) (*actions.ProposeResult, error)
	CheckOverride(ctx context.Context, a *actions.Action, confirm, connectionName string) error
}

// actionExecutor is the part of actions.Executor the handler uses.
type actionExecutor interface {
	Start(ctx context.Context, actionID int64) error
	Cancel(actionID int64) bool
}

// actionConnections looks up a monitored connection.
type actionConnections interface {
	GetConnection(ctx context.Context, id int) (*database.MonitoredConnection, error)
	GetAlertConnectionID(ctx context.Context, alertID int64) (*int, error)
}

// actionAccess is the part of auth.RBACChecker the handler uses.
type actionAccess interface {
	CanAccessConnection(ctx context.Context, connectionID int) (bool, string)
	HasWriteAccess(ctx context.Context, connectionID int) bool
	// IsSuperuser is the scope-aware superuser check: a token narrowed
	// to particular connections or tools does not carry it.
	IsSuperuser(ctx context.Context) bool
}

// ConversationOwnership reports whether the named user owns the
// conversation. The handler cannot import the conversations package, so
// the server supplies this.
type ConversationOwnership func(ctx context.Context, conversationID, username string) (bool, error)

// ActionHandler serves the proposed-action endpoints.
type ActionHandler struct {
	store         actionStore
	service       actionService
	executor      actionExecutor
	connections   actionConnections
	access        actionAccess
	owns          ConversationOwnership
	snapshotLimit int

	// denialRecorder audits refused requests; it may be nil.
	denialRecorder denialRecorder
}

// NewActionHandler builds the handler. snapshotLimit is the most bytes
// of the caller's context stored with a proposal.
func NewActionHandler(store actionStore, service actionService,
	executor actionExecutor, connections actionConnections,
	access actionAccess, owns ConversationOwnership,
	snapshotLimit int) *ActionHandler {

	return &ActionHandler{
		store: store, service: service, executor: executor,
		connections: connections, access: access, owns: owns,
		snapshotLimit: snapshotLimit,
	}
}

// SetDenialRecorder installs the function used to audit refused
// requests; the server wires it to RBACHandler.RecordDenial.
func (h *ActionHandler) SetDenialRecorder(record func(*http.Request, string)) {
	h.denialRecorder = record
}

// RegisterRoutes registers the action endpoints. The method-qualified
// patterns are more specific than the connection and conversation
// subtree registrations, so they take precedence over them.
func (h *ActionHandler) RegisterRoutes(mux *http.ServeMux,
	authWrapper func(http.HandlerFunc) http.HandlerFunc) {

	mux.HandleFunc("POST /api/v1/connections/{id}/actions", authWrapper(h.handleCreate))
	mux.HandleFunc("GET /api/v1/actions/{id}", authWrapper(h.handleGet))
	mux.HandleFunc("POST /api/v1/actions/{id}/execute", authWrapper(h.handleExecute))
	mux.HandleFunc("POST /api/v1/actions/{id}/cancel", authWrapper(h.handleCancel))
	mux.HandleFunc("POST /api/v1/conversations/{id}/actions/link", authWrapper(h.handleLink))
}

// createActionRequest is the body of a proposal.
type createActionRequest struct {
	SQL            string  `json:"sql"`
	DatabaseName   string  `json:"database_name"`
	Rationale      string  `json:"rationale"`
	Source         string  `json:"source"`
	Context        string  `json:"context"`
	AlertID        *int64  `json:"alert_id"`
	ConversationID *string `json:"conversation_id"`
	Execute        bool    `json:"execute"`
}

// actionResponse is an action together with the review chain state.
type actionResponse struct {
	*actions.Action
	AttemptsLeft   int             `json:"attempts_left"`
	ChainExhausted bool            `json:"chain_exhausted"`
	BestAttempt    *actions.Action `json:"best_attempt,omitempty"`
	// ExecuteError is set when the caller asked for the action to run
	// on creation and it could not be approved or started; the action
	// is still recorded and can be executed from its own endpoint.
	ExecuteError string `json:"execute_error,omitempty"`
}

// executeActionRequest is the body of an execute request.
type executeActionRequest struct {
	Override bool   `json:"override"`
	Confirm  string `json:"confirm"`
}

// linkActionsRequest is the body of a conversation link request.
type linkActionsRequest struct {
	ActionIDs []int64 `json:"action_ids"`
}

// pathID parses the {id} path value as a positive integer.
func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// actorVia returns the channel the request arrived on.
func actorVia(ctx context.Context) string {
	if auth.IsAPITokenFromContext(ctx) {
		return "token"
	}
	return "session"
}

// refuseWrite audits and answers a 403 for a caller with no write access
// to the connection.
func (h *ActionHandler) refuseWrite(w http.ResponseWriter, r *http.Request,
	action string, connectionID int) {

	h.denialRecorder.refuseTarget(w, r, action,
		connectionDenialTarget(connectionID), connectionWriteDenied,
		connectionWriteDenied)
}

// truncateUTF8 shortens s to at most limit bytes without splitting a
// character, and reports whether it did.
func truncateUTF8(s string, limit int) (string, bool) {
	if limit < 0 || len(s) <= limit {
		return s, false
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}

// proposalEvidence builds the evidence stored with a proposal from the
// caller's context, shortened to the configured limit.
func (h *ActionHandler) proposalEvidence(snapshot string) json.RawMessage {
	if snapshot == "" {
		return nil
	}
	text, truncated := truncateUTF8(snapshot, h.snapshotLimit)
	// A string and a bool always marshal, so the error cannot occur.
	b, _ := json.Marshal( //nolint:errcheck // a string and a bool always marshal
		map[string]any{
			"context":           text,
			"context_truncated": truncated,
		})
	return b
}

// validateCreate checks a proposal body, returning the message for a 400
// when it is not acceptable.
func validateCreate(req *createActionRequest) string {
	if strings.TrimSpace(req.SQL) == "" {
		return "sql is required"
	}
	if len(req.SQL) > actionMaxSQLBytes {
		return "sql is too large"
	}
	if len(req.Rationale) > actionMaxRationaleBytes {
		return "rationale is too long"
	}
	if !actionSources[req.Source] {
		return "source is required and must be one of chat_block, " +
			"alert_analysis, query_analysis, server_analysis or chart_analysis"
	}
	if req.DatabaseName != "" {
		if err := database.ValidateDatabaseName(req.DatabaseName); err != nil {
			return "Invalid database name: " + err.Error()
		}
	}
	if req.ConversationID != nil &&
		(*req.ConversationID == "" || len(*req.ConversationID) > actionMaxConversationID) {
		return "conversation_id is invalid"
	}
	if req.AlertID != nil && *req.AlertID <= 0 {
		return "alert_id is invalid"
	}
	return ""
}

// proposeErrorStatus maps a Propose error to a status and message.
func proposeErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, actions.ErrEmptyProposal):
		return http.StatusBadRequest, "The SQL contains no statements"
	case errors.Is(err, actions.ErrNoWrites):
		return http.StatusBadRequest, "The SQL contains no writes; run read-only queries with the query endpoint"
	case errors.Is(err, actions.ErrTransactionControl):
		return http.StatusBadRequest, "The SQL must not contain transaction control; the executor manages transactions"
	case errors.Is(err, actions.ErrTimeoutOverride):
		return http.StatusBadRequest, "The SQL must not change lock_timeout or statement_timeout or reset session settings"
	case errors.Is(err, actions.ErrBadRevision):
		return http.StatusBadRequest, "The proposal cannot revise that action"
	case errors.Is(err, actions.ErrChainExhausted):
		return http.StatusConflict, "The maximum number of attempts for this change has been reached"
	}
	log.Printf("[ERROR] Propose action failed: %v", err)
	return http.StatusInternalServerError, "The proposal could not be recorded or reviewed"
}

// handleCreate serves POST /api/v1/connections/{id}/actions.
func (h *ActionHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	connectionID, ok := h.writableConnection(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	var req createActionRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}
	if msg := validateCreate(&req); msg != "" {
		RespondError(w, http.StatusBadRequest, msg)
		return
	}
	if !h.checkCreateLinks(ctx, w, &req, connectionID) {
		return
	}

	res, err := h.service.Propose(ctx, h.proposeRequest(&req, connectionID))
	if err != nil {
		status, msg := proposeErrorStatus(err)
		RespondError(w, status, msg)
		return
	}

	action := res.Action
	executeError := ""
	if req.Execute && action.ReviewStatus == "passed" {
		action, executeError = h.startIfStillWritable(r, action, connectionID)
	}

	RespondJSON(w, http.StatusCreated, actionResponse{
		Action:         action,
		AttemptsLeft:   res.AttemptsLeft,
		ChainExhausted: res.ChainExhausted,
		BestAttempt:    res.BestAttempt,
		ExecuteError:   executeError,
	})
}

// writableConnection reads the connection named in the path and checks
// the caller may write to it, answering the refusal itself. It answers
// 404 for an id out of range, a connection the caller cannot see and
// one that does not exist, then checks write access last, so that
// visibility is decided before anything about write access is revealed.
func (h *ActionHandler) writableConnection(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, ok := pathID(r)
	if !ok || id > int64(^uint32(0)>>1) {
		RespondError(w, http.StatusNotFound, connectionNotFound)
		return 0, false
	}
	connectionID := int(id)
	ctx := r.Context()

	if canAccess, _ := h.access.CanAccessConnection(ctx, connectionID); !canAccess {
		RespondError(w, http.StatusNotFound, connectionNotFound)
		return 0, false
	}
	if _, err := h.connections.GetConnection(ctx, connectionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			RespondError(w, http.StatusNotFound, connectionNotFound)
			return 0, false
		}
		log.Printf("[ERROR] Get connection %d failed: %v", connectionID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to read the connection")
		return 0, false
	}
	if !h.access.HasWriteAccess(ctx, connectionID) {
		h.refuseWrite(w, r, deniedActionActionCreate, connectionID)
		return 0, false
	}
	return connectionID, true
}

// checkCreateLinks checks the conversation and the alert a proposal
// links to, in that order, answering the refusal itself.
func (h *ActionHandler) checkCreateLinks(ctx context.Context, w http.ResponseWriter,
	req *createActionRequest, connectionID int) bool {

	if req.ConversationID != nil &&
		!h.ownsConversation(ctx, w, *req.ConversationID) {
		return false
	}
	if req.AlertID != nil && !h.alertOnConnection(ctx, w, *req.AlertID, connectionID) {
		return false
	}
	return true
}

// proposeRequest builds the service request for a validated create
// request.
func (h *ActionHandler) proposeRequest(req *createActionRequest, connectionID int) actions.ProposeRequest {
	preq := actions.ProposeRequest{
		ConnectionID:   connectionID,
		SQL:            req.SQL,
		Rationale:      req.Rationale,
		Source:         req.Source,
		ConversationID: req.ConversationID,
		AlertID:        req.AlertID,
		Evidence:       h.proposalEvidence(req.Context),
	}
	if req.DatabaseName != "" {
		name := req.DatabaseName
		preq.DatabaseName = &name
	}
	return preq
}

// ownsConversation answers 404 and returns false unless the caller owns
// the conversation. A conversation that is missing and one that belongs
// to someone else get the same answer.
func (h *ActionHandler) ownsConversation(ctx context.Context, w http.ResponseWriter,
	conversationID string) bool {

	username := auth.GetUsernameFromContext(ctx)
	if username == "" || conversationID == "" || len(conversationID) > actionMaxConversationID {
		RespondError(w, http.StatusNotFound, conversationNotFound)
		return false
	}
	owned := false
	if h.owns != nil {
		var err error
		owned, err = h.owns(ctx, conversationID, username)
		if err != nil {
			log.Printf("[ERROR] Conversation ownership check failed: %v", err)
			RespondError(w, http.StatusInternalServerError, "Failed to check the conversation")
			return false
		}
	}
	if !owned {
		RespondError(w, http.StatusNotFound, conversationNotFound)
		return false
	}
	return true
}

// alertOnConnection answers 404 and returns false unless the alert
// exists and belongs to the connection the action is proposed for. The
// caller can see that connection, so the alert is visible to them too; a
// missing alert, a system alert and one on another connection get the
// same answer.
func (h *ActionHandler) alertOnConnection(ctx context.Context, w http.ResponseWriter,
	alertID int64, connectionID int) bool {

	got, err := h.connections.GetAlertConnectionID(ctx, alertID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		RespondError(w, http.StatusNotFound, alertNotFound)
		return false
	case err != nil:
		log.Printf("[ERROR] Get alert %d failed: %v", alertID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to read the alert")
		return false
	case got == nil || *got != connectionID:
		RespondError(w, http.StatusNotFound, alertNotFound)
		return false
	}
	return true
}

// startIfStillWritable checks write access again and then approves and
// starts the action. The review runs between the first check and this
// one and can take a while, so a grant removed in the meantime must stop
// the run. A refusal is audited and reported through execute_error, as
// any other failure to start is: the action stays recorded, reviewed and
// unapproved, and nothing runs.
func (h *ActionHandler) startIfStillWritable(r *http.Request, a *actions.Action,
	connectionID int) (*actions.Action, string) {

	if !h.access.HasWriteAccess(r.Context(), connectionID) {
		if h.denialRecorder != nil {
			h.denialRecorder(withDeniedTarget(r, deniedActionActionCreate,
				connectionDenialTarget(connectionID)), connectionWriteDenied)
		}
		return a, connectionWriteDenied
	}
	return h.approveAndStart(r.Context(), a)
}

// approveAndStart records the caller's approval of a reviewed action and
// starts it, returning the action as it then stands and, when it could
// not be approved or started, a short reason for the response. The
// action is left as proposed (or approved) and can still be executed
// from its own endpoint where that is allowed.
func (h *ActionHandler) approveAndStart(ctx context.Context, a *actions.Action) (*actions.Action, string) {
	by := auth.GetUsernameFromContext(ctx)
	failure := ""
	if err := h.store.Approve(ctx, a.ID, "approved", by, actorVia(ctx)); err != nil {
		log.Printf("[ERROR] Approve action %d failed: %v", a.ID, err)
		return a, "The action could not be approved"
	}
	if err := h.executor.Start(ctx, a.ID); err != nil {
		log.Printf("[ERROR] Start action %d failed: %v", a.ID, err)
		failure = "The action was approved but could not be started"
		switch {
		case errors.Is(err, actions.ErrShuttingDown):
			failure = "The action was approved but the server is shutting down"
		case errors.Is(err, actions.ErrTooManyPending):
			failure = "The action was approved but the server is busy; try again shortly"
		}
	}
	if fresh, err := h.store.Get(ctx, a.ID); err == nil {
		return fresh, failure
	}
	return a, failure
}

// viewFor returns the action as the caller may see it, never changing a.
// The evidence holds the context the proposer supplied, and the
// conversation id links to their private chat, so only the proposer and
// a superuser (scope-aware, so not through a narrowed token) get them.
//
// The statement text, and what the run reported, may hold a password
// (ALTER ROLE ... PASSWORD, say), and anyone with read access to the
// connection can read the action, so only the proposer and the approver,
// who decided to run that text, see it as stored. Everyone else, a
// superuser included, gets it masked as the other routes that return
// statement text mask it (GitHub issues #670 and #724), with the
// rationale and result rows withheld; see maskActionText.
func (h *ActionHandler) viewFor(ctx context.Context, a *actions.Action) *actions.Action {
	username := auth.GetUsernameFromContext(ctx)
	proposer := username != "" && username == a.ProposedBy
	approver := username != "" && a.ApprovedBy != nil && *a.ApprovedBy == username
	c := *a
	if !proposer && !h.access.IsSuperuser(ctx) {
		c.Evidence = nil
		c.ConversationID = nil
	}
	if !proposer && !approver {
		maskActionText(&c)
	}
	return &c
}

// withheldResultError replaces every result error of an action any of
// whose statements held a credential, for anyone but the proposer and
// the approver.
const withheldResultError = "error withheld: the action holds a credential"

// maskActionText replaces every text field of a that may hold a secret
// with a masked copy, or withholds it, leaving the slices a shared with
// the stored action untouched. Statement text goes through
// metrics.RedactStatementText, and the review's reasons and the run's
// errors through redact.Text. When any statement of the action held a
// credential, every result error is replaced with withheldResultError
// instead, since PostgreSQL quotes the offending literal in errors such
// as a failed cast, in a form redact.Text cannot tell from any other
// quoted value, and a later statement can read back a value an earlier
// one wrote.
// The rationale and the rows a statement returned are withheld (nil)
// instead: the rationale is prose, which can state a password in words
// redact.Text cannot detect ("the new password is ..."), and a result
// cell can hold a bare credential under any column name, so neither can
// be masked reliably. The column names
// are kept, so the caller can see that rows were returned. The preview,
// undo, verification and outcome documents, which nothing writes yet,
// are withheld too, since their masking cannot be known until something
// does.
func maskActionText(a *actions.Action) {
	credential := holdsCredential(a)
	a.Statements = mapStrings(a.Statements, metrics.RedactStatementText)
	a.Rationale = nil
	if a.Review != nil {
		v := *a.Review
		v.Reasons = mapStrings(v.Reasons, redact.Text)
		a.Review = &v
	}
	if a.Results != nil {
		results := make([]actions.StatementResult, len(a.Results))
		for i := range a.Results {
			res := a.Results[i]
			switch {
			case res.Error == "":
			case credential:
				res.Error = withheldResultError
			default:
				res.Error = redact.Text(res.Error)
			}
			res.Statement = metrics.RedactStatementText(res.Statement)
			res.Rows = nil
			res.Truncated = false
			results[i] = res
		}
		a.Results = results
	}
	a.Preview, a.Undo, a.Verification, a.Outcome = nil, nil, nil, nil
}

// holdsCredential reports whether metrics.RedactStatementText masks any
// statement of a, in its statements or its results.
func holdsCredential(a *actions.Action) bool {
	for _, s := range a.Statements {
		if metrics.RedactStatementText(s) != s {
			return true
		}
	}
	for i := range a.Results {
		if s := a.Results[i].Statement; metrics.RedactStatementText(s) != s {
			return true
		}
	}
	return false
}

// mapStrings returns a new slice holding f of each element of in, or nil
// for a nil in.
func mapStrings(in []string, f func(string) string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = f(s)
	}
	return out
}

// loadVisibleAction reads the action named in the path. It answers 404,
// with one message, when the action does not exist or its connection is
// not visible to the caller, and returns nil.
func (h *ActionHandler) loadVisibleAction(w http.ResponseWriter, r *http.Request) *actions.Action {
	id, ok := pathID(r)
	if !ok {
		RespondError(w, http.StatusNotFound, actionNotFound)
		return nil
	}
	a, err := h.store.Get(r.Context(), id)
	if errors.Is(err, actions.ErrNotFound) {
		RespondError(w, http.StatusNotFound, actionNotFound)
		return nil
	}
	if err != nil {
		log.Printf("[ERROR] Get action %d failed: %v", id, err)
		RespondError(w, http.StatusInternalServerError, "Failed to read the action")
		return nil
	}
	if canAccess, _ := h.access.CanAccessConnection(r.Context(), a.ConnectionID); !canAccess {
		RespondError(w, http.StatusNotFound, actionNotFound)
		return nil
	}
	return a
}

// handleGet serves GET /api/v1/actions/{id}.
func (h *ActionHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	a := h.loadVisibleAction(w, r)
	if a == nil {
		return
	}
	RespondJSON(w, http.StatusOK, h.viewFor(r.Context(), a))
}

// decodeOptionalBody decodes a JSON body into dest, accepting an empty
// one. It answers 400 and returns false for a malformed body.
func decodeOptionalBody(w http.ResponseWriter, r *http.Request, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, apiconst.MaxRequestBodySize)
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil && !errors.Is(err, io.EOF) {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return false
	}
	return true
}

// handleExecute serves POST /api/v1/actions/{id}/execute. Write access
// to the connection is checked first and again for an override, before
// anything about the action changes, so a caller who has lost access
// cannot start it and an override is never recorded for one.
func (h *ActionHandler) handleExecute(w http.ResponseWriter, r *http.Request) {
	a := h.loadVisibleAction(w, r)
	if a == nil {
		return
	}
	ctx := r.Context()
	if !h.access.HasWriteAccess(ctx, a.ConnectionID) {
		h.refuseWrite(w, r, deniedActionActionExecute, a.ConnectionID)
		return
	}

	var req executeActionRequest
	if !decodeOptionalBody(w, r, &req) {
		return
	}

	approval, ok := h.executeApproval(w, r, a, &req)
	if !ok {
		return
	}
	if !h.approveForExecute(ctx, w, a, approval) {
		return
	}
	if !h.startExecution(ctx, w, a.ID) {
		return
	}

	fresh, err := h.store.Get(ctx, a.ID)
	if err != nil {
		fresh = a
	}
	RespondJSON(w, http.StatusAccepted, h.viewFor(ctx, fresh))
}

// executeApproval decides the kind of approval an execute request
// records, answering the refusal itself. An override must pass the
// override rules; otherwise the action must have passed review.
func (h *ActionHandler) executeApproval(w http.ResponseWriter, r *http.Request,
	a *actions.Action, req *executeActionRequest) (string, bool) {

	if req.Override {
		if !h.checkOverride(w, r, a, req.Confirm) {
			return "", false
		}
		return "overridden", true
	}
	if a.ReviewStatus != "passed" {
		RespondError(w, http.StatusConflict,
			"The action has not passed review; it cannot be executed without an override")
		return "", false
	}
	return "approved", true
}

// isExecuteRetry reports whether an execute request retries an earlier
// decision rather than making a new one. An action approved or
// overridden earlier whose start failed or never happened is retried
// without deciding again, but only by whoever decided it or a superuser
// (scope-aware, so not through a narrowed token), and only through the
// same kind of request: an overridden action is retried as an override,
// so the override rules have been applied again. Anyone else falls
// through to Approve, which refuses it. The claim in Start stays
// the atomic gate either way.
func (h *ActionHandler) isExecuteRetry(ctx context.Context, a *actions.Action, approval, by string) bool {
	if a.Approval != approval || a.ExecStatus != "not_started" {
		return false
	}
	if by != "" && a.ApprovedBy != nil && *a.ApprovedBy == by {
		return true
	}
	return h.access.IsSuperuser(ctx)
}

// approveForExecute records the approval, answering the refusal itself.
// A retry by whoever decided records nothing new; a superuser's retry of
// someone else's decision records the superuser as the approver, since
// the run is then theirs.
func (h *ActionHandler) approveForExecute(ctx context.Context, w http.ResponseWriter,
	a *actions.Action, approval string) bool {

	by := auth.GetUsernameFromContext(ctx)
	var err error
	switch {
	case !h.isExecuteRetry(ctx, a, approval, by):
		err = h.store.Approve(ctx, a.ID, approval, by, actorVia(ctx))
	case a.ApprovedBy != nil && *a.ApprovedBy != by:
		err = h.store.Reapprove(ctx, a.ID, approval, *a.ApprovedBy, by, actorVia(ctx))
	}
	if err == nil {
		return true
	}
	if errors.Is(err, actions.ErrInvalidState) || errors.Is(err, actions.ErrNotFound) {
		RespondError(w, http.StatusConflict, "The action has already been approved or started")
		return false
	}
	log.Printf("[ERROR] Approve action %d failed: %v", a.ID, err)
	RespondError(w, http.StatusInternalServerError, "Failed to approve the action")
	return false
}

// actionStartRetryAfter is the Retry-After, in seconds, sent when too
// many action claims are unsettled; it matches the longest delay between
// attempts to settle one.
const actionStartRetryAfter = "30"

// startExecution starts an approved action, answering the refusal
// itself.
func (h *ActionHandler) startExecution(ctx context.Context, w http.ResponseWriter, id int64) bool {
	switch err := h.executor.Start(ctx, id); {
	case err == nil:
		return true
	case errors.Is(err, actions.ErrAlreadyStarted):
		RespondError(w, http.StatusConflict, "The action has already been started or cannot be started")
	case errors.Is(err, actions.ErrShuttingDown):
		RespondError(w, http.StatusServiceUnavailable, "The server is shutting down")
	case errors.Is(err, actions.ErrTooManyPending):
		w.Header().Set("Retry-After", actionStartRetryAfter)
		RespondError(w, http.StatusServiceUnavailable, "The server is busy; try again shortly")
	default:
		log.Printf("[ERROR] Start action %d failed: %v", id, err)
		RespondError(w, http.StatusInternalServerError, "Failed to start the action")
	}
	return false
}

// checkOverride applies the override rules and answers the refusal, so
// it returns true only when the override may go ahead. The caller has
// already checked write access; the override is session-only, and the
// service checks that the action was vetoed and the connection's name
// typed exactly.
func (h *ActionHandler) checkOverride(w http.ResponseWriter, r *http.Request,
	a *actions.Action, confirm string) bool {

	ctx := r.Context()
	if auth.IsAPITokenFromContext(ctx) {
		h.denialRecorder.refuseTarget(w, r, deniedActionActionExecute,
			connectionDenialTarget(a.ConnectionID), actionOverrideDenied,
			actionOverrideDenied)
		return false
	}
	if !h.access.HasWriteAccess(ctx, a.ConnectionID) {
		h.refuseWrite(w, r, deniedActionActionExecute, a.ConnectionID)
		return false
	}
	conn, err := h.connections.GetConnection(ctx, a.ConnectionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			RespondError(w, http.StatusNotFound, actionNotFound)
			return false
		}
		log.Printf("[ERROR] Get connection %d failed: %v", a.ConnectionID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to read the connection")
		return false
	}
	switch err := h.service.CheckOverride(ctx, a, confirm, conn.Name); {
	case err == nil:
		return true
	case errors.Is(err, actions.ErrConfirmMismatch):
		RespondError(w, http.StatusBadRequest,
			"The confirmation does not match the connection name")
	case errors.Is(err, actions.ErrOverrideNotAllowed):
		RespondError(w, http.StatusConflict,
			"Only an action that failed review can be overridden")
	default:
		log.Printf("[ERROR] Check override for action %d failed: %v", a.ID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to check the override")
	}
	return false
}

// handleCancel serves POST /api/v1/actions/{id}/cancel.
func (h *ActionHandler) handleCancel(w http.ResponseWriter, r *http.Request) {
	a := h.loadVisibleAction(w, r)
	if a == nil {
		return
	}
	if !h.access.HasWriteAccess(r.Context(), a.ConnectionID) {
		h.refuseWrite(w, r, deniedActionActionCancel, a.ConnectionID)
		return
	}
	if !h.executor.Cancel(a.ID) {
		RespondError(w, http.StatusConflict, "The action is not running")
		return
	}
	RespondJSON(w, http.StatusAccepted, map[string]any{
		"id":               a.ID,
		"cancel_requested": true,
	})
}

// handleLink serves POST /api/v1/conversations/{id}/actions/link. It
// attaches the caller's own unlinked actions to a conversation the
// caller owns.
func (h *ActionHandler) handleLink(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	username := auth.GetUsernameFromContext(ctx)
	conversationID := r.PathValue("id")
	if !h.ownsConversation(ctx, w, conversationID) {
		return
	}

	var req linkActionsRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}
	if len(req.ActionIDs) == 0 || len(req.ActionIDs) > actionMaxLinkIDs {
		RespondError(w, http.StatusBadRequest, "action_ids must hold between 1 and 100 ids")
		return
	}
	for _, id := range req.ActionIDs {
		if id <= 0 {
			RespondError(w, http.StatusBadRequest, "action_ids must be positive")
			return
		}
	}
	if err := h.store.LinkConversation(ctx, req.ActionIDs, conversationID, username); err != nil {
		log.Printf("[ERROR] Link actions failed: %v", err)
		RespondError(w, http.StatusInternalServerError, "Failed to link the actions")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// NewActionPoolOpener returns the Opener the action executor uses to
// reach a monitored database. It builds the connection exactly as the
// query endpoint does, through the same dial guard, but does not mark
// the session read-only, since actions write.
func NewActionPoolOpener(ds *database.Datastore) actions.Opener {
	return func(ctx context.Context, connectionID int, databaseName *string) (*pgxpool.Pool, error) {
		conn, password, err := ds.GetConnectionWithPassword(ctx, connectionID)
		if err != nil {
			return nil, fmt.Errorf("load connection %d: %w", connectionID, err)
		}
		name := ""
		if databaseName != nil {
			name = *databaseName
		}
		connStr, err := ds.BuildConnectionString(conn, password, name)
		if err != nil {
			return nil, fmt.Errorf("build connection string: %w", err)
		}
		return openQueryPool(ctx, connStr, "pgEdge AI DBA Workbench - Action",
			ds.GuardMonitoredConnConfig)
	}
}
