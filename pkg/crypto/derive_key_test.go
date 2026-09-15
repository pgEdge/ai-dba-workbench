/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package crypto

import (
	"bytes"
	"testing"
)

func TestDeriveKeyIsDeterministicAndFullLength(t *testing.T) {
	salt := []byte("pgedge-ai-workbench/some-subsystem/v1")

	first := DeriveKey(testSecret, salt)
	if len(first) != keySize {
		t.Fatalf("key is %d bytes, want %d", len(first), keySize)
	}

	// The key has to survive a restart, so the same inputs must give the
	// same key every time.
	if !bytes.Equal(first, DeriveKey(testSecret, salt)) {
		t.Error("the same secret and salt produced two different keys")
	}
}

func TestDeriveKeySeparatesSubsystemsBySalt(t *testing.T) {
	first := DeriveKey(testSecret, []byte("subsystem-one"))
	second := DeriveKey(testSecret, []byte("subsystem-two"))

	if bytes.Equal(first, second) {
		t.Error("two salts produced the same key, so the subsystems are not separated")
	}
	if bytes.Equal(first, DeriveKey("another-secret", []byte("subsystem-one"))) {
		t.Error("two secrets produced the same key")
	}
}

func TestDeriveKeyRefusesInputsThatWouldNotBeSecret(t *testing.T) {
	if key := DeriveKey("", []byte("a-salt")); key != nil {
		t.Error("an empty secret must not yield a key derived from the public salt alone")
	}
	if key := DeriveKey(testSecret, nil); key != nil {
		t.Error("an empty salt must be refused")
	}
}
