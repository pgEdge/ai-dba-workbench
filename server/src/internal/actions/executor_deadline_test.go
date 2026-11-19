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
	for _, c := range p.conns {
		_ = c.Close()
	}
}

// proxiedRunner returns a stmtRunner whose connection to the fixture's
// target passes through a freezing proxy, with a short response deadline.
func (f *execFixture) proxiedRunner(t *testing.T) (*stmtRunner, *freezingProxy) {
	t.Helper()
	cfg := f.target.Config().ConnConfig.Config.Copy()
	p := newFreezingProxy(t, net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))))
	host, port, err := net.SplitHostPort(p.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	portNum, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Host, cfg.Port, cfg.Fallbacks = host, uint16(portNum), nil
	cfg.TLSConfig = nil
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

// TestRunnerDeadlineWhenTargetStopsResponding covers a target that keeps
// the connection open but stops answering. Each round trip gives up at
// its deadline and closes the connection; a statement that may have
// reached the server outside a transaction, or a COMMIT, has an unknown
// outcome, whilst one inside a transaction that was never committed
// simply failed.
func TestRunnerDeadlineWhenTargetStopsResponding(t *testing.T) {
	t.Parallel()
	f := newExecFixture(t, "_x_dl", time.Second, time.Minute, 10)

	tests := []struct {
		name          string
		transactional bool
		freezeAt      string // freeze before this exec, or before the statement when empty
		wantUnknown   bool
		wantIn        string // where the message is expected: statement or commit
	}{
		{"statement outside a transaction", false, "", true, "statement"},
		{"statement in a transaction", true, "", false, "statement"},
		{"commit", true, "COMMIT", true, "commit"},
		{"timeouts", false, "SET", false, "statement"},
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
			st := &runState{results: newResults([]string{"SELECT 1"}), transactional: tc.transactional}
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
