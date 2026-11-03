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
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// embeddingsTx is one transaction EnsureColdFrontEmbeddings began. It
// records each Exec and fails the first whose SQL contains failOn.
type embeddingsTx struct {
	pgx.Tx
	execs                 []string
	failOn                string
	commitErr             error
	committed, rolledBack bool
}

func (x *embeddingsTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	x.execs = append(x.execs, sql)
	if x.failOn != "" && strings.Contains(sql, x.failOn) {
		return pgconn.CommandTag{}, errors.New("exec failed: " + x.failOn)
	}
	return pgconn.CommandTag{}, nil
}

func (x *embeddingsTx) Commit(context.Context) error {
	if x.commitErr != nil {
		return x.commitErr
	}
	x.committed = true
	return nil
}

func (x *embeddingsTx) Rollback(context.Context) error {
	if !x.committed {
		x.rolledBack = true
	}
	return nil
}

// fakeEmbeddingsConn answers the state query with state and hands out a
// new embeddingsTx, configured by failOn and commitErr, per Begin.
type fakeEmbeddingsConn struct {
	scriptedQuerier
	failOn    string
	commitErr error
	beginErr  error
	txs       []*embeddingsTx
}

func (c *fakeEmbeddingsConn) Begin(context.Context) (pgx.Tx, error) {
	if c.beginErr != nil {
		return nil, c.beginErr
	}
	tx := &embeddingsTx{failOn: c.failOn, commitErr: c.commitErr}
	c.txs = append(c.txs, tx)
	return tx, nil
}

// captureLog sends the standard logger, which pkg/logger writes to, to
// a buffer for the rest of the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

func TestEnsureColdFrontEmbeddings(t *testing.T) {
	const tier2 = "alerter Tier 2 anomaly matching and chat memory embeddings are disabled"
	state := func(available, anomaly, chat bool) []scriptedRow {
		return []scriptedRow{{values: []any{available, anomaly, chat}}}
	}

	tests := []struct {
		name      string
		rows      []scriptedRow
		failOn    string
		commitErr error
		beginErr  error
		// wantTxs lists, per transaction begun, a statement fragment
		// it must issue; wantCommitted says whether each commits.
		wantTxs       []string
		wantCommitted []bool
		wantLog       string
	}{
		{
			name:    "state query fails",
			rows:    []scriptedRow{{err: errors.New("boom")}},
			wantLog: tier2 + ": failed to check for pgvector: boom",
		},
		{
			name: "pgvector not available",
			rows: state(false, false, false),
		},
		{
			name: "both objects exist",
			rows: state(true, true, true),
		},
		{
			name:          "creates both, each in its own transaction",
			rows:          state(true, false, false),
			wantTxs:       []string{"CREATE EXTENSION IF NOT EXISTS vector", "ALTER TABLE public.chat_memories"},
			wantCommitted: []bool{true, true},
		},
		{
			name:          "only the chat memory column is missing",
			rows:          state(true, true, false),
			wantTxs:       []string{"ALTER TABLE public.chat_memories"},
			wantCommitted: []bool{true},
		},
		{
			name:          "anomaly setup fails",
			rows:          state(true, false, false),
			failOn:        "CREATE EXTENSION",
			wantTxs:       []string{"CREATE EXTENSION IF NOT EXISTS vector"},
			wantCommitted: []bool{false},
			wantLog:       tier2 + ": pgvector schema setup: exec failed",
		},
		{
			name:          "chat memory setup fails",
			rows:          state(true, true, false),
			failOn:        "ALTER TABLE public.chat_memories",
			wantTxs:       []string{"ALTER TABLE public.chat_memories"},
			wantCommitted: []bool{false},
			wantLog:       "chat memory embeddings are disabled: add chat_memories embedding column/index",
		},
		{
			name:     "begin fails",
			rows:     state(true, false, false),
			beginErr: errors.New("no connection"),
			wantLog:  tier2 + ": failed to begin transaction: no connection",
		},
		{
			name:          "commit fails",
			rows:          state(true, false, true),
			commitErr:     errors.New("commit refused"),
			wantTxs:       []string{"CREATE EXTENSION IF NOT EXISTS vector"},
			wantCommitted: []bool{false},
			wantLog:       tier2 + ": failed to commit transaction: commit refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := captureLog(t)
			c := &fakeEmbeddingsConn{
				scriptedQuerier: scriptedQuerier{rows: tt.rows},
				failOn:          tt.failOn,
				commitErr:       tt.commitErr,
				beginErr:        tt.beginErr,
			}
			EnsureColdFrontEmbeddings(context.Background(), c)

			if len(c.txs) != len(tt.wantTxs) {
				t.Fatalf("began %d transactions, want %d", len(c.txs), len(tt.wantTxs))
			}
			for i, tx := range c.txs {
				all := strings.Join(tx.execs, "\n")
				if !strings.Contains(all, tt.wantTxs[i]) {
					t.Errorf("transaction %d issued %q, want %q", i+1, all, tt.wantTxs[i])
				}
				// pg_duckdb refuses a SAVEPOINT once a transaction
				// has touched DuckDB, so none is taken here.
				if strings.Contains(strings.ToUpper(all), "SAVEPOINT") {
					t.Errorf("transaction %d took a SAVEPOINT: %q", i+1, all)
				}
				if tx.committed != tt.wantCommitted[i] || tx.rolledBack == tt.wantCommitted[i] {
					t.Errorf("transaction %d committed=%v rolledBack=%v, want committed=%v",
						i+1, tx.committed, tx.rolledBack, tt.wantCommitted[i])
				}
			}
			if tt.wantLog == "" {
				if strings.Contains(buf.String(), "disabled") {
					t.Errorf("unexpected log: %q", buf.String())
				}
			} else if !strings.Contains(buf.String(), tt.wantLog) {
				t.Errorf("log %q does not contain %q", buf.String(), tt.wantLog)
			}
		})
	}
}
