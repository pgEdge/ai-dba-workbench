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
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// freezingProxy forwards TCP connections to a target until frozen. Once
// frozen it keeps every socket open but forwards nothing in either
// direction, like a proxy that has stopped passing traffic on.
type freezingProxy struct {
	ln     net.Listener
	target string
	frozen atomic.Bool
	mu     sync.Mutex
	conns  []net.Conn
	// closed is set by close under mu, so that a connection accepted
	// while the proxy is closing is closed rather than registered after
	// the walk over conns and leaked.
	closed bool
}

func newFreezingProxy(t *testing.T, target string) *freezingProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &freezingProxy{ln: ln, target: target}
	t.Cleanup(p.close)
	go p.serve()
	return p
}

func (p *freezingProxy) serve() {
	for {
		client, err := p.ln.Accept()
		if err != nil {
			return
		}
		server, err := net.Dial("tcp", p.target)
		if err != nil {
			_ = client.Close()
			continue
		}
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			_ = client.Close()
			_ = server.Close()
			return
		}
		p.conns = append(p.conns, client, server)
		p.mu.Unlock()
		go p.pipe(server, client)
		go p.pipe(client, server)
	}
}

// pipe copies from src to dst, discarding what it reads once frozen.
func (p *freezingProxy) pipe(dst io.Writer, src io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if err != nil {
			return
		}
		if p.frozen.Load() {
			continue
		}
		if _, err := dst.Write(buf[:n]); err != nil {
			return
		}
	}
}

func (p *freezingProxy) close() {
	_ = p.ln.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for _, c := range p.conns {
		_ = c.Close()
	}
}

// proxiedRunner returns a stmtRunner whose connection to the fixture's
// target passes through a freezing proxy, with a short response deadline.
// The proxy forwards bytes, so a TLS session is negotiated through it as
// it would be directly; each fallback (the non-TLS attempt of sslmode
// prefer, say) is pointed at the proxy too. A target reached over a Unix
// socket cannot be proxied over TCP, so the test is skipped for one.
func (f *execFixture) proxiedRunner(t *testing.T) (*stmtRunner, *freezingProxy) {
	t.Helper()
	cfg := f.target.Config().ConnConfig.Config.Copy()
	if isUnixSocket(cfg.Host) {
		t.Skipf("the test target %q is a Unix socket; the freezing proxy needs a TCP target", cfg.Host)
	}
	p := newFreezingProxy(t, net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))))
	host, port, err := net.SplitHostPort(p.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Host, cfg.Port = host, uint16(portNum)
	fallbacks := cfg.Fallbacks[:0]
	for _, fb := range cfg.Fallbacks {
		// Only fallbacks to the same server differ in transport alone;
		// another host would bypass the proxy.
		if fb.Host == f.target.Config().ConnConfig.Host && fb.Port == f.target.Config().ConnConfig.Port {
			fb.Host, fb.Port = host, uint16(portNum)
			fallbacks = append(fallbacks, fb)
		}
	}
	cfg.Fallbacks = fallbacks
	pgConn, err := pgconn.ConnectConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pgConn.Close(context.Background()) })
	return &stmtRunner{
		ctx: context.Background(), execCtx: context.Background(), pgConn: pgConn,
		rowLimit: 10, settings: "SET lock_timeout = 1000", timeout: 300 * time.Millisecond,
		exec: func(c context.Context, sql string) error { return pgConn.Exec(c, sql).Close() },
	}, p
}

// isUnixSocket reports whether a pgconn host names a Unix socket
// directory, as pgconn reads it.
func isUnixSocket(host string) bool {
	return strings.HasPrefix(host, "/")
}

// TestRunnerDeadlineWhenTargetStopsResponding covers a target that keeps
// the connection open but stops answering. Each round trip gives up at
// its deadline and closes the connection; a statement that may have
// reached the server outside a transaction, or a COMMIT, has an unknown
// outcome, whilst one inside a transaction that was never committed
// simply failed.
func TestRunnerDeadlineWhenTargetStopsResponding(t *testing.T) {
	t.Parallel()
	f := newExecFixture(t, "_x_dl", time.Second, time.Minute, 10)
	mustExec(t, f.target, "CREATE SEQUENCE dl_seq")

	// The statement calls nextval, so its note shows whether the run
	// claims a rollback (sequenceNote) or an unknown outcome.
	tests := []struct {
		name          string
		transactional bool
		freezeAt      string // freeze before this exec, or before the statement when empty
		wantUnknown   bool
		wantIn        string // where the message is expected: statement or commit
		wantNote      string
	}{
		{"statement outside a transaction", false, "", true, "statement", sequenceUnknownNote},
		{"statement in a transaction", true, "", false, "statement", sequenceNote},
		{"commit", true, "COMMIT", true, "commit", sequenceUnknownNote},
		{"timeouts", false, "SET", false, "statement", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, p := f.proxiedRunner(t)
			real := r.exec
			r.exec = func(c context.Context, sql string) error {
				if tc.freezeAt != "" && strings.HasPrefix(sql, tc.freezeAt) {
					p.frozen.Store(true)
				}
				return real(c, sql)
			}
			if tc.freezeAt == "" {
				r.beforeDispatch = func() { p.frozen.Store(true) }
			}
			st := &runState{results: newResults([]string{"SELECT nextval('dl_seq')"}), transactional: tc.transactional}
			if tc.transactional {
				r.runTransaction(st)
			} else {
				r.runIndividually(st)
			}
			if st.unknown != tc.wantUnknown {
				t.Errorf("unknown = %v, want %v: %+v", st.unknown, tc.wantUnknown, st.results)
			}
			res := st.results[0]
			if tc.wantIn == "commit" {
				if len(st.results) != 2 {
					t.Fatalf("results = %+v", st.results)
				}
				res = st.results[1]
			}
			if !strings.Contains(res.Error, "stopped responding") || res.Applied {
				t.Errorf("result = %+v", res)
			}
			if note := st.results[0].Note; note != tc.wantNote {
				t.Errorf("note = %q, want %q", note, tc.wantNote)
			}
			if !r.pgConn.IsClosed() {
				t.Error("the connection was left open")
			}
		})
	}
}

// TestExecuteAcquireDeadline proves the wait for a connection is bounded
// by the response deadline: a target that accepts the connection and
// never answers fails the action instead of hanging it.
func TestExecuteAcquireDeadline(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var held []net.Conn
	var mu sync.Mutex
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			_ = c.Close()
		}
	})

	f := newExecFixture(t, "_x_dla", time.Second, 100*time.Millisecond, 10)
	f.exec.responseGrace = 100 * time.Millisecond
	f.exec.opener = func(ctx context.Context, _ int, _ *string) (*pgxpool.Pool, error) {
		cfg := f.target.Config().Copy()
		host, port, _ := net.SplitHostPort(ln.Addr().String())
		portNum, _ := strconv.Atoi(port)
		cfg.ConnConfig.Host, cfg.ConnConfig.Port = host, uint16(portNum)
		cfg.ConnConfig.Fallbacks, cfg.ConnConfig.TLSConfig = nil, nil
		cfg.ConnConfig.ConnectTimeout = 0
		cfg.MaxConns = 1
		return pgxpool.NewWithConfig(ctx, cfg)
	}
	a := f.run(t, "CREATE TABLE never_made (id int)")
	if a.ExecStatus != "failed" || !strings.Contains(a.Results[0].Error, "stopped responding") {
		t.Fatalf("action = %s %+v", a.ExecStatus, a.Results)
	}
}

// TestRunnerCancelInFlightFails proves a cancel request that cannot
// reach the server is logged and returns, rather than blocking or
// panicking: once the proxy has gone, the cancel connection is refused.
func TestRunnerCancelInFlightFails(t *testing.T) {
	t.Parallel()
	f := newExecFixture(t, "_x_dlc", time.Second, time.Minute, 10)
	r, p := f.proxiedRunner(t)
	p.close()
	r.inFlight = true
	r.cancelInFlight()
}
