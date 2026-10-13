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
	"strings"
	"testing"
)

func TestDescribeDatastoreToolDefinition(t *testing.T) {
	def := DescribeDatastoreTool(nil).Definition
	if def.Name != "describe_datastore" {
		t.Errorf("name = %q", def.Name)
	}
	if len(def.InputSchema.Required) != 0 {
		t.Errorf("describe_datastore should take no required arguments, got %v", def.InputSchema.Required)
	}
	if _, ok := def.InputSchema.Properties["source"]; !ok {
		t.Error("input schema lacks source")
	}
	if !strings.Contains(def.Description, "query_datastore") || def.CompactDescription == "" {
		t.Errorf("unexpected description: %q", def.Description)
	}
}

func TestDescribeDatastoreNilPool(t *testing.T) {
	runToolCases(t, DescribeDatastoreTool(nil), []toolCase{
		{name: "list", raw: true, wantErr: true, want: "Datastore not configured"},
		{name: "one source", raw: true, args: map[string]any{"source": "alerts"},
			wantErr: true, want: "Datastore not configured"},
	})
}

func TestDescribeDatastoreScopesLegendCoversEveryScope(t *testing.T) {
	seen := map[datastoreScope]bool{}
	for _, s := range describeDatastoreScopes {
		seen[s] = true
	}
	for _, src := range configurationSources() {
		if !seen[src.Scope] {
			t.Errorf("source %s uses scope %s, which the legend omits", src.Name, src.Scope)
		}
	}
	if !seen[scopeConnection] {
		t.Error("legend omits the metric sources' connection scope")
	}
}
