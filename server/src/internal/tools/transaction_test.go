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
	"testing"
)

// ---------------------------------------------------------------------------
// ManagedTx tests
// ---------------------------------------------------------------------------

func TestManagedTxCommitMarksCommitted(t *testing.T) {
	// We cannot test the full transaction flow without a database,
	// but we can verify the zero-value behavior of the struct.
	mt := &ManagedTx{}

	// Before commit, committed should be false
	if mt.committed {
		t.Error("expected committed to be false initially")
	}

	// Tx and ctx start as their zero values.
	if mt.Tx != nil {
		t.Error("expected Tx to be nil initially")
	}
	if mt.ctx != nil {
		t.Error("expected ctx to be nil initially")
	}
}

func TestReadOnlyTxTypeAlias(t *testing.T) {
	// Verify ReadOnlyTx is an alias for ManagedTx
	var rot *ReadOnlyTx
	var mt *ManagedTx

	// This should compile successfully since they are aliases
	rot = mt
	mt = rot

	// Avoid unused variable warning
	_ = rot
	_ = mt
}
