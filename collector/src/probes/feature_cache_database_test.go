/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Tests covering the per-database feature-detection cache. Feature
// detection is not server-wide: an extension can be installed in one
// database and absent from another on the same server, which is the
// failure behind issues #435 and #438, so a verdict reached in one
// database must never be reused in another.
package probes

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// createScratchDatabase creates an additional database on the
// integration server and returns a pool connected to it. The database is
// dropped when the test finishes.
func createScratchDatabase(t *testing.T, suffix string) *pgxpool.Pool {
	t.Helper()

	base, ok := integrationConnString()
	if !ok {
		t.Skip("TEST_AI_WORKBENCH_SERVER (or TEST_DB_CONN) not set")
	}

	name := fmt.Sprintf("ai_workbench_probes_%s_%d", suffix,
		time.Now().UnixNano())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, replaceProbeDatabase(base, "postgres"))
	if err != nil {
		t.Skipf("connect to admin database: %v", err)
	}
	defer adminPool.Close()

	if _, err := adminPool.Exec(ctx,
		fmt.Sprintf("CREATE DATABASE %s", name)); err != nil {
		t.Skipf("create scratch database: %v", err)
	}

	pool, err := pgxpool.New(ctx, replaceProbeDatabase(base, name))
	if err != nil {
		t.Fatalf("connect to scratch database: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()

		dropCtx, dropCancel := context.WithTimeout(
			context.Background(), 30*time.Second)
		defer dropCancel()

		dropPool, dropErr := pgxpool.New(dropCtx,
			replaceProbeDatabase(base, "postgres"))
		if dropErr != nil {
			t.Logf("drop scratch database %s: %v", name, dropErr)
			return
		}
		defer dropPool.Close()

		if _, dropErr := dropPool.Exec(dropCtx, fmt.Sprintf(
			"DROP DATABASE IF EXISTS %s WITH (FORCE)", name)); dropErr != nil {
			t.Logf("drop scratch database %s: %v", name, dropErr)
		}
	})

	return pool
}

// TestCachedCheck_PerDatabaseVerdicts is the regression test for #435 at
// the cache level: the same connection name and check name, against two
// different databases, must reach and keep two independent verdicts.
func TestCachedCheck_PerDatabaseVerdicts(t *testing.T) {
	mainPool := requireIntegrationPool(t)
	mainConn := acquireConn(t, mainPool)

	altPool := createScratchDatabase(t, "altdb")
	altConn := acquireConn(t, altPool)

	const (
		connName  = "per-database-cache"
		checkName = "synthetic_feature"
	)

	mainDB := connectionDatabaseName(mainConn)
	altDB := connectionDatabaseName(altConn)
	if mainDB == "" || altDB == "" {
		t.Fatalf("expected both connections to report a database, got %q and %q",
			mainDB, altDB)
	}
	if mainDB == altDB {
		t.Fatalf("expected two distinct databases, both reported %q", mainDB)
	}

	for _, db := range []string{mainDB, altDB} {
		key := featureCacheKey{connectionName: connName,
			databaseName: db, checkName: checkName}
		featureCache.Delete(key)
		defer featureCache.Delete(key)
	}

	// The first database answers false, as an RDS monitoring connection
	// pointed at a database without pg_stat_statements would.
	calls := 0
	got, err := cachedCheck(connName, mainConn, checkName,
		func() (bool, error) {
			calls++
			return false, nil
		})
	if err != nil {
		t.Fatalf("cachedCheck on %s: %v", mainDB, err)
	}
	if got {
		t.Errorf("cachedCheck on %s = true, want false", mainDB)
	}

	// The second database must be asked in its own right rather than
	// inheriting the first database's negative verdict.
	got, err = cachedCheck(connName, altConn, checkName,
		func() (bool, error) {
			calls++
			return true, nil
		})
	if err != nil {
		t.Fatalf("cachedCheck on %s: %v", altDB, err)
	}
	if !got {
		t.Errorf("cachedCheck on %s = false, want true: the verdict from %s was reused",
			altDB, mainDB)
	}
	if calls != 2 {
		t.Errorf("check function called %d times, want 2 (once per database)", calls)
	}

	// Both verdicts must survive as a pair, each still cached.
	got, err = cachedCheck(connName, mainConn, checkName,
		func() (bool, error) {
			t.Error("check function called again for the first database")
			return true, nil
		})
	if err != nil || got {
		t.Errorf("cached verdict for %s = (%v, %v), want (false, nil)", mainDB, got, err)
	}
	got, err = cachedCheck(connName, altConn, checkName,
		func() (bool, error) {
			t.Error("check function called again for the second database")
			return false, nil
		})
	if err != nil || !got {
		t.Errorf("cached verdict for %s = (%v, %v), want (true, nil)", altDB, got, err)
	}
}

// TestConnectionDatabaseName_NilConn confirms the helper tolerates a nil
// connection, which callers in tests pass when no real database is
// involved.
func TestConnectionDatabaseName_NilConn(t *testing.T) {
	if got := connectionDatabaseName(nil); got != "" {
		t.Errorf("connectionDatabaseName(nil) = %q, want \"\"", got)
	}
}

// TestPgStatStatementsProbe_PerDatabaseExtensionVerdict is the
// end-to-end regression test for #435: with the extension installed in
// one database and absent from another on the same monitored
// connection, probing the database without it first must not suppress
// collection in the database that has it.
func TestPgStatStatementsProbe_PerDatabaseExtensionVerdict(t *testing.T) {
	mainPool := requireIntegrationPool(t)
	mainConn := acquireConn(t, mainPool)
	requirePgStatStatementsReadable(t, mainConn)

	altPool := createScratchDatabase(t, "nopgss")
	altConn := acquireConn(t, altPool)

	ctx := context.Background()
	pgVersion := detectPgVersion(t, mainConn)
	p := newPgStatStatementsProbeForTest()

	const connName = "issue-435-regression"

	// The scratch database deliberately has no pg_stat_statements, so
	// the probe reports the extension as absent there.
	if _, err := p.Execute(ctx, connName, altConn, pgVersion); !errors.Is(
		err, ErrExtensionNotInstalled) {
		t.Fatalf("Execute on the extension-less database = %v, want ErrExtensionNotInstalled",
			err)
	}

	// The database that does have the extension must still be probed on
	// its own merits, not silently skipped because another database on
	// the same connection answered no.
	metrics, err := p.Execute(ctx, connName, mainConn, pgVersion)
	if err != nil {
		t.Fatalf("Execute on the database with the extension: %v", err)
	}
	if metrics == nil {
		t.Fatal("Execute returned nil metrics for a database that has the extension: " +
			"the negative verdict from the other database was reused")
	}
}
