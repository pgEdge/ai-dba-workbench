/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package flagutil provides helpers for reasoning about command line
// flags. Configuration in the Workbench is layered (built-in defaults,
// then the configuration file, then command line flags), which means a
// caller must be able to tell "the operator passed this flag" from "the
// flag is sitting at its registered default". Comparing a flag's value
// against its default cannot make that distinction, because an operator
// may legitimately pass a value that happens to equal the default.
package flagutil

import "flag"

// Set records the names of the flags that were explicitly present on
// the command line. The zero value is usable: a nil Set simply reports
// that no flag was passed.
type Set map[string]bool

// Passed returns the Set of flags that fs saw on the command line.
// Flags left at their registered defaults are absent from the result,
// so callers can apply an override only when the operator asked for
// it. A nil FlagSet yields an empty Set rather than a panic, which
// keeps callers free of nil checks.
func Passed(fs *flag.FlagSet) Set {
	passed := make(Set)
	if fs == nil {
		return passed
	}
	fs.Visit(func(f *flag.Flag) {
		passed[f.Name] = true
	})
	return passed
}

// Has reports whether the named flag was explicitly passed on the
// command line.
func (s Set) Has(name string) bool {
	return s[name]
}
