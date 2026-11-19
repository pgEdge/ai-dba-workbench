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
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/pkg/rollback"
	"github.com/pgedge/ai-workbench/server/internal/sqllex"
)

// ErrAlreadyStarted is returned by Start when the action cannot be
// claimed: it has already run or is running, or it has been neither
// reviewed nor overridden.
var ErrAlreadyStarted = errors.New("action cannot be started")

// defaultRowLimit caps the rows kept per statement when the caller gives
// no positive limit.
const defaultRowLimit = 1000

// finishTimeout bounds the write that records the outcome, which runs on
// a context that ignores cancellation so a stopped action still gets
// its result recorded.
const finishTimeout = 30 * time.Second

// Opener opens a single-connection pool on the monitored database an
// action targets. The executor closes the pool after use. The pool must
// not set default_transaction_read_only.
type Opener func(ctx context.Context, connectionID int, databaseName *string) (*pgxpool.Pool, error)

// runningAction is the handle Cancel uses for an executing action.
type runningAction struct {
	cancel        context.CancelFunc
	stopRequested atomic.Bool
}

// Executor runs approved actions against their target databases.
type Executor struct {
	store            *Store
	opener           Opener
	lockTimeout      time.Duration
	statementTimeout time.Duration
	rowLimit         int

	mu      sync.Mutex
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
		running:          make(map[int64]*runningAction),
	}
}

// Wait blocks until every action started so far has finished. It is
// meant for shutdown and tests.
func (e *Executor) Wait() {
	e.wg.Wait()
}

// Start claims the action and runs it in a goroutine detached from ctx's
// cancellation, recording the outcome on the action. It returns
// ErrAlreadyStarted when the claim fails.
func (e *Executor) Start(ctx context.Context, actionID int64) error {
	ok, err := e.store.ClaimForExecution(ctx, actionID)
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
	e.mu.Unlock()

	e.wg.Add(1)
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

// Cancel cancels a running action and reports whether one was running.
// The action is then recorded with the stopped-by-request status.
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
	var (
		status        = "failed"
		transactional = true
		results       []StatementResult
	)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ERROR] Action %d executor panicked: %v", id, r)
			status = "failed"
			if len(results) == 0 {
				results = []StatementResult{{Error: "internal error while executing the action"}}
			}
		}
		if run.stopRequested.Load() {
			status = "cancelled" //nolint:misspell // value fixed by the table CHECK
		}
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
		defer cancel()
		if err := e.store.FinishExecution(finishCtx, id, status, transactional, results); err != nil {
			log.Printf("[ERROR] Failed to record outcome of action %d: %v", id, err)
		}
	}()

	action, err := e.store.Get(ctx, id)
	if err != nil {
		log.Printf("[ERROR] Failed to load action %d: %v", id, err)
		results = []StatementResult{{Error: "could not load the action"}}
		return
	}
	status, transactional, results = e.runAction(ctx, action)
}

// runAction runs the action's statements and returns the end status,
// whether they ran in one transaction and the per-statement results.
func (e *Executor) runAction(ctx context.Context, a *Action) (string, bool, []StatementResult) {
	stmts, badIdx, err := prepareStatements(a.Statements)
	transactional := !slices.ContainsFunc(stmts, sqllex.RequiresNoTransaction)
	if err != nil {
		return "failed", transactional, failedBefore(stmts, badIdx, err.Error())
	}

	pool, err := e.opener(ctx, a.ConnectionID, a.DatabaseName)
	if err != nil {
		// The error can quote the connection string, so only a fixed
		// message goes into the result.
		log.Printf("[ERROR] Action %d: could not open the target database", a.ID)
		return "failed", transactional, failedBefore(stmts, 0, "could not connect to the target database")
	}
	defer pool.Close()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		log.Printf("[ERROR] Action %d: could not acquire a connection", a.ID)
		return "failed", transactional, failedBefore(stmts, 0, safeExecError(err))
	}
	defer conn.Release()
	pgConn := conn.Conn().PgConn()
	exec := func(c context.Context, sql string) error {
		return pgConn.Exec(c, sql).Close()
	}

	for _, setting := range []struct {
		name string
		d    time.Duration
	}{{"lock_timeout", e.lockTimeout}, {"statement_timeout", e.statementTimeout}} {
		if err := exec(ctx, fmt.Sprintf("SET %s = %d", setting.name, setting.d.Milliseconds())); err != nil {
			return "failed", transactional, failedBefore(stmts, 0, "could not set "+setting.name+": "+safeExecError(err))
		}
	}

	var results []StatementResult
	if transactional {
		results = e.runTransaction(ctx, pgConn, exec, stmts)
	} else {
		results = e.runIndividually(ctx, pgConn, exec, stmts)
	}
	status := "succeeded"
	for i := range results {
		if results[i].Error != "" {
			status = "failed"
			break
		}
	}
	return status, transactional, results
}

// failedBefore reports every statement as not applied, with message on
// the statement at index bad.
func failedBefore(stmts []string, bad int, message string) []StatementResult {
	results := make([]StatementResult, len(stmts))
	for i, s := range stmts {
		results[i] = StatementResult{Statement: s}
	}
	if bad >= 0 && bad < len(results) {
		results[bad].Error = message
	}
	return results
}

// prepareStatements flattens the stored statements into single
// statements, and refuses what must not run. A stored entry is split with
// SplitStatements under both readings of a plain quoted literal; if they
// disagree the text is ambiguous and is refused, because the server might
// split it differently from the classifier. Explicit transaction control
// is refused because the executor owns the transaction. The returned
// index is the offending statement, or -1 with a nil error.
func prepareStatements(stored []string) ([]string, int, error) {
	var out []string
	bad := -1
	var badErr error
	for _, s := range stored {
		parts := sqllex.SplitStatements(s, sqllex.StandardStrings)
		if !slices.Equal(parts, sqllex.SplitStatements(s, sqllex.EscapeStrings)) {
			return append(out, s), len(out), errors.New("statement is ambiguous: it splits differently depending on how string literals are read")
		}
		for _, p := range parts {
			if bad < 0 && sqllex.IsTransactionControl(p) {
				bad = len(out)
				badErr = errors.New("transaction control statements are not allowed in an action")
			}
			out = append(out, p)
		}
	}
	return out, bad, badErr
}

// runTransaction runs every statement in one transaction. The first
// failure rolls it back, and every statement is then reported as not
// applied. A clean run commits and reports every statement as applied.
func (e *Executor) runTransaction(ctx context.Context, pgConn *pgconn.PgConn, exec rollback.SimpleExec, stmts []string) []StatementResult {
	results := make([]StatementResult, len(stmts))
	for i, s := range stmts {
		results[i] = StatementResult{Statement: s}
	}
	if err := exec(ctx, "BEGIN"); err != nil {
		results[0].Error = "could not start a transaction: " + safeExecError(err)
		return results
	}
	failed := false
	for i, s := range stmts {
		r := runOne(ctx, pgConn, s, e.rowLimit)
		r.Applied = false
		results[i] = r
		if r.Error != "" {
			failed = true
			break
		}
	}
	if failed {
		rollbackQuietly(ctx, exec)
		for i := range results {
			results[i].Applied = false
		}
		return results
	}
	if err := exec(ctx, "COMMIT"); err != nil {
		rollbackQuietly(ctx, exec)
		results[len(results)-1].Error = "commit failed: " + safeExecError(err)
		return results
	}
	for i := range results {
		results[i].Applied = true
	}
	return results
}

// runIndividually runs each statement on its own, stopping at the first
// failure. Statements after it are reported as not applied.
func (e *Executor) runIndividually(ctx context.Context, pgConn *pgconn.PgConn, exec rollback.SimpleExec, stmts []string) []StatementResult {
	results := make([]StatementResult, len(stmts))
	for i, s := range stmts {
		results[i] = StatementResult{Statement: s}
	}
	for i, s := range stmts {
		r := runOne(ctx, pgConn, s, e.rowLimit)
		r.Applied = r.Error == ""
		if r.Error == "" {
			if msg := openTransactionMessage(pgConn); msg != "" {
				rollbackQuietly(ctx, exec)
				r.Error = msg
				r.Applied = false
			}
		}
		results[i] = r
		if r.Error != "" {
			break
		}
	}
	return results
}

// openTransactionMessage returns a failure message when a statement left
// the connection in a transaction, which would carry into the statements
// after it, and an empty string otherwise.
func openTransactionMessage(c connState) string {
	if c.TxStatus() != 'I' {
		return "the statement left a transaction open, so it was rolled back; the statements after this one were not run"
	}
	return ""
}

// connState is the part of *pgconn.PgConn that the post-statement check
// reads.
type connState interface {
	ParameterStatus(key string) string
	TxStatus() byte
}

// runOne runs one statement over the simple query protocol, which sends
// the text unaltered, and collects its command tag, rows affected and
// result rows (up to rowLimit). In a transaction the encoding check
// still applies, so a statement that changes client_encoding fails.
func runOne(ctx context.Context, pgConn *pgconn.PgConn, stmt string, rowLimit int) StatementResult {
	res := StatementResult{Statement: stmt}
	start := time.Now()
	defer func() { res.DurationMS = time.Since(start).Milliseconds() }()

	mrr := pgConn.Exec(ctx, stmt)
	for mrr.NextResult() {
		rr := mrr.ResultReader()
		if fields := rr.FieldDescriptions(); len(fields) > 0 && res.Columns == nil {
			res.Columns = make([]string, len(fields))
			for i, f := range fields {
				res.Columns[i] = f.Name
			}
		}
		for rr.NextRow() {
			if len(res.Rows) >= rowLimit {
				res.Truncated = true
				continue
			}
			vals := rr.Values()
			row := make([]string, len(vals))
			for i, v := range vals {
				if v == nil {
					row[i] = "NULL"
				} else {
					row[i] = string(v)
				}
			}
			res.Rows = append(res.Rows, row)
		}
		tag, err := rr.Close()
		if err != nil {
			continue // surfaced by mrr.Close below
		}
		res.CommandTag = tag.String()
		res.RowsAffected += tag.RowsAffected()
	}
	if err := mrr.Close(); err != nil {
		res.Error = safeExecError(err)
		res.CommandTag = ""
		res.RowsAffected = 0
		return res
	}
	if msg := encodingMessage(pgConn); msg != "" {
		res.Error = msg
	}
	return res
}

// encodingMessage reports a client_encoding change. The classifier reads
// statement text as UTF-8, so a changed encoding stops the run (see
// requireUTF8 in the query handlers).
func encodingMessage(c connState) string {
	if c.ParameterStatus("client_encoding") != "UTF8" {
		return "client_encoding must remain UTF8; the statements after this one were not run"
	}
	return ""
}

// rollbackQuietly rolls back the open transaction, if any. A failure is
// only logged: the usual cause is a connection that cancellation already
// closed, and the server rolls back a transaction whose connection has
// gone.
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
		return "execution timed out"
	default:
		return "the database connection failed"
	}
}
