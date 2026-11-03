/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/pgedge/ai-workbench/pkg/logger"
	"github.com/pgedge/ai-workbench/pkg/rollback"
)

// coldFrontEmbeddingsStateQuery reports whether pgvector can be
// installed and which of the two embedding objects already exist.
const coldFrontEmbeddingsStateQuery = `
    SELECT EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'vector'),
           to_regclass('public.anomaly_embeddings') IS NOT NULL,
           EXISTS (SELECT 1 FROM pg_attribute
                   WHERE attrelid = to_regclass('public.chat_memories')
                     AND attname = 'embedding'
                     AND NOT attisdropped)
`

// embeddingsConn is what EnsureColdFrontEmbeddings needs from a
// datastore connection that is not inside a transaction.
type embeddingsConn interface {
	rowQuerier
	txBeginner
}

// coldFrontEmbeddingsTimeout bounds EnsureColdFrontEmbeddings at
// startup; adding chat_memories.embedding waits for an ACCESS EXCLUSIVE
// lock on that table.
const coldFrontEmbeddingsTimeout = 30 * time.Second

// ensureEmbeddingsAfterMigrate runs EnsureColdFrontEmbeddings once the
// migrations have run, under ColdFront only: in the default mode
// migration #1 has already created the pgvector objects.
func ensureEmbeddingsAfterMigrate(conn embeddingsConn, useColdFront bool) {
	if !useColdFront {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), coldFrontEmbeddingsTimeout)
	defer cancel()
	EnsureColdFrontEmbeddings(ctx, conn)
}

// EnsureColdFrontEmbeddings creates, under ColdFront, the pgvector
// objects migration #1 creates in the default mode: the vector
// extension, anomaly_embeddings (alerter Tier 2 anomaly matching) and
// the chat_memories.embedding column (chat memory similarity search).
//
// Migration #1 cannot create them under ColdFront: its setup runs in
// SAVEPOINTs, which pg_duckdb refuses once the transaction has written
// to Iceberg, and migration #1 creates the metrics tables first. Here
// each object is created in a transaction of its own that touches no
// Iceberg table and takes no SAVEPOINT, so a failure only rolls that
// transaction back, as the SAVEPOINT does in the default mode. Running
// after the migrations at every startup also covers a ColdFront
// datastore that ran migration #1 before this step existed, and a
// datastore on which pgvector was installed later. An object that
// already exists is skipped, so a normal startup takes no lock on
// either table. Failures are logged, not returned: as in the default
// mode, the Workbench runs without the embeddings.
func EnsureColdFrontEmbeddings(ctx context.Context, conn embeddingsConn) {
	var available, haveAnomaly, haveChat bool
	if err := conn.QueryRow(ctx, coldFrontEmbeddingsStateQuery).Scan(
		&available, &haveAnomaly, &haveChat); err != nil {
		logger.Startupf("pgvector setup skipped, so alerter Tier 2 anomaly "+
			"matching and chat memory embeddings are disabled: failed to "+
			"check for pgvector: %v", err)
		return
	}
	if !available && !haveAnomaly {
		logger.Info("pgvector extension not available, so alerter Tier 2 " +
			"anomaly matching and chat memory embeddings are disabled")
		return
	}

	if !haveAnomaly {
		if err := runInOwnTx(ctx, conn, createAnomalyEmbeddings); err != nil {
			logger.Startupf("pgvector setup skipped, so alerter Tier 2 "+
				"anomaly matching and chat memory embeddings are disabled: %v", err)
			return
		}
	}
	if !haveChat {
		if err := runInOwnTx(ctx, conn, addChatMemoryEmbedding); err != nil {
			logger.Startupf("chat_memories embedding setup skipped, so chat "+
				"memory embeddings are disabled: %v", err)
		}
	}
}

// runInOwnTx runs body in a new transaction on conn and commits it,
// rolling it back if body or the commit fails.
func runInOwnTx(ctx context.Context, conn txBeginner,
	body func(context.Context, pgx.Tx) error) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback.Tx(ctx, tx) //nolint:errcheck // no-op after commit

	if err := body(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}
