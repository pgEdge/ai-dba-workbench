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
	stateIn := func(available, anomaly bool, chatTable string, chat bool) []scriptedRow {
		return []scriptedRow{{values: []any{available, anomaly, chatTable, chat}}}
	}
	state := func(available, anomaly, chat bool) []scriptedRow {
		return stateIn(available, anomaly, "public.chat_memories", chat)
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
			// Shown without -v, once per startup.
			name:    "pgvector not available",
			rows:    state(false, false, false),
			wantLog: "pgvector extension not available, so " + tier2,
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
			// The datastore role's search_path put chat_memories in
			// a schema of its own, as live: the column goes there.
			name:          "chat_memories outside public",
			rows:          stateIn(true, true, `"cold front".chat_memories`, false),
			wantTxs:       []string{`ALTER TABLE "cold front".chat_memories`},
			wantCommitted: []bool{true},
		},
		{
			name:    "no chat_memories on the search_path",
			rows:    stateIn(true, true, "", false),
			wantLog: "chat memory embeddings are disabled: no chat_memories table is on the datastore role's search_path",
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

			checkEmbeddingsTxs(t, c.txs, tt.wantTxs, tt.wantCommitted)
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

// checkEmbeddingsTxs asserts that txs are the transactions wanted: each
// issues its wanted statement, takes no SAVEPOINT, and is committed or
// rolled back as wanted.
func checkEmbeddingsTxs(t *testing.T, txs []*embeddingsTx, want []string, committed []bool) {
	t.Helper()
	if len(txs) != len(want) {
		t.Fatalf("began %d transactions, want %d", len(txs), len(want))
	}
	for i, tx := range txs {
		all := strings.Join(tx.execs, "\n")
		if !strings.Contains(all, want[i]) {
			t.Errorf("transaction %d issued %q, want %q", i+1, all, want[i])
		}
		// pg_duckdb refuses a SAVEPOINT once a transaction has touched
		// DuckDB, so none is taken here.
		if strings.Contains(strings.ToUpper(all), "SAVEPOINT") {
			t.Errorf("transaction %d took a SAVEPOINT: %q", i+1, all)
		}
		if tx.committed != committed[i] || tx.rolledBack == committed[i] {
			t.Errorf("transaction %d committed=%v rolledBack=%v, want committed=%v",
				i+1, tx.committed, tx.rolledBack, committed[i])
		}
	}
}

func TestEnsureEmbeddingsAfterMigrate(t *testing.T) {
	// The default mode leaves the setup to migration #1.
	c := &fakeEmbeddingsConn{}
	ensureEmbeddingsAfterMigrate(c, false)
	if len(c.sql) != 0 || len(c.txs) != 0 {
		t.Errorf("default mode issued %q and began %d transactions", c.sql, len(c.txs))
	}

	// ColdFront runs it.
	c = &fakeEmbeddingsConn{scriptedQuerier: scriptedQuerier{
		rows: []scriptedRow{{values: []any{true, false, "public.chat_memories", false}}}}}
	ensureEmbeddingsAfterMigrate(c, true)
	if len(c.sql) != 1 || len(c.txs) != 2 {
		t.Errorf("ColdFront issued %q and began %d transactions, want the state query and 2", c.sql, len(c.txs))
	}
}
