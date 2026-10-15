/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
// Package redacttest holds the inputs the redaction patterns are
// tested on, shared by the redact package's tests and the server's
// PostgreSQL tests so both sides are checked against the same cases.
package redacttest

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Case is one input and the text redact.Text, and the server's
// RedactedText, should turn it into. The secrets are all "example-secret" or start "s3cr", so a
// test can check none survives.
type Case struct {
	Name, In, Want string
}

// Cases are the inputs both the Go and the PostgreSQL side are tested
// on.
var Cases = []Case{
	{"create role", "CREATE ROLE app LOGIN PASSWORD 'example-secret'",
		"CREATE ROLE app LOGIN PASSWORD '********'"},
	{"alter user encrypted", "alter user app with encrypted password 'example-secret' valid until 'infinity'",
		"alter user app with encrypted password '********' valid until 'infinity'"},
	{"escaped quote", "ALTER ROLE app PASSWORD 'exa''mple-secret'",
		"ALTER ROLE app PASSWORD '********'"},
	{"backslash escaped quote", `ALTER ROLE app PASSWORD 'exa\'mple-secret' VALID UNTIL 'infinity'`,
		"ALTER ROLE app PASSWORD '********' VALID UNTIL 'infinity'"},
	{"escape string", "ALTER ROLE app PASSWORD E'example-secret'",
		"ALTER ROLE app PASSWORD '********'"},
	{"unicode escape string", "ALTER ROLE app PASSWORD U&'example-secret'",
		"ALTER ROLE app PASSWORD '********'"},
	{"dollar quoted", "ALTER ROLE app PASSWORD $$exam$ple-secret$$ VALID UNTIL 'infinity'",
		"ALTER ROLE app PASSWORD '********' VALID UNTIL 'infinity'"},
	{"tagged dollar quoted", "ALTER ROLE app PASSWORD $pw$exa$mple $$secret$pw$;",
		"ALTER ROLE app PASSWORD '********';"},
	{"truncated statement", "CREATE ROLE app PASSWORD 'example-sec",
		"CREATE ROLE app PASSWORD '********'"},
	{"truncated after escaped quote", "CREATE ROLE app PASSWORD 'exa''mple-sec",
		"CREATE ROLE app PASSWORD '********'"},
	{"truncated dollar quoted", "CREATE ROLE app PASSWORD $$example-sec",
		"CREATE ROLE app PASSWORD '********'"},
	{"password null kept", "ALTER ROLE app PASSWORD NULL", "ALTER ROLE app PASSWORD NULL"},
	{"user mapping", "CREATE USER MAPPING FOR app SERVER remote OPTIONS (user 'app', password 'example-secret')",
		"CREATE USER MAPPING FOR app SERVER remote OPTIONS (user 'app', password '********')"},
	{"set to", "SET app.password TO 'example-secret'", "SET app.password TO '********'"},
	{"set quoted name", `SET "app.api_key" TO E'example-secret'`, `SET "app.api_key" TO '********'`},
	{"set equals", "SET app.password = 'example secret'", "SET app.password = ********"},
	{"set_config", "SELECT set_config('app.password', 'example-secret', false)",
		"SELECT set_config('app.password', '********', false)"},
	{"alter system command", "ALTER SYSTEM SET archive_command TO 'curl -u app:example-secret -T %p https://backup.example.com'",
		"ALTER SYSTEM SET archive_command TO '********'"},
	{"alter system conninfo", "ALTER SYSTEM SET primary_conninfo = $$host=192.0.2.1 password=example-secret$$",
		"ALTER SYSTEM SET primary_conninfo = ********"},
	{"conninfo unquoted", "CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 user=app password=example-secret dbname=db' PUBLICATION p",
		"CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 user=app password=******** dbname=db' PUBLICATION p"},
	{"dblink", "SELECT dblink_connect('host=192.0.2.1 password = s3cret')",
		"SELECT dblink_connect('host=192.0.2.1 password = ********"},
	{"unquoted value with brackets", "SELECT dblink_connect('password=s3cr)e;t\"x rest')",
		"SELECT dblink_connect('password=******** rest')"},
	{"conninfo quoted", `primary_conninfo = host=192.0.2.1 password='example\'secret' port=5432`,
		`primary_conninfo = ******** password=******** port=5432`},
	{"escape string after equals", "host=192.0.2.1 password=E'example secret' dbname=db",
		"host=192.0.2.1 password=******** dbname=db"},
	{"named argument", "SELECT f(password => 'example secret')", "SELECT f(password => ********)"},
	{"double quoted shell value", `PGPASSWORD="example secret" pg_dump db`, "PGPASSWORD=******** pg_dump db"},
	{"cloud key", "AWS_SECRET_ACCESS_KEY=example-secret aws s3 cp %p s3://bucket/%f",
		"AWS_SECRET_ACCESS_KEY=******** aws s3 cp %p s3://bucket/%f"},
	{"token", "curl -H token=s3cret-token https://api.example.com", "curl -H token=******** https://api.example.com"},
	{"ldapbindpasswd", "ldapserver=ldap.example.com ldapbindpasswd=example-secret",
		"ldapserver=ldap.example.com ldapbindpasswd=********"},
	{"radiussecrets", "radiusservers=192.0.2.1 radiussecrets=s3cr,s3cr2",
		"radiusservers=192.0.2.1 radiussecrets=********"},
	{"uri", "could not connect to postgresql://app:example-secret@192.0.2.1:5432/db",
		"could not connect to postgresql://app:********@192.0.2.1:5432/db"},
	{"uri postgres scheme", "postgres://app:s3cret@db.example.com/x", "postgres://app:********@db.example.com/x"},
	{"uri other scheme", "redis://app:s3cr/et@cache.example.com:6379", "redis://app:********@cache.example.com:6379"},
	{"uri truncated", "connect to postgresql://app:example-sec", "connect to postgresql://app:********@"},
	{"uri without password", "postgresql://app@192.0.2.1/db", "postgresql://app@192.0.2.1/db"},
	{"json row image", `{"id": 1, "user_password": "example-secret", "note": "password"}`,
		`{"id": 1, "user_password": "********", "note": "password"}`},
	{"json escaped quote", `{"api_secret":"ex\"ample-secret","n":2}`, `{"api_secret":"********","n":2}`},
	{"json other secret keys", `{"api_key": "example-secret", "token": 31337, "creds": {"private_key": "example-secret"}}`,
		`{"api_key": "********", "token": "********", "creds": {"private_key": "********"}}`},
	{"json object value", `{"secret": {"value": "example-secret"}, "n": 1}`, `{"secret": "********", "n": 1}`},
	{"json array value", `{"passwords": ["s3cr1", "s3cr2"], "n": 1}`, `{"passwords": "********", "n": 1}`},
	{"doubled quote conninfo", "CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 password=''s3cret a b''' PUBLICATION p",
		"CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 password=********' PUBLICATION p"},
	{"doubled quote with escaped quote", "CREATE SUBSCRIPTION s CONNECTION 'password=''s3cr''''et x'' dbname=db' PUBLICATION p",
		"CREATE SUBSCRIPTION s CONNECTION 'password=******** dbname=db' PUBLICATION p"},
	{"doubled quote backslash escape", `SELECT dblink_connect('password=''s3cr\''et x'' dbname=db')`,
		"SELECT dblink_connect('password=******** dbname=db')"},
	{"positional node_create", "SELECT spock.node_create('n1', 'host=192.0.2.1 password=''s3cret x''')",
		"SELECT spock.node_create('n1', 'host=192.0.2.1 password=********')"},
	{"nested role password", "EXECUTE 'ALTER ROLE app PASSWORD ''s3cret x'''",
		"EXECUTE 'ALTER ROLE app PASSWORD '********''"},
	{"unquoted dollar value", "host=192.0.2.1 password=$s3cret dbname=db", "host=192.0.2.1 password=******** dbname=db"},
	{"ordinary identifiers kept", "UPDATE t SET sort_key = 42, bypass = true, primary_key = 'abc' WHERE salt = 1",
		"UPDATE t SET sort_key = 42, bypass = true, primary_key = 'abc' WHERE salt = 1"},
	{"ordinary json keys kept", `{"cache_key": "abc", "sort_key": 1, "id": 2}`, `{"cache_key": "abc", "sort_key": 1, "id": 2}`},
	{"password with no space", "ALTER ROLE app PASSWORD'example-secret' LOGIN", "ALTER ROLE app PASSWORD'********' LOGIN"},
	{"password after comment", "ALTER ROLE app PASSWORD/**/'example-secret'", "ALTER ROLE app PASSWORD/**/'********'"},
	{"password after comments and space", "ALTER ROLE app PASSWORD /* x */ /*y*/E'example-secret'",
		"ALTER ROLE app PASSWORD /* x */ /*y*/'********'"},
	{"unquoted value like a parameter", "host=192.0.2.1 password=$9s3cret dbname=db", "host=192.0.2.1 password=******** dbname=db"},
	{"parameter before a bracket kept", "SELECT dblink_connect(password=$1)", "SELECT dblink_connect(password=$1)"},
	{"backslash quoted conninfo", `CREATE SUBSCRIPTION s CONNECTION E'host=192.0.2.1 password=\'s3cret a b\'' PUBLICATION p`,
		`CREATE SUBSCRIPTION s CONNECTION E'host=192.0.2.1 password=********' PUBLICATION p`},
	{"backslash quoted with escaped quote", `SELECT dblink_connect(E'password=\'s3cr\\\'et x\' dbname=db')`,
		`SELECT dblink_connect(E'password=******** dbname=db')`},
	{"backslash quoted role password", `EXECUTE E'ALTER ROLE app PASSWORD \'s3cret x\''`,
		`EXECUTE E'ALTER ROLE app PASSWORD '********''`},
	{"backslash quoted with escaped backslash", `SELECT dblink_connect(E'password=\'s3cr\\et x\' dbname=db')`,
		`SELECT dblink_connect(E'password=******** dbname=db')`},
	{"quoted role password after concatenation", "EXECUTE 'ALTER ROLE app ' || 'PASSWORD ''s3cret a b'''",
		"EXECUTE 'ALTER ROLE app ' || 'PASSWORD '********''"},
	{"quoted role password after a comment", "EXECUTE 'PASSWORD/* c */''s3cret a b'''", "EXECUTE 'PASSWORD/* c */'********''"},
	{"set to after a comment", "SET app.password TO /* c */ 'example-secret'", "SET app.password TO /* c */ '********'"},
	{"set to with no space", "SET app.password TO'example-secret'", "SET app.password TO'********'"},
	{"comment holding stars and a slash", "ALTER ROLE app PASSWORD /* ** / */ 'example-secret'",
		"ALTER ROLE app PASSWORD /* ** / */ '********'"},
	{"line comment", "ALTER ROLE app PASSWORD -- set it\n'example-secret' LOGIN", "ALTER ROLE app PASSWORD -- set it\n'********' LOGIN"},
	{"equals after a comment", "SET app.password = /* c */ 'example secret'", "SET app.password = /* c */ ********"},
	{"json escape string conninfo", `{"stmt": "CONNECTION E'host=192.0.2.1 password=\\'s3cret a b\\''"}`,
		`{"stmt": "CONNECTION E'host=192.0.2.1 password=********'"}`},
	{"json escape string with escaped quote", `{"stmt": "SELECT dblink_connect(E'password=\\'s3cr\\\\\\'et x\\' dbname=db')"}`,
		`{"stmt": "SELECT dblink_connect(E'password=******** dbname=db')"}`},
	{"json role password with escaped quote", `{"stmt": "ALTER ROLE app PASSWORD E's3cr\\'et x'"}`,
		`{"stmt": "ALTER ROLE app PASSWORD '********'"}`},
	{"json conninfo with escaped quote", `{"c": "host=192.0.2.1 password='s3cr\\'et x' port=5432"}`,
		`{"c": "host=192.0.2.1 password=******** port=5432"}`},
	{"json double quoted shell value", `{"cmd": "PGPASSWORD=\"example secret\" pg_dump db"}`, `{"cmd": "PGPASSWORD=******** pg_dump db"}`},
	{"json quoted name", `{"stmt": "SET \"app.api_key\" TO 'example-secret'"}`, `{"stmt": "SET \"app.api_key\" TO '********'"}`},
	{"json parameter kept", `{"stmt": "SELECT 1 WHERE password_encryption = $1"}`, `{"stmt": "SELECT 1 WHERE password_encryption = $1"}`},
	{"plain text kept", "SELECT * FROM t WHERE password_encryption = $1", "SELECT * FROM t WHERE password_encryption = $1"},
	{"normalised parameter", "ALTER ROLE app PASSWORD $1", "ALTER ROLE app PASSWORD $1"},
	{"normalised set parameter", "SELECT set_config('app.password', $1, false)",
		"SELECT set_config('app.password', $1, false)"},
}

// JSONText returns s as a JSON string, as to_jsonb(s)::text writes it
// in a row image: quotes and backslashes are escaped.
func JSONText(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return string(bytes.TrimSuffix(b.Bytes(), []byte("\n")))
}

// JSONFormOK reports whether got, the masked JSON form of c.In, holds
// no secret and matches the JSON form of c.Want. A value at the end of
// the text may also take the closing JSON quote, since an unquoted or
// truncated value is masked to the next white space or the end. A case
// whose input is already JSON text always passes: escaped twice, it is
// not a form a row image holds.
func JSONFormOK(c Case, got string) bool {
	if strings.HasPrefix(c.In, "{") {
		return true
	}
	if SecretLeft(got) || strings.Contains(got, "secret a b") {
		return false
	}
	want := JSONText(c.Want)
	return got == want || got == strings.TrimSuffix(want, `"`)
}

// SecretLeft reports whether s still holds one of the test secrets.
func SecretLeft(s string) bool {
	return strings.Contains(s, "example") && strings.Contains(s, "secret") && !strings.Contains(s, "example.com") ||
		strings.Contains(s, "s3cr") || strings.Contains(s, "31337")
}

// AdversarialInputs returns inputs of about size bytes built to make a
// regular expression engine backtrack: runs of quotes, backslashes,
// dollar signs and comments, repeated secret names and long runs of the
// characters that may surround a secret name.
func AdversarialInputs(size int) map[string]string {
	return map[string]string{
		"quote pairs":        "password '" + strings.Repeat("''", size/2),
		"nested quotes":      "password=''" + strings.Repeat("''''", size/4),
		"lone quotes":        "password ''" + strings.Repeat("'", size),
		"backslashes":        "password='" + strings.Repeat(`\'`, size/3),
		"dollar signs":       "PASSWORD $pw$" + strings.Repeat("$a", size/2),
		"repeated keywords":  strings.Repeat("password=", size/9),
		"json keys":          `{"password": ` + strings.Repeat(`"password": `, size/12),
		"uri":                "postgresql://" + strings.Repeat("a:", size/2),
		"identifier run":     strings.Repeat("a.b_1", size/5) + " password",
		"identifier quotes":  strings.Repeat("a_password'", size/11),
		"quotes and dollars": strings.Repeat("'$", size/2),
		"dollar digits":      "password=" + strings.Repeat("$1", size/2),
		"comments":           "PASSWORD" + strings.Repeat("/*/*", size/4),
		"backslash quotes":   `password=\'` + strings.Repeat(`\\'`, size/3),
	}
}
