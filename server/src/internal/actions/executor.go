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
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/pkg/rollback"
	"github.com/pgedge/ai-workbench/server/internal/pgexec"
	"github.com/pgedge/ai-workbench/server/internal/sqllex"
)

var (
	// ErrAlreadyStarted is returned by Start when the action cannot be
	// claimed: it has already run or is running, it has been neither
	// reviewed nor overridden, or another Start for it is still claiming
	// it or settling its claim.
	ErrAlreadyStarted = errors.New("action cannot be started")

	// ErrTooManyPending is returned by Start when maxPendingClaims
	// actions are already being claimed or having their claims settled,
	// which means the datastore is failing; a later try may succeed.
	ErrTooManyPending = errors.New("too many action claims are unsettled")

	// ErrShuttingDown is returned by Start after Shutdown has been called.
	ErrShuttingDown = errors.New("executor is shutting down")
)

// defaultRowLimit caps the rows kept per statement when the caller gives
// no positive limit.
const defaultRowLimit = 1000

// finishTimeout bounds each attempt to record an outcome, which must
// complete even after the run has been stopped.
const finishTimeout = 30 * time.Second

// claimTimeout bounds the claim Start makes. The claim runs on a context
// detached from the request, so a client that disconnects mid-claim
// cannot leave the row claimed with no run behind it; a claim that
// commits but loses its reply is told apart by its token (Store.claim),
// so it gets a run too.
const claimTimeout = 30 * time.Second

// cancelRequestTimeout bounds the cancel request sent to the server.
const cancelRequestTimeout = 5 * time.Second

// responseGrace is how much longer than statement_timeout the executor
// waits for the target to answer a round trip. statement_timeout makes
// the server end a slow statement; this deadline catches a server, or a
// proxy in front of it, that has stopped answering altogether, which no
// server setting can.
const responseGrace = 30 * time.Second

// noResponseMessage is the error text for a round trip the target did
// not answer before its deadline.
const noResponseMessage = "the database stopped responding, so the connection was closed"

// Recording an outcome is retried, with a delay that doubles from
// finishBackoffBase up to finishBackoffMax, before it is given up. The
// target has already been changed by then, so only the write to the
// datastore is repeated, never the statements.
const (
	finishAttempts    = 5
	finishBackoffBase = 200 * time.Millisecond
	finishBackoffMax  = 5 * time.Second
)

// A claim whose outcome Start could not establish (errClaimUnsettled)
// is settled in the background (settleClaim), with a delay between
// attempts that doubles from settleBackoffBase up to settleBackoffMax.
// A failed attempt is retried up to settleAttempts times, and an
// attempt that finds nothing to release is retried until
// claimSettledAfter has passed, so settling takes at most about five
// minutes. After Shutdown, retries continue only within its context.
const (
	settleAttempts    = 8
	settleBackoffBase = time.Second
	settleBackoffMax  = 30 * time.Second
)

// maxPendingClaims caps the actions being claimed or having their claims
// settled at once, so a failing datastore cannot pile up settling
// goroutines.
const maxPendingClaims = 64

// maxConcurrentSettles caps the release attempts in flight at once. An
// attempt holds a datastore connection for up to claimReconcileTimeout
// whilst it waits on the claim's row lock, and the datastore pool has
// only 20 connections by default (config.DefaultPoolMaxConns), so
// maxPendingClaims attempts at once could starve every other request.
const maxConcurrentSettles = 2

// claimStore is the part of Store used to claim an action, and to
// release a claim that no run was started under.
type claimStore interface {
	claim(ctx context.Context, id int64, token string) (bool, error)
	releaseClaim(ctx context.Context, id int64, token string) (bool, error)
}

// outcomeStore is the part of Store used to record an outcome.
type outcomeStore interface {
	Get(ctx context.Context, id int64) (*Action, error)
	FinishExecution(ctx context.Context, id int64, status string, transactional bool, results []StatementResult) error
}

// Opener opens a single-connection pool on the monitored database an
// action targets. The executor closes the pool after use. The pool must
// not set default_transaction_read_only.
type Opener func(ctx context.Context, connectionID int, databaseName *string) (*pgxpool.Pool, error)

// runningAction is the handle Cancel uses for an executing action.
type runningAction struct {
	cancel        context.CancelFunc
	stopRequested atomic.Bool
}

// runState is what a run has established so far. It lives outside the run
// so that the partial results survive a panic.
type runState struct {
	results       []StatementResult
	transactional bool
	// unknown is set when the outcome cannot be established: the
	// connection was lost mid-statement in a non-transactional run, a
	// COMMIT failed without a server verdict, or the executor panicked.
	unknown bool
	// sent is set once a statement has been sent to the database.
	sent bool
}

// Executor runs approved actions against their target databases.
type Executor struct {
	store            *Store
	opener           Opener
	lockTimeout      time.Duration
	statementTimeout time.Duration
	rowLimit         int

	// outcomes records outcomes; it is the store outside tests.
	outcomes outcomeStore
	// claims claims actions and settles unsettled claims; it is the
	// store outside tests.
	claims claimStore
	// settleBackoff is the first delay between attempts to settle a
	// claim.
	settleBackoff time.Duration
	// claimSettledAfter is how long after a claim began settleClaim
	// keeps retrying a release that finds nothing; it is the store's
	// claimSettledAfter outside tests.
	claimSettledAfter time.Duration
	// finishBackoff is the first delay between attempts to record an
	// outcome.
	finishBackoff time.Duration
	// stopping is closed by Shutdown. Outcome retries then continue only
	// until shutdownDone is closed.
	stopping chan struct{}
	// shutdownDone is the Done channel of the context passed to the first
	// Shutdown, set before stopping is closed: the end of the graceful
	// shutdown budget.
	shutdownDone <-chan struct{}
	// responseGrace is added to statementTimeout to give the deadline
	// for each round trip to the target.
	responseGrace time.Duration
	// settleSlots holds maxConcurrentSettles tokens, one taken for each
	// release attempt settleClaim makes.
	settleSlots chan struct{}

	mu      sync.Mutex
	closed  bool
	running map[int64]*runningAction
	// pending holds the actions a Start is claiming, or whose claim is
	// being settled; Start refuses an action already in it.
	pending map[int64]struct{}
	wg      sync.WaitGroup
}

// NewExecutor returns an Executor. rowLimit caps the result rows kept per
// statement; a value below one selects a default of 1000.
func NewExecutor(store *Store, opener Opener, lockTimeout, statementTimeout time.Duration, rowLimit int) *Executor {
	if rowLimit < 1 {
		rowLimit = defaultRowLimit
	}
	return &Executor{
		store:             store,
		opener:            opener,
		lockTimeout:       lockTimeout,
		statementTimeout:  statementTimeout,
		rowLimit:          rowLimit,
		outcomes:          store,
		claims:            store,
		settleBackoff:     settleBackoffBase,
		claimSettledAfter: claimSettledAfter,
		pending:           make(map[int64]struct{}),
		settleSlots:       newSettleSlots(maxConcurrentSettles),
		finishBackoff:     finishBackoffBase,
		stopping:          make(chan struct{}),
		responseGrace:     responseGrace,
		running:           make(map[int64]*runningAction),
	}
}

// Shutdown stops the executor taking new actions, cancels the running
// ones and waits for them to record their outcomes, or for ctx to end.
// A failed outcome write is retried until ctx ends; the statements are
// never run again.
func (e *Executor) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	if !e.closed {
		e.shutdownDone = ctx.Done()
		close(e.stopping)
	}
	e.closed = true
	for _, run := range e.running {
		run.stopRequested.Store(true)
		run.cancel()
	}
	e.mu.Unlock()

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Start claims the action and runs it in a goroutine detached from ctx's
// cancellation, recording the outcome on the action. It returns
// ErrAlreadyStarted when the claim fails, or whilst another Start for
// the action is still claiming it or settling its claim, and
// ErrShuttingDown after Shutdown. When the claim's outcome is not known
// (errClaimUnsettled), Start returns the error and settles the claim in
// the background (settleClaim), so a claim that did commit is returned
// to not_started rather than left executing with no run behind it.
func (e *Executor) Start(ctx context.Context, actionID int64) error {
	// The WaitGroup is incremented under the same lock Shutdown takes to
	// set closed, so Shutdown's Wait never races with an Add.
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return ErrShuttingDown
	}
	if _, busy := e.pending[actionID]; busy {
		e.mu.Unlock()
		return ErrAlreadyStarted
	}
	if len(e.pending) >= maxPendingClaims {
		e.mu.Unlock()
		return ErrTooManyPending
	}
	e.pending[actionID] = struct{}{}
	e.wg.Add(1)
	e.mu.Unlock()

	// Until they are handed on, the pending entry and the WaitGroup slot
	// are this call's to give back, panic included; the slot is kept
	// only once a run or the settling goroutine owns it.
	handedOff, keepSlot := false, false
	defer func() {
		if handedOff {
			return
		}
		e.unpend(actionID)
		if !keepSlot {
			e.wg.Done()
		}
	}()

	token := uuid.NewString()
	began := time.Now()
	cctx, cancelClaim := context.WithTimeout(context.WithoutCancel(ctx), claimTimeout)
	ok, err := e.claims.claim(cctx, actionID, token)
	cancelClaim()
	if errors.Is(err, errClaimUnsettled) {
		// The WaitGroup slot and the pending entry pass to the settling
		// goroutine, so Shutdown waits for it and Start refuses the
		// action until it is done.
		handedOff = true
		go func() {
			defer e.wg.Done()
			defer e.unpend(actionID)
			e.settleClaim(actionID, token, began)
		}()
		return err
	}
	if err != nil {
		return err
	}
	if !ok {
		return ErrAlreadyStarted
	}

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	run := &runningAction{cancel: cancel}
	e.mu.Lock()
	e.running[actionID] = run
	if e.closed {
		// Shutdown began between the check above and the claim.
		run.stopRequested.Store(true)
		cancel()
	}
	e.mu.Unlock()

	// The run owns the WaitGroup slot from here; the pending entry is
	// given back by the deferred call, the claim now guarding the row.
	keepSlot = true
	go func() {
		defer e.wg.Done()
		defer func() {
			e.mu.Lock()
			delete(e.running, actionID)
			e.mu.Unlock()
			cancel()
		}()
		e.execute(runCtx, run, actionID)
	}()
	return nil
}

// unpend removes the action from pending.
func (e *Executor) unpend(id int64) {
	e.mu.Lock()
	delete(e.pending, id)
	e.mu.Unlock()
}

// settleClaim settles a claim on the action, under token, whose outcome
// Start could not establish; began is when the claim was begun. If the
// claim committed, the action is returned to not_started; nothing was
// started under the claim, so nothing has run against the target. The
// action is not run instead: its caller has already been told it could
// not be started, and a run that began minutes later would be a
// surprise. Its approval stands, so it can be started again. A row that
// is not executing under token was never claimed by this call, or has
// moved on, and is left alone.
//
// Each attempt locks the row and then runs one guarded UPDATE, so a
// claim taken meanwhile under another token can never be touched. The
// claim's UPDATE took the row, so whilst its transaction is open the
// release's SELECT ... FOR UPDATE waits on the row lock, and the UPDATE
// after it sees how the claim ended: an attempt that finds nothing to
// release ran after the claim aborted. A wait longer than
// claimLockTimeout fails the attempt, which is retried like any other
// failure. An attempt that finds nothing is nevertheless treated as
// final only once claimSettledAfter has passed since began, as an extra
// margin, and retried before that. A failed attempt is retried up to
// settleAttempts times. At most maxConcurrentSettles attempts run at
// once across the executor. After Shutdown, retries continue only
// within its context, as outcome writes do; a committed claim left then
// is marked unknown by the startup sweep (MarkInterruptedUnknown).
func (e *Executor) settleClaim(id int64, token string, began time.Time) {
	delay := e.settleBackoff
	for failures := 0; ; {
		if !awaitOrShutdown(e, e.settleSlots) {
			log.Printf("[ERROR] Stopped settling the claim on action %d at shutdown; if it committed, the next start marks it unknown", id)
			return
		}
		actx, cancel := context.WithTimeout(context.Background(), claimReconcileTimeout)
		released, err := e.claims.releaseClaim(actx, id, token)
		cancel()
		e.settleSlots <- struct{}{}
		switch {
		case err == nil && released:
			log.Printf("[WARN] Action %d: a claim whose outcome was lost was returned to not_started", id)
			return
		case err == nil && time.Since(began) >= e.claimSettledAfter:
			log.Printf("[INFO] Action %d: stopped settling a claim whose outcome was lost, since nothing is executing under it", id)
			return
		case err != nil:
			failures++
			if failures == settleAttempts {
				log.Printf("[ERROR] Action %d needs manual review: gave up settling a claim whose outcome was lost; if the claim committed, the next server start marks the action unknown although nothing ran: %v", id, err)
				return
			}
			log.Printf("[WARN] Settling the claim on action %d failed (attempt %d of %d), retrying: %v",
				id, failures, settleAttempts, err)
		}
		if !e.pause(delay) {
			log.Printf("[ERROR] Stopped settling the claim on action %d at shutdown; if it committed, the next start marks it unknown", id)
			return
		}
		delay = min(2*delay, settleBackoffMax)
	}
}

// Cancel stops a running action and reports whether one was running. The
// server is asked to cancel the statement in progress; the action is then
// recorded as stopped unless it had already succeeded.
func (e *Executor) Cancel(actionID int64) bool {
	e.mu.Lock()
	run, ok := e.running[actionID]
	e.mu.Unlock()
	if !ok {
		return false
	}
	run.stopRequested.Store(true)
	run.cancel()
	return true
}

// execute runs a claimed action and records its outcome. It never
// returns an error: there is no caller to receive one, so failures are
// logged and, where possible, recorded on the action.
func (e *Executor) execute(ctx context.Context, run *runningAction, id int64) {
	st := &runState{transactional: true}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ERROR] Action %d executor panicked: %v", id, r)
			markPanic(st)
		}
		e.recordOutcome(ctx, id, finalStatus(st, run.stopRequested.Load()), st)
	}()

	action, err := e.store.Get(ctx, id)
	if err != nil {
		log.Printf("[ERROR] Failed to load action %d: %v", id, err)
		st.results = []StatementResult{{Error: "could not load the action"}}
		return
	}
	e.runAction(ctx, action, st)
}

// recordOutcome writes the outcome to the action, retrying a failed
// write a few times with a growing delay, since losing it would leave the
// action executing until the next startup marks it unknown. After
// Shutdown, retries continue only within its context.
func (e *Executor) recordOutcome(ctx context.Context, id int64, status string, st *runState) {
	delay := e.finishBackoff
	for attempt := 1; ; attempt++ {
		err := e.finishOnce(ctx, id, status, st, attempt > 1)
		if err == nil {
			return
		}
		if errors.Is(err, ErrInvalidState) || attempt == finishAttempts {
			log.Printf("[ERROR] Failed to record outcome of action %d: %v", id, err)
			return
		}
		log.Printf("[WARN] Recording outcome of action %d failed (attempt %d of %d), retrying: %v",
			id, attempt, finishAttempts, err)
		if !e.pause(delay) {
			log.Printf("[ERROR] Gave up recording outcome of action %d at shutdown: %v", id, err)
			return
		}
		delay = min(2*delay, finishBackoffMax)
	}
}

// finishOnce makes one attempt to record the outcome. On a retry, an
// earlier attempt may have been written even though it reported an
// error, so ErrInvalidState counts as success when the stored outcome is
// the one being recorded.
func (e *Executor) finishOnce(ctx context.Context, id int64, status string, st *runState, retry bool) error {
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer cancel()
	err := e.outcomes.FinishExecution(fctx, id, status, st.transactional, st.results)
	if retry && errors.Is(err, ErrInvalidState) {
		a, gerr := e.outcomes.Get(fctx, id)
		if gerr == nil && a.ExecStatus == status &&
			a.Transactional != nil && *a.Transactional == st.transactional {
			return nil
		}
	}
	return err
}

// pause waits for d and reports true, or reports false at once if the
// context passed to Shutdown ends first.
func (e *Executor) pause(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	return awaitOrShutdown(e, t.C)
}

// awaitOrShutdown receives from ready and reports true, or reports false
// if the context passed to Shutdown ends first.
func awaitOrShutdown[T any](e *Executor, ready <-chan T) bool {
	select {
	case <-ready:
		return true
	case <-e.stopping:
	}
	// stopping is closed only after shutdownDone is set, under mu.
	e.mu.Lock()
	done := e.shutdownDone
	e.mu.Unlock()
	select {
	case <-ready:
		return true
	case <-done:
		return false
	}
}

// newSettleSlots returns a channel holding n tokens; settleClaim takes
// one for each release attempt and puts it back after.
func newSettleSlots(n int) chan struct{} {
	slots := make(chan struct{}, n)
	for range n {
		slots <- struct{}{}
	}
	return slots
}

// markPanic records a panic in st. A non-transactional run may already
// have changed the database, so its outcome becomes unknown and the
// partial results are kept; otherwise the run simply failed.
func markPanic(st *runState) {
	if len(st.results) == 0 {
		st.results = []StatementResult{{Error: "internal error while executing the action"}}
		return
	}
	if st.transactional {
		for i := range st.results {
			st.results[i].Applied = false
		}
		if !hasError(st.results) {
			st.results[0].Error = "internal error while executing the action"
		}
		return
	}
	if !st.sent {
		// Nothing reached the database, so the run simply failed.
		if !hasError(st.results) {
			st.results[0].Error = "internal error while executing the action"
		}
		return
	}
	st.unknown = true
	for i := range st.results {
		if !st.results[i].Applied && st.results[i].Error == "" {
			st.results[i].Error = "outcome unknown: internal error while executing the action"
			return
		}
	}
}

func hasError(results []StatementResult) bool {
	return slices.ContainsFunc(results, func(r StatementResult) bool { return r.Error != "" })
}

// finalStatus maps a finished run onto an exec_status end state. A stop
// request only makes the status stopped when the run did not succeed.
func finalStatus(st *runState, stopRequested bool) string {
	switch {
	case st.unknown:
		return "unknown"
	case !hasError(st.results):
		return "succeeded"
	case stopRequested:
		return "cancelled" //nolint:misspell // value fixed by the table CHECK
	default:
		return "failed"
	}
}

// runAction runs the action's statements, exactly as stored and one per
// Exec, recording progress in st.
func (e *Executor) runAction(ctx context.Context, a *Action, st *runState) {
	stmts := a.Statements
	st.results = newResults(stmts)
	if !slices.ContainsFunc(stmts, func(s string) bool { return strings.TrimSpace(s) != "" }) {
		st.results = []StatementResult{{Error: "the action has no statements"}}
		return
	}
	st.transactional = !slices.ContainsFunc(stmts, sqllex.RequiresNoTransaction)
	for i, s := range stmts {
		if sqllex.IsTransactionControl(s) {
			st.results[i].Error = "transaction control statements are not allowed in an action"
			return
		}
	}

	pool, err := e.opener(ctx, a.ConnectionID, a.DatabaseName)
	if err != nil {
		// The error can quote the connection string, so only a fixed
		// message goes into the result.
		log.Printf("[ERROR] Action %d: could not open the target database", a.ID)
		st.results[0].Error = "could not connect to the target database"
		return
	}
	defer pool.Close()

	timeout := e.statementTimeout + e.responseGrace
	actx, cancelAcquire := context.WithTimeout(ctx, timeout)
	defer cancelAcquire()
	conn, err := pool.Acquire(actx)
	if err != nil {
		log.Printf("[ERROR] Action %d: could not acquire a connection", a.ID)
		st.results[0].Error = safeExecError(err)
		return
	}
	defer conn.Release()
	pgConn := conn.Conn().PgConn()

	// Statements run on a context that is never stopped, so pgx does
	// not break the socket and leave the server statement running.
	// Stopping ctx instead sends a cancel request for the statement in
	// flight, which the server answers with an error, leaving the
	// connection usable for the rollback and the outcome known.
	r := &stmtRunner{
		actionID: a.ID,
		ctx:      ctx,
		execCtx:  context.WithoutCancel(ctx),
		pgConn:   pgConn,
		rowLimit: e.rowLimit,
		timeout:  timeout,
		state:    st,
		settings: fmt.Sprintf("SET lock_timeout = %d; SET statement_timeout = %d",
			e.lockTimeout.Milliseconds(), e.statementTimeout.Milliseconds()),
		exec: func(c context.Context, sql string) error {
			return pgConn.Exec(c, sql).Close()
		},
	}
	stop := context.AfterFunc(ctx, r.cancelInFlight)
	defer stop()
	if st.transactional {
		r.runTransaction(st)
	} else {
		r.runIndividually(st)
	}
}

func newResults(stmts []string) []StatementResult {
	results := make([]StatementResult, len(stmts))
	for i, s := range stmts {
		results[i] = StatementResult{Statement: s}
	}
	return results
}

// stmtRunner holds what running a batch on one connection needs.
type stmtRunner struct {
	actionID int64
	ctx      context.Context // stopped to stop the run
	execCtx  context.Context // never stopped; used for the statements
	pgConn   *pgconn.PgConn
	rowLimit int
	settings string
	exec     rollback.SimpleExec
	state    *runState // may be nil in tests
	// timeout bounds each round trip to the target; zero means none.
	timeout time.Duration
	// beforeDispatch, when set, is called after the timeouts are set and
	// before the statement is sent. It is a hook for tests.
	beforeDispatch func()

	// mu orders sending a statement against a stop request. A statement
	// is only sent, and inFlight set, under mu after checking ctx, and
	// cancelInFlight takes mu before deciding whether to send a cancel
	// request, so a stop either finds the statement not yet sent, and it
	// is never sent, or finds it in flight and cancels it. Sending holds
	// mu until the statement is flushed, so with a large statement and a
	// slow server a stop can wait up to the round-trip deadline before
	// its cancel request is sent.
	mu       sync.Mutex
	inFlight bool
	// cancelSent records that a cancel request was sent while the
	// current statement was in flight. Only then can a 57014
	// (query_canceled) error be the stop's doing rather than the
	// statement timeout's, which PostgreSQL reports with the same code.
	cancelSent bool
}

// dispatch sends stmt, unless the run has been stopped, in which case it
// returns nil and nothing is sent.
func (r *stmtRunner) dispatch(ctx context.Context, stmt string) *pgconn.MultiResultReader {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx.Err() != nil {
		return nil
	}
	if r.state != nil {
		r.state.sent = true
	}
	r.inFlight = true
	r.cancelSent = false
	return r.pgConn.Exec(ctx, stmt)
}

// endDispatch records that the statement in flight has finished.
func (r *stmtRunner) endDispatch() {
	r.mu.Lock()
	r.inFlight = false
	r.mu.Unlock()
}

// cancelInFlight asks the server to cancel the statement in flight, if
// there is one. It runs once the run's context is stopped.
func (r *stmtRunner) cancelInFlight() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.inFlight {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cancelRequestTimeout)
	defer cancel()
	// Even a cancel request that reports failure may have reached the
	// server, so it counts as sent.
	r.cancelSent = true
	if err := r.pgConn.CancelRequest(ctx); err != nil {
		log.Printf("[WARN] Action %d: cancel request failed: %v", r.actionID, err)
	}
}

// stoppedByCancel reports whether a cancel request was sent while the
// last statement was in flight.
func (r *stmtRunner) stoppedByCancel() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancelSent
}

// roundTrip returns the context for one round trip to the target: the
// never-stopped context, with the response deadline when one is set.
func (r *stmtRunner) roundTrip() (context.Context, context.CancelFunc) {
	if r.timeout <= 0 {
		return r.execCtx, func() {}
	}
	return context.WithTimeout(r.execCtx, r.timeout)
}

// execBounded runs sql through exec within the response deadline.
func (r *stmtRunner) execBounded(sql string) error {
	ctx, cancel := r.roundTrip()
	defer cancel()
	err := r.exec(ctx, sql)
	r.closeIfUnresponsive(err)
	return err
}

// closeIfUnresponsive closes the connection when err shows the target
// did not answer in time. pgconn has normally closed it already; closing
// it here as well makes sure nothing more is sent on it.
func (r *stmtRunner) closeIfUnresponsive(err error) {
	if !noResponse(err) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), cancelRequestTimeout)
	defer cancel()
	_ = r.pgConn.Close(ctx) //nolint:errcheck // the connection is being abandoned
}

// noResponse reports whether err is a round trip that hit its deadline.
// Only the response deadline can expire on the never-stopped context.
func noResponse(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

const stoppedMessage = "the action was stopped before this statement ran"

// step runs statement i, first checking for a stop request and then
// re-sending the timeouts, so that an earlier statement that disabled
// them cannot affect this one. The stop request is checked again as the
// statement is sent, so one that arrives while the timeouts are being set
// is not lost. ran reports whether the statement itself
// was sent; err is then the raw error it produced, which is also recorded
// in the result as safe text.
func (r *stmtRunner) step(results []StatementResult, i int) (ran bool, err error) {
	if r.ctx.Err() != nil {
		results[i].Error = stoppedMessage
		return false, nil
	}
	if err := r.execBounded(r.settings); err != nil {
		results[i].Error = "could not set the timeouts: " + safeExecError(err)
		return false, nil
	}
	if r.beforeDispatch != nil {
		r.beforeDispatch()
	}
	ctx, cancel := r.roundTrip()
	defer cancel()
	start := time.Now()
	mrr := r.dispatch(ctx, results[i].Statement)
	if mrr == nil {
		results[i].Error = stoppedMessage
		return false, nil
	}
	res, err := readResult(mrr, r.pgConn, results[i].Statement, r.rowLimit)
	r.endDispatch()
	res.DurationMS = time.Since(start).Milliseconds()
	r.closeIfUnresponsive(err)
	if err != nil {
		res.Error = safeExecError(err)
	}
	results[i] = res
	return true, err
}

// runTransaction runs every statement in one transaction. The first
// failure rolls it back, and every statement is then reported as not
// applied. A clean run commits, and a failed COMMIT is a result of its
// own.
func (r *stmtRunner) runTransaction(st *runState) {
	results := st.results
	if err := r.execBounded("BEGIN"); err != nil {
		results[0].Error = "could not start a transaction: " + safeExecError(err)
		return
	}
	for i := range results {
		ran, err := r.step(results, i)
		if err != nil || results[i].Error != "" {
			if ran {
				i++
			}
			rollbackQuietly(r.execCtx, r.exec)
			clearApplied(results)
			noteSequencesKept(results[:i], false)
			return
		}
	}
	r.commit(st)
}

// stoppedBeforeCommitMessage is the COMMIT result of a transaction rolled
// back because the run was stopped after its last statement.
const stoppedBeforeCommitMessage = "the action was stopped before COMMIT, so the transaction was rolled back"

// commit commits the transaction runTransaction opened, recording a
// failed COMMIT as a result of its own. A stop request that arrived
// after the last statement rolls the transaction back instead, so a
// cancel request the server ignored still stops the change.
func (r *stmtRunner) commit(st *runState) {
	results := st.results
	if r.ctx.Err() != nil {
		rollbackQuietly(r.execCtx, r.exec)
		noteSequencesKept(st.results, false)
		st.results = append(st.results, StatementResult{Statement: "COMMIT", Error: stoppedBeforeCommitMessage})
		clearApplied(st.results)
		return
	}
	// COMMIT runs on the never-stopped context, so a stop request cannot
	// abandon it and leave its outcome unknown; only the response
	// deadline bounds it.
	if err := r.execBounded("COMMIT"); err != nil {
		commit := StatementResult{Statement: "COMMIT"}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			commit.Error = pgErr.Message
			rollbackQuietly(r.execCtx, r.exec)
		} else {
			commit.Error = "outcome unknown: " + lostDuring(err, "COMMIT")
			st.unknown = true
		}
		noteSequencesKept(st.results, st.unknown)
		st.results = append(st.results, commit)
		clearApplied(st.results)
		return
	}
	for i := range results {
		results[i].Applied = true
	}
}

// clearApplied reports every statement as not applied, clearing the
// command tag and rows affected, which describe work that was undone.
func clearApplied(results []StatementResult) {
	for i := range results {
		results[i].Applied = false
		results[i].CommandTag = ""
		results[i].RowsAffected = 0
		results[i].Columns = nil
		results[i].Rows = nil
		results[i].Truncated = false
	}
}

// runIndividually runs each statement on its own, stopping at the first
// failure. Statements after it are reported as not applied. If the
// connection is lost mid-statement the outcome of that statement, and so
// of the action, is unknown.
func (r *stmtRunner) runIndividually(st *runState) {
	results := st.results
	for i := range results {
		ran, err := r.step(results, i)
		if r.outcomeUnknown(st, i, ran, err) {
			return
		}
		// A statement that ran without a server error took effect, even
		// if a check on its side effects then stops the batch.
		results[i].Applied = ran && err == nil
		if results[i].Error != "" || r.leftTransactionOpen(results[i:i+1]) {
			return
		}
	}
}

// outcomeUnknown notes the effects a failed statement may have left and
// reports whether its outcome is unknown, marking st so when it is: the
// server did not answer, or a cancel request stopped the statement part
// way through, which may leave partial work (an invalid index, say).
// PostgreSQL reports a statement timeout and a cancel request with the
// same code, 57014, so the error counts as the stop's only when a cancel
// request was sent whilst the statement was in flight; a stop that came
// after the statement had already failed leaves its error, and its
// known outcome, as they were.
func (r *stmtRunner) outcomeUnknown(st *runState, i int, ran bool, err error) bool {
	if err == nil {
		return false
	}
	res := &st.results[i]
	var pgErr *pgconn.PgError
	isPgErr := errors.As(err, &pgErr)
	stopped := isPgErr && pgErr.Code == "57014" && r.stoppedByCancel()
	if ran {
		noteFailure(res, !isPgErr || stopped)
	}
	switch {
	case !isPgErr:
		res.Error = "outcome unknown: " + lostDuring(err, "the statement")
	case stopped:
		res.Applied = false
		res.Error = "outcome unknown: the statement was stopped part way through"
	default:
		return false
	}
	st.unknown = true
	return true
}

// leftTransactionOpen rolls back, and reports as failed, the one
// statement in res if it left a transaction open. It is defensive: a statement cannot
// normally open one, because transaction control is refused before
// anything runs.
func (r *stmtRunner) leftTransactionOpen(res []StatementResult) bool {
	msg := openTransactionMessage(r.pgConn)
	if msg == "" {
		return false
	}
	rollbackQuietly(r.execCtx, r.exec)
	clearApplied(res)
	noteSequencesKept(res, false)
	res[0].Error = msg
	return true
}

// sequenceNote warns that a statement undone by a rollback may still
// have moved a sequence, since PostgreSQL never rolls back nextval or
// setval.
const sequenceNote = "this statement calls nextval or setval, and a rollback does not undo sequence changes, " +
	"so the sequence may have advanced or been set even though the statement was not applied"

// sequenceUnknownNote is sequenceNote for a statement whose outcome is
// unknown, which may or may not have been applied, or rolled back.
const sequenceUnknownNote = "this statement calls nextval or setval, and its outcome is unknown; sequence changes " +
	"are never rolled back, so the sequence may have advanced or been set whether or not the statement was applied"

// invalidIndexNote warns that a failed concurrent index build may have
// left an invalid index, which PostgreSQL does not remove.
const invalidIndexNote = "a concurrent index build that fails can leave an invalid index behind; " +
	"check pg_index.indisvalid and drop or rebuild the index"

// addNote appends note to the result's note.
func addNote(res *StatementResult, note string) {
	if res.Note != "" {
		res.Note += "; "
	}
	res.Note += note
}

// noteSequencesKept notes, on each statement in results that a rollback
// undid, that the rollback kept any sequence change it made. With
// unknown set, whether the statements were applied is not known, and
// the note says so rather than claiming a rollback.
func noteSequencesKept(results []StatementResult, unknown bool) {
	for i := range results {
		if sqllex.ChangesSequence(results[i].Statement) {
			addNote(&results[i], sequenceNoteFor(unknown))
		}
	}
}

// sequenceNoteFor returns the sequence note for a confirmed failure, or
// for an unknown outcome.
func sequenceNoteFor(unknown bool) string {
	if unknown {
		return sequenceUnknownNote
	}
	return sequenceNote
}

// noteFailure notes the effects a statement run on its own may have
// left when it failed after it was sent: a sequence change, or an
// invalid index from a concurrent build. unknown is set when the
// statement's outcome is not known.
func noteFailure(res *StatementResult, unknown bool) {
	if sqllex.ChangesSequence(res.Statement) {
		addNote(res, sequenceNoteFor(unknown))
	}
	if sqllex.BuildsIndexConcurrently(res.Statement) {
		addNote(res, invalidIndexNote)
	}
}

// lostDuring describes a round trip, named by what, that ended without
// a verdict from the server.
func lostDuring(err error, what string) string {
	if noResponse(err) {
		return noResponseMessage + " while " + what + " ran"
	}
	return "the connection was lost while " + what + " ran"
}

// openTransactionMessage returns a failure message when a statement left
// the connection in a transaction, which would carry into the statements
// after it, and an empty string otherwise.
func openTransactionMessage(c pgexec.ConnState) string {
	if pgexec.TxLeftOpen(c) {
		return "the statement left a transaction open, so it was rolled back; the statements after this one were not run"
	}
	return ""
}

// readResult reads the outcome of one statement sent over the simple
// query protocol, which sends the text unaltered, collecting its command
// tag, rows affected and result rows (up to rowLimit). It returns the
// error from the server or connection, if any, alongside the result; a
// changed client_encoding is reported in the result alone.
func readResult(mrr *pgconn.MultiResultReader, pgConn *pgconn.PgConn, stmt string, rowLimit int) (StatementResult, error) {
	res := StatementResult{Statement: stmt}
	for mrr.NextResult() {
		rr := mrr.ResultReader()
		if fields := rr.FieldDescriptions(); len(fields) > 0 && res.Columns == nil {
			res.Columns = make([]string, len(fields))
			for i, f := range fields {
				res.Columns[i] = f.Name
			}
		}
		var truncated bool
		res.Rows, truncated = pgexec.AppendRows(res.Rows, rr, rowLimit)
		res.Truncated = res.Truncated || truncated
		tag, err := rr.Close()
		if err != nil {
			continue // surfaced by mrr.Close below
		}
		res.CommandTag = tag.String()
		res.RowsAffected += tag.RowsAffected()
	}
	if err := mrr.Close(); err != nil {
		res.CommandTag = ""
		res.RowsAffected = 0
		return res, err
	}
	if msg := encodingMessage(pgConn); msg != "" {
		res.Error = msg
	}
	return res, nil
}

// encodingMessage reports a client_encoding change. The classifier reads
// statement text as UTF-8, so a changed encoding stops the run (see
// requireUTF8 in the query handlers).
func encodingMessage(c pgexec.ConnState) string {
	if !pgexec.EncodingIsUTF8(c) {
		return "client_encoding must remain UTF8; the statements after this one were not run"
	}
	return ""
}

// rollbackQuietly rolls back the open transaction, if any. A failure is
// only logged: the usual cause is a lost connection, and the server rolls
// back a transaction whose connection has gone.
func rollbackQuietly(ctx context.Context, exec rollback.SimpleExec) {
	if err := rollback.Simple(ctx, exec); err != nil {
		log.Printf("[WARN] Action rollback failed: %v", err)
	}
}

// safeExecError turns an execution error into text that is safe to store
// and show: the server's own message for a PostgreSQL error, and a fixed
// description otherwise, since other errors can quote connection details.
func safeExecError(err error) string {
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr):
		return pgErr.Message
	case errors.Is(err, context.Canceled):
		return "execution was stopped"
	case errors.Is(err, context.DeadlineExceeded):
		return noResponseMessage
	default:
		return "the database connection failed"
	}
}
