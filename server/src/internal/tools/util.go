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

import "strings"

// sanitizeTSVField replaces characters that break tab-separated output
// (tabs, newlines, carriage returns) with single spaces. This variant is
// intended for human- and LLM-readable TSV output emitted by tools such
// as list_probes and list_connections. It deliberately does not escape
// with backslash sequences because the output is not round-tripped
// through a TSV parser; keeping the text readable matters more than
// losslessly preserving the original separator characters.
//
// Numeric fields do not require this helper; only untrusted string
// fields that may contain tabs or newlines should be wrapped.
func sanitizeTSVField(s string) string {
	if s == "" {
		return s
	}
	replacer := strings.NewReplacer(
		"\t", " ",
		"\n", " ",
		"\r", " ",
	)
	return replacer.Replace(s)
}
