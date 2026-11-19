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

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/pkg/rollback"
	"github.com/pgedge/ai-workbench/server/internal/pgexec"
	"github.com/pgedge/ai-workbench/server/internal/sqllex"
)

var (
	// ErrAlreadyStarted is returned by Start when the action cannot be
	// claimed: it has already run or is running, or it has been neither
	// reviewed nor overridden.
	ErrAlreadyStarted = errors.New("action cannot be started")

	// ErrShuttingDown is returned by Start after Shutdown has been called.
	ErrShuttingDown = errors.New("executor is shutting down")
)

// defaultRowLimit caps the rows kept per statement when the caller gives
// no positive limit.
const defaultRowLimit = 1000

// finishTimeout bounds each attempt to record an outcome, which must
// complete even after the run has been stopped.
const finishTimeout = 30 * time.Second

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
	// finishBackoff is the first delay between attempts to record an
	// outcome.
	finishBackoff time.Duration
	// stopping is closed by Shutdown, abandoning outcome retries.
	stopping chan struct{}
	// responseGrace is added to statementTimeout to give the deadline
	// for each round trip to the target.
	responseGrace time.Duration

	mu      sync.Mutex
	closed  bool
	running map[int64]*runningAction
	wg      sync.WaitGroup
}

// NewExecutor returns an Executor. rowLimit caps the result rows kept per
// statement; a value below one selects a default of 1000.
func NewExecutor(store *Store, opener Opener, lockTimeout, statementTimeout time.Duration, rowLimit int) *Executor {
	if rowLimit < 1 {
		rowLimit = defaultRowLimit
	}
	return &Executor{
		store:            store,
		opener:           opener,
		lockTimeout:      lockTimeout,
		statementTimeout: statementTimeout,
		rowLimit:         rowLimit,
		outcomes:         store,
		finishBackoff:    finishBackoffBase,
		stopping:         make(chan struct{}),
		responseGrace:    responseGrace,
		running:          make(map[int64]*runningAction),
	}
}

// Shutdown stops the executor taking new actions, cancels the running
// ones and waits for them to record their outcomes, or for ctx to end.
func (e *Executor) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	if !e.closed {
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
// ErrAlreadyStarted when the claim fails and ErrShuttingDown after
// Shutdown.
func (e *Executor) Start(ctx context.Context, actionID int64) error {
	// The WaitGroup is incremented under the same lock Shutdown takes to
	// set closed, so Shutdown's Wait never races with an Add.
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return ErrShuttingDown
	}
	e.wg.Add(1)
	e.mu.Unlock()

	ok, err := e.store.ClaimForExecution(ctx, actionID)
	if err != nil || !ok {
		e.wg.Done()
		if err != nil {
			return err
		}
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
// action executing until the next startup marks it unknown. Retries stop
// at Shutdown.
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

// pause waits for d and reports true, or reports false at once if
// Shutdown is called first.
func (e *Executor) pause(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-e.stopping:
		return false
	}
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
	// is never sent, or finds it in flight and cancels it.
	mu       sync.Mutex
	inFlight bool
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
	if err := r.pgConn.CancelRequest(ctx); err != nil {
		log.Printf("[WARN] Action %d: cancel request failed: %v", r.actionID, err)
	}
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
	failed := false
	for i := range results {
		_, err := r.step(results, i)
		if err != nil || results[i].Error != "" {
			failed = true
			break
		}
	}
	if failed {
		rollbackQuietly(r.execCtx, r.exec)
		clearApplied(results)
		return
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
		st.results = append(results, StatementResult{Statement: "COMMIT", Error: stoppedBeforeCommitMessage})
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
		results = append(results, commit)
		st.results = results
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
		var pgErr *pgconn.PgError
		if err != nil && !errors.As(err, &pgErr) {
			results[i].Error = "outcome unknown: " + lostDuring(err, "the statement")
			st.unknown = true
			return
		}
		// A statement stopped by a cancel request may have left partial
		// work (an invalid index, say), so its outcome is unknown.
		if pgErr != nil && pgErr.Code == "57014" && r.ctx.Err() != nil {
			results[i].Applied = false
			results[i].Error = "outcome unknown: the statement was stopped part way through"
			st.unknown = true
			return
		}
		// A statement that ran without a server error took effect, even
		// if a check on its side effects then stops the batch.
		results[i].Applied = ran && err == nil
		if results[i].Error != "" {
			return
		}
		// Defensive: a statement cannot normally open a transaction,
		// because transaction control is refused before anything runs.
		if msg := openTransactionMessage(r.pgConn); msg != "" {
			rollbackQuietly(r.execCtx, r.exec)
			clearApplied(results[i : i+1])
			results[i].Error = msg
			return
		}
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
