/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package actions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/server/internal/testutil/scratchdb"
)

// newTestStore brings up a scratch database with the collector's schema
// and returns a Store on it plus a connection id to attach actions to.
func newTestStore(t *testing.T, suffix string) (*Store, *pgxpool.Pool, int) {
	t.Helper()
	pool := scratchdb.NewCollectorSchema(t, suffix, 16)
	var connID int
	if err := pool.QueryRow(context.Background(),
		`INSERT INTO connections (name, host, database_name, username, owner_username) VALUES ('test-conn', 'localhost', 'postgres', 'postgres', 'alice') RETURNING id`,
	).Scan(&connID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	return NewStore(pool), pool, connID
}

func sampleAction(connID int, by string) NewAction {
	return NewAction{
		ConnectionID: connID,
		Statements:   []string{"CREATE INDEX CONCURRENTLY i ON t (a)", "ANALYZE t"},
		Source:       "chat_tool",
		ProposedBy:   by,
		ProposedVia:  "session",
		Rationale:    "speed up lookups",
		Evidence:     json.RawMessage(`{"model":"test"}`),
	}
}

func TestStoreLifecycle(t *testing.T) {
	s, _, connID := newTestStore(t, "_act_life")
	ctx := context.Background()

	a, err := s.Create(ctx, sampleAction(connID, "alice"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if a.ID == 0 || a.ReviewStatus != "pending" || a.Approval != "none" ||
		a.ExecStatus != "not_started" || a.Attempt != 1 {
		t.Fatalf("unexpected new action: %+v", a)
	}
	if len(a.Statements) != 2 || a.Rationale == nil || *a.Rationale != "speed up lookups" {
		t.Fatalf("statements/rationale not stored: %+v", a)
	}

	// Not yet reviewed or approved: the claim must refuse.
	ok, err := s.ClaimForExecution(ctx, a.ID)
	if err != nil || ok {
		t.Fatalf("claim before review: ok=%v err=%v", ok, err)
	}

	v := Verdict{Decision: "pass", Danger: "low", Reasons: []string{"safe"}, Reviewer: "test-model"}
	if err := s.SetReview(ctx, a.ID, "passed", v); err != nil {
		t.Fatalf("set review: %v", err)
	}
	if err := s.Approve(ctx, a.ID, "approved", "bob", "session"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	got, err := s.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ReviewStatus != "passed" || got.Review == nil || got.Review.Danger != "low" ||
		got.Approval != "approved" || got.ApprovedBy == nil || *got.ApprovedBy != "bob" ||
		got.ApprovedVia == nil || *got.ApprovedVia != "session" || got.ApprovedAt == nil {
		t.Fatalf("review/approval not stored: %+v", got)
	}
	if !got.UpdatedAt.After(a.UpdatedAt) {
		t.Errorf("updated_at not advanced: %v vs %v", got.UpdatedAt, a.UpdatedAt)
	}

	ok, err = s.ClaimForExecution(ctx, a.ID)
	if err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	claimed, err := s.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("get after claim: %v", err)
	}
	if claimed.ExecStatus != "executing" || claimed.ExecutedAt == nil {
		t.Fatalf("claim must record the start time: %+v", claimed)
	}
	ok, err = s.ClaimForExecution(ctx, a.ID)
	if err != nil || ok {
		t.Fatalf("second claim: ok=%v err=%v", ok, err)
	}

	results := []StatementResult{
		{Statement: "CREATE INDEX CONCURRENTLY i ON t (a)", CommandTag: "CREATE INDEX", DurationMS: 5, Applied: true},
		{Statement: "ANALYZE t", Columns: []string{"c"}, Rows: [][]string{{"1"}}, Truncated: true, Error: "boom"},
	}
	if err := s.FinishExecution(ctx, a.ID, "failed", false, results); err != nil {
		t.Fatalf("finish: %v", err)
	}
	got, err = s.Get(ctx, a.ID)
	if err != nil {
		t.Fatalf("get after finish: %v", err)
	}
	if got.ExecStatus != "failed" || got.Transactional == nil || *got.Transactional ||
		got.ExecutedAt == nil || !got.ExecutedAt.Equal(*claimed.ExecutedAt) ||
		len(got.Results) != 2 ||
		got.Results[0].CommandTag != "CREATE INDEX" || !got.Results[1].Truncated ||
		got.Results[1].Rows[0][0] != "1" {
		t.Fatalf("results not stored: %+v", got)
	}

	// Finishing a row that is not executing is refused.
	if err := s.FinishExecution(ctx, a.ID, "succeeded", true, nil); !errors.Is(err, ErrInvalidState) {
		t.Errorf("finish on a finished action: %v", err)
	}
	if err := s.FinishExecution(ctx, a.ID, "bogus", true, nil); err == nil {
		t.Error("finish with an invalid status should fail")
	}

	// Not found paths and validation.
	if _, err := s.Get(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Errorf("get missing: %v", err)
	}
	if err := s.SetReview(ctx, 999999, "passed", v); !errors.Is(err, ErrNotFound) {
		t.Errorf("review missing: %v", err)
	}
	if err := s.SetReview(ctx, a.ID, "pending", v); err == nil {
		t.Error("set review with invalid status should fail")
	}
	if err := s.Approve(ctx, 999999, "approved", "bob", "session"); !errors.Is(err, ErrNotFound) {
		t.Errorf("approve missing: %v", err)
	}
	if err := s.Approve(ctx, a.ID, "none", "bob", "session"); err == nil {
		t.Error("approve with invalid approval should fail")
	}
	if err := s.Approve(ctx, a.ID, "approved", "bob", "cookie"); err == nil {
		t.Error("approve with invalid via should fail")
	}
	if err := s.Approve(ctx, a.ID, "approved", "bob", "session"); !errors.Is(err, ErrInvalidState) {
		t.Errorf("approve after execution should be refused: %v", err)
	}
	if ok, err := s.ClaimForExecution(ctx, 999999); err != nil || ok {
		t.Errorf("claim missing: ok=%v err=%v", ok, err)
	}

	// A veto plus an override is claimable.
	b, err := s.Create(ctx, sampleAction(connID, "alice"))
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	if err := s.SetReview(ctx, b.ID, "vetoed", Verdict{Decision: "veto", Danger: "high"}); err != nil {
		t.Fatalf("veto: %v", err)
	}
	if ok, err := s.ClaimForExecution(ctx, b.ID); err != nil || ok {
		t.Errorf("vetoed, unapproved action must not be claimable: ok=%v err=%v", ok, err)
	}
	if err := s.Approve(ctx, b.ID, "overridden", "bob", "session"); err != nil {
		t.Fatalf("override: %v", err)
	}
	if ok, err := s.ClaimForExecution(ctx, b.ID); err != nil || !ok {
		t.Fatalf("claim overridden: ok=%v err=%v", ok, err)
	}

	// Chain fields and invalid inserts.
	na := sampleAction(connID, "alice")
	ds, conv := "db1", "conv-1"
	na.DatabaseName, na.ConversationID, na.ParentActionID, na.Attempt = &ds, &conv, &a.ID, 2
	c, err := s.Create(ctx, na)
	if err != nil {
		t.Fatalf("create chained: %v", err)
	}
	if c.Attempt != 2 || c.ParentActionID == nil || *c.ParentActionID != a.ID ||
		c.DatabaseName == nil || *c.DatabaseName != "db1" {
		t.Fatalf("chain fields not stored: %+v", c)
	}
	bad := sampleAction(connID, "alice")
	bad.Source = "nonsense"
	if _, err := s.Create(ctx, bad); err == nil {
		t.Error("create with invalid source should fail")
	}
	empty := sampleAction(connID, "alice")
	empty.Statements = nil
	empty.Evidence = nil
	empty.Rationale = ""
	d, err := s.Create(ctx, empty)
	if err != nil {
		t.Fatalf("create empty: %v", err)
	}
	if d.Statements == nil || len(d.Statements) != 0 || d.Rationale != nil || d.Evidence != nil {
		t.Fatalf("empty action wrong: %+v", d)
	}
}

func TestClaimRequiresRecordedDecision(t *testing.T) {
	s, pool, connID := newTestStore(t, "_act_dec")
	ctx := context.Background()

	cases := []struct {
		name     string
		review   string
		approval string // set directly so impossible combinations are covered
		want     bool
	}{
		{"passed, no approval", "passed", "none", false},
		{"vetoed, no approval", "vetoed", "none", false},
		{"pending, approved", "pending", "approved", false},
		{"vetoed, approved", "vetoed", "approved", false},
		{"passed, overridden", "passed", "overridden", false},
		{"pending, overridden", "pending", "overridden", false},
		{"passed, approved", "passed", "approved", true},
		{"vetoed, overridden", "vetoed", "overridden", true},
	}
	for _, tc := range cases {
		a, err := s.Create(ctx, sampleAction(connID, "alice"))
		if err != nil {
			t.Fatalf("%s: create: %v", tc.name, err)
		}
		if _, err := pool.Exec(ctx,
			`UPDATE proposed_actions SET review_status = $2, approval = $3 WHERE id = $1`,
			a.ID, tc.review, tc.approval); err != nil {
			t.Fatalf("%s: set state: %v", tc.name, err)
		}
		ok, err := s.ClaimForExecution(ctx, a.ID)
		if err != nil || ok != tc.want {
			t.Errorf("%s: claim ok=%v err=%v, want %v", tc.name, ok, err, tc.want)
		}
	}
}

func TestClaimForExecutionConcurrent(t *testing.T) {
	s, _, connID := newTestStore(t, "_act_claim")
	ctx := context.Background()
	a, err := s.Create(ctx, sampleAction(connID, "alice"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetReview(ctx, a.ID, "passed", Verdict{Decision: "pass", Danger: "low"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctx, a.ID, "approved", "bob", "session"); err != nil {
		t.Fatal(err)
	}

	const n = 10
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ok, err := s.ClaimForExecution(ctx, a.ID)
			if err != nil {
				errs <- err
				return
			}
			if ok {
				wins.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("claim error: %v", err)
	}
	if got := wins.Load(); got != 1 {
		t.Fatalf("expected exactly one winner, got %d", got)
	}
}

func TestMarkInterruptedUnknown(t *testing.T) {
	s, pool, connID := newTestStore(t, "_act_intr")
	ctx := context.Background()

	mk := func() *Action {
		a, err := s.Create(ctx, sampleAction(connID, "alice"))
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	running, idle := mk(), mk()
	if err := s.SetReview(ctx, running.ID, "passed", Verdict{Decision: "pass", Danger: "low"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctx, running.ID, "approved", "bob", "session"); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ClaimForExecution(ctx, running.ID); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}

	n, err := s.MarkInterruptedUnknown(ctx)
	if err != nil || n != 1 {
		t.Fatalf("mark: n=%d err=%v", n, err)
	}
	got, err := s.Get(ctx, running.ID)
	if err != nil {
		t.Fatalf("get running: %v", err)
	}
	if got.ExecStatus != "unknown" {
		t.Errorf("running row is %q, want unknown", got.ExecStatus)
	}
	got, err = s.Get(ctx, idle.ID)
	if err != nil {
		t.Fatalf("get idle: %v", err)
	}
	if got.ExecStatus != "not_started" {
		t.Errorf("idle row is %q, want not_started", got.ExecStatus)
	}
	if n, err := s.MarkInterruptedUnknown(ctx); err != nil || n != 0 {
		t.Errorf("second mark: n=%d err=%v", n, err)
	}

	// The run that was still going records its real outcome over the
	// sweep's guess, once; a recorded outcome is never replaced.
	results := []StatementResult{{Statement: "SELECT 1", Applied: true}}
	if err := s.FinishExecution(ctx, running.ID, "succeeded", true, results); err != nil {
		t.Fatalf("finish after the sweep: %v", err)
	}
	if got, err = s.Get(ctx, running.ID); err != nil || got.ExecStatus != "succeeded" || len(got.Results) != 1 {
		t.Errorf("after finish = %+v, %v", got, err)
	}
	if err := s.FinishExecution(ctx, running.ID, "failed", true, nil); !errors.Is(err, ErrInvalidState) {
		t.Errorf("second finish = %v, want ErrInvalidState", err)
	}
	if err := s.FinishExecution(ctx, idle.ID, "succeeded", true, nil); !errors.Is(err, ErrInvalidState) {
		t.Errorf("finish of an unclaimed action = %v, want ErrInvalidState", err)
	}

	pool.Close()
	if _, err := s.MarkInterruptedUnknown(ctx); err == nil {
		t.Error("expected an error on a closed pool")
	}
}

func TestLinkConversationOnlyOwnRows(t *testing.T) {
	s, pool, connID := newTestStore(t, "_act_link")
	ctx := context.Background()

	create := func(na NewAction) *Action {
		t.Helper()
		a, err := s.Create(ctx, na)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		return a
	}
	mine := create(sampleAction(connID, "alice"))
	theirs := create(sampleAction(connID, "mallory"))
	na := sampleAction(connID, "alice")
	existing := "other-conv"
	na.ConversationID = &existing
	linked := create(na)

	ids := []int64{mine.ID, theirs.ID, linked.ID}
	if err := s.LinkConversation(ctx, ids, "conv-9", "alice"); err != nil {
		t.Fatalf("link: %v", err)
	}
	conv := func(id int64) *string {
		a, err := s.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return a.ConversationID
	}
	if c := conv(mine.ID); c == nil || *c != "conv-9" {
		t.Errorf("own row not linked: %v", c)
	}
	if c := conv(theirs.ID); c != nil {
		t.Errorf("another user's row was linked: %v", *c)
	}
	if c := conv(linked.ID); c == nil || *c != existing {
		t.Errorf("already linked row was changed: %v", c)
	}
	if err := s.LinkConversation(ctx, nil, "conv-9", "alice"); err != nil {
		t.Errorf("empty id list: %v", err)
	}

	pool.Close()
	if err := s.LinkConversation(ctx, ids, "x", "alice"); err == nil {
		t.Error("expected an error on a closed pool")
	}
}

func TestStoreErrorsOnClosedPool(t *testing.T) {
	s, pool, connID := newTestStore(t, "_act_err")
	ctx := context.Background()
	a, err := s.Create(ctx, sampleAction(connID, "alice"))
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	if _, err := s.Create(ctx, sampleAction(connID, "alice")); err == nil {
		t.Error("create")
	}
	if _, err := s.Get(ctx, a.ID); err == nil || errors.Is(err, ErrNotFound) {
		t.Error("get")
	}
	if err := s.SetReview(ctx, a.ID, "passed", Verdict{}); err == nil {
		t.Error("set review")
	}
	if err := s.Approve(ctx, a.ID, "approved", "b", "session"); err == nil {
		t.Error("approve")
	}
	if err := s.Reapprove(ctx, a.ID, "approved", "b", "c", "session"); err == nil || errors.Is(err, ErrInvalidState) {
		t.Error("reapprove")
	}
	if _, err := s.ClaimForExecution(ctx, a.ID); err == nil {
		t.Error("claim")
	}
	if err := s.FinishExecution(ctx, a.ID, "failed", false, nil); err == nil {
		t.Error("finish")
	}
}

// TestReapprove proves a retry by another user is recorded as theirs
// only while the decision it retries still stands unstarted, and as a
// compare-and-swap on the approver.
func TestReapprove(t *testing.T) {
	s, _, connID := newTestStore(t, "_act_reap")
	ctx := context.Background()
	a, err := s.Create(ctx, sampleAction(connID, "alice"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetReview(ctx, a.ID, "passed", Verdict{Decision: "pass", Danger: "low"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Reapprove(ctx, a.ID, "approved", "bob", "root", "session"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("reapprove before any approval = %v, want ErrInvalidState", err)
	}
	if err := s.Approve(ctx, a.ID, "approved", "bob", "session"); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name                        string
		approval, previous, by, via string
		wantErr                     error // nil for success; errAny for any non-state error
	}{
		{"bad approval", "none", "bob", "root", "session", errAny},
		{"bad channel", "approved", "bob", "root", "cookie", errAny},
		{"other kind", "overridden", "bob", "root", "session", ErrInvalidState},
		{"stale approver", "approved", "carol", "root", "session", ErrInvalidState},
		{"retry", "approved", "bob", "root", "token", nil},
		{"lost race", "approved", "bob", "dave", "session", ErrInvalidState},
	}
	for _, tc := range tests {
		err := s.Reapprove(ctx, a.ID, tc.approval, tc.previous, tc.by, tc.via)
		switch {
		case tc.wantErr == errAny:
			if err == nil || errors.Is(err, ErrInvalidState) {
				t.Errorf("%s: err = %v", tc.name, err)
			}
		case !errors.Is(err, tc.wantErr):
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.wantErr)
		}
	}
	got, err := s.Get(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ApprovedBy == nil || *got.ApprovedBy != "root" || got.ApprovedVia == nil || *got.ApprovedVia != "token" {
		t.Errorf("approved_by = %v via %v, want root via token", got.ApprovedBy, got.ApprovedVia)
	}
	if ok, err := s.ClaimForExecution(ctx, a.ID); err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	if err := s.Reapprove(ctx, a.ID, "approved", "root", "bob", "session"); !errors.Is(err, ErrInvalidState) {
		t.Errorf("reapprove after the start = %v, want ErrInvalidState", err)
	}
}

// errAny marks a table case that expects an error other than a state
// error.
var errAny = errors.New("any error")

// TestGetRejectsUndecodableColumns proves a stored document that does not
// decode into its field is reported as an error naming the column,
// rather than returned half-filled.
func TestGetRejectsUndecodableColumns(t *testing.T) {
	s, pool, connID := newTestStore(t, "_act_dec")
	ctx := context.Background()
	tests := []struct {
		column, update, want string
	}{
		{"statements", `UPDATE proposed_actions SET statements = '{"not": "a list"}' WHERE id = $1`, "decode statements"},
		{"review", `UPDATE proposed_actions SET review = '"not a verdict"' WHERE id = $1`, "decode review"},
		{"results", `UPDATE proposed_actions SET results = '{"not": "a list"}' WHERE id = $1`, "decode results"},
	}
	for _, tt := range tests {
		t.Run(tt.column, func(t *testing.T) {
			a, err := s.Create(ctx, sampleAction(connID, "alice"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, tt.update, a.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Get(ctx, a.ID); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

// TestCreateFailsMidTransaction proves Create reports a failure of the
// revision replacement and of the commit, and leaves no row behind.
// Triggers in the test's own scratch database make each step fail.
func TestCreateFailsMidTransaction(t *testing.T) {
	s, pool, connID := newTestStore(t, "_act_ctx")
	ctx := context.Background()
	parent, err := s.Create(ctx, sampleAction(connID, "alice"))
	if err != nil {
		t.Fatal(err)
	}
	mustExec := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`CREATE FUNCTION test_refuse() RETURNS trigger LANGUAGE plpgsql
        AS $$ BEGIN RAISE EXCEPTION 'refused by test'; END $$`)
	tests := []struct {
		name, trigger, drop, want string
	}{
		{
			"replacement fails",
			`CREATE TRIGGER test_refuse_delete BEFORE DELETE ON proposed_actions
                FOR EACH STATEMENT EXECUTE FUNCTION test_refuse()`,
			`DROP TRIGGER test_refuse_delete ON proposed_actions`,
			"replace unreviewed revision",
		},
		{
			"commit fails",
			`CREATE CONSTRAINT TRIGGER test_refuse_commit AFTER INSERT ON proposed_actions
                DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION test_refuse()`,
			`DROP TRIGGER test_refuse_commit ON proposed_actions`,
			"create proposed action",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mustExec(tt.trigger)
			defer mustExec(tt.drop)
			n := sampleAction(connID, "alice")
			n.ParentActionID = &parent.ID
			if _, err := s.Create(ctx, n); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			var rows int
			if err := pool.QueryRow(ctx,
				`SELECT count(*) FROM proposed_actions WHERE parent_action_id = $1`,
				parent.ID).Scan(&rows); err != nil {
				t.Fatal(err)
			}
			if rows != 0 {
				t.Errorf("revisions = %d, want 0", rows)
			}
		})
	}
}
