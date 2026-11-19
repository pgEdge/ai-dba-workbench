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
	"fmt"
	"strings"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/actions"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/mcp"
)

// fakeProposer records the request it receives and returns a canned
// result or error.
type fakeProposer struct {
	req   actions.ProposeRequest
	calls int
	res   *actions.ProposeResult
	err   error
}

func (f *fakeProposer) Propose(_ context.Context, req actions.ProposeRequest) (*actions.ProposeResult, error) {
	f.calls++
	f.req = req
	return f.res, f.err
}

func passedResult() *actions.ProposeResult {
	return &actions.ProposeResult{
		Action: &actions.Action{
			ID:           11,
			ReviewStatus: "passed",
			Statements:   []string{"DELETE FROM t WHERE id = 1"},
			Review: &actions.Verdict{
				Decision: "pass", Danger: "low", Reasons: []string{"narrow delete"},
			},
		},
		AttemptsLeft: 2,
	}
}

// runPropose calls the tool with args under ctx and returns the response.
func runPropose(t *testing.T, tool Tool, ctx context.Context, args map[string]any) mcp.ToolResponse {
	t.Helper()
	args["__context"] = ctx
	resp, err := tool.Handler(args)
	if err != nil {
		t.Fatalf("Handler returned error: %v", err)
	}
	return resp
}

func validProposeArgs() map[string]any {
	return map[string]any{
		"connection_id": float64(3),
		"sql":           "DELETE FROM t WHERE id = 1",
		"rationale":     "remove a duplicate row",
	}
}

func writerChecker(t *testing.T) *auth.RBACChecker {
	return auth.NewRBACChecker(newExecuteTestAuthStore(t))
}

func TestProposeActionSuccess(t *testing.T) {
	fp := &fakeProposer{res: passedResult()}
	tool := ProposeActionTool(fp, nil, writerChecker(t), nil)

	args := validProposeArgs()
	args["database_name"] = "appdb"
	resp := runPropose(t, tool, superuserTokenContext(), args)
	if resp.IsError {
		t.Fatalf("unexpected error: %s", responseText(resp))
	}

	var out proposeActionResult
	if err := json.Unmarshal([]byte(responseText(resp)), &out); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if out.ActionID != 11 || out.ReviewStatus != "passed" || out.Danger != "low" ||
		out.AttemptsLeft != 2 || out.ChainExhausted || out.BestActionID != nil ||
		len(out.Reasons) != 1 || len(out.Statements) != 1 {
		t.Errorf("unexpected result: %+v", out)
	}
	if fp.req.ConnectionID != 3 || fp.req.Source != "chat_tool" ||
		fp.req.DatabaseName == nil || *fp.req.DatabaseName != "appdb" ||
		fp.req.Rationale != "remove a duplicate row" {
		t.Errorf("unexpected request: %+v", fp.req)
	}
	if fp.req.ConversationID != nil || fp.req.Evidence != nil {
		t.Errorf("no metadata supplied, got conversation %v evidence %s",
			fp.req.ConversationID, fp.req.Evidence)
	}
}

func TestProposeActionVetoChainExhausted(t *testing.T) {
	fp := &fakeProposer{res: &actions.ProposeResult{
		Action: &actions.Action{
			ID: 9, ReviewStatus: "vetoed",
			Review: &actions.Verdict{Decision: "veto", Danger: "high",
				Reasons: []string{"no WHERE clause"}},
		},
		ChainExhausted: true,
		BestAttempt:    &actions.Action{ID: 7},
	}}
	tool := ProposeActionTool(fp, nil, writerChecker(t), nil)
	args := validProposeArgs()
	args["revises_action_id"] = float64(8)

	resp := runPropose(t, tool, superuserTokenContext(), args)
	var out proposeActionResult
	if err := json.Unmarshal([]byte(responseText(resp)), &out); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if out.ReviewStatus != "vetoed" || !out.ChainExhausted ||
		out.BestActionID == nil || *out.BestActionID != 7 || out.Danger != "high" {
		t.Errorf("unexpected result: %+v", out)
	}
	if fp.req.RevisesActionID == nil || *fp.req.RevisesActionID != 8 {
		t.Errorf("revises_action_id not passed: %+v", fp.req)
	}
}

func TestProposeActionNilReviewAndStatements(t *testing.T) {
	fp := &fakeProposer{res: &actions.ProposeResult{Action: &actions.Action{ID: 1}}}
	tool := ProposeActionTool(fp, nil, writerChecker(t), nil)
	resp := runPropose(t, tool, superuserTokenContext(), validProposeArgs())
	if !strings.Contains(responseText(resp), `"reasons":[]`) ||
		!strings.Contains(responseText(resp), `"statements":[]`) {
		t.Errorf("expected empty arrays, got %s", responseText(resp))
	}
}

func TestProposeActionNoWriteAccess(t *testing.T) {
	capture := &toolDenialCapture{}
	fp := &fakeProposer{res: passedResult()}
	// A nil checker denies every check, as for a caller without access.
	tool := ProposeActionTool(fp, nil, nil, capture.record)

	resp := runPropose(t, tool, superuserTokenContext(), validProposeArgs())
	if !resp.IsError || responseText(resp) != proposeActionWriteDenied {
		t.Fatalf("expected refusal, got %+v", resp)
	}
	if fp.calls != 0 {
		t.Error("Propose must not be called without write access")
	}
	want := toolDenial{deniedActionConnectionQueryWrite, "connections/3", proposeActionWriteDenied}
	if len(capture.denials) != 1 || capture.denials[0] != want {
		t.Errorf("expected %v recorded, got %v", want, capture.denials)
	}
}

func TestProposeActionInvalidArgs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{"missing connection", func(a map[string]any) { delete(a, "connection_id") }, "connection_id"},
		{"fractional connection", func(a map[string]any) { a["connection_id"] = 1.5 }, "connection_id"},
		{"zero connection", func(a map[string]any) { a["connection_id"] = float64(0) }, "connection_id"},
		{"missing sql", func(a map[string]any) { delete(a, "sql") }, "'sql'"},
		{"blank sql", func(a map[string]any) { a["sql"] = "  " }, "'sql'"},
		{"huge sql", func(a map[string]any) { a["sql"] = strings.Repeat("x", proposeMaxSQLBytes+1) }, "too large"},
		{"missing rationale", func(a map[string]any) { delete(a, "rationale") }, "'rationale'"},
		{"huge rationale", func(a map[string]any) { a["rationale"] = strings.Repeat("x", proposeMaxRationaleBytes+1) }, "too long"},
		{"database not a string", func(a map[string]any) { a["database_name"] = 5 }, "database_name"},
		{"database invalid", func(a map[string]any) { a["database_name"] = "bad\x00name" }, "database_name"},
		{"revises not a number", func(a map[string]any) { a["revises_action_id"] = "x" }, "revises_action_id"},
		{"revises not positive", func(a map[string]any) { a["revises_action_id"] = float64(0) }, "revises_action_id"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fp := &fakeProposer{res: passedResult()}
			tool := ProposeActionTool(fp, nil, writerChecker(t), nil)
			args := validProposeArgs()
			tc.mutate(args)
			resp := runPropose(t, tool, superuserTokenContext(), args)
			if !resp.IsError || !strings.Contains(responseText(resp), tc.want) {
				t.Errorf("expected error mentioning %q, got %+v", tc.want, resp)
			}
			if fp.calls != 0 {
				t.Error("Propose must not be called for invalid arguments")
			}
		})
	}
}

func TestProposeActionOptionalArgsAccepted(t *testing.T) {
	fp := &fakeProposer{res: passedResult()}
	tool := ProposeActionTool(fp, nil, writerChecker(t), nil)
	args := validProposeArgs()
	args["database_name"] = ""
	args["revises_action_id"] = nil
	args["connection_id"] = 3 // an int, not a float64
	resp := runPropose(t, tool, superuserTokenContext(), args)
	if resp.IsError {
		t.Fatalf("unexpected error: %s", responseText(resp))
	}
	if fp.req.DatabaseName != nil || fp.req.RevisesActionID != nil {
		t.Errorf("empty optionals must stay nil: %+v", fp.req)
	}
}

func TestProposeActionErrorMapping(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{actions.ErrEmptyProposal, "no statements"},
		{actions.ErrNoWrites, "query_database"},
		{actions.ErrTransactionControl, "transaction control"},
		{actions.ErrTimeoutOverride, "lock_timeout"},
		{fmt.Errorf("wrapped: %w", actions.ErrBadRevision), "revises_action_id"},
		{actions.ErrChainExhausted, "the decision is theirs"},
		{errors.New("pq: password=hunter2 host=10.0.0.1"), "could not be recorded"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			fp := &fakeProposer{err: tc.err}
			tool := ProposeActionTool(fp, nil, writerChecker(t), nil)
			resp := runPropose(t, tool, superuserTokenContext(), validProposeArgs())
			text := responseText(resp)
			if !resp.IsError || !strings.Contains(text, tc.want) {
				t.Errorf("expected error containing %q, got %q", tc.want, text)
			}
			if strings.Contains(text, "hunter2") || strings.Contains(text, "10.0.0.1") {
				t.Errorf("internal detail leaked: %q", text)
			}
		})
	}
}

func TestProposeActionNilResult(t *testing.T) {
	fp := &fakeProposer{}
	tool := ProposeActionTool(fp, nil, writerChecker(t), nil)
	resp := runPropose(t, tool, superuserTokenContext(), validProposeArgs())
	if !resp.IsError {
		t.Errorf("expected error for empty result, got %+v", resp)
	}
}

// ownerContext returns a superuser token context for user, so the RBAC
// gate passes and the conversation check has a username to test.
func ownerContext(user string) context.Context {
	return context.WithValue(superuserTokenContext(), auth.UsernameContextKey, user)
}

// ownedBy returns an ownership check under which only owner owns
// conversation id, recording the ids it was asked about.
func ownedBy(owner, id string, asked *[]string) ConversationOwnership {
	return func(_ context.Context, conversationID, username string) (bool, error) {
		*asked = append(*asked, conversationID)
		return conversationID == id && username == owner, nil
	}
}

func TestProposeActionCallMetaPassedThrough(t *testing.T) {
	t.Parallel()
	fp := &fakeProposer{res: passedResult()}
	var asked []string
	tool := ProposeActionTool(fp, ownedBy("alice", "conv-123", &asked), writerChecker(t), nil)
	ctx := mcp.WithCallMeta(ownerContext("alice"), mcp.CallMeta{
		ConversationID: "conv-123", Model: "test-model", Provider: "test-provider",
	})
	if resp := runPropose(t, tool, ctx, validProposeArgs()); resp.IsError {
		t.Fatalf("unexpected error: %s", responseText(resp))
	}

	if fp.req.ConversationID == nil || *fp.req.ConversationID != "conv-123" {
		t.Errorf("conversation id not passed: %v", fp.req.ConversationID)
	}
	if len(asked) != 1 || asked[0] != "conv-123" {
		t.Errorf("ownership not checked for the call's conversation: %v", asked)
	}
	var ev map[string]string
	if err := json.Unmarshal(fp.req.Evidence, &ev); err != nil {
		t.Fatalf("evidence is not JSON: %v", err)
	}
	if ev["model"] != "test-model" || ev["provider"] != "test-provider" {
		t.Errorf("unexpected evidence: %v", ev)
	}
}

// TestProposeActionConversationRefused covers every conversation id the
// caller may not attach a proposal to: each is refused before anything
// is recorded, with the same text whether the conversation is another
// user's or missing.
func TestProposeActionConversationRefused(t *testing.T) {
	t.Parallel()
	failing := func(context.Context, string, string) (bool, error) {
		return false, errors.New("pq: password=hunter2")
	}
	tests := []struct {
		name string
		owns func(*[]string) ConversationOwnership
		ctx  context.Context
		id   string
		want string
	}{
		{"another user's conversation", func(a *[]string) ConversationOwnership {
			return ownedBy("mallory", "conv-9", a)
		}, ownerContext("alice"), "conv-9", proposeConversationDenied},
		{"missing conversation", func(a *[]string) ConversationOwnership {
			return ownedBy("alice", "conv-9", a)
		}, ownerContext("alice"), "conv-other", proposeConversationDenied},
		{"oversized id", func(a *[]string) ConversationOwnership {
			return ownedBy("alice", "conv-9", a)
		}, ownerContext("alice"), strings.Repeat("c", proposeMaxConversationID+1), proposeConversationDenied},
		{"no username", func(a *[]string) ConversationOwnership {
			return ownedBy("", "conv-9", a)
		}, superuserTokenContext(), "conv-9", proposeConversationDenied},
		{"no ownership check", func(*[]string) ConversationOwnership { return nil },
			ownerContext("alice"), "conv-9", proposeConversationDenied},
		{"check fails", func(*[]string) ConversationOwnership { return failing },
			ownerContext("alice"), "conv-9", "could not be checked"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fp := &fakeProposer{res: passedResult()}
			var asked []string
			tool := ProposeActionTool(fp, tc.owns(&asked), writerChecker(t), nil)
			ctx := mcp.WithCallMeta(tc.ctx, mcp.CallMeta{ConversationID: tc.id})
			resp := runPropose(t, tool, ctx, validProposeArgs())
			text := responseText(resp)
			if !resp.IsError || !strings.Contains(text, tc.want) {
				t.Fatalf("expected refusal containing %q, got %+v", tc.want, resp)
			}
			if strings.Contains(text, "hunter2") {
				t.Errorf("internal detail leaked: %q", text)
			}
			if fp.calls != 0 {
				t.Error("Propose must not be called for a conversation the caller does not own")
			}
		})
	}
}

// TestProposeActionNoConversationSkipsCheck records a proposal with no
// conversation without consulting the ownership check.
func TestProposeActionNoConversationSkipsCheck(t *testing.T) {
	t.Parallel()
	fp := &fakeProposer{res: passedResult()}
	var asked []string
	tool := ProposeActionTool(fp, ownedBy("alice", "conv-9", &asked), writerChecker(t), nil)
	if resp := runPropose(t, tool, ownerContext("alice"), validProposeArgs()); resp.IsError {
		t.Fatalf("unexpected error: %s", responseText(resp))
	}
	if len(asked) != 0 || fp.req.ConversationID != nil {
		t.Errorf("no conversation supplied, but checked %v and stored %v", asked, fp.req.ConversationID)
	}
}

// TestProposeEvidenceCapsModelAndProvider drops a model or provider
// longer than the limit, keeping the other.
func TestProposeEvidenceCapsModelAndProvider(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("m", proposeMaxModelName+1)
	exact := strings.Repeat("p", proposeMaxModelName)
	var ev map[string]string
	if err := json.Unmarshal(proposeEvidence(mcp.CallMeta{Model: long, Provider: exact}), &ev); err != nil {
		t.Fatalf("evidence is not JSON: %v", err)
	}
	if ev["model"] != "" || ev["provider"] != exact {
		t.Errorf("unexpected evidence: %v", ev)
	}
	if got := proposeEvidence(mcp.CallMeta{Model: long, Provider: long}); got != nil {
		t.Errorf("both oversized: expected no evidence, got %s", got)
	}
}

// TestProposeActionRealCheckerRefusesWrite runs the tool under a real
// RBAC checker for a signed-in user with no grants on a connection
// private to someone else: the checker refuses write access, the refusal
// is audited and nothing is proposed.
func TestProposeActionRealCheckerRefusesWrite(t *testing.T) {
	capture := &toolDenialCapture{}
	fp := &fakeProposer{res: passedResult()}
	tool := ProposeActionTool(fp, nil, privateChecker(t), capture.record)

	resp := runPropose(t, tool, restrictedUserContext(), validProposeArgs())
	if !resp.IsError || responseText(resp) != proposeActionWriteDenied {
		t.Fatalf("expected refusal, got %+v", resp)
	}
	if fp.calls != 0 {
		t.Error("Propose must not be called without write access")
	}
	want := toolDenial{deniedActionConnectionQueryWrite, "connections/3", proposeActionWriteDenied}
	if len(capture.denials) != 1 || capture.denials[0] != want {
		t.Errorf("expected %v recorded, got %v", want, capture.denials)
	}
}

func TestProposeActionMissingContext(t *testing.T) {
	fp := &fakeProposer{res: passedResult()}
	tool := ProposeActionTool(fp, nil, nil, nil)
	resp, err := tool.Handler(validProposeArgs())
	if err != nil || !resp.IsError {
		t.Errorf("expected refusal without context, got %+v, %v", resp, err)
	}
}

func TestProposeEvidenceEmpty(t *testing.T) {
	if got := proposeEvidence(mcp.CallMeta{}); got != nil {
		t.Errorf("expected nil, got %s", got)
	}
}

func TestInt64Arg(t *testing.T) {
	for _, v := range []any{"1", nil, true, 1.5} {
		if _, ok := int64Arg(v); ok {
			t.Errorf("int64Arg(%v) should fail", v)
		}
	}
	if n, ok := int64Arg(int64(5)); !ok || n != 5 {
		t.Errorf("int64Arg(int64(5)) = %d, %v", n, ok)
	}
}

func TestProviderRegistersProposeAction(t *testing.T) {
	enabled := true
	disabled := false
	tests := []struct {
		name     string
		flag     *bool
		proposer ActionProposer
		want     bool
	}{
		{"enabled with proposer", &enabled, &fakeProposer{}, true},
		{"default with proposer", nil, &fakeProposer{}, true},
		{"disabled", &disabled, &fakeProposer{}, false},
		{"no proposer", &enabled, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Builtins.Tools.ProposeAction = tc.flag
			p, _ := newExecuteTestProvider(t, cfg)
			if _, ok := p.baseRegistry.Get("propose_action"); ok {
				t.Fatal("tool registered before a proposer was set")
			}
			if tc.proposer != nil {
				p.SetActionProposer(tc.proposer, nil)
			}
			if _, ok := p.baseRegistry.Get("propose_action"); ok != tc.want {
				t.Errorf("registered = %v, want %v", ok, tc.want)
			}
		})
	}
}

func TestProviderExecutesProposeAction(t *testing.T) {
	p, _ := newExecuteTestProvider(t, &config.Config{})
	fp := &fakeProposer{res: passedResult()}
	p.SetActionProposer(fp, nil)

	resp, err := p.Execute(superuserTokenContext(), "propose_action", validProposeArgs())
	if err != nil || resp.IsError {
		t.Fatalf("Execute: %+v, %v", resp, err)
	}
	if fp.calls != 1 {
		t.Errorf("expected one Propose call, got %d", fp.calls)
	}
}
