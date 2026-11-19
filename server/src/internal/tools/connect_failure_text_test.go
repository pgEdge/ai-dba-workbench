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
	"net"
	"os"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/pgedge/ai-workbench/server/internal/auth"
	"github.com/pgedge/ai-workbench/server/internal/config"
	"github.com/pgedge/ai-workbench/server/internal/database"
)

// selectedSession is a SessionProvider whose every token has selected
// connection 7.
type selectedSession struct{}

func (selectedSession) GetConnectionSession(string) (*database.ConnectionSession, error) {
	return &database.ConnectionSession{ConnectionID: 7}, nil
}

func (selectedSession) ClearConnectionSession(string) error { return nil }

// fixedConnInfo is a ConnectionInfoProvider whose every connection is
// one documentation address.
type fixedConnInfo struct{}

func (fixedConnInfo) GetConnectionWithPassword(context.Context, int) (*database.MonitoredConnection, string, error) {
	return &database.MonitoredConnection{}, "", nil
}

func (fixedConnInfo) BuildConnectionString(*database.MonitoredConnection, string, string) (string, error) {
	return "postgres://u@192.0.2.1:5432/x?sslmode=disable", nil
}

// failingDials are dial guards making the connect fail as a refused
// port, a filtered port and a host that closes the connection would.
func failingDials() map[string]database.DialGuard {
	failWith := func(err error) database.DialGuard {
		return func(c *pgconn.Config) {
			c.DialFunc = func(context.Context, string, string) (net.Conn, error) { return nil, err }
		}
	}
	return map[string]database.DialGuard{
		"refused":  failWith(&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}),
		"filtered": failWith(&net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}),
		"eof": func(c *pgconn.Config) {
			c.DialFunc = func(context.Context, string, string) (net.Conn, error) {
				client, server := net.Pipe()
				go func() {
					_, _ = server.Read(make([]byte, 1024))
					_ = server.Close()
				}()
				return client, nil
			}
		},
	}
}

// TestExecuteFailedConnectTextIsFixed checks that a tool call on the
// selected connection answers a failed connect with the same fixed text
// whatever made it fail, so that the reply does not tell a refused port
// from a filtered one (issue #850).
func TestExecuteFailedConnectTextIsFixed(t *testing.T) {
	for name, guard := range failingDials() {
		t.Run(name, func(t *testing.T) {
			cm := database.NewClientManager(nil)
			t.Cleanup(func() { _ = cm.CloseAll() })
			cm.SetMonitoredDialGuard(guard)

			provider := newExecuteProvider(t, &config.Config{})
			provider.clientResolver = &database.ClientResolver{
				TokenExtractor: auth.GetTokenHashFromContext,
				Sessions:       selectedSession{},
				ConnInfo:       fixedConnInfo{},
				ClientManager:  cm,
			}

			resp, err := provider.Execute(superuserTokenContext(), "query_database",
				map[string]any{"query": "SELECT 1"})
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
			if !resp.IsError || len(resp.Content) != 1 ||
				resp.Content[0].Text != database.FailedConnectText {
				t.Errorf("response = %+v, want only %q", resp, database.FailedConnectText)
			}
		})
	}
}
