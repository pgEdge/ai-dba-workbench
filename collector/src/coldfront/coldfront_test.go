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
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func TestStorageType(t *testing.T) {
	tests := []struct {
		pgType  string
		want    string
		wantErr string
	}{
		// Stored as they are.
		{pgType: "bigint", want: "bigint"},
		{pgType: "integer", want: "integer"},
		{pgType: "smallint", want: "smallint"},
		{pgType: "real", want: "real"},
		{pgType: "double precision", want: "double precision"},
		{pgType: "boolean", want: "boolean"},
		{pgType: "timestamp with time zone", want: "timestamp with time zone"},
		{pgType: "timestamp without time zone", want: "timestamp without time zone"},
		{pgType: "date", want: "date"},
		{pgType: "time without time zone", want: "time without time zone"},
		{pgType: "uuid", want: "uuid"},
		{pgType: "text", want: "text"},
		{pgType: "bytea", want: "bytea"},
		{pgType: "jsonb", want: "jsonb"},
		{pgType: "json", want: "json"},
		{pgType: "interval", want: "interval"},
		{pgType: "character varying(255)", want: "character varying(255)"},
		{pgType: "character(1)", want: "character(1)"},
		{pgType: "numeric(10,2)", want: "numeric(10,2)"},
		{pgType: "numeric(38, 0)", want: "numeric(38, 0)"},
		{pgType: " text ", want: "text"},
		{pgType: "text[]", want: "text[]"},
		{pgType: "bigint[]", want: "bigint[]"},
		{pgType: "character varying(10)[]", want: "character varying(10)[]"},

		// Rejected by ColdFront and mapped.
		{pgType: "oid", want: "bigint"},
		{pgType: "xid", want: "bigint"},
		{pgType: "inet", want: "text"},
		{pgType: `"char"`, want: "text"},
		{pgType: "name", want: "text"},
		{pgType: "numeric", want: "numeric(38,0)"},

		// Not supported at all.
		{pgType: "cidr", wantErr: "no mapping"},
		{pgType: "numeric(39,0)", wantErr: "no mapping"},
		{pgType: "numeric(99999999999999999999,0)", wantErr: "no mapping"},
		{pgType: "point", wantErr: "no mapping"},
		{pgType: "timestamp with time zone[]", wantErr: "array of"},
		{pgType: "jsonb[]", wantErr: "array of"},
		{pgType: "json[]", wantErr: "array of"},
		{pgType: "interval[]", wantErr: "array of"},
		{pgType: "oid[]", wantErr: "array of"},
		{pgType: "text[][]", wantErr: "array of"},
	}
	for _, tt := range tests {
		t.Run(tt.pgType, func(t *testing.T) {
			got, err := StorageType(tt.pgType)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("StorageType(%q) error = %v, want one containing %q", tt.pgType, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("StorageType(%q) unexpected error: %v", tt.pgType, err)
			}
			if got != tt.want {
				t.Errorf("StorageType(%q) = %q, want %q", tt.pgType, got, tt.want)
			}
		})
	}
}

// TestSpecificationIsStorable checks every table of the specification:
// each column has a ColdFront type, the partition column exists with the
// type the partition transform needs, names are unique, and a column a
// later migration adds already has a type ColdFront stores natively,
// because that migration's ALTER TABLE runs unchanged on the view.
func TestSpecificationIsStorable(t *testing.T) {
	if len(tables) == 0 {
		t.Fatal("the specification lists no tables")
	}
	seenTables := map[string]bool{}
	for _, tbl := range tables {
		if seenTables[tbl.Name] {
			t.Errorf("table %s is listed twice", tbl.Name)
		}
		seenTables[tbl.Name] = true

		if tbl.CreatedIn <= 0 {
			t.Errorf("metrics.%s has no creating migration", tbl.Name)
		}

		seenCols := map[string]bool{}
		hasCollectedAt := false
		for _, c := range tbl.Columns {
			if seenCols[c.Name] {
				t.Errorf("metrics.%s.%s is listed twice", tbl.Name, c.Name)
			}
			seenCols[c.Name] = true

			st, err := StorageType(c.PGType)
			if err != nil {
				t.Errorf("metrics.%s.%s: %v", tbl.Name, c.Name, err)
				continue
			}
			if c.AddedIn != 0 {
				if c.AddedIn <= tbl.CreatedIn {
					t.Errorf("metrics.%s.%s is added in migration %d, not after the table's creation in %d",
						tbl.Name, c.Name, c.AddedIn, tbl.CreatedIn)
				}
				if st != c.PGType {
					t.Errorf("metrics.%s.%s is added by ALTER TABLE as %s, which ColdFront stores as %s; the migration would need translating",
						tbl.Name, c.Name, c.PGType, st)
				}
			}
			if c.Name == "collected_at" {
				hasCollectedAt = c.PGType == "timestamp with time zone" && c.AddedIn == 0
			}
		}
		if !hasCollectedAt {
			t.Errorf("metrics.%s has no timestamptz collected_at column to partition by", tbl.Name)
		}
	}
}

// TestSpecificationMappedTypes pins the counts of mapped columns given
// in issue #755, so that a new column with a mapped type is a
// deliberate change.
func TestSpecificationMappedTypes(t *testing.T) {
	counts := map[string]int{}
	for _, tbl := range tables {
		for _, c := range tbl.Columns {
			if _, ok := mappedTypes[c.PGType]; ok {
				counts[c.PGType]++
			}
		}
	}
	want := map[string]int{
		"oid": 17, "inet": 2, "xid": 2, `"char"`: 1, "name": 1, "numeric": 2,
	}
	if !reflect.DeepEqual(counts, want) {
		t.Errorf("mapped column counts = %v, want %v", counts, want)
	}
}

func TestLookupAndTables(t *testing.T) {
	tbl, ok := Lookup("pg_connectivity")
	if !ok {
		t.Fatal("Lookup(pg_connectivity) found nothing")
	}
	if tbl.Name != "pg_connectivity" || tbl.CreatedIn != 1 {
		t.Errorf("Lookup(pg_connectivity) = %+v", tbl)
	}
	if _, ok := Lookup("not_a_metrics_table"); ok {
		t.Error("Lookup found a table that does not exist")
	}

	all := Tables()
	names := TableNames()
	if len(all) != len(tables) || len(names) != len(tables) {
		t.Fatalf("Tables() has %d and TableNames() %d entries, want %d", len(all), len(names), len(tables))
	}
	for i := range all {
		if all[i].Name != names[i] {
			t.Errorf("Tables()[%d] = %s but TableNames()[%d] = %s", i, all[i].Name, i, names[i])
		}
	}
	all[0].Name = "changed"
	if tables[0].Name == "changed" {
		t.Error("Tables() returned the specification itself rather than a copy")
	}
}

func TestCreateColumnsOmitsLaterColumns(t *testing.T) {
	tbl, _ := Lookup("pg_stat_activity")
	for _, c := range tbl.CreateColumns() {
		if c.Name == "query_id" {
			t.Fatal("CreateColumns includes query_id, which migration 12 adds")
		}
	}
	if len(tbl.CreateColumns()) != len(tbl.Columns)-1 {
		t.Errorf("CreateColumns has %d columns, want %d", len(tbl.CreateColumns()), len(tbl.Columns)-1)
	}
}

func TestColumnsJSON(t *testing.T) {
	tbl, _ := Lookup("pg_connectivity")
	got, err := tbl.ColumnsJSON()
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"name":"connection_id","type":"integer"},` +
		`{"name":"collected_at","type":"timestamp with time zone"},` +
		`{"name":"response_time_ms","type":"double precision"}]`
	if got != want {
		t.Errorf("ColumnsJSON() = %s, want %s", got, want)
	}

	tbl, _ = Lookup("pg_database")
	got, err = tbl.ColumnsJSON()
	if err != nil {
		t.Fatal(err)
	}
	var defs []columnDef
	if err := json.Unmarshal([]byte(got), &defs); err != nil {
		t.Fatal(err)
	}
	types := map[string]string{}
	for _, d := range defs {
		types[d.Name] = d.Type
	}
	for col, want := range map[string]string{
		"datdba": "bigint", "datlocprovider": "text", "datfrozenxid": "bigint",
		"datname": "text", "collected_at": "timestamp with time zone",
	} {
		if types[col] != want {
			t.Errorf("pg_database.%s declared as %q, want %q", col, types[col], want)
		}
	}
}

func TestColumnsJSONRejectsUnsupportedType(t *testing.T) {
	tbl := Table{Name: "bogus", Columns: []Column{{Name: "addr", PGType: "cidr"}}}
	if _, err := tbl.ColumnsJSON(); err == nil || !strings.Contains(err.Error(), "metrics.bogus.addr") {
		t.Errorf("ColumnsJSON() error = %v, want one naming metrics.bogus.addr", err)
	}
}

func TestConverterFor(t *testing.T) {
	if ConverterFor("inet") == nil || ConverterFor(`"char"`) == nil {
		t.Error("inet and \"char\" need converters")
	}
	// pgx encodes the Go values of these mapped types into their
	// ColdFront types directly, so they need none.
	for _, pgType := range []string{"oid", "xid", "name", "numeric", "text", "bigint", ""} {
		if ConverterFor(pgType) != nil {
			t.Errorf("ConverterFor(%q) is not nil", pgType)
		}
	}
}

func TestInetToText(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want any
	}{
		{"IPv4 host", netip.MustParsePrefix("10.1.2.3/32"), "10.1.2.3"},
		{"IPv6 host", netip.MustParsePrefix("2001:db8::1/128"), "2001:db8::1"},
		{"IPv4 network", netip.MustParsePrefix("10.0.0.0/8"), "10.0.0.0/8"},
		{"IPv4 with host bits", netip.PrefixFrom(netip.MustParseAddr("10.1.2.3"), 8), "10.1.2.3/8"},
		{"address", netip.MustParseAddr("192.0.2.1"), "192.0.2.1"},
		{"invalid prefix", netip.Prefix{}, nil},
		{"invalid address", netip.Addr{}, nil},
		{"string", "192.0.2.1", "192.0.2.1"},
		{"nil", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inetToText(tt.in); got != tt.want {
				t.Errorf("inetToText(%v) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestQcharToText(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want any
	}{
		{"rune", 'c', "c"},
		{"zero rune", rune(0), ""},
		{"byte", byte('i'), "i"},
		{"zero byte", byte(0), ""},
		{"string", "b", "b"},
		{"nil", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := qcharToText(tt.in); got != tt.want {
				t.Errorf("qcharToText(%#v) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestTableConverters(t *testing.T) {
	act, _ := Lookup("pg_stat_activity")
	convs := act.Converters([]string{"connection_id", "client_addr", "unknown", "datid"})
	if len(convs) != 4 {
		t.Fatalf("Converters returned %d entries, want 4", len(convs))
	}
	for i, wantSet := range []bool{false, true, false, false} {
		if (convs[i] != nil) != wantSet {
			t.Errorf("Converters()[%d] set = %v, want %v", i, convs[i] != nil, wantSet)
		}
	}
	if got := convs[1](netip.MustParsePrefix("192.0.2.7/32")); got != "192.0.2.7" {
		t.Errorf("client_addr converter returned %#v", got)
	}

	conn, _ := Lookup("pg_connectivity")
	if convs := conn.Converters([]string{"connection_id", "collected_at", "response_time_ms"}); convs != nil {
		t.Errorf("pg_connectivity needs no conversion, got %d converters", len(convs))
	}
}
