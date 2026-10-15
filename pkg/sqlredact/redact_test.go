/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package sqlredact

import (
	"strings"
	"testing"
)

func TestRedactQuery(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// Statements with nothing to redact.
		{"empty", "", ""},
		{"no trigger", "SELECT * FROM t WHERE a = 'x'", "SELECT * FROM t WHERE a = 'x'"},
		{
			"pg_stat_statements placeholder",
			"ALTER ROLE app PASSWORD $1",
			"ALTER ROLE app PASSWORD $1",
		},
		{"password null", "ALTER ROLE app PASSWORD NULL", "ALTER ROLE app PASSWORD NULL"},
		{
			"secret name not followed by a value",
			"SELECT password FROM users",
			"SELECT password FROM users",
		},
		{
			"trigger in a comment only",
			"SELECT 1 /* token */ -- password\n",
			"SELECT 1 /* token */ -- password\n",
		},
		{
			"connection string without a password",
			"CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 user=rep' PUBLICATION p",
			"CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 user=rep' PUBLICATION p",
		},
		{
			"URI without a password",
			"SELECT dblink_connect('postgresql://rep@192.0.2.1/db')",
			"SELECT dblink_connect('postgresql://rep@192.0.2.1/db')",
		},

		// Role passwords.
		{
			"create role",
			"CREATE ROLE app LOGIN PASSWORD 's3cret' VALID UNTIL '2030-01-01'",
			"CREATE ROLE app LOGIN PASSWORD '********' VALID UNTIL '2030-01-01'",
		},
		{
			"alter user, encrypted, lower case",
			"alter user app with encrypted password 'md5abc';",
			"alter user app with encrypted password '********';",
		},
		{
			"scram verifier",
			"ALTER USER app PASSWORD 'SCRAM-SHA-256$4096:c2FsdA==$a:b'",
			"ALTER USER app PASSWORD '********'",
		},
		{
			"escape string",
			`ALTER ROLE app PASSWORD E'it\'s'`,
			"ALTER ROLE app PASSWORD '********'",
		},
		{
			"unicode string",
			`ALTER ROLE app PASSWORD U&'d\0061t' UESCAPE '\'`,
			`ALTER ROLE app PASSWORD '********' UESCAPE '\'`,
		},
		{
			"dollar quoted",
			"ALTER ROLE app PASSWORD $q$it's$q$",
			"ALTER ROLE app PASSWORD '********'",
		},
		{
			"plain dollar quote",
			"ALTER ROLE app PASSWORD $$x$$ LOGIN",
			"ALTER ROLE app PASSWORD '********' LOGIN",
		},
		{
			"doubled quote inside",
			"ALTER ROLE app PASSWORD 'it''s' LOGIN",
			"ALTER ROLE app PASSWORD '********' LOGIN",
		},
		{
			"comment between keyword and value",
			"ALTER ROLE app PASSWORD /* new */ 'x' LOGIN",
			"ALTER ROLE app PASSWORD /* new */ '********' LOGIN",
		},
		{
			"continued constant",
			"ALTER ROLE app PASSWORD 'ab'\n'cd' LOGIN",
			"ALTER ROLE app PASSWORD '********' LOGIN",
		},
		{
			"backslash in a plain constant redacts the rest",
			`ALTER ROLE app PASSWORD 'ab\' LOGIN 'cd'`,
			"ALTER ROLE app PASSWORD '********'",
		},
		{
			"truncated inside the password",
			"ALTER ROLE app PASSWORD 'sec",
			"ALTER ROLE app PASSWORD '********'",
		},
		{
			"two statements",
			"ALTER ROLE a PASSWORD 'x'; ALTER ROLE b PASSWORD 'y'",
			"ALTER ROLE a PASSWORD '********'; ALTER ROLE b PASSWORD '********'",
		},
		{
			"already redacted",
			"ALTER ROLE app PASSWORD '********'",
			"ALTER ROLE app PASSWORD '********'",
		},

		// User mappings, servers and options.
		{
			"user mapping options",
			"CREATE USER MAPPING FOR app SERVER s OPTIONS (user 'remote', password 'pw')",
			"CREATE USER MAPPING FOR app SERVER s OPTIONS (user 'remote', password '********')",
		},
		{
			"alter user mapping set and add",
			"ALTER USER MAPPING FOR app SERVER s OPTIONS (SET password 'a', ADD sslpassword 'b')",
			"ALTER USER MAPPING FOR app SERVER s OPTIONS (SET password '********', ADD sslpassword '********')",
		},
		{
			"quoted option name",
			`CREATE SERVER s FOREIGN DATA WRAPPER w OPTIONS ("Password" 'pw', host '192.0.2.1')`,
			`CREATE SERVER s FOREIGN DATA WRAPPER w OPTIONS ("Password" '********', host '192.0.2.1')`,
		},
		{
			"quoted option name with a doubled quote",
			`OPTIONS ("my""password" 'pw')`,
			`OPTIONS ("my""password" '********')`,
		},
		{
			"other secret names",
			"OPTIONS (passwd 'a', api_token 'b', client_secret 'c', aws_credentials 'd')",
			"OPTIONS (passwd '********', api_token '********', client_secret '********', aws_credentials '********')",
		},

		// Separators.
		{
			"set with equals",
			"SET myext.api_token = 'abc'",
			"SET myext.api_token = '********'",
		},
		{
			"set with to",
			"ALTER SYSTEM SET ssl_passphrase_command TO 'echo pw'",
			"ALTER SYSTEM SET ssl_passphrase_command TO '********'",
		},
		{
			"named argument",
			"SELECT f(secret => 'a', token := 'b')",
			"SELECT f(secret => '********', token := '********')",
		},
		{
			"comparison",
			"SELECT 1 FROM users WHERE password = 'pw' AND name = 'x'",
			"SELECT 1 FROM users WHERE password = '********' AND name = 'x'",
		},
		{
			"other operator is not a separator",
			"SELECT 1 FROM users WHERE token <> 'x'",
			"SELECT 1 FROM users WHERE token <> 'x'",
		},

		// Connection strings.
		{
			"subscription connection",
			"CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 user=rep password=pw dbname=db' PUBLICATION p",
			"CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 user=rep password=******** dbname=db' PUBLICATION p",
		},
		{
			"quoted password inside a connection string",
			"ALTER SUBSCRIPTION s CONNECTION 'host=192.0.2.1 password=''a b'''",
			"ALTER SUBSCRIPTION s CONNECTION 'host=192.0.2.1 password=********'",
		},
		{
			"dblink URI",
			"SELECT dblink_connect('c', 'postgresql://rep:pw@192.0.2.1/db')",
			"SELECT dblink_connect('c', 'postgresql://rep:********@192.0.2.1/db')",
		},
		{
			"primary_conninfo",
			"ALTER SYSTEM SET primary_conninfo = 'host=192.0.2.1 sslpassword=k'",
			"ALTER SYSTEM SET primary_conninfo = 'host=192.0.2.1 sslpassword=********'",
		},
		{
			"national prefix kept",
			"SELECT dblink(N'host=192.0.2.1 password=pw', 'SELECT 1')",
			"SELECT dblink(N'host=192.0.2.1 password=********', 'SELECT 1')",
		},
		{
			"dollar-quoted connection string",
			"SELECT dblink($c$host=192.0.2.1 password=pw$c$)",
			"SELECT dblink($c$host=192.0.2.1 password=********$c$)",
		},
		{
			"truncated dollar-quoted connection string",
			"SELECT dblink($$host=192.0.2.1 password=pw",
			"SELECT dblink($$host=192.0.2.1 password=********",
		},
		{
			"dollar-quoted text that cannot be parsed",
			"SELECT $$the password is pw$$",
			"SELECT $$********$$",
		},
		{
			"escape-string connection string goes in full",
			`SELECT dblink(E'host=x password=pw\n')`,
			"SELECT dblink('********')",
		},
		{
			"truncated connection string",
			"SELECT dblink('host=192.0.2.1 password=pw",
			"SELECT dblink('host=192.0.2.1 password=********",
		},
		{
			"backslash in a connection string redacts the rest",
			`SELECT dblink('host=x password=a\', 'SELECT 1')`,
			"SELECT dblink('********'",
		},
		{
			"text that mentions a password is replaced",
			"SELECT 'reset your password'",
			"SELECT '********'",
		},

		// Lexing corners.
		{
			"identifier with a dollar is not a dollar quote",
			"SELECT a$b, token = 'x'",
			"SELECT a$b, token = '********'",
		},
		{
			"runs of whitespace and a final line comment",
			"ALTER ROLE app  PASSWORD\t\t'x' -- password",
			"ALTER ROLE app  PASSWORD\t\t'********' -- password",
		},
		{
			"longer dollar tag",
			"ALTER ROLE app PASSWORD $tag_1$x$tag_1$",
			"ALTER ROLE app PASSWORD '********'",
		},
		{
			"lone dollar",
			"SELECT $ password 'x'",
			"SELECT $ password '********'",
		},
		{
			"nested block comment",
			"/* a /* password 'x' */ b */ SELECT 1",
			"/* a /* password 'x' */ b */ SELECT 1",
		},
		{
			"unterminated comment",
			"SELECT 1 /* password 'x'",
			"SELECT 1 /* password 'x'",
		},
		{
			"unterminated quoted identifier",
			`SELECT "password 'x'`,
			`SELECT "password 'x'`,
		},
		{
			"bit string after a secret name",
			"SELECT token = B'0101'",
			"SELECT token = '********'",
		},
		{
			"prefix letter that is an identifier",
			"SELECT e, password = x'ff'",
			"SELECT e, password = '********'",
		},
		{
			"u without ampersand is an identifier",
			"SELECT u, token 'x'",
			"SELECT u, token '********'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactQuery(tt.in)
			if got != tt.want {
				t.Errorf("RedactQuery(%q)\n got  %q\n want %q", tt.in, got, tt.want)
			}
			if again := RedactQuery(got); again != got {
				t.Errorf("not idempotent: %q became %q", got, again)
			}
		})
	}
}

func TestRedactQuery_NeverLeaksAPassword(t *testing.T) {
	const secret = "Zq9secret"
	for _, q := range []string{
		"CREATE ROLE r PASSWORD '" + secret + "'",
		"CREATE ROLE r PASSWORD E'" + secret + "'",
		"CREATE ROLE r PASSWORD $$" + secret + "$$",
		"CREATE ROLE r PASSWORD $t$" + secret + "$t$",
		"CREATE USER MAPPING FOR r SERVER s OPTIONS (password '" + secret + "')",
		"CREATE SUBSCRIPTION s CONNECTION 'password=" + secret + "' PUBLICATION p",
		"CREATE SUBSCRIPTION s CONNECTION 'postgres://u:" + secret + "@h/d' PUBLICATION p",
		"CREATE ROLE r PASSWORD '" + secret[:4],
	} {
		if got := RedactQuery(q); strings.Contains(got, secret[:4]) {
			t.Errorf("RedactQuery(%q) = %q leaks the secret", q, got)
		}
	}
}

func TestMightContainSecret(t *testing.T) {
	for _, s := range []string{"PASSWD", "x Secret", "postgresql://h", "Private_Key", "credentials", "TOKEN", "passphrase"} {
		if !MightContainSecret(s) {
			t.Errorf("MightContainSecret(%q) = false", s)
		}
	}
	if MightContainSecret("SELECT 1") {
		t.Error("MightContainSecret(SELECT 1) = true")
	}
}

func TestCandidatePatterns(t *testing.T) {
	patterns := CandidatePatterns()
	if len(patterns) != len(triggerFragments) {
		t.Fatalf("got %d patterns, want %d", len(patterns), len(triggerFragments))
	}
	for i, p := range patterns {
		if p != "%"+triggerFragments[i]+"%" {
			t.Errorf("pattern %d = %q", i, p)
		}
	}
	// Every secret name must be caught by the cheap check, or RedactQuery
	// would skip a statement it should redact.
	for _, f := range secretNameFragments {
		if !MightContainSecret(f) {
			t.Errorf("secret name fragment %q not covered by the trigger fragments", f)
		}
	}
}
