/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Tests that exercise version-conditional branches inside
// GetQueryForVersion implementations. These do not require a database
// connection because the function is pure: it returns the right SQL
// shape for the supplied version.
package probes

import (
	"strings"
	"testing"
)

func TestPgHbaFileRulesProbe_GetQueryForVersion(t *testing.T) {
	p := NewPgHbaFileRulesProbe(&ProbeConfig{
		Name: ProbeNamePgHbaFileRules,
	})

	pre16 := p.GetQueryForVersion(15)
	if !strings.Contains(pre16, "line_number AS rule_number") {
		t.Errorf("pre-16 query should alias line_number to "+
			"rule_number, got %q", pre16)
	}
	if !strings.Contains(pre16, "NULL::text AS file_name") {
		t.Errorf("pre-16 query should NULL file_name, got %q", pre16)
	}

	post16 := p.GetQueryForVersion(16)
	if strings.Contains(post16, "line_number AS rule_number") {
		t.Errorf("PG16+ should select rule_number directly")
	}
	if strings.Contains(post16, "NULL::text AS file_name") {
		t.Errorf("PG16+ should select file_name directly")
	}
}

func TestPgIdentFileMappingsProbe_GetQueryForVersion(t *testing.T) {
	p := NewPgIdentFileMappingsProbe(&ProbeConfig{
		Name: ProbeNamePgIdentFileMappings,
	})

	pre16 := p.GetQueryForVersion(15)
	if !strings.Contains(pre16, "line_number AS map_number") {
		t.Errorf("pre-16 query should alias line_number to "+
			"map_number, got %q", pre16)
	}
	if !strings.Contains(pre16, "NULL::text AS file_name") {
		t.Errorf("pre-16 should NULL file_name")
	}

	post16 := p.GetQueryForVersion(16)
	if strings.Contains(post16, "line_number AS map_number") {
		t.Errorf("PG16+ should select map_number directly")
	}
}
