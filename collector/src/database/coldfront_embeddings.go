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
// installed, whether anomaly_embeddings exists, the quoted,
// schema-qualified name of the chat_memories table (empty if there is
// none) and whether that table already has its embedding column.
//
// chat_memories is resolved through the search_path, as migration #1's
// unqualified CREATE TABLE and the server's queries resolve it, rather
// than assumed to be in public: when the datastore role's search_path
// starts with an existing schema (the "$user" entry, for a role named
// after a schema, such as the coldfront role of a ColdFront image),
// migration #1 created the table there, and an ALTER of
// public.chat_memories failed with SQLSTATE 42P01 at every startup.
const coldFrontEmbeddingsStateQuery = `
    SELECT EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'vector'),
           to_regclass('public.anomaly_embeddings') IS NOT NULL,
           COALESCE((SELECT format('%I.%I', n.nspname, c.relname)
                     FROM pg_class c
                     JOIN pg_namespace n ON n.oid = c.relnamespace
                     WHERE c.oid = to_regclass('chat_memories')), ''),
           EXISTS (SELECT 1 FROM pg_attribute
                   WHERE attrelid = to_regclass('chat_memories')
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
//
// The embedding column goes on the chat_memories table the search_path
// resolves, which is the one migration #1 created and the server
// queries; see coldFrontEmbeddingsStateQuery.
func EnsureColdFrontEmbeddings(ctx context.Context, conn embeddingsConn) {
	var available, haveAnomaly, haveChat bool
	var chatTable string
	if err := conn.QueryRow(ctx, coldFrontEmbeddingsStateQuery).Scan(
		&available, &haveAnomaly, &chatTable, &haveChat); err != nil {
		logger.Startupf("pgvector setup skipped, so alerter Tier 2 anomaly "+
			"matching and chat memory embeddings are disabled: failed to "+
			"check for pgvector: %v", err)
		return
	}
	if !available && !haveAnomaly {
		logger.Startup("pgvector extension not available, so alerter Tier 2 " +
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
	ensureChatMemoryEmbedding(ctx, conn, chatTable, haveChat)
}

// ensureChatMemoryEmbedding adds the embedding column and its index to
// chatTable, the quoted, schema-qualified chat_memories table, unless it
// already has them. An empty chatTable means the search_path resolves
// no chat_memories table, which the migrations always create.
func ensureChatMemoryEmbedding(ctx context.Context, conn txBeginner, chatTable string, haveChat bool) {
	if haveChat {
		return
	}
	if chatTable == "" {
		logger.Startup("chat_memories embedding setup skipped, so chat " +
			"memory embeddings are disabled: no chat_memories table is on " +
			"the datastore role's search_path")
		return
	}
	err := runInOwnTx(ctx, conn, func(ctx context.Context, tx pgx.Tx) error {
		return addChatMemoryEmbedding(ctx, tx, chatTable)
	})
	if err != nil {
		logger.Startupf("chat_memories embedding setup skipped, so chat "+
			"memory embeddings are disabled: %v", err)
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
