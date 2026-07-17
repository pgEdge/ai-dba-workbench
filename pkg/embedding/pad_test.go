/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package embedding

import (
	"strings"
	"testing"
)

func TestPadTo_NilAndEmpty(t *testing.T) {
	for _, in := range [][]float32{nil, {}} {
		out, err := PadTo(in, MaxDimensions)
		if err != nil {
			t.Fatalf("PadTo(%v): unexpected error: %v", in, err)
		}
		if out != nil {
			t.Errorf("PadTo(%v) = %v, want nil", in, out)
		}
	}
}

func TestPadTo_RightPadsWithZeros(t *testing.T) {
	in := []float32{1, 2, 3}
	out, err := PadTo(in, 5)
	if err != nil {
		t.Fatalf("PadTo: unexpected error: %v", err)
	}
	if len(out) != 5 {
		t.Fatalf("len(out) = %d, want 5", len(out))
	}
	want := []float32{1, 2, 3, 0, 0}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("out[%d] = %v, want %v", i, out[i], want[i])
		}
	}
}

func TestPadTo_ExactLengthReturnsIndependentCopy(t *testing.T) {
	in := []float32{1, 2, 3}
	out, err := PadTo(in, 3)
	if err != nil {
		t.Fatalf("PadTo: unexpected error: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("len(out) = %d, want 3", len(out))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("out[%d] = %v, want %v", i, out[i], in[i])
		}
	}
	// Mutating the result must not affect the caller's input.
	out[0] = 99
	if in[0] != 1 {
		t.Errorf("input mutated through returned slice: in[0] = %v", in[0])
	}
}

func TestPadTo_RejectsOversizedVector(t *testing.T) {
	in := []float32{1, 2, 3, 4, 5}
	out, err := PadTo(in, 4)
	if err == nil {
		t.Fatalf("expected error for oversized vector, got out=%v", out)
	}
	if out != nil {
		t.Errorf("expected nil slice on error, got %v", out)
	}
	msg := err.Error()
	if !strings.Contains(msg, "5") || !strings.Contains(msg, "4") {
		t.Errorf("error message %q should mention input (5) and limit (4)", msg)
	}
	if !strings.Contains(msg, "exceeds maximum supported") {
		t.Errorf("error message %q missing expected wording", msg)
	}
}

func TestPadTo_MaxDimensionsValue(t *testing.T) {
	if MaxDimensions != 4000 {
		t.Errorf("MaxDimensions = %d, want 4000", MaxDimensions)
	}
	// A vector at exactly the ceiling is accepted and padded to itself.
	in := make([]float32, MaxDimensions)
	in[0] = 1
	out, err := PadTo(in, MaxDimensions)
	if err != nil {
		t.Fatalf("PadTo at ceiling: %v", err)
	}
	if len(out) != MaxDimensions {
		t.Errorf("len(out) = %d, want %d", len(out), MaxDimensions)
	}
	// One element beyond the ceiling is rejected.
	if _, err := PadTo(make([]float32, MaxDimensions+1), MaxDimensions); err == nil {
		t.Errorf("expected error for vector exceeding MaxDimensions")
	}
}
