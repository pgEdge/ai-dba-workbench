/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"

	"github.com/pgedge/ai-workbench/server/internal/actions"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// ActionProposer is the part of actions.Service that propose_action uses.
type ActionProposer interface {
	Propose(ctx context.Context, req actions.ProposeRequest) (*actions.ProposeResult, error)
}

// ConversationOwnership reports whether the named user owns the
// conversation. The server supplies it from the conversation store; the
// REST create path applies the same check.
type ConversationOwnership func(ctx context.Context, conversationID, username string) (bool, error)

// Limits on the free-text arguments and call metadata, so a runaway call
// cannot store an unbounded row.
const (
	proposeMaxSQLBytes       = 256 * 1024
	proposeMaxRationaleBytes = 16 * 1024
	proposeMaxConversationID = 128
	proposeMaxModelName      = 128
)

// proposeConversationDenied is the refusal for a conversation id the
// caller does not own. A missing conversation and another user's get the
// same text, so the tool cannot be used to probe ids.
const proposeConversationDenied = "The conversation this call belongs to was not found for your account. Nothing has been recorded."

// proposeActionWriteDenied is the refusal for a connection the caller
// cannot write to. It is the same text whether the connection is missing,
// hidden or merely read-only, so the tool cannot be used to probe IDs.
const proposeActionWriteDenied = "Permission denied: you do not have write access to this connection"

// proposeActionResult is the JSON document the tool returns.
type proposeActionResult struct {
	ActionID       int64    `json:"action_id"`
	ReviewStatus   string   `json:"review_status"`
	Danger         string   `json:"danger"`
	Reasons        []string `json:"reasons"`
	AttemptsLeft   int      `json:"attempts_left"`
	ChainExhausted bool     `json:"chain_exhausted"`
	BestActionID   *int64   `json:"best_action_id"`
	Statements     []string `json:"statements"`
}

// ProposeActionTool creates the propose_action tool. It only records a
// proposal and has it reviewed; it never runs SQL, approves or overrides.
// A conversation id in the call metadata is recorded only when owns
// confirms the caller owns that conversation; otherwise the call is
// refused, and with owns nil every conversation id is refused.
func ProposeActionTool(proposer ActionProposer, owns ConversationOwnership,
	rbacChecker *auth.RBACChecker, record DenialRecorder) Tool {
	return Tool{
		Definition: mcp.Tool{
			Name: "propose_action",
			Description: `Propose a change to a monitored database for review. This is the ONLY way to change a database through the Workbench: never tell the user a change has been made, and never ask them to paste your SQL into a client, when you can propose it here instead. Read-only queries do not need it; use query_database for those.

<how_it_works>
- Nothing runs when you call this tool. The SQL is stored as a proposed action and checked by an automated reviewer.
- A human then decides: the user sees the proposal, the reviewer's verdict and the danger rating, and chooses whether to approve and run it. You cannot approve, run or override an action.
- The result tells you the review_status (passed or vetoed), a danger rating (low, moderate, high or critical) and the reviewer's reasons.
</how_it_works>

<sql_rules>
- Provide the exact statements you want run, separated by semicolons. Prefer one small, targeted change per proposal.
- Do NOT include transaction control (BEGIN, COMMIT, ROLLBACK, SAVEPOINT and so on); the executor manages transactions itself.
- Do NOT set lock_timeout or statement_timeout, or reset session settings; the executor sets them.
- The batch must contain at least one write. Run read-only checks with query_database first and cite what you found in the rationale.
</sql_rules>

<rationale>
Explain what the change does, why it is needed and the evidence for it (for example the query results or alert that prompted it). Be honest about risk; the reviewer and the user both read it.
</rationale>

<after_a_veto>
If review_status is "vetoed", read every reason, then call propose_action again with a revised statement that addresses each reason and pass the vetoed action's action_id as revises_action_id. Do not resubmit the same SQL. The number of attempts is limited: attempts_left says how many remain. Only a vetoed action can be revised.
When chain_exhausted is true you may not propose again for that change; best_action_id names the least dangerous attempt. Tell the user the outcome and that the decision is now theirs: do not try to work around the limit.
</after_a_veto>

<requirements>
You need write access to the connection. The result's statements field lists the statements as they were split and stored.
</requirements>`,
			CompactDescription: `Propose a write (INSERT, UPDATE, DELETE, DDL, maintenance) against a monitored database for review. The only way to change a database: nothing runs until a human approves. No transaction control or timeout changes in the SQL. If vetoed, revise to address every reason and pass revises_action_id; once chain_exhausted is true the user decides.`,
			InputSchema: mcp.InputSchema{
				Type: "object",
				Properties: map[string]any{
					"connection_id": map[string]any{
						"type":        "integer",
						"description": "ID of the monitored connection the change is for. Use list_connections to discover IDs.",
					},
					"database_name": map[string]any{
						"type":        "string",
						"description": "Database to run in. If omitted, the connection's default database is used.",
					},
					"sql": map[string]any{
						"type":        "string",
						"description": "The statements to run, separated by semicolons. No transaction control and no changes to lock_timeout or statement_timeout.",
					},
					"rationale": map[string]any{
						"type":        "string",
						"description": "What the change does, why it is needed and the evidence for it.",
					},
					"revises_action_id": map[string]any{
						"type":        "integer",
						"description": "The action_id of a vetoed proposal this one replaces. Required when addressing a veto.",
					},
				},
				Required: []string{"connection_id", "sql", "rationale"},
			},
		},
		Handler: func(args map[string]any) (mcp.ToolResponse, error) {
			ctx, ok := args["__context"].(context.Context)
			if !ok {
				ctx = context.Background()
			}
			req, msg := parseProposeArgs(args)
			if msg != "" {
				return mcp.NewToolError(msg)
			}

			if !rbacChecker.HasWriteAccess(ctx, req.ConnectionID) {
				return record.refuse(ctx, deniedActionConnectionQueryWrite,
					connectionDenialTarget(req.ConnectionID),
					proposeActionWriteDenied)
			}

			meta := mcp.CallMetaFromContext(ctx)
			if meta.ConversationID != "" {
				if msg := checkProposeConversation(ctx, owns, meta.ConversationID); msg != "" {
					return mcp.NewToolError(msg)
				}
				id := meta.ConversationID
				req.ConversationID = &id
			}
			req.Source = "chat_tool"
			req.Evidence = proposeEvidence(meta)

			res, err := proposer.Propose(ctx, req)
			if err != nil {
				return mcp.NewToolError(proposeErrorMessage(err))
			}
			return proposeResponse(res)
		},
	}
}

// checkProposeConversation returns the refusal for a conversation id
// the caller may not attach a proposal to, or "" when they own it. An
// oversized id, an anonymous caller and a missing check are refused like
// a conversation owned by someone else.
func checkProposeConversation(ctx context.Context, owns ConversationOwnership, id string) string {
	username := auth.GetUsernameFromContext(ctx)
	if owns == nil || username == "" || len(id) > proposeMaxConversationID {
		return proposeConversationDenied
	}
	owned, err := owns(ctx, id, username)
	if err != nil {
		log.Printf("[ERROR] propose_action conversation ownership check failed: %v", err)
		return "The conversation could not be checked. Nothing has been recorded. Tell the user, and try again later."
	}
	if !owned {
		return proposeConversationDenied
	}
	return ""
}

// parseProposeArgs validates the arguments and builds the request. A
// non-empty second value is the tool error to return.
func parseProposeArgs(args map[string]any) (actions.ProposeRequest, string) {
	var req actions.ProposeRequest

	id, ok := intArg(args["connection_id"])
	if !ok || id <= 0 {
		return req, "Missing or invalid 'connection_id' parameter"
	}
	req.ConnectionID = id

	sql, msg := requiredTextArg(args, "sql", proposeMaxSQLBytes,
		"Missing or invalid 'sql' parameter",
		"'sql' is too large; propose a smaller change")
	if msg != "" {
		return req, msg
	}
	req.SQL = sql

	rationale, msg := requiredTextArg(args, "rationale", proposeMaxRationaleBytes,
		"Missing or invalid 'rationale' parameter: explain why the change is needed",
		"'rationale' is too long; shorten it")
	if msg != "" {
		return req, msg
	}
	req.Rationale = rationale

	if req.DatabaseName, msg = databaseNameArg(args); msg != "" {
		return req, msg
	}
	if req.RevisesActionID, msg = revisesActionArg(args); msg != "" {
		return req, msg
	}
	return req, ""
}

// requiredTextArg reads a string argument that must not be blank and
// must be at most limit bytes long. A non-empty second value is the tool
// error to return: missing for an absent, non-string or blank value, and
// tooLong for one over the limit.
func requiredTextArg(args map[string]any, key string, limit int,
	missing, tooLong string) (string, string) {

	v, ok := args[key].(string)
	if !ok || strings.TrimSpace(v) == "" {
		return "", missing
	}
	if len(v) > limit {
		return "", tooLong
	}
	return v, ""
}

// databaseNameArg reads the optional 'database_name' argument. An
// absent, null or empty value gives nil; anything else must be a valid
// database name. A non-empty second value is the tool error to return.
func databaseNameArg(args map[string]any) (*string, string) {
	raw, present := args["database_name"]
	if !present || raw == nil {
		return nil, ""
	}
	name, isString := raw.(string)
	if !isString {
		return nil, "Invalid 'database_name' parameter"
	}
	if name == "" {
		return nil, ""
	}
	if err := database.ValidateDatabaseName(name); err != nil {
		return nil, "Invalid 'database_name' parameter"
	}
	return &name, ""
}

// revisesActionArg reads the optional 'revises_action_id' argument. An
// absent or null value gives nil; anything else must be a positive
// integer. A non-empty second value is the tool error to return.
func revisesActionArg(args map[string]any) (*int64, string) {
	raw, present := args["revises_action_id"]
	if !present || raw == nil {
		return nil, ""
	}
	rev, isNum := int64Arg(raw)
	if !isNum || rev <= 0 {
		return nil, "Invalid 'revises_action_id' parameter"
	}
	return &rev, ""
}

// intArg reads a JSON number as an int.
func intArg(v any) (int, bool) {
	n, ok := int64Arg(v)
	return int(n), ok && n == int64(int(n))
}

// int64Arg reads a JSON number, which arrives as a float64, or an
// already-integral Go integer. Fractions are rejected.
func int64Arg(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		if n != float64(int64(n)) {
			return 0, false
		}
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}

// proposeEvidence records the model and provider the client supplied;
// with neither, no evidence is stored. A value longer than
// proposeMaxModelName is dropped, so the client cannot store an
// unbounded string.
func proposeEvidence(meta mcp.CallMeta) json.RawMessage {
	model, provider := capMetaValue(meta.Model), capMetaValue(meta.Provider)
	if model == "" && provider == "" {
		return nil
	}
	b, err := json.Marshal(map[string]string{
		"model":    model,
		"provider": provider,
	})
	if err != nil {
		return nil
	}
	return b
}

// capMetaValue returns v, or "" when it is longer than
// proposeMaxModelName.
func capMetaValue(v string) string {
	if len(v) > proposeMaxModelName {
		return ""
	}
	return v
}

// proposeErrorMessage turns a Propose error into text the model can act
// on. Errors the service does not define are logged and replaced with a
// generic message, so internal detail never reaches the caller.
func proposeErrorMessage(err error) string {
	switch {
	case errors.Is(err, actions.ErrEmptyProposal):
		return "The SQL contains no statements. Provide the statements to run."
	case errors.Is(err, actions.ErrNoWrites):
		return "The SQL contains no writes. propose_action is only for changes; run read-only queries directly with query_database."
	case errors.Is(err, actions.ErrTransactionControl):
		return "The SQL must not contain transaction control (BEGIN, COMMIT, ROLLBACK, SAVEPOINT and so on); the executor manages transactions. Remove them and propose again."
	case errors.Is(err, actions.ErrTimeoutOverride):
		return "The SQL must not change lock_timeout or statement_timeout or reset session settings; the executor sets them. Remove those statements and propose again."
	case errors.Is(err, actions.ErrBadRevision):
		return "revises_action_id does not name a vetoed action of yours on this connection that can still be revised. Only the latest vetoed attempt in a chain can be revised; omit revises_action_id to start a new proposal."
	case errors.Is(err, actions.ErrChainExhausted):
		return "The maximum number of attempts for this change has been reached. Do not propose it again: tell the user the outcome and that the decision is theirs."
	}
	log.Printf("[ERROR] propose_action failed: %v", err)
	return "The proposal could not be recorded or reviewed. Nothing has been run. Tell the user, and try again later."
}

// proposeResponse renders the service result as the JSON text block.
func proposeResponse(res *actions.ProposeResult) (mcp.ToolResponse, error) {
	if res == nil || res.Action == nil {
		return mcp.NewToolError("The proposal could not be recorded or reviewed. Nothing has been run.")
	}
	out := proposeActionResult{
		ActionID:       res.Action.ID,
		ReviewStatus:   res.Action.ReviewStatus,
		Reasons:        []string{},
		AttemptsLeft:   res.AttemptsLeft,
		ChainExhausted: res.ChainExhausted,
		Statements:     res.Action.Statements,
	}
	if out.Statements == nil {
		out.Statements = []string{}
	}
	if v := res.Action.Review; v != nil {
		out.Danger = v.Danger
		if v.Reasons != nil {
			out.Reasons = v.Reasons
		}
	}
	if res.BestAttempt != nil {
		id := res.BestAttempt.ID
		out.BestActionID = &id
	}
	b, err := json.Marshal(out)
	if err != nil {
		return mcp.NewToolError("The proposal was recorded but its result could not be rendered.")
	}
	return mcp.NewToolSuccess(string(b))
}
