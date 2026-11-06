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
	"net/netip"
)

// Converter rewrites a value that pgx decoded from a monitored server
// into one that encodes into the column's ColdFront type.
type Converter func(any) any

// converters holds the conversion for each PostgreSQL type whose pgx Go
// value does not encode into the type StorageType maps it to. The other
// mapped types need none: pgx decodes oid and xid to uint32, which it
// encodes into bigint; name to string, which it encodes into text; and
// unbounded numeric to pgtype.Numeric, which it encodes into
// numeric(38,0).
var converters = map[string]Converter{
	"inet":   inetToText,
	`"char"`: qcharToText,
}

// ConverterFor returns the conversion a value of a column declared as
// pgType needs before it is inserted into ColdFront, or nil when the
// value can be inserted as it is.
func ConverterFor(pgType string) Converter {
	return converters[pgType]
}

// Converters returns, for each of the named columns of the table, the
// conversion its values need, or nil for a column that needs none or is
// not in the specification. It returns nil when no column needs a
// conversion, so callers can skip copying the rows.
func (t Table) Converters(columns []string) []Converter {
	types := make(map[string]string, len(t.Columns))
	for _, c := range t.Columns {
		types[c.Name] = c.PGType
	}

	var out []Converter
	for i, name := range columns {
		conv := ConverterFor(types[name])
		if conv == nil {
			continue
		}
		if out == nil {
			out = make([]Converter, len(columns))
		}
		out[i] = conv
	}
	return out
}

// inetToText renders an inet value the way PostgreSQL's inet output
// function does: a host address without its prefix length, and any
// other prefix as address/length. pgx decodes inet to netip.Prefix,
// whose own String method always appends the length.
func inetToText(v any) any {
	switch x := v.(type) {
	case netip.Prefix:
		if !x.IsValid() {
			return nil
		}
		if x.Bits() == x.Addr().BitLen() {
			return x.Addr().String()
		}
		return x.String()
	case netip.Addr:
		if !x.IsValid() {
			return nil
		}
		return x.String()
	default:
		return v
	}
}

// qcharToText renders a "char" value as a one-character string. pgx
// decodes "char" to a rune, which it would otherwise encode into text as
// its decimal code point. The zero byte is "char"'s empty value.
func qcharToText(v any) any {
	switch x := v.(type) {
	case rune:
		if x == 0 {
			return ""
		}
		return string(x)
	case byte:
		if x == 0 {
			return ""
		}
		return string(rune(x))
	default:
		return v
	}
}
