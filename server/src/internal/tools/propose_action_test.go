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
	tool := ProposeActionTool(fp, writerChecker(t), nil)

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
	tool := ProposeActionTool(fp, writerChecker(t), nil)
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
	tool := ProposeActionTool(fp, writerChecker(t), nil)
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
	tool := ProposeActionTool(fp, nil, capture.record)

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
			tool := ProposeActionTool(fp, writerChecker(t), nil)
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
	tool := ProposeActionTool(fp, writerChecker(t), nil)
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
			tool := ProposeActionTool(fp, writerChecker(t), nil)
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
	tool := ProposeActionTool(fp, writerChecker(t), nil)
	resp := runPropose(t, tool, superuserTokenContext(), validProposeArgs())
	if !resp.IsError {
		t.Errorf("expected error for empty result, got %+v", resp)
	}
}

func TestProposeActionCallMetaPassedThrough(t *testing.T) {
	fp := &fakeProposer{res: passedResult()}
	tool := ProposeActionTool(fp, writerChecker(t), nil)
	ctx := mcp.WithCallMeta(superuserTokenContext(), mcp.CallMeta{
		ConversationID: "conv-123", Model: "test-model", Provider: "test-provider",
	})
	runPropose(t, tool, ctx, validProposeArgs())

	if fp.req.ConversationID == nil || *fp.req.ConversationID != "conv-123" {
		t.Errorf("conversation id not passed: %v", fp.req.ConversationID)
	}
	var ev map[string]string
	if err := json.Unmarshal(fp.req.Evidence, &ev); err != nil {
		t.Fatalf("evidence is not JSON: %v", err)
	}
	if ev["model"] != "test-model" || ev["provider"] != "test-provider" {
		t.Errorf("unexpected evidence: %v", ev)
	}

	// An oversized conversation id is dropped rather than stored.
	fp2 := &fakeProposer{res: passedResult()}
	tool2 := ProposeActionTool(fp2, writerChecker(t), nil)
	ctx2 := mcp.WithCallMeta(superuserTokenContext(), mcp.CallMeta{
		ConversationID: strings.Repeat("c", proposeMaxConversationID+1),
	})
	runPropose(t, tool2, ctx2, validProposeArgs())
	if fp2.req.ConversationID != nil {
		t.Error("oversized conversation id must be dropped")
	}
}

func TestProposeActionMissingContext(t *testing.T) {
	fp := &fakeProposer{res: passedResult()}
	tool := ProposeActionTool(fp, nil, nil)
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
				p.SetActionProposer(tc.proposer)
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
	p.SetActionProposer(fp)

	resp, err := p.Execute(superuserTokenContext(), "propose_action", validProposeArgs())
	if err != nil || resp.IsError {
		t.Fatalf("Execute: %+v, %v", resp, err)
	}
	if fp.calls != 1 {
		t.Errorf("expected one Propose call, got %d", fp.calls)
	}
}
