/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package stage recreates an incident on a disposable PostgreSQL
// instance and in the Workbench datastore before an investigation, and
// removes it afterwards.
//
// Each run gets its own database on the instance, created from scratch
// and dropped at the end, so runs cannot see each other's state. Only a
// fixture's Teardown SQL touches anything instance-wide. The harness
// must only ever be pointed at a throwaway instance: fixtures run
// arbitrary SQL as a superuser. Stage enforces this: it refuses to run
// unless both DSNs name only loopback hosts and the instance holds the
// DisposableMarker database, which run-evaluation.sh creates in its
// throwaway container.
package stage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/pgedge/ai-workbench/server/internal/evalharness/incident"
	"github.com/pgedge/ai-workbench/server/internal/sqllex"
)

// ConnectionSpec is what the Workbench needs to reach a staged database.
type ConnectionSpec struct {
	Name     string
	Host     string
	Port     int
	Database string
	Username string
	Password string
	SSLMode  string
}

// Registry adds and removes Workbench connections. APIRegistry does it
// through the REST API, as a user would.
type Registry interface {
	Create(ctx context.Context, spec ConnectionSpec) (int, error)
	Delete(ctx context.Context, id int) error
}

// Stager stages incidents.
type Stager struct {
	// AdminDSN connects to the disposable instance as a superuser; its
	// database is the one Teardown SQL runs in.
	AdminDSN string
	// DatastoreDSN connects to the Workbench datastore, where alerts and
	// metric history are inserted.
	DatastoreDSN string
	// Registry registers the staged database as a Workbench connection.
	Registry Registry
	// SettleTimeout bounds the wait for blocking workload sessions to be
	// seen waiting; it defaults to 30 seconds.
	SettleTimeout time.Duration
	// Now returns the current time; it defaults to time.Now.
	Now func() time.Time
	// RollbackTimeout bounds the cleanup after a failed Stage; it
	// defaults to DefaultRollbackTimeout.
	RollbackTimeout time.Duration

	// marker overrides DisposableMarker; the package tests use their
	// own database so they need not create the marker on a shared
	// instance.
	marker string
}

// DisposableMarker is the database whose presence marks an instance as
// disposable. Stage refuses an instance without it, because fixtures
// change instance-wide settings, drop replication slots and add
// partitions to the datastore.
const DisposableMarker = "ai_dba_evaluate_disposable"

// markerSQL reports whether the marker database exists.
const markerSQL = `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_database WHERE datname = $1)`

// DefaultRollbackTimeout bounds the cleanup after a failed Stage.
const DefaultRollbackTimeout = 2 * time.Minute

// RollbackError reports that cleaning up after a failed Stage also
// failed, so the instance may still hold the incident's changes and
// later replays on it cannot be trusted.
type RollbackError struct {
	Err error
}

func (e *RollbackError) Error() string {
	return "teardown after failed staging: " + e.Err.Error()
}

func (e *RollbackError) Unwrap() error { return e.Err }

// Staged is one staged incident.
type Staged struct {
	ConnectionID   int
	ConnectionName string
	Database       string
	// Role is the login role the Workbench connection uses. It holds
	// pg_monitor and pg_read_all_data rather than superuser, so Ellie
	// investigates with the privileges a monitoring user would have.
	Role string

	stager      *Stager
	roleCreated bool
	incident    *incident.Incident
	created     bool
	// setupStarted records that the fixture's setup began, so its
	// teardown SQL has something to undo.
	setupStarted bool
	cancel       context.CancelFunc
	sessions     []*pgx.Conn
	wg           sync.WaitGroup
}

// Stage creates the incident's database, runs its setup, registers the
// connection, inserts the alert and metric history and starts the
// workload. On failure it undoes whatever it had done; on success the
// caller must call Teardown.
func (s *Stager) Stage(ctx context.Context, inc *incident.Incident) (*Staged, error) {
	if err := s.checkDisposable(ctx); err != nil {
		return nil, err
	}
	suffix, err := randomSuffix()
	if err != nil {
		return nil, err
	}
	st := &Staged{
		Database:       "eval_" + strings.ReplaceAll(inc.ID, "-", "_") + "_" + suffix,
		ConnectionName: "eval-" + inc.ID + "-" + suffix,
		Role:           "eval_" + strings.ReplaceAll(inc.ID, "-", "_") + "_" + suffix,
		stager:         s,
		incident:       inc,
	}
	if err := st.stage(ctx); err != nil {
		// Clean up with a fresh, bounded context: ctx may be the reason
		// staging failed.
		timeout := s.RollbackTimeout
		if timeout <= 0 {
			timeout = DefaultRollbackTimeout
		}
		tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()
		if tdErr := st.Teardown(tctx); tdErr != nil {
			err = errors.Join(err, &RollbackError{Err: tdErr})
		}
		return nil, err
	}
	return st, nil
}

// checkDisposable returns an error unless both DSNs name only loopback
// hosts and each instance holds the marker database.
func (s *Stager) checkDisposable(ctx context.Context) error {
	marker := s.marker
	if marker == "" {
		marker = DisposableMarker
	}
	// Check both hosts before connecting to either.
	dsns := []struct{ name, dsn string }{{"admin", s.AdminDSN}, {"datastore", s.DatastoreDSN}}
	cfgs := make([]*pgx.ConnConfig, len(dsns))
	for i, d := range dsns {
		cfg, err := pgx.ParseConfig(d.dsn)
		if err != nil {
			return fmt.Errorf("parse %s DSN: %w", d.name, err)
		}
		if err := requireLoopback(cfg); err != nil {
			return fmt.Errorf("refusing to stage incidents: the %s DSN %w", d.name, err)
		}
		cfgs[i] = cfg
	}
	for i, d := range dsns {
		conn, err := pgx.ConnectConfig(ctx, cfgs[i])
		if err != nil {
			return fmt.Errorf("connect with the %s DSN: %w", d.name, err)
		}
		var ok bool
		err = conn.QueryRow(ctx, markerSQL, marker).Scan(&ok)
		closeConn(ctx, conn)
		if err != nil {
			return fmt.Errorf("look for the %s database with the %s DSN: %w", marker, d.name, err)
		}
		if !ok {
			return fmt.Errorf("refusing to stage incidents: the instance the %s DSN names has no %s database, "+
				"so it is not marked as disposable", d.name, marker)
		}
	}
	return nil
}

// requireLoopback returns an error unless every host cfg may connect to,
// the primary and each fallback, is localhost or a loopback address.
func requireLoopback(cfg *pgx.ConnConfig) error {
	hosts := []string{cfg.Host}
	for _, fb := range cfg.Fallbacks {
		hosts = append(hosts, fb.Host)
	}
	for _, host := range hosts {
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("names non-loopback host %q", host)
		}
	}
	return nil
}

// sslModeOf returns the libpq sslmode that pgx resolved for cfg, so the
// staged connection uses the same TLS mode as the admin DSN. pgx keeps
// no copy of the sslmode string, so it is read back from the TLS
// configurations pgx built from it; this follows pgx's own parsing of
// whitespace, quoting, the ssl=true alias, PGSSLMODE, sslrootcert and
// sslnegotiation, which a separate parser could get wrong and so
// downgrade TLS.
func sslModeOf(cfg *pgx.ConnConfig) string {
	plaintextFallback, tlsFallback := false, false
	for _, fb := range cfg.Fallbacks {
		// Only the primary host's own fallbacks show its mode; another
		// host, such as a Unix socket, has its own TLS settings.
		if fb.Host != cfg.Host || fb.Port != cfg.Port {
			continue
		}
		if fb.TLSConfig == nil {
			plaintextFallback = true
		} else {
			tlsFallback = true
		}
	}
	tc := cfg.TLSConfig
	switch {
	case tc == nil && tlsFallback:
		return "allow"
	case tc == nil:
		return "disable"
	case plaintextFallback:
		return "prefer"
	case !tc.InsecureSkipVerify:
		return "verify-full"
	case tc.VerifyPeerCertificate != nil:
		return "verify-ca"
	default:
		return "require"
	}
}

// createRoleSQL builds the CREATE ROLE for the connection's login role.
const createRoleSQL = `
    SELECT format('CREATE ROLE %I LOGIN PASSWORD %L IN ROLE pg_monitor, pg_read_all_data',
                  $1::text, $2::text)`

func randomPassword() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate role password: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func randomSuffix() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate run suffix: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func (st *Staged) stage(ctx context.Context) error {
	s := st.stager
	adminCfg, err := pgx.ParseConfig(s.AdminDSN)
	if err != nil {
		return fmt.Errorf("parse admin DSN: %w", err)
	}
	sslMode := sslModeOf(adminCfg)

	if err := execGenerated(ctx, adminCfg, "SELECT format('CREATE DATABASE %I', $1::text)", st.Database); err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	st.created = true

	st.setupStarted = true
	if err := runStatements(ctx, st.databaseConfig(adminCfg), st.incident.Setup); err != nil {
		return fmt.Errorf("setup: %w", err)
	}

	password, err := randomPassword()
	if err != nil {
		return err
	}
	if err := execGenerated(ctx, adminCfg, createRoleSQL, st.Role, password); err != nil {
		return fmt.Errorf("create role: %w", err)
	}
	st.roleCreated = true

	id, err := s.Registry.Create(ctx, ConnectionSpec{
		Name: st.ConnectionName, Host: adminCfg.Host, Port: int(adminCfg.Port),
		Database: st.Database, Username: st.Role, Password: password,
		SSLMode: sslMode,
	})
	if err != nil {
		return fmt.Errorf("register connection: %w", err)
	}
	st.ConnectionID = id

	if err := st.seedDatastore(ctx); err != nil {
		return err
	}
	return st.startWorkload(ctx, adminCfg)
}

// databaseConfig returns a copy of cfg pointed at the staged database.
func (st *Staged) databaseConfig(cfg *pgx.ConnConfig) *pgx.ConnConfig {
	c := cfg.Copy()
	c.Database = st.Database
	return c
}

// execGenerated runs a query that returns a DDL statement built by the
// server with format('%I'), then executes that statement. Quoting the
// identifier on the server keeps the Go SQL constant.
func execGenerated(ctx context.Context, cfg *pgx.ConnConfig, query string, args ...any) error {
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeConn(ctx, conn)
	var stmt string
	if err := conn.QueryRow(ctx, query, args...).Scan(&stmt); err != nil {
		return err
	}
	_, err = conn.Exec(ctx, stmt)
	return err
}

// runStatements runs each statement of a fixture SQL script on its own,
// so that statements that cannot run in a transaction block (VACUUM,
// ALTER SYSTEM) work.
func runStatements(ctx context.Context, cfg *pgx.ConnConfig, script string) error {
	statements := sqllex.SplitStatements(script, sqllex.StandardStrings)
	if len(statements) == 0 {
		return nil
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeConn(ctx, conn)
	return execAll(ctx, conn, statements)
}

// closeTimeout bounds closing a connection.
const closeTimeout = 5 * time.Second

// closeConn closes conn even when ctx is already canceled, so the server
// sees a clean Terminate, but never waits longer than ctx's deadline or
// closeTimeout, whichever is sooner. pgx closes the socket either way.
func closeConn(ctx context.Context, conn *pgx.Conn) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeTimeout)
	defer cancel()
	if dl, ok := ctx.Deadline(); ok {
		var cancelDL context.CancelFunc
		cctx, cancelDL = context.WithDeadline(cctx, dl)
		defer cancelDL()
	}
	_ = conn.Close(cctx)
}

func execAll(ctx context.Context, conn *pgx.Conn, statements []string) error {
	for _, stmt := range statements {
		if _, err := conn.Exec(ctx, stmt, pgx.QueryExecModeSimpleProtocol); err != nil {
			return fmt.Errorf("%s: %w", firstLine(stmt), err)
		}
	}
	return nil
}

func firstLine(stmt string) string {
	line, _, _ := strings.Cut(stmt, "\n")
	if len(line) > 80 {
		line = line[:80] + "..."
	}
	return line
}

const insertAlertSQL = `
    INSERT INTO alerts (alert_type, connection_id, database_name, probe_name,
                        metric_name, metric_value, threshold_value, operator,
                        severity, title, description, object_name, status,
                        triggered_at, last_updated)
    VALUES ('threshold', $1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, $6,
            NULLIF($7, ''), $8, $9, $10, NULLIF($11, ''), 'active', $12, $12)`

// metricsColumnsSQL lists a metrics table's columns; no rows means the
// table does not exist.
const metricsColumnsSQL = `
    SELECT column_name::text FROM information_schema.columns
     WHERE table_schema = 'metrics' AND table_name = $1`

// defaultPartitionSQL returns the DDL that gives a partitioned metrics
// table a DEFAULT partition, so recorded samples of any age have
// somewhere to go, or no row when it has one already or is not
// partitioned.
const defaultPartitionSQL = `
    SELECT format('CREATE TABLE metrics.%I PARTITION OF metrics.%I DEFAULT',
                  $1::text || '_eval_default', $1::text)
      FROM pg_catalog.pg_partitioned_table
     WHERE partrelid = to_regclass(format('metrics.%I', $1::text))
       AND partdefid = 0`

// insertSampleSQL returns the INSERT for one metrics table. The row is
// built from JSON by jsonb_populate_record, so the column list need not
// be assembled in Go.
const insertSampleSQL = `
    SELECT format('INSERT INTO metrics.%1$I SELECT * FROM jsonb_populate_record(NULL::metrics.%1$I, $1::jsonb)',
                  $1::text)`

func (st *Staged) seedDatastore(ctx context.Context) error {
	s := st.stager
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	start := now().UTC()

	conn, err := pgx.Connect(ctx, s.DatastoreDSN)
	if err != nil {
		return fmt.Errorf("connect to datastore: %w", err)
	}
	defer closeConn(ctx, conn)

	a := st.incident.Alert
	triggered := start.Add(-time.Duration(a.TriggeredAgo))
	if _, err := conn.Exec(ctx, insertAlertSQL, st.ConnectionID, st.Database, a.Probe, a.Metric,
		a.Value, a.Threshold, a.Operator, a.Severity, a.Title, strings.TrimSpace(a.Description),
		a.Object, triggered); err != nil {
		return fmt.Errorf("insert alert: %w", err)
	}

	for _, series := range st.incident.Metrics {
		if err := insertSeries(ctx, conn, st.ConnectionID, start, series); err != nil {
			return fmt.Errorf("metrics.%s: %w", series.Table, err)
		}
	}
	return nil
}

func insertSeries(ctx context.Context, conn *pgx.Conn, connID int, start time.Time, series incident.MetricSeries) error {
	rows, err := conn.Query(ctx, metricsColumnsSQL, series.Table)
	if err != nil {
		return err
	}
	cols, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	if len(cols) == 0 {
		return errors.New("no such table in the datastore")
	}
	known := make(map[string]bool, len(cols))
	for _, c := range cols {
		known[c] = true
	}
	for _, sample := range series.Samples {
		for col := range sample.Values {
			if !known[col] {
				return fmt.Errorf("unknown column %q", col)
			}
		}
	}

	var ddl string
	switch err := conn.QueryRow(ctx, defaultPartitionSQL, series.Table).Scan(&ddl); {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return err
	default:
		if _, err := conn.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("add default partition: %w", err)
		}
	}

	var insert string
	if err := conn.QueryRow(ctx, insertSampleSQL, series.Table).Scan(&insert); err != nil {
		return err
	}
	for _, sample := range series.Samples {
		row := make(map[string]any, len(sample.Values)+2)
		for k, v := range sample.Values {
			row[k] = v
		}
		row["connection_id"] = connID
		row["collected_at"] = start.Add(-time.Duration(sample.Ago)).Format(time.RFC3339Nano)
		data, err := json.Marshal(row)
		if err != nil {
			return fmt.Errorf("encode sample: %w", err)
		}
		if _, err := conn.Exec(ctx, insert, string(data)); err != nil {
			return fmt.Errorf("insert sample: %w", err)
		}
	}
	return nil
}

// lockWaitersSQL counts sessions in a database that are waiting on a
// heavyweight lock.
const lockWaitersSQL = `
    SELECT count(*) FROM pg_catalog.pg_stat_activity
     WHERE datname = $1 AND wait_event_type = 'Lock'`

func (st *Staged) startWorkload(ctx context.Context, adminCfg *pgx.ConnConfig) error {
	if len(st.incident.Workload) == 0 {
		return nil
	}
	// The workload outlives this call, so it gets its own context,
	// canceled by Teardown.
	wctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	st.cancel = cancel

	cfg := st.databaseConfig(adminCfg)
	blocking := 0
	failures := make(chan error, len(st.incident.Workload))
	for _, sess := range st.incident.Workload {
		conn, err := pgx.ConnectConfig(ctx, cfg)
		if err != nil {
			return fmt.Errorf("workload %s: connect: %w", sess.Name, err)
		}
		st.sessions = append(st.sessions, conn)
		statements := sqllex.SplitStatements(sess.SQL, sqllex.StandardStrings)
		if !sess.Blocks {
			if err := execAll(ctx, conn, statements); err != nil {
				return fmt.Errorf("workload %s: %w", sess.Name, err)
			}
			continue
		}
		blocking++
		st.wg.Add(1)
		go func(name string) {
			defer st.wg.Done()
			if err := execAll(wctx, conn, statements); err != nil && wctx.Err() == nil {
				failures <- fmt.Errorf("workload %s: %w", name, err)
				return
			}
			if wctx.Err() == nil {
				failures <- fmt.Errorf("workload %s finished instead of waiting on a lock", name)
			}
		}(sess.Name)
	}
	if blocking == 0 {
		return nil
	}
	return st.waitForLockWaiters(ctx, adminCfg, blocking, failures)
}

// waitForLockWaiters polls until want sessions are waiting on locks in
// the staged database, a blocking session fails, or the settle timeout
// passes.
func (st *Staged) waitForLockWaiters(ctx context.Context, adminCfg *pgx.ConnConfig, want int, failures <-chan error) error {
	timeout := st.stager.SettleTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	// The settle deadline also bounds the watch connection and its
	// queries, so a stalled database cannot hold staging up.
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	settleErr := func(waiting int) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("only %d of %d blocking workload sessions were waiting on a lock after %s", waiting, want, timeout)
	}
	conn, err := pgx.ConnectConfig(sctx, adminCfg)
	if err != nil {
		return fmt.Errorf("connect to watch the workload: %w", err)
	}
	defer closeConn(ctx, conn)

	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	waiting := 0
	for {
		if err := conn.QueryRow(sctx, lockWaitersSQL, st.Database).Scan(&waiting); err != nil {
			if sctx.Err() != nil {
				return settleErr(waiting)
			}
			return fmt.Errorf("watch the workload: %w", err)
		}
		// A session that failed after being counted must not let the
		// count through.
		select {
		case err := <-failures:
			return err
		default:
		}
		if waiting >= want {
			return nil
		}
		select {
		case err := <-failures:
			return err
		case <-sctx.Done():
			return settleErr(waiting)
		case <-tick.C:
		}
	}
}

// PromptData returns the details of the staged incident that the
// investigation prompt refers to.
func (st *Staged) PromptData() incident.PromptData {
	return incident.PromptData{
		ConnectionID:   st.ConnectionID,
		ConnectionName: st.ConnectionName,
		Database:       st.Database,
	}
}

// Teardown stops the workload, removes the connection (and with it the
// alert and metric history), drops the database and the login role, and
// runs the fixture's Teardown SQL if setup began. It carries on past failures and reports them all.
func (st *Staged) Teardown(ctx context.Context) error {
	var errs []error
	if st.cancel != nil {
		st.cancel()
	}
	st.wg.Wait()
	for _, c := range st.sessions {
		if err := c.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("close workload session: %w", err))
		}
	}
	st.sessions = nil

	s := st.stager
	if st.ConnectionID != 0 {
		if err := s.Registry.Delete(ctx, st.ConnectionID); err != nil {
			errs = append(errs, fmt.Errorf("delete connection %d: %w", st.ConnectionID, err))
		}
		st.ConnectionID = 0
	}

	adminCfg, err := pgx.ParseConfig(s.AdminDSN)
	if err != nil {
		return errors.Join(append(errs, fmt.Errorf("parse admin DSN: %w", err))...)
	}
	if st.created {
		if err := execGenerated(ctx, adminCfg, "SELECT format('DROP DATABASE IF EXISTS %I WITH (FORCE)', $1::text)", st.Database); err != nil {
			errs = append(errs, fmt.Errorf("drop database %s: %w", st.Database, err))
		}
		st.created = false
	}
	if st.roleCreated {
		if err := execGenerated(ctx, adminCfg, "SELECT format('DROP ROLE IF EXISTS %I', $1::text)", st.Role); err != nil {
			errs = append(errs, fmt.Errorf("drop role %s: %w", st.Role, err))
		}
		st.roleCreated = false
	}
	// The fixture's teardown SQL undoes its setup, so it runs only once
	// setup has begun.
	if st.setupStarted {
		if err := runStatements(ctx, adminCfg, st.incident.Teardown); err != nil {
			errs = append(errs, fmt.Errorf("teardown SQL: %w", err))
		}
	}
	return errors.Join(errs...)
}
