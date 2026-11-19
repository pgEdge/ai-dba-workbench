/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package pgexec

import (
	"reflect"
	"testing"
)

type fakeConn struct {
	enc string
	tx  byte
}

func (f fakeConn) ParameterStatus(string) string { return f.enc }
func (f fakeConn) TxStatus() byte                { return f.tx }

type fakeRows struct {
	rows [][][]byte
	i    int
}

func (f *fakeRows) NextRow() bool {
	f.i++
	return f.i <= len(f.rows)
}
func (f *fakeRows) Values() [][]byte { return f.rows[f.i-1] }

func TestConnChecks(t *testing.T) {
	if !EncodingIsUTF8(fakeConn{"UTF8", 'I'}) || EncodingIsUTF8(fakeConn{"SJIS", 'I'}) {
		t.Error("EncodingIsUTF8 wrong")
	}
	if TxLeftOpen(fakeConn{"UTF8", 'I'}) || !TxLeftOpen(fakeConn{"UTF8", 'T'}) {
		t.Error("TxLeftOpen wrong")
	}
}

func TestRowValuesRendersNull(t *testing.T) {
	got := RowValues([][]byte{[]byte("a"), nil, []byte("")})
	if !reflect.DeepEqual(got, []string{"a", "NULL", ""}) {
		t.Errorf("got %v", got)
	}
}

func TestAppendRowsTruncates(t *testing.T) {
	src := &fakeRows{rows: [][][]byte{{[]byte("1")}, {[]byte("2")}, {[]byte("3")}}}
	rows, trunc := AppendRows(nil, src, 2)
	if !trunc || len(rows) != 2 || rows[1][0] != "2" {
		t.Errorf("rows=%v trunc=%v", rows, trunc)
	}
	src = &fakeRows{rows: [][][]byte{{[]byte("1")}}}
	rows, trunc = AppendRows(nil, src, 2)
	if trunc || len(rows) != 1 {
		t.Errorf("rows=%v trunc=%v", rows, trunc)
	}
}
