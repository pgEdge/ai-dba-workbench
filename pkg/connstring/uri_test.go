/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package connstring

import (
	"net/url"
	"testing"
)

func TestEncodeURIQuery(t *testing.T) {
	cases := []struct {
		name  string
		query url.Values
		want  string
	}{
		{"empty", url.Values{}, ""},
		{"space and literal plus", url.Values{"application_name": {"a+b c"}},
			"application_name=a%2Bb%20c"},
		{"query syntax stays in the value", url.Values{"sslmode": {"x&host=evil.example.com"}},
			"sslmode=x%26host%3Devil.example.com"},
		{"path", url.Values{"sslrootcert": {"/etc/ssl/my certs/root+ca.crt"}},
			"sslrootcert=%2Fetc%2Fssl%2Fmy%20certs%2Froot%2Bca.crt"},
		{"sorted keys", url.Values{"b": {"2"}, "a": {"1"}}, "a=1&b=2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := EncodeURIQuery(tc.query)
			if got != tc.want {
				t.Fatalf("EncodeURIQuery = %q, want %q", got, tc.want)
			}
			// Decoding with libpq's rules ('+' literal, %20 a space)
			// must give back every value unchanged.
			back, err := url.ParseQuery(got)
			if err != nil {
				t.Fatalf("ParseQuery: %v", err)
			}
			for k, v := range tc.query {
				if back.Get(k) != v[0] {
					t.Errorf("%s round-tripped as %q, want %q", k, back.Get(k), v[0])
				}
			}
		})
	}
}
