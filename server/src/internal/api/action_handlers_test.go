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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/pgedge/ai-workbench/server/internal/actions"
	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// fakeActionBackend implements every dependency of ActionHandler.
type fakeActionBackend struct {
	mu sync.Mutex

	actions map[int64]*actions.Action

	canAccess bool
	canWrite  bool
	// revokeAfter, when positive, makes HasWriteAccess answer false from
	// its Nth call onward, simulating a grant removed mid-request.
	revokeAfter int
	writeCalls  int

	connErr error

	proposeResult *actions.ProposeResult
	proposeErr    error
	proposed      []actions.ProposeRequest

	overrideErr error
	overrides   int

	approveErr error
	approvals  []string

	startErr error
	started  []int64

	cancelOK  bool
	linkErr   error
	linked    []int64
	linkedTo  string
	ownsConv  bool
	ownsErr   error
	getErr    error
	connName  string
	startOnce map[int64]bool
}

func newFakeActionBackend() *fakeActionBackend {
	return &fakeActionBackend{
		actions:   map[int64]*actions.Action{},
		canAccess: true,
		canWrite:  true,
		connName:  "prod-db",
		ownsConv:  true,
		startOnce: map[int64]bool{},
	}
}

func (f *fakeActionBackend) Get(_ context.Context, id int64) (*actions.Action, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	a, ok := f.actions[id]
	if !ok {
		return nil, actions.ErrNotFound
	}
	c := *a
	return &c, nil
}

func (f *fakeActionBackend) Approve(_ context.Context, id int64, approval, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.approveErr != nil {
		return f.approveErr
	}
	a, ok := f.actions[id]
	if !ok || a.ExecStatus != "not_started" {
		return actions.ErrNotFound
	}
	a.Approval = approval
	f.approvals = append(f.approvals, approval)
	return nil
}

func (f *fakeActionBackend) LinkConversation(_ context.Context, ids []int64, conv, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.linked, f.linkedTo = ids, conv
	return f.linkErr
}

func (f *fakeActionBackend) Propose(_ context.Context, req actions.ProposeRequest) (*actions.ProposeResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.proposed = append(f.proposed, req)
	if f.proposeErr != nil {
		return nil, f.proposeErr
	}
	return f.proposeResult, nil
}

func (f *fakeActionBackend) CheckOverride(_ context.Context, _ *actions.Action, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.overrides++
	return f.overrideErr
}

func (f *fakeActionBackend) Start(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return f.startErr
	}
	if f.startOnce[id] {
		return actions.ErrAlreadyStarted
	}
	f.startOnce[id] = true
	f.started = append(f.started, id)
	if a, ok := f.actions[id]; ok {
		a.ExecStatus = "executing"
	}
	return nil
}

func (f *fakeActionBackend) Cancel(int64) bool { return f.cancelOK }

func (f *fakeActionBackend) GetConnection(_ context.Context, id int) (*database.MonitoredConnection, error) {
	if f.connErr != nil {
		return nil, f.connErr
	}
	return &database.MonitoredConnection{ID: id, Name: f.connName}, nil
}

func (f *fakeActionBackend) CanAccessConnection(context.Context, int) (bool, string) {
	return f.canAccess, ""
}

func (f *fakeActionBackend) HasWriteAccess(context.Context, int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeCalls++
	if f.revokeAfter > 0 && f.writeCalls >= f.revokeAfter {
		return false
	}
	return f.canWrite
}

func (f *fakeActionBackend) setWrite(v bool) {
	f.mu.Lock()
	f.canWrite = v
	f.mu.Unlock()
}

type denial struct{ action, target, reason string }

type actionTestEnv struct {
	b       *fakeActionBackend
	h       *ActionHandler
	mux     *http.ServeMux
	denials []denial
}

func newActionTestEnv(t *testing.T) *actionTestEnv {
	t.Helper()
	b := newFakeActionBackend()
	e := &actionTestEnv{b: b}
	e.h = NewActionHandler(b, b, b, b, b,
		func(context.Context, string, string) (bool, error) { return b.ownsConv, b.ownsErr },
		32)
	e.h.SetDenialRecorder(func(r *http.Request, reason string) {
		d, _ := deniedTargetFrom(r)
		e.denials = append(e.denials, denial{d.action, d.target, reason})
	})
	e.mux = http.NewServeMux()
	e.h.RegisterRoutes(e.mux, func(h http.HandlerFunc) http.HandlerFunc { return h })
	return e
}

func (e *actionTestEnv) do(method, path, body string, token bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	ctx := context.WithValue(req.Context(), auth.UsernameContextKey, "tester")
	ctx = context.WithValue(ctx, auth.IsAPITokenContextKey, token)
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func notStarted(id int64, review string) *actions.Action {
	return &actions.Action{
		ID: id, ConnectionID: 7, ReviewStatus: review,
		Approval: "none", ExecStatus: "not_started",
	}
}

const goodCreate = `{"sql":"DELETE FROM t","source":"chat_block"}`

func TestCreateAction(t *testing.T) {
	e := newActionTestEnv(t)
	e.b.proposeResult = &actions.ProposeResult{
		Action: notStarted(1, "passed"), AttemptsLeft: 2,
	}
	e.b.actions[1] = e.b.proposeResult.Action

	rec := e.do("POST", "/api/v1/connections/7/actions", goodCreate, false)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["attempts_left"] != float64(2) || resp["chain_exhausted"] != false {
		t.Errorf("response = %v", resp)
	}
	if len(e.b.started) != 0 {
		t.Error("an action was started without execute")
	}
}

func TestCreateActionExecute(t *testing.T) {
	for _, review := range []string{"passed", "vetoed"} {
		e := newActionTestEnv(t)
		a := notStarted(1, review)
		e.b.actions[1] = a
		e.b.proposeResult = &actions.ProposeResult{Action: a}

		body := `{"sql":"DELETE FROM t","source":"alert_analysis","execute":true}`
		rec := e.do("POST", "/api/v1/connections/7/actions", body, false)
		if rec.Code != http.StatusCreated {
			t.Fatalf("%s: status = %d: %s", review, rec.Code, rec.Body)
		}
		want := 0
		if review == "passed" {
			want = 1
		}
		if len(e.b.started) != want {
			t.Errorf("%s: started %d actions, want %d", review, len(e.b.started), want)
		}
	}
}

func TestCreateActionExecuteFailuresStillCreate(t *testing.T) {
	e := newActionTestEnv(t)
	a := notStarted(1, "passed")
	e.b.actions[1] = a
	e.b.proposeResult = &actions.ProposeResult{Action: a}
	e.b.approveErr = errors.New("boom")

	body := `{"sql":"DELETE FROM t","source":"chat_block","execute":true}`
	if rec := e.do("POST", "/api/v1/connections/7/actions", body, false); rec.Code != http.StatusCreated {
		t.Fatalf("approve failure: status = %d", rec.Code)
	}

	e.b.approveErr = nil
	e.b.startErr = errors.New("boom")
	e.b.getErr = errors.New("boom")
	if rec := e.do("POST", "/api/v1/connections/7/actions", body, false); rec.Code != http.StatusCreated {
		t.Fatalf("start failure: status = %d", rec.Code)
	}
}

func TestCreateActionStoresTruncatedContext(t *testing.T) {
	e := newActionTestEnv(t)
	e.b.proposeResult = &actions.ProposeResult{Action: notStarted(1, "passed")}

	long := strings.Repeat("é", 40) // 80 bytes, limit is 32
	body := `{"sql":"DELETE FROM t","source":"chat_block","context":"` + long + `"}`
	if rec := e.do("POST", "/api/v1/connections/7/actions", body, false); rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var ev struct {
		Context   string `json:"context"`
		Truncated bool   `json:"context_truncated"`
	}
	if err := json.Unmarshal(e.b.proposed[0].Evidence, &ev); err != nil {
		t.Fatal(err)
	}
	if len(ev.Context) > 32 || !ev.Truncated || strings.ContainsRune(ev.Context, '�') {
		t.Errorf("evidence = %+v", ev)
	}
}

func TestCreateActionStatuses(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		body   string
		setup  func(*fakeActionBackend)
		status int
	}{
		{"bad connection id", "/api/v1/connections/abc/actions", goodCreate, nil, 404},
		{"huge connection id", "/api/v1/connections/99999999999/actions", goodCreate, nil, 404},
		{"invisible connection", "/api/v1/connections/7/actions", goodCreate,
			func(b *fakeActionBackend) { b.canAccess = false }, 404},
		{"missing connection", "/api/v1/connections/7/actions", goodCreate,
			func(b *fakeActionBackend) { b.connErr = errors.New("none") }, 404},
		{"no write access", "/api/v1/connections/7/actions", goodCreate,
			func(b *fakeActionBackend) { b.canWrite = false }, 403},
		{"bad json", "/api/v1/connections/7/actions", `{`, nil, 400},
		{"empty sql", "/api/v1/connections/7/actions", `{"sql":" ","source":"chat_block"}`, nil, 400},
		{"sql too big", "/api/v1/connections/7/actions",
			`{"sql":"` + strings.Repeat("a", actionMaxSQLBytes+1) + `","source":"chat_block"}`, nil, 400},
		{"rationale too long", "/api/v1/connections/7/actions",
			`{"sql":"x","source":"chat_block","rationale":"` + strings.Repeat("a", actionMaxRationaleBytes+1) + `"}`, nil, 400},
		{"missing source", "/api/v1/connections/7/actions", `{"sql":"x"}`, nil, 400},
		{"reserved source", "/api/v1/connections/7/actions", `{"sql":"x","source":"chat_tool"}`, nil, 400},
		{"bad database", "/api/v1/connections/7/actions",
			`{"sql":"x","source":"chat_block","database_name":"a\u0001b"}`, nil, 400},
		{"bad conversation", "/api/v1/connections/7/actions",
			`{"sql":"x","source":"chat_block","conversation_id":""}`, nil, 400},
		{"bad alert", "/api/v1/connections/7/actions",
			`{"sql":"x","source":"chat_block","alert_id":0}`, nil, 400},
		{"empty proposal", "/api/v1/connections/7/actions", goodCreate,
			func(b *fakeActionBackend) { b.proposeErr = actions.ErrEmptyProposal }, 400},
		{"no writes", "/api/v1/connections/7/actions", goodCreate,
			func(b *fakeActionBackend) { b.proposeErr = actions.ErrNoWrites }, 400},
		{"transaction control", "/api/v1/connections/7/actions", goodCreate,
			func(b *fakeActionBackend) { b.proposeErr = actions.ErrTransactionControl }, 400},
		{"timeout override", "/api/v1/connections/7/actions", goodCreate,
			func(b *fakeActionBackend) { b.proposeErr = actions.ErrTimeoutOverride }, 400},
		{"bad revision", "/api/v1/connections/7/actions", goodCreate,
			func(b *fakeActionBackend) { b.proposeErr = actions.ErrBadRevision }, 400},
		{"chain exhausted", "/api/v1/connections/7/actions", goodCreate,
			func(b *fakeActionBackend) { b.proposeErr = actions.ErrChainExhausted }, 409},
		{"internal error", "/api/v1/connections/7/actions", goodCreate,
			func(b *fakeActionBackend) { b.proposeErr = errors.New("db down") }, 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newActionTestEnv(t)
			if tt.setup != nil {
				tt.setup(e.b)
			}
			rec := e.do("POST", tt.path, tt.body, false)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.status, rec.Body)
			}
			if tt.status == 403 {
				if len(e.denials) != 1 || e.denials[0].action != deniedActionActionCreate {
					t.Errorf("denials = %+v", e.denials)
				}
			}
		})
	}
}

func TestGetAction(t *testing.T) {
	e := newActionTestEnv(t)
	e.b.actions[5] = notStarted(5, "passed")

	if rec := e.do("GET", "/api/v1/actions/5", "", false); rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	missing := e.do("GET", "/api/v1/actions/6", "", false)
	bad := e.do("GET", "/api/v1/actions/x", "", false)
	e.b.canAccess = false
	hidden := e.do("GET", "/api/v1/actions/5", "", false)
	for name, rec := range map[string]*httptest.ResponseRecorder{"missing": missing, "bad": bad, "hidden": hidden} {
		if rec.Code != 404 {
			t.Errorf("%s: status = %d", name, rec.Code)
		}
	}
	if missing.Body.String() != hidden.Body.String() {
		t.Errorf("missing and hidden differ: %q vs %q", missing.Body, hidden.Body)
	}

	e.b.canAccess = true
	e.b.getErr = errors.New("db down")
	if rec := e.do("GET", "/api/v1/actions/5", "", false); rec.Code != 500 {
		t.Errorf("store error: status = %d", rec.Code)
	}
}

func TestExecuteAction(t *testing.T) {
	e := newActionTestEnv(t)
	e.b.actions[1] = notStarted(1, "passed")

	if rec := e.do("POST", "/api/v1/actions/1/execute", "", false); rec.Code != 202 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if len(e.b.started) != 1 || e.b.approvals[0] != "approved" {
		t.Errorf("started=%v approvals=%v", e.b.started, e.b.approvals)
	}
}

// TestExecuteAfterWriteAccessRevoked proves a caller who has lost write
// access cannot start an action, and that the refusal leaves the action
// untouched.
func TestExecuteAfterWriteAccessRevoked(t *testing.T) {
	e := newActionTestEnv(t)
	e.b.actions[1] = notStarted(1, "passed")
	e.b.setWrite(false)

	rec := e.do("POST", "/api/v1/actions/1/execute", `{}`, false)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	a := e.b.actions[1]
	if a.ExecStatus != "not_started" || a.Approval != "none" {
		t.Errorf("action changed: %+v", a)
	}
	if len(e.b.started) != 0 || len(e.b.approvals) != 0 {
		t.Error("refused request still approved or started the action")
	}
	if len(e.denials) != 1 || e.denials[0].action != deniedActionActionExecute ||
		e.denials[0].reason != connectionWriteDenied {
		t.Errorf("denials = %+v", e.denials)
	}
}

// TestOverrideChecksWriteAccessAgain proves the override path asks for
// write access itself, rather than trusting the earlier check: a grant
// removed between the two checks stops the override.
func TestOverrideChecksWriteAccessAgain(t *testing.T) {
	e := newActionTestEnv(t)
	e.b.actions[1] = notStarted(1, "vetoed")
	e.b.revokeAfter = 2

	rec := e.do("POST", "/api/v1/actions/1/execute",
		`{"override":true,"confirm":"prod-db"}`, false)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if e.b.overrides != 0 || len(e.b.started) != 0 {
		t.Error("override went ahead after the grant was removed")
	}
}

// TestOverrideWithTokenRefused proves a token cannot override a veto,
// even one with write access, and that nothing is recorded.
func TestOverrideWithTokenRefused(t *testing.T) {
	e := newActionTestEnv(t)
	e.b.actions[1] = notStarted(1, "vetoed")

	rec := e.do("POST", "/api/v1/actions/1/execute",
		`{"override":true,"confirm":"prod-db"}`, true)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if e.b.actions[1].Approval != "none" || len(e.b.started) != 0 || e.b.overrides != 0 {
		t.Error("a token override had an effect")
	}
	if len(e.denials) != 1 || e.denials[0].reason != actionOverrideDenied {
		t.Errorf("denials = %+v", e.denials)
	}
}

func TestOverrideSession(t *testing.T) {
	e := newActionTestEnv(t)
	e.b.actions[1] = notStarted(1, "vetoed")

	rec := e.do("POST", "/api/v1/actions/1/execute",
		`{"override":true,"confirm":"prod-db"}`, false)
	if rec.Code != 202 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if e.b.overrides != 1 || e.b.approvals[0] != "overridden" {
		t.Errorf("overrides=%d approvals=%v", e.b.overrides, e.b.approvals)
	}
}

func TestExecuteActionStatuses(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		body   string
		review string
		setup  func(*fakeActionBackend)
		status int
	}{
		{"missing", "9", "", "passed", nil, 404},
		{"hidden", "1", "", "passed", func(b *fakeActionBackend) { b.canAccess = false }, 404},
		{"bad body", "1", "{", "passed", nil, 400},
		{"not reviewed", "1", "", "pending", nil, 409},
		{"vetoed without override", "1", "", "vetoed", nil, 409},
		{"override confirm mismatch", "1", `{"override":true,"confirm":"x"}`, "vetoed",
			func(b *fakeActionBackend) { b.overrideErr = actions.ErrConfirmMismatch }, 400},
		{"override not allowed", "1", `{"override":true}`, "passed",
			func(b *fakeActionBackend) { b.overrideErr = actions.ErrOverrideNotAllowed }, 409},
		{"override check fails", "1", `{"override":true}`, "vetoed",
			func(b *fakeActionBackend) { b.overrideErr = errors.New("x") }, 500},
		{"override connection gone", "1", `{"override":true}`, "vetoed",
			func(b *fakeActionBackend) { b.connErr = errors.New("x") }, 404},
		{"already started", "1", "", "passed",
			func(b *fakeActionBackend) { b.actions[1].ExecStatus = "executing" }, 409},
		{"approve fails", "1", "", "passed",
			func(b *fakeActionBackend) { b.approveErr = errors.New("x") }, 500},
		{"start already started", "1", "", "passed",
			func(b *fakeActionBackend) { b.startErr = actions.ErrAlreadyStarted }, 409},
		{"shutting down", "1", "", "passed",
			func(b *fakeActionBackend) { b.startErr = actions.ErrShuttingDown }, 503},
		{"start fails", "1", "", "passed",
			func(b *fakeActionBackend) { b.startErr = errors.New("x") }, 500},
		{"store error", "1", "", "passed",
			func(b *fakeActionBackend) { b.getErr = errors.New("x") }, 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newActionTestEnv(t)
			e.b.actions[1] = notStarted(1, tt.review)
			if tt.setup != nil {
				tt.setup(e.b)
			}
			rec := e.do("POST", "/api/v1/actions/"+tt.id+"/execute", tt.body, false)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.status, rec.Body)
			}
		})
	}
}

// TestDoubleExecute proves two concurrent executes of one action start
// it once; the loser gets 409.
func TestDoubleExecute(t *testing.T) {
	e := newActionTestEnv(t)
	e.b.actions[1] = notStarted(1, "passed")

	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- e.do("POST", "/api/v1/actions/1/execute", "", false).Code
		}()
	}
	wg.Wait()
	close(codes)

	got := map[int]int{}
	for c := range codes {
		got[c]++
	}
	if got[202] != 1 || got[409] != 1 {
		t.Errorf("codes = %v, want one 202 and one 409", got)
	}
	if len(e.b.started) != 1 {
		t.Errorf("started %d times", len(e.b.started))
	}
}

func TestCancelAction(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		setup  func(*fakeActionBackend)
		status int
	}{
		{"running", "1", func(b *fakeActionBackend) { b.cancelOK = true }, 202},
		{"not running", "1", nil, 409},
		{"missing", "9", nil, 404},
		{"no write access", "1", func(b *fakeActionBackend) { b.canWrite = false; b.cancelOK = true }, 403},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newActionTestEnv(t)
			e.b.actions[1] = notStarted(1, "passed")
			if tt.setup != nil {
				tt.setup(e.b)
			}
			rec := e.do("POST", "/api/v1/actions/"+tt.id+"/cancel", "", false)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.status, rec.Body)
			}
			if tt.status == 403 && (len(e.denials) != 1 || e.denials[0].action != deniedActionActionCancel) {
				t.Errorf("denials = %+v", e.denials)
			}
		})
	}
}

func TestLinkActions(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		body   string
		setup  func(*fakeActionBackend)
		status int
	}{
		{"ok", "/api/v1/conversations/c1/actions/link", `{"action_ids":[1,2]}`, nil, 204},
		{"not owner", "/api/v1/conversations/c1/actions/link", `{"action_ids":[1]}`,
			func(b *fakeActionBackend) { b.ownsConv = false }, 404},
		{"ownership error", "/api/v1/conversations/c1/actions/link", `{"action_ids":[1]}`,
			func(b *fakeActionBackend) { b.ownsErr = errors.New("x") }, 500},
		{"long id", "/api/v1/conversations/" + strings.Repeat("a", 129) + "/actions/link",
			`{"action_ids":[1]}`, nil, 404},
		{"bad body", "/api/v1/conversations/c1/actions/link", `{`, nil, 400},
		{"no ids", "/api/v1/conversations/c1/actions/link", `{"action_ids":[]}`, nil, 400},
		{"too many ids", "/api/v1/conversations/c1/actions/link",
			`{"action_ids":[` + strings.TrimSuffix(strings.Repeat("1,", actionMaxLinkIDs+1), ",") + `]}`, nil, 400},
		{"non-positive id", "/api/v1/conversations/c1/actions/link", `{"action_ids":[0]}`, nil, 400},
		{"store error", "/api/v1/conversations/c1/actions/link", `{"action_ids":[1]}`,
			func(b *fakeActionBackend) { b.linkErr = errors.New("x") }, 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newActionTestEnv(t)
			if tt.setup != nil {
				tt.setup(e.b)
			}
			rec := e.do("POST", tt.path, tt.body, false)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.status, rec.Body)
			}
		})
	}
}

func TestLinkActionsNilOwnership(t *testing.T) {
	e := newActionTestEnv(t)
	e.h.owns = nil
	rec := e.do("POST", "/api/v1/conversations/c1/actions/link", `{"action_ids":[1]}`, false)
	if rec.Code != 404 {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestLinkActionsNoUser(t *testing.T) {
	e := newActionTestEnv(t)
	req := httptest.NewRequest("POST", "/api/v1/conversations/c1/actions/link",
		bytes.NewBufferString(`{"action_ids":[1]}`))
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestTruncateUTF8(t *testing.T) {
	if s, cut := truncateUTF8("abc", 5); s != "abc" || cut {
		t.Errorf("short: %q %v", s, cut)
	}
	if s, cut := truncateUTF8("aé", 2); s != "a" || !cut {
		t.Errorf("split rune: %q %v", s, cut)
	}
	if s, cut := truncateUTF8("abc", -1); s != "abc" || cut {
		t.Errorf("negative: %q %v", s, cut)
	}
}

func TestProposalEvidenceEmpty(t *testing.T) {
	e := newActionTestEnv(t)
	if ev := e.h.proposalEvidence(""); ev != nil {
		t.Errorf("evidence = %s", ev)
	}
}

func TestActorVia(t *testing.T) {
	ctx := context.WithValue(context.Background(), auth.IsAPITokenContextKey, true)
	if actorVia(ctx) != "token" || actorVia(context.Background()) != "session" {
		t.Error("actorVia mismatch")
	}
}
