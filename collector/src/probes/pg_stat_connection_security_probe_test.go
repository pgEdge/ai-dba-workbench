/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package probes

import (
	"context"
	"testing"
	"time"
)

func newPgStatConnectionSecurityProbeForTest() *PgStatConnectionSecurityProbe {
	return NewPgStatConnectionSecurityProbe(&ProbeConfig{
		Name:                      ProbeNamePgStatConnectionSecurity,
		CollectionIntervalSeconds: 300,
		RetentionDays:             30,
		IsEnabled:                 true,
	})
}

func TestPgStatConnectionSecurityProbe_Surface(t *testing.T) {
	p := newPgStatConnectionSecurityProbeForTest()
	if p.GetName() != ProbeNamePgStatConnectionSecurity {
		t.Errorf("GetName()")
	}
	if p.IsDatabaseScoped() {
		t.Error("connection_security is server-scoped")
	}
	if p.GetQuery() != "" {
		t.Error("GetQuery should be empty; query is built by Execute")
	}
}

func TestPgStatConnectionSecurityProbe_StoreEmpty(t *testing.T) {
	p := newPgStatConnectionSecurityProbeForTest()
	if err := p.Store(context.Background(), nil, 1, time.Now(),
		nil); err != nil {
		t.Errorf("Store(nil) = %v", err)
	}
}

func TestPgStatConnectionSecurityProbe_ExecuteAndStore(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatConnectionSecurityProbeForTest()
	ctx := context.Background()
	pgVersion := detectPgVersion(t, conn)

	metrics, err := p.Execute(ctx, "sec-conn", conn, pgVersion)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// pg_stat_ssl always has at least one row for the current backend.
	if len(metrics) == 0 {
		t.Fatal("expected at least one ssl row")
	}
	// Cache path
	if _, err := p.Execute(ctx, "sec-conn", conn, pgVersion); err != nil {
		t.Fatalf("Execute cached: %v", err)
	}

	if err := p.Store(ctx, conn, 1, time.Now().UTC(),
		metrics); err != nil {
		t.Fatalf("Store: %v", err)
	}
}

func TestPgStatConnectionSecurityProbe_CheckHelpers(t *testing.T) {
	pool := requireIntegrationPool(t)
	conn := acquireConn(t, pool)
	p := newPgStatConnectionSecurityProbeForTest()
	ctx := context.Background()

	if _, err := p.checkGSSAPIAvailable(ctx, conn); err != nil {
		t.Errorf("checkGSSAPIAvailable: %v", err)
	}
	if _, err := p.checkCredentialsDelegatedColumn(ctx,
		conn); err != nil {
		t.Errorf("checkCredentialsDelegatedColumn: %v", err)
	}
}
