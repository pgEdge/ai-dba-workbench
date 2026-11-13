/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package hostvalidation

import (
	"context"
	"errors"
	"testing"
)

func TestTakeHostAddr(t *testing.T) {
	cases := []struct {
		name     string
		params   map[string]string
		wantOK   bool
		wantErr  error
		wantAddr string
	}{
		{"absent", map[string]string{"application_name": "x"}, false, nil, ""},
		{"nil map", nil, false, nil, ""},
		{"ipv4", map[string]string{"hostaddr": "192.0.2.10"}, true, nil, "192.0.2.10"},
		{"ipv6", map[string]string{"hostaddr": "2001:db8::1"}, true, nil, "2001:db8::1"},
		{"bracketed ipv6", map[string]string{"hostaddr": "[2001:db8::1]"}, true, nil, "2001:db8::1"},
		{"host name", map[string]string{"hostaddr": "db.example.com"}, true, ErrHostAddrNotIP, ""},
		{"with port", map[string]string{"hostaddr": "192.0.2.10:5432"}, true, ErrHostAddrNotIP, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lookup, ok, err := TakeHostAddr(tc.params)
			if ok != tc.wantOK || !errors.Is(err, tc.wantErr) {
				t.Fatalf("ok, err = %v, %v; want %v, %v", ok, err, tc.wantOK, tc.wantErr)
			}
			if _, left := tc.params[HostAddrParam]; left {
				t.Error("hostaddr left in params")
			}
			if tc.wantAddr == "" {
				if lookup != nil {
					t.Error("lookup returned with no usable hostaddr")
				}
				return
			}
			addrs, err := lookup(context.Background(), "db.example.com")
			if err != nil || len(addrs) != 1 || addrs[0] != tc.wantAddr {
				t.Errorf("lookup = %v, %v; want [%s]", addrs, err, tc.wantAddr)
			}
		})
	}
}
