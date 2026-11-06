/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package coldfront

import (
	"encoding/json"
	"fmt"
)

// tablesByName indexes tables for Lookup.
var tablesByName = func() map[string]Table {
	m := make(map[string]Table, len(tables))
	for _, t := range tables {
		m[t.Name] = t
	}
	return m
}()

// Tables returns the specification of every metrics table. The slice is
// a copy, so callers cannot modify the specification.
func Tables() []Table {
	out := make([]Table, len(tables))
	copy(out, tables)
	return out
}

// TableNames returns the name of every metrics table.
func TableNames() []string {
	names := make([]string, len(tables))
	for i, t := range tables {
		names[i] = t.Name
	}
	return names
}

// Lookup returns the specification of the named metrics table.
func Lookup(name string) (Table, bool) {
	t, ok := tablesByName[name]
	return t, ok
}

// CreateColumns returns the columns the table is created with, in order:
// every column except those a later migration adds.
func (t Table) CreateColumns() []Column {
	var cols []Column
	for _, c := range t.Columns {
		if c.AddedIn == 0 {
			cols = append(cols, c)
		}
	}
	return cols
}

// columnDef is one element of create_iceberg_table's p_columns argument.
type columnDef struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ColumnsJSON returns the p_columns argument of
// coldfront.create_iceberg_table for the table: a JSON array of
// {"name", "type"} objects holding the columns the table is created
// with and their ColdFront types.
func (t Table) ColumnsJSON() (string, error) {
	cols := t.CreateColumns()
	defs := make([]columnDef, 0, len(cols))
	for _, c := range cols {
		st, err := StorageType(c.PGType)
		if err != nil {
			return "", fmt.Errorf("metrics.%s.%s: %w", t.Name, c.Name, err)
		}
		defs = append(defs, columnDef{Name: c.Name, Type: st})
	}
	b, err := json.Marshal(defs)
	if err != nil {
		return "", fmt.Errorf("metrics.%s: failed to encode columns: %w", t.Name, err)
	}
	return string(b), nil
}
