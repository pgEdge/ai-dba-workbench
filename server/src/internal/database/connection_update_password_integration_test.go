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
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/pkg/crypto"
)

// connUpdatePasswordTestSchema creates the minimum subset of the connection
// hierarchy tables exercised by CreateConnection and UpdateConnectionFull.
// It mirrors the shape used in production (collector/src/database/schema.go),
// limited to the columns referenced by those two queries and by
// scanFullConnection.
const connUpdatePasswordTestSchema = `
DROP TABLE IF EXISTS connections CASCADE;

CREATE TABLE connections (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    host VARCHAR(255) NOT NULL DEFAULT '',
    hostaddr VARCHAR(255),
    port INTEGER NOT NULL DEFAULT 5432,
    database_name VARCHAR(255) NOT NULL DEFAULT '',
    username VARCHAR(255) NOT NULL DEFAULT '',
    password_encrypted TEXT,
    sslmode VARCHAR(32),
    sslcert TEXT,
    sslkey TEXT,
    sslrootcert TEXT,
    owner_username VARCHAR(255),
    owner_token VARCHAR(255),
    is_monitored BOOLEAN NOT NULL DEFAULT TRUE,
    is_shared BOOLEAN NOT NULL DEFAULT FALSE,
    membership_source VARCHAR(16) NOT NULL DEFAULT 'auto',
    cluster_id INTEGER,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`

const connUpdatePasswordTestTeardown = `
DROP TABLE IF EXISTS connections CASCADE;
`

// connUpdatePasswordTestSecret is a fixed 32-byte server secret so the
// encrypt/decrypt round-trip runs end-to-end in these tests.
const connUpdatePasswordTestSecret = "test-server-secret-32-bytes-long!"

// newConnUpdatePasswordTestDatastore wires up a *Datastore against the
// TEST_AI_WORKBENCH_SERVER Postgres instance with only the connections
// table the update-password path needs, and a fixed serverSecret so the
// password encryption path runs. The caller receives a cleanup that drops
// the schema and closes the pool.
func newConnUpdatePasswordTestDatastore(t *testing.T) (*Datastore, *pgxpool.Pool, func()) {
	t.Helper()

	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping connection update password integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Test database ping failed: %v", err)
	}

	if _, err := pool.Exec(ctx, connUpdatePasswordTestSchema); err != nil {
		pool.Close()
		t.Fatalf("Failed to create connection update password test schema: %v", err)
	}

	ds := NewTestDatastoreWithSecret(pool, connUpdatePasswordTestSecret)

	cleanup := func() {
		if _, err := pool.Exec(context.Background(), connUpdatePasswordTestTeardown); err != nil {
			t.Logf("connection update password teardown failed: %v", err)
		}
		pool.Close()
	}

	return ds, pool, cleanup
}

// readEncryptedPassword returns the raw password_encrypted column for a
// connection row, distinguishing SQL NULL (returns "", false) from a
// present value (returns the ciphertext, true).
func readEncryptedPassword(t *testing.T, pool *pgxpool.Pool, id int) (string, bool) {
	t.Helper()
	var enc *string
	err := pool.QueryRow(context.Background(),
		`SELECT password_encrypted FROM connections WHERE id = $1`, id).Scan(&enc)
	if err != nil {
		t.Fatalf("read password_encrypted (id=%d): %v", id, err)
	}
	if enc == nil {
		return "", false
	}
	return *enc, true
}

// TestUpdateConnectionFullPasswordGuard verifies the defense-in-depth guard
// added for issue #331: on update, a nil, empty, or whitespace-only password
// must leave the stored password_encrypted column untouched, while a
// non-blank password must re-encrypt and replace it. This mirrors the
// documented "leave blank to keep unchanged" client behavior, enforced here
// at the datastore layer so a direct API call cannot clobber a real
// credential with an encrypted blank value.
func TestUpdateConnectionFullPasswordGuard(t *testing.T) {
	ds, pool, cleanup := newConnUpdatePasswordTestDatastore(t)
	defer cleanup()

	ctx := context.Background()

	emptyStr := ""
	whitespaceStr := "   "
	newPassword := "brand-new-secret"
	seedDesc := "seed description"

	tests := []struct {
		name string
		// password is the pointer supplied in the update params; a nil
		// pointer models an omitted field.
		password *string
		// wantChanged indicates whether the stored ciphertext is expected
		// to differ from the original after the update.
		wantChanged bool
		// wantPlaintext, when non-empty, is the plaintext the stored
		// ciphertext must decrypt to after the update.
		wantPlaintext string
	}{
		{
			name:          "nil password leaves stored password unchanged",
			password:      nil,
			wantChanged:   false,
			wantPlaintext: "original-secret",
		},
		{
			name:          "empty string password leaves stored password unchanged",
			password:      &emptyStr,
			wantChanged:   false,
			wantPlaintext: "original-secret",
		},
		{
			name:          "whitespace-only password leaves stored password unchanged",
			password:      &whitespaceStr,
			wantChanged:   false,
			wantPlaintext: "original-secret",
		},
		{
			name:          "non-empty password re-encrypts and replaces",
			password:      &newPassword,
			wantChanged:   true,
			wantPlaintext: newPassword,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Seed a fresh connection with a known, non-empty password so
			// each case starts from an encrypted credential on disk.
			created, err := ds.CreateConnection(ctx, ConnectionCreateParams{
				Name:          "guard-conn",
				Description:   &seedDesc,
				Host:          "127.0.0.1",
				Port:          5432,
				DatabaseName:  "db",
				Username:      "user",
				Password:      "original-secret",
				OwnerUsername: "alice",
			})
			if err != nil {
				t.Fatalf("CreateConnection failed: %v", err)
			}
			t.Cleanup(func() {
				if _, err := pool.Exec(context.Background(),
					`DELETE FROM connections WHERE id = $1`, created.ID); err != nil {
					t.Logf("cleanup delete conn %d: %v", created.ID, err)
				}
			})

			originalCipher, ok := readEncryptedPassword(t, pool, created.ID)
			if !ok {
				t.Fatalf("expected seeded connection to have an encrypted password")
			}

			// Change an unrelated field alongside the password so the
			// UPDATE statement always has at least one column to set,
			// matching how the API sends partial updates.
			newDesc := "updated description"
			if _, err := ds.UpdateConnectionFull(ctx, created.ID, ConnectionUpdateParams{
				Description: &newDesc,
				Password:    tc.password,
			}); err != nil {
				t.Fatalf("UpdateConnectionFull failed: %v", err)
			}

			afterCipher, ok := readEncryptedPassword(t, pool, created.ID)
			if !ok {
				t.Fatalf("password_encrypted became NULL after update; the "+
					"column must never be cleared by an update (case %q)", tc.name)
			}

			if tc.wantChanged {
				if afterCipher == originalCipher {
					t.Errorf("expected stored ciphertext to change, but it was unchanged")
				}
			} else {
				if afterCipher != originalCipher {
					t.Errorf("expected stored ciphertext to be unchanged, got a different value")
				}
			}

			// The ciphertext must always decrypt back to the plaintext we
			// expect: the original for the "keep unchanged" cases, the new
			// value for the replace case.
			plaintext, err := crypto.DecryptPassword(afterCipher, connUpdatePasswordTestSecret)
			if err != nil {
				t.Fatalf("DecryptPassword failed: %v", err)
			}
			if plaintext != tc.wantPlaintext {
				t.Errorf("stored password decrypted to %q, want %q", plaintext, tc.wantPlaintext)
			}
		})
	}
}
