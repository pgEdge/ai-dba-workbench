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

import "testing"

func TestRedactPasswords(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "  \t", "  \t"},
		{"no password", "host=primary.example.com port=5432 user=replicator",
			"host=primary.example.com port=5432 user=replicator"},
		{"unquoted password",
			"host=primary.example.com user=replicator password=s3cret application_name=standby1",
			"host=primary.example.com user=replicator password=******** application_name=standby1"},
		{"quoted password with spaces and escapes",
			`user=replicator password='it\'s a \\secret' host=192.0.2.1`,
			`user=replicator password=******** host=192.0.2.1`},
		{"spaces around equals",
			"host = 192.0.2.1 password =  s3cret  port=5432",
			"host = 192.0.2.1 password =  ********  port=5432"},
		{"password last", "host=192.0.2.1 password=s3cret",
			"host=192.0.2.1 password=********"},
		{"unquoted escaped space", `password=a\ b host=192.0.2.1`,
			`password=******** host=192.0.2.1`},
		{"trailing backslash", `host=192.0.2.1 password=abc\`,
			`host=192.0.2.1 password=********`},
		{"empty quoted password", "password='' host=192.0.2.1",
			"password=******** host=192.0.2.1"},
		{"sslpassword", "sslkey=/etc/key sslpassword=keypass",
			"sslkey=/etc/key sslpassword=********"},
		{"uppercase keyword", "PASSWORD=s3cret host=192.0.2.1",
			"PASSWORD=******** host=192.0.2.1"},
		{"several passwords", "password=a password=b",
			"password=******** password=********"},
		{"password-like value is not a key", "application_name=password=x",
			"application_name=password=x"},
		{"quoted value containing password text",
			"application_name='password=x' host=192.0.2.1",
			"application_name='password=x' host=192.0.2.1"},
		{"nested dbname connection string",
			"dbname='host=192.0.2.1 password=s3cret' user=replicator",
			"dbname='host=192.0.2.1 password=********' user=replicator"},
		{"nested dbname URI",
			"dbname=postgresql://u:s3cret@192.0.2.1/db user=replicator",
			"dbname='postgresql://u:********@192.0.2.1/db' user=replicator"},
		{"plain dbname untouched", "dbname=postgres password=x",
			"dbname=postgres password=********"},
		{"nested dbname without password untouched",
			"dbname='host=192.0.2.1' user=u", "dbname='host=192.0.2.1' user=u"},
		{"unterminated quote fails closed", "host=192.0.2.1 password='s3cret",
			RedactedPassword},
		{"missing equals fails closed", "host 192.0.2.1 password=s3cret",
			RedactedPassword},
		{"bare word fails closed", "s3cret", RedactedPassword},
		{"key at end fails closed", "host=192.0.2.1 password", RedactedPassword},
		{"empty value at end", "host=192.0.2.1 password=",
			"host=192.0.2.1 password=********"},

		{"URI without password", "postgresql://replicator@192.0.2.1:5432/postgres",
			"postgresql://replicator@192.0.2.1:5432/postgres"},
		{"URI userinfo password",
			"postgresql://replicator:s3cret@192.0.2.1:5432/postgres?application_name=standby1",
			"postgresql://replicator:********@192.0.2.1:5432/postgres?application_name=standby1"},
		{"postgres scheme", "postgres://u:p%40ss@192.0.2.1/db",
			"postgres://u:********@192.0.2.1/db"},
		{"URI empty password", "postgresql://u:@192.0.2.1/db",
			"postgresql://u:@192.0.2.1/db"},
		{"URI query password",
			"postgresql://192.0.2.1/db?user=u&password=s3cret&sslmode=require",
			"postgresql://192.0.2.1/db?user=u&password=********&sslmode=require"},
		{"URI encoded query key",
			"postgresql://192.0.2.1/db?pass%77ord=s3cret",
			"postgresql://192.0.2.1/db?pass%77ord=********"},
		{"URI bad encoding in key", "postgresql://192.0.2.1/db?pa%zzss=x&sslpassword=y",
			"postgresql://192.0.2.1/db?pa%zzss=x&sslpassword=********"},
		{"URI param without value", "postgresql://192.0.2.1/db?flag&password=x",
			"postgresql://192.0.2.1/db?flag&password=********"},
		{"URI userinfo and query",
			"postgresql://u:a@192.0.2.1,192.0.2.2:5433/db?sslpassword=b",
			"postgresql://u:********@192.0.2.1,192.0.2.2:5433/db?sslpassword=********"},
		{"URI at sign after path is not userinfo",
			"postgresql://192.0.2.1/db@x?password=y",
			"postgresql://192.0.2.1/db@x?password=********"},
		{"URI IPv6 host", "postgresql://u:p@[2001:db8::1]:5432/db?password=q",
			"postgresql://u:********@[2001:db8::1]:5432/db?password=********"},
		{"URI question mark in brackets", "postgresql://[2001:db8::1?]/db",
			"postgresql://[2001:db8::1?]/db"},
		{"URI host only", "postgresql://192.0.2.1", "postgresql://192.0.2.1"},
		{"scheme is case-sensitive", "POSTGRESQL://u:p@192.0.2.1",
			RedactedPassword},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactPasswords(tt.in)
			if got != tt.want {
				t.Errorf("RedactPasswords(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if again := RedactPasswords(got); again != got {
				t.Errorf("RedactPasswords is not idempotent: %q then %q", got, again)
			}
		})
	}
}
