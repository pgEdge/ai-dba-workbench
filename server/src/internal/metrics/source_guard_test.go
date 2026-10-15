/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package metrics

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// credentialTableRef matches SQL that names a metrics table whose rows
// can hold a credential: a setting value, an hba option or error, or
// statement and row text.
var credentialTableRef = regexp.MustCompile(
	`metrics\.(pg_settings|pg_hba_file_rules|pg_stat_activity|pg_stat_statements|` +
		`spock_exception_log|spock_resolutions|pg_stat_replication)\b`)

// maskingHelperUse matches a call to one of the masking helpers in this
// package.
var maskingHelperUse = regexp.MustCompile(
	`metrics\.(RedactedText|MaskedSetting|MaskedWhole|MaskedOptions|MaskedProbeColumn)\(`)

// reviewedCredentialTableReaders are the server source files, relative
// to server/src, that read a credentialTableRef table without a masking
// helper, each reviewed as returning no credential.
var reviewedCredentialTableReaders = map[string]string{
	"internal/api/perf_summary_connection_groups.go": "reads only usename, datname, client address and state " +
		"from pg_stat_activity",
	"internal/api/server_info_handlers.go": "reads pg_settings for the fixed keySettings names only, " +
		"none of which holds a credential",
	"internal/database/cluster_queries.go": "reads only MAX(collected_at) from pg_stat_activity",
	"internal/database/estate_queries.go": "reads pg_settings for a fixed list of memory and planner " +
		"settings only",
	"internal/database/health.go": "probes that pg_settings exists with SELECT 1 ... LIMIT 0",
}

// TestCredentialTableReadersAreReviewed fails when server code reads a
// metrics table that can hold a credential, unless the file uses a
// masking helper or has been reviewed and listed in
// reviewedCredentialTableReaders. A file that uses a helper must still
// mask every value it returns; review it when the guard does not.
func TestCredentialTableReadersAreReviewed(t *testing.T) {
	root := filepath.Join("..", "..")
	seen := map[string]bool{}
	var unreviewed []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "vendor" || d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path) // #nosec G304 -- walks this repository's own source tree
		if err != nil {
			return err
		}
		if !credentialTableRef.Match(src) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		seen[rel] = true
		if _, ok := reviewedCredentialTableReaders[rel]; ok || maskingHelperUse.Match(src) {
			return nil
		}
		unreviewed = append(unreviewed, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk source tree: %v", err)
	}
	if len(seen) == 0 {
		t.Fatal("found no server source reading a credential table; the walk is broken")
	}
	sort.Strings(unreviewed)
	for _, rel := range unreviewed {
		t.Errorf("%s reads a metrics table that can hold a credential: mask what it returns "+
			"with a masking helper, or review it and add it to reviewedCredentialTableReaders", rel)
	}
	for rel := range reviewedCredentialTableReaders {
		if !seen[rel] {
			t.Errorf("%s is listed in reviewedCredentialTableReaders but no longer reads a "+
				"credential table; remove it", rel)
		}
	}
}
