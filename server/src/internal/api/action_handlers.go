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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgedge/ai-workbench/server/internal/actions"
	"github.com/pgedge/ai-workbench/server/internal/apiconst"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
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
)

// actionStore is the part of actions.Store the handler uses.
type actionStore interface {
	Get(ctx context.Context, id int64) (*actions.Action, error)
	Approve(ctx context.Context, id int64, approval, by, via string) error
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
}

// actionAccess is the part of auth.RBACChecker the handler uses.
type actionAccess interface {
	CanAccessConnection(ctx context.Context, connectionID int) (bool, string)
	HasWriteAccess(ctx context.Context, connectionID int) bool
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
	id, ok := pathID(r)
	if !ok || id > int64(^uint32(0)>>1) {
		RespondError(w, http.StatusNotFound, connectionNotFound)
		return
	}
	connectionID := int(id)
	ctx := r.Context()

	if canAccess, _ := h.access.CanAccessConnection(ctx, connectionID); !canAccess {
		RespondError(w, http.StatusNotFound, connectionNotFound)
		return
	}
	if _, err := h.connections.GetConnection(ctx, connectionID); err != nil {
		RespondError(w, http.StatusNotFound, connectionNotFound)
		return
	}
	if !h.access.HasWriteAccess(ctx, connectionID) {
		h.refuseWrite(w, r, deniedActionActionCreate, connectionID)
		return
	}

	var req createActionRequest
	if !DecodeJSONBody(w, r, &req) {
		return
	}
	if msg := validateCreate(&req); msg != "" {
		RespondError(w, http.StatusBadRequest, msg)
		return
	}

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

	res, err := h.service.Propose(ctx, preq)
	if err != nil {
		status, msg := proposeErrorStatus(err)
		RespondError(w, status, msg)
		return
	}

	action := res.Action
	if req.Execute && action.ReviewStatus == "passed" {
		action = h.approveAndStart(ctx, action)
	}

	RespondJSON(w, http.StatusCreated, actionResponse{
		Action:         action,
		AttemptsLeft:   res.AttemptsLeft,
		ChainExhausted: res.ChainExhausted,
		BestAttempt:    res.BestAttempt,
	})
}

// approveAndStart records the caller's approval of a reviewed action and
// starts it, returning the action as it then stands. A failure is
// logged and leaves the action as proposed: it can still be executed
// from its own endpoint.
func (h *ActionHandler) approveAndStart(ctx context.Context, a *actions.Action) *actions.Action {
	by := auth.GetUsernameFromContext(ctx)
	if err := h.store.Approve(ctx, a.ID, "approved", by, actorVia(ctx)); err != nil {
		log.Printf("[ERROR] Approve action %d failed: %v", a.ID, err)
		return a
	}
	if err := h.executor.Start(ctx, a.ID); err != nil {
		log.Printf("[ERROR] Start action %d failed: %v", a.ID, err)
	}
	if fresh, err := h.store.Get(ctx, a.ID); err == nil {
		return fresh
	}
	return a
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
	RespondJSON(w, http.StatusOK, a)
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

	approval := "approved"
	if req.Override {
		if !h.checkOverride(w, r, a, req.Confirm) {
			return
		}
		approval = "overridden"
	} else if a.ReviewStatus != "passed" {
		RespondError(w, http.StatusConflict,
			"The action has not passed review; it cannot be executed without an override")
		return
	}

	by := auth.GetUsernameFromContext(ctx)
	if err := h.store.Approve(ctx, a.ID, approval, by, actorVia(ctx)); err != nil {
		if errors.Is(err, actions.ErrNotFound) {
			RespondError(w, http.StatusConflict, "The action has already been started")
			return
		}
		log.Printf("[ERROR] Approve action %d failed: %v", a.ID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to approve the action")
		return
	}

	switch err := h.executor.Start(ctx, a.ID); {
	case err == nil:
	case errors.Is(err, actions.ErrAlreadyStarted):
		RespondError(w, http.StatusConflict, "The action has already been started or cannot be started")
		return
	case errors.Is(err, actions.ErrShuttingDown):
		RespondError(w, http.StatusServiceUnavailable, "The server is shutting down")
		return
	default:
		log.Printf("[ERROR] Start action %d failed: %v", a.ID, err)
		RespondError(w, http.StatusInternalServerError, "Failed to start the action")
		return
	}

	fresh, err := h.store.Get(ctx, a.ID)
	if err != nil {
		fresh = a
	}
	RespondJSON(w, http.StatusAccepted, fresh)
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
		RespondError(w, http.StatusNotFound, actionNotFound)
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
	if username == "" || conversationID == "" || len(conversationID) > actionMaxConversationID {
		RespondError(w, http.StatusNotFound, conversationNotFound)
		return
	}
	owned := false
	if h.owns != nil {
		var err error
		owned, err = h.owns(ctx, conversationID, username)
		if err != nil {
			log.Printf("[ERROR] Conversation ownership check failed: %v", err)
			RespondError(w, http.StatusInternalServerError, "Failed to check the conversation")
			return
		}
	}
	if !owned {
		RespondError(w, http.StatusNotFound, conversationNotFound)
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
