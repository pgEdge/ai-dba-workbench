/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package flagutil

import (
	"flag"
	"io"
	"testing"
)

// newTestFlagSet builds a flag set with a string and an int flag whose
// defaults mirror the collector's, so tests can pass a value that
// coincides with a default.
func newTestFlagSet() (*flag.FlagSet, *string, *int) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	sslMode := fs.String("pg-sslmode", "prefer", "SSL mode")
	port := fs.Int("pg-port", 5432, "port")
	return fs, sslMode, port
}

func TestPassed_NoFlags(t *testing.T) {
	fs, _, _ := newTestFlagSet()
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	passed := Passed(fs)
	if len(passed) != 0 {
		t.Errorf("Passed() = %v, want an empty set", passed)
	}
	if passed.Has("pg-port") {
		t.Error("Has(pg-port) = true, want false when nothing was passed")
	}
}

// TestPassed_ExplicitDefaultValue is the case that motivates the
// package: a flag passed with the value that is also its registered
// default must still be reported as passed.
func TestPassed_ExplicitDefaultValue(t *testing.T) {
	fs, sslMode, port := newTestFlagSet()
	if err := fs.Parse([]string{"-pg-port", "5432", "-pg-sslmode", "prefer"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	passed := Passed(fs)
	if !passed.Has("pg-port") {
		t.Error("Has(pg-port) = false, want true for an explicitly passed flag")
	}
	if !passed.Has("pg-sslmode") {
		t.Error("Has(pg-sslmode) = false, want true for an explicitly passed flag")
	}
	if *port != 5432 || *sslMode != "prefer" {
		t.Errorf("parsed values changed unexpectedly: port=%d sslmode=%q", *port, *sslMode)
	}
}

func TestPassed_SubsetOfFlags(t *testing.T) {
	fs, _, _ := newTestFlagSet()
	if err := fs.Parse([]string{"-pg-port", "6000"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}

	passed := Passed(fs)
	if !passed.Has("pg-port") {
		t.Error("Has(pg-port) = false, want true")
	}
	if passed.Has("pg-sslmode") {
		t.Error("Has(pg-sslmode) = true, want false for an unpassed flag")
	}
}

func TestPassed_NilFlagSet(t *testing.T) {
	passed := Passed(nil)
	if passed == nil {
		t.Fatal("Passed(nil) = nil, want an empty set")
	}
	if passed.Has("anything") {
		t.Error("Has() on an empty set = true, want false")
	}
}

func TestSet_HasOnNilSet(t *testing.T) {
	var s Set
	if s.Has("pg-port") {
		t.Error("Has() on a nil Set = true, want false")
	}
}
