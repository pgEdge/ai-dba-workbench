/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package coldfrontsql

import "testing"

// TestViolations checks the rules against the shapes that failed live on
// ColdFront, and against the replacements that worked, so the guard
// cannot quietly stop matching.
func TestViolations(t *testing.T) {
	bad := map[string]string{
		"untyped parameter arithmetic": `SELECT 1 FROM metrics.t WHERE collected_at > NOW() - INTERVAL '1 day' * $1`,
		"untyped parameter comparison": `SELECT 1 FROM metrics.t WHERE connection_id = $2`,
		"untyped NULL test":            `SELECT 1 FROM metrics.t WHERE ($2 IS NULL OR collected_at <= $2::timestamptz)`,
		"cast NULL test":               `SELECT 1 FROM metrics.t WHERE ($2::timestamptz IS NULL OR collected_at <= $2::timestamptz)`,
		"NULL test after comparison":   `SELECT 1 FROM metrics.t WHERE (collected_at <= $2::timestamptz OR $2::timestamptz IS NULL)`,
		"NOT NULL test":                `SELECT 1 FROM metrics.t WHERE $1::int IS NOT NULL`,
		"spaced cast NULL test":        `SELECT 1 FROM metrics.t WHERE $2 :: timestamp with time zone is null`,
		"ANY over a parameter":         `SELECT 1 FROM metrics.t WHERE connection_id = ANY($1::int[])`,
		"ANY over a literal":           `SELECT 1 FROM metrics.t WHERE connection_id = ANY('{2,3}'::int[])`,
		"unnest":                       `SELECT 1 FROM metrics.t WHERE connection_id IN (SELECT unnest($1::int[]))`,
		"uncast avg":                   `SELECT avg(numbackends) FROM metrics.t`,
		"avg cast too late":            `SELECT avg(numbackends) + 1 FROM metrics.t`,
		"unbalanced avg":               `SELECT avg(numbackends FROM metrics.t`,
		"pg_lsn":                       `SELECT (sent_lsn::pg_lsn - replay_lsn::pg_lsn) FROM metrics.t`,
		"make_interval":                `SELECT 1 FROM metrics.t WHERE collected_at > NOW() - make_interval(days => $1::int)`,
		"host over text":               `SELECT host(client_addr) FROM metrics.t`,
		"host over a text cast":        `SELECT HOST(client_addr::text) FROM metrics.t`,
		"inet cast before host ends":   `SELECT host(client_addr::inet || '') FROM metrics.t`,
		"unbalanced host":              `SELECT host(client_addr::inet FROM metrics.t`,
		"inet cast":                    `SELECT client_addr::inet FROM metrics.t`,
		"inet cast beside host":        `SELECT host(client_addr::inet), server_addr::inet FROM metrics.t`,
		"COALESCE-open upper bound":    `SELECT 1 FROM metrics.t WHERE collected_at <= COALESCE($2::timestamptz, collected_at)`,
		"spaced COALESCE parameter":    `SELECT 1 FROM metrics.t WHERE collected_at <= coalesce ( $2::timestamptz, NOW())`,
		"infinite upper bound":         `SELECT COALESCE(LEAD(collected_at) OVER w, 'infinity'::timestamptz) FROM metrics.t`,
		"infinite lower bound":         `SELECT '-infinity'::timestamptz FROM metrics.t`,
		"capitalised infinity":         `SELECT 1 FROM metrics.t WHERE collected_at < 'Infinity'`,
	}
	for name, sql := range bad {
		if len(Violations(sql)) == 0 {
			t.Errorf("%s: no violation reported for %q", name, sql)
		}
	}

	good := map[string]string{
		"cast parameter arithmetic": `SELECT 1 FROM metrics.t WHERE collected_at > NOW() - INTERVAL '1 day' * $1::int`,
		"cast parameter comparison": `SELECT 1 FROM metrics.t WHERE connection_id = $1::int AND name = $2::text`,
		"plain upper bound":         `SELECT 1 FROM metrics.t WHERE collected_at <= $2::timestamptz`,
		"COALESCE over a column":    `SELECT COALESCE(datname, '') FROM metrics.t`,
		"NULL test on a column":     `SELECT 1 FROM metrics.t WHERE valid_from IS NULL OR collected_at >= valid_from`,
		"integer literal list":      `SELECT 1 FROM metrics.t WHERE connection_id IN (2, 3)`,
		"cast avg":                  `SELECT avg(numbackends)::float, AVG(x) :: float8 FROM metrics.t`,
		"array_agg subscript":       `SELECT (array_agg(x ORDER BY y DESC))[1] FROM metrics.t`,
		"uncast EXTRACT":            `SELECT MAX(EXTRACT(EPOCH FROM (collected_at - xact_start)))::float FROM metrics.t`,
		"host over an inet cast":    `SELECT host(client_addr::inet), Host ( s.server_addr :: INET ) FROM metrics.t s`,
		"NULL-open range join":      `SELECT 1 FROM metrics.t a JOIN r m ON (m.valid_from IS NULL OR a.collected_at >= m.valid_from) AND (m.valid_to IS NULL OR a.collected_at < m.valid_to)`,
	}
	for name, sql := range good {
		if p := Violations(sql); len(p) != 0 {
			t.Errorf("%s: unexpected violations %v for %q", name, p, sql)
		}
	}
}

// TestViolationsReportsEach checks that one statement breaking several
// rules reports each of them, rather than stopping at the first.
func TestViolationsReportsEach(t *testing.T) {
	sql := `SELECT avg(x), sent_lsn::pg_lsn FROM metrics.t WHERE id = $1 AND c < 'infinity'`
	if got := len(Violations(sql)); got != 4 {
		t.Errorf("Violations reported %d problems, want 4: %v", got, Violations(sql))
	}
}

// TestViolationsExcusesOnlyHostInetArgs checks that an inet cast is
// excused only where it is the argument of host(), so a second cast in
// the same statement is still reported, once.
func TestViolationsExcusesOnlyHostInetArgs(t *testing.T) {
	sql := `SELECT host(a::inet), b::inet, host(c::inet) FROM metrics.t`
	if got := Violations(sql); len(got) != 1 {
		t.Errorf("Violations reported %d problems, want 1: %v", len(got), got)
	}
}

// TestMatchingParen pins the closing-parenthesis search, including the
// nested and unbalanced cases.
func TestMatchingParen(t *testing.T) {
	cases := []struct {
		s    string
		open int
		want int
	}{
		{"(a)", 0, 2},
		{"f((a)(b))", 1, 8},
		{"((a)", 0, -1},
	}
	for _, c := range cases {
		if got := matchingParen(c.s, c.open); got != c.want {
			t.Errorf("matchingParen(%q, %d) = %d, want %d", c.s, c.open, got, c.want)
		}
	}
}
