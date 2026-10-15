/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// textRedactionCase is one input and the text RedactedText should turn
// it into. Every secret is one secretLeft finds, so a test can
// check none survives.
type textRedactionCase struct {
	name, in, want string
}

var textRedactionCases = []textRedactionCase{
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
	{"backslash and newline in a quoted password", "ALTER ROLE app PASSWORD E's3cr\\\net x' LOGIN",
		"ALTER ROLE app PASSWORD '********' LOGIN"},
	{"backslash quoted with escaped backslash", `SELECT dblink_connect(E'password=\'s3cr\\et x\' dbname=db')`,
		`SELECT dblink_connect(E'password=******** dbname=db')`},
	// The conninfo password hunter2\'Secret123 battery: its escaped
	// backslash is \\\\ and its escaped quote \\\' in the escape string.
	{"backslash quoted with escaped backslash and quote",
		`CREATE SUBSCRIPTION s CONNECTION E'host=h password=\'hunter2\\\\\\\'Secret123 battery\'' PUBLICATION p`,
		`CREATE SUBSCRIPTION s CONNECTION E'host=h password=********' PUBLICATION p`},
	// The conninfo password hunter2\Secret123 battery.
	{"backslash quoted with a lone escaped backslash",
		`CREATE SUBSCRIPTION s CONNECTION E'host=h password=\'hunter2\\\\Secret123 battery\'' PUBLICATION p`,
		`CREATE SUBSCRIPTION s CONNECTION E'host=h password=********' PUBLICATION p`},
	// Not a secret, but a quoted word password followed by a quote
	// reads as PASSWORD '...', so the rest is masked: more than the
	// value, never less.
	{"password word before concatenation", `SELECT 'password ' || name FROM users WHERE id = 1`,
		`SELECT 'password '********'`},
	{"quoted role password after concatenation", "EXECUTE 'ALTER ROLE app ' || 'PASSWORD ''s3cret a b'''",
		"EXECUTE 'ALTER ROLE app ' || 'PASSWORD '********''"},
	{"quoted role password after a comment", "EXECUTE 'PASSWORD/* c */''s3cret a b'''", "EXECUTE 'PASSWORD/* c */'********''"},
	{"set to after a comment", "SET app.password TO /* c */ 'example-secret'", "SET app.password TO /* c */ '********'"},
	{"set to with no space", "SET app.password TO'example-secret'", "SET app.password TO'********'"},
	{"comment holding stars and a slash", "ALTER ROLE app PASSWORD /* ** / */ 'example-secret'",
		"ALTER ROLE app PASSWORD /* ** / */ '********'"},
	{"line comment", "ALTER ROLE app PASSWORD -- set it\n'example-secret' LOGIN", "ALTER ROLE app PASSWORD -- set it\n'********' LOGIN"},
	{"equals after a comment", "SET app.password = /* c */ 'example secret'", "SET app.password = ********"},
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
	// The inputs below are from the fourth security review, which
	// tested the patterns in both engines; the review's case numbers
	// are kept in the names.
	{"C1: conninfo value starting with --", "CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 password=--Secret123 dbname=d' PUBLICATION p",
		"CREATE SUBSCRIPTION s CONNECTION 'host=192.0.2.1 password=******** dbname=d' PUBLICATION p"},
	{"C1b: bare conninfo --", "host=192.0.2.1 password=--Secret123 dbname=d",
		"host=192.0.2.1 password=******** dbname=d"},
	{"C1c: value starting /*", "host=192.0.2.1 password=/*x*/hunter2 dbname=d",
		"host=192.0.2.1 password=******** dbname=d"},
	{"C1d: PGPASSWORD --", "PGPASSWORD=--hunter2 pg_dump -h 192.0.2.1 db",
		"PGPASSWORD=******** pg_dump -h 192.0.2.1 db"},
	{"C2: set_config cast", "SELECT set_config('app.password'::text, 'hunter2', false)",
		"SELECT set_config('app.password'::text, '********', false)"},
	{"C3: set_config comment", "SELECT set_config('app.password',/**/'hunter2', false)",
		"SELECT set_config('app.password',/**/'********', false)"},
	{"C4: set_config line comment", "SELECT set_config('app.password', --c\n'hunter2', false)",
		"SELECT set_config('app.password', --c\n'********', false)"},
	{"C5: set_config named", "SELECT set_config(setting_name => 'app.password', new_value => 'hunter2', is_local => false)",
		"SELECT set_config(setting_name => 'app.password', new_value => '********', is_local => false)"},
	{"C6: alter system comment before =", "ALTER SYSTEM SET archive_command /* x */ = 'curl -u admin:hunter2 -T %p https://backup.example.com'",
		"ALTER SYSTEM SET archive_command /* x */ = '********'"},
	{"C7: alter system comment after SET", "ALTER SYSTEM SET/**/archive_command = 'sshpass -p hunter2 scp %p backup:'",
		"ALTER SYSTEM SET/**/archive_command = '********'"},
	{"C7b: alter system newline", "ALTER SYSTEM SET\narchive_command = 'sshpass -p hunter2 scp %p backup:'",
		"ALTER SYSTEM SET\narchive_command = '********'"},
	{"C8: uri user with quote", "SELECT dblink_connect('postgresql://o''brien:hunter2@db.example.com/app')",
		"SELECT dblink_connect('postgresql://o''brien:********@db.example.com/app')"},
	{"C8b: uri user with double quote", "postgresql://a\"b:hunter2@db.example.com/app",
		"postgresql://a\"b:********@db.example.com/app"},
	{"C9: libpq backslash space", "host=192.0.2.1 password=correct\\ horse\\ battery dbname=d",
		"host=192.0.2.1 password=******** dbname=d"},
	{"C12: json pass key", "{\"user\": \"app\", \"pass\": \"hunter2\", \"pwd\": \"hunter2\"}",
		"{\"user\": \"app\", \"pass\": \"********\", \"pwd\": \"********\"}"},
	{"C13: concat after PASSWORD", "EXECUTE 'ALTER ROLE app PASSWORD ' || '''hunter2''';",
		"EXECUTE 'ALTER ROLE app PASSWORD '********'********''';"},
	{"C13b: concat split", "EXECUTE 'ALTER ROLE app PASSWORD' || ' ''hunter2''';",
		"EXECUTE 'ALTER ROLE app PASSWORD'********' ''********''';"},
	{"C14: crlf", "ALTER ROLE app PASSWORD\r\n'hunter2'",
		"ALTER ROLE app PASSWORD\r\n'********'"},
	{"C15: E string escaped quote", "ALTER ROLE app PASSWORD E'hun\\'ter2'",
		"ALTER ROLE app PASSWORD '********'"},
	{"C16: user mapping quoted option", "CREATE USER MAPPING FOR u SERVER s OPTIONS (user 'u', \"password\" 'hunter2')",
		"CREATE USER MAPPING FOR u SERVER s OPTIONS (user 'u', \"password\" '********')"},
	{"C20: E string trailing escaped backslash", "ALTER ROLE app PASSWORD E'hunter2\\\\' LOGIN",
		"ALTER ROLE app PASSWORD '********'"},
	{"C21: E conninfo escaped backslash then quote", "SELECT dblink_connect(E'password=\\'hun\\\\\\\\\\'ter2 x\\' dbname=db')",
		"SELECT dblink_connect(E'password=******** dbname=db')"},
	{"C22: PASSWORD after quote char", "EXECUTE 'ALTER ROLE app'||'PASSWORD ''hunter2'''",
		"EXECUTE 'ALTER ROLE app'||'PASSWORD '********''"},
	{"C23: tab sep in E-string", "EXECUTE E'ALTER ROLE app PASSWORD\\t\\'hunter2\\''",
		"EXECUTE E'ALTER ROLE app PASSWORD\\t'********''"},
	{"C24: alter role password then comment then dollar", "ALTER ROLE app PASSWORD --x\n$q$hunter2$q$",
		"ALTER ROLE app PASSWORD --x\n'********'"},
	{"C26: SET var TO without space via comment", "SET app.password/**/TO/**/'hunter2'",
		"SET app.password/**/TO/**/'********'"},
	{"C27: quoted name value with space before comma in JSON", "{\"stmt\": \"SELECT set_config('app.password', 'hunter2', false)\"}",
		"{\"stmt\": \"SELECT set_config('app.password', '********', false)\"}"},
	{"C28: URI with @ in user via percent", "postgresql://app%40x:hunter2@db.example.com/app",
		"postgresql://app%40x:********@db.example.com/app"},
	{"C29: conninfo keyword uppercase with spaces", "HOST=h PASSWORD = 'hunter2'",
		"HOST=h PASSWORD = ********"},
	{"C30: password value then multiline", "CREATE SUBSCRIPTION s CONNECTION 'host=h\npassword=hunter2\ndbname=d' PUBLICATION p",
		"CREATE SUBSCRIPTION s CONNECTION 'host=h\npassword=********\ndbname=d' PUBLICATION p"},
	{"C31: PASSWORD followed by U& UESCAPE", "ALTER ROLE app PASSWORD U&'hunter2!0032' UESCAPE '!'",
		"ALTER ROLE app PASSWORD '********' UESCAPE '!'"},
	{"C32: SET credential with E", "ALTER SYSTEM SET restore_command = E'sshpass -p hunter2 scp backup:%f %p'",
		"ALTER SYSTEM SET restore_command = '********'"},
	{"C33: ALTER ROLE SET", "ALTER ROLE app SET archive_command = 'sshpass -p hunter2 x'",
		"ALTER ROLE app SET archive_command = '********'"},
	{"C35: em space", "host=h password=hunter2\u2003rest",
		"host=h password=********"},
}

// textRedactionKnownLimits are forms the patterns do not mask, each with
// the text both engines return, so that a change in what they do is
// noticed: format() fills %L from an argument the patterns cannot tie
// to PASSWORD; a value starting with a $1-style parameter is kept, as
// statements normalised by pg_stat_statements hold them; a quoted value
// followed directly by more text, as a shell joins 'abc'secret, is
// masked only to its closing quote; YAML and HTTP header syntax and
// hyphenated names are not matched; a value
// nested in a JSON object under a secret key is masked only to the
// first closing brace; and a no-break space is not white space, as it
// is not to PostgreSQL's own parser.
var textRedactionKnownLimits = []textRedactionCase{
	{"C10: format %L", "DO $$BEGIN EXECUTE format('ALTER ROLE %I PASSWORD %L', 'app', 'hunter2'); END$$",
		"DO $$BEGIN EXECUTE format('ALTER ROLE %I PASSWORD %L', '********', 'hunter2'); END$$"},
	{"C11: dollar digits comma", "host=192.0.2.1 password=$12,hunter2 dbname=d",
		"host=192.0.2.1 password=$12,hunter2 dbname=d"},
	{"C11b: dollar digits end", "host=192.0.2.1 password=$42",
		"host=192.0.2.1 password=$42"},
	{"C17: shell quote suffix", "PGPASSWORD='abc'hunter2 pg_dump db",
		"PGPASSWORD=********hunter2 pg_dump db"},
	{"C17b: shell dq suffix", "PGPASSWORD=\"abc\"hunter2 pg_dump db",
		"PGPASSWORD=********hunter2 pg_dump db"},
	{"C18: yaml colon", "password: hunter2",
		"password: hunter2"},
	{"C19: json nested object", "{\"password\": {\"a\": \"}\", \"b\": \"hunter2\"}}",
		"{\"password\": \"********\"\", \"b\": \"hunter2\"}}"},
	{"C25: secret key with hyphen header", "curl -H 'X-Api-Key: hunter2' https://api.example.com",
		"curl -H 'X-Api-Key: hunter2' https://api.example.com"},
	{"C34: PASSWORD with non-breaking space", "ALTER ROLE app PASSWORD\u00a0'hunter2'",
		"ALTER ROLE app PASSWORD\u00a0'hunter2'"},
}

// jsonWants are the plain-text forms of the masked JSON forms of cases
// whose masked JSON form is not the JSON form of want, keyed by case
// name: in JSON text a backslash escape is doubled, so the patterns
// there mask more than the value, but never less.
var jsonWants = map[string]string{
	// The line break is \n in JSON text, which an unquoted value
	// takes as an escape, so the value runs to the next space.
	"C30: password value then multiline": "CREATE SUBSCRIPTION s CONNECTION 'host=h\npassword=******** PUBLICATION p",
}

// secretMarkers are the secrets the cases hold, other than
// example-secret, which secretLeft finds by its two halves so that
// example.com is not taken for one.
var secretMarkers = []string{"s3cr", "31337", "hunter2", "Secret123", "horse", "battery"}

// secretLeft reports whether s still holds one of the test secrets.
func secretLeft(s string) bool {
	if strings.Contains(s, "example") && strings.Contains(s, "secret") && !strings.Contains(s, "example.com") {
		return true
	}
	for _, m := range secretMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// applyTextRedactionsInGo applies textRedactions with Go's regexp
// package, converting PostgreSQL's \1 to Go's ${1}, after cutting s to
// maxTextLength characters as RedactedText's left() does.
func applyTextRedactionsInGo(t *testing.T, s string) string {
	t.Helper()
	if r := []rune(s); len(r) > maxTextLength {
		s = string(r[:maxTextLength])
	}
	for _, r := range textRedactions {
		// PostgreSQL's . matches a newline and, since every quantifier
		// is greedy, it takes the longest match: s and Longest do the
		// same in Go.
		re, err := regexp.Compile("(?is)" + r.Pattern)
		if err != nil {
			t.Fatalf("%s: %v", r.Name, err)
		}
		re.Longest()
		s = re.ReplaceAllString(s, strings.ReplaceAll(r.Replacement, `\1`, "${1}"))
	}
	return s
}

func TestTextRedactions(t *testing.T) {
	for _, tc := range textRedactionCases {
		t.Run(tc.name, func(t *testing.T) {
			got := applyTextRedactionsInGo(t, tc.in)
			if got != tc.want {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
			if secretLeft(got) {
				t.Errorf("a secret survived: %q", got)
			}
		})
	}
}

// jsonText returns s as a JSON string, as to_jsonb(s)::text writes it
// in a row image: quotes and backslashes are escaped.
func jsonText(t *testing.T, s string) string {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		t.Fatalf("encode %q: %v", s, err)
	}
	return string(bytes.TrimSuffix(b.Bytes(), []byte("\n")))
}

// checkJSONForm checks got, the masked JSON form of tc.in, holds no
// secret and matches the JSON form of tc.want, or of its jsonWants
// entry. A value at the end of the text may also take the closing JSON
// quote, since an unquoted or truncated value is masked to the next
// white space or the end. An input that is already JSON text need only
// hold no secret: escaped twice, its exact form is not one a row image
// holds.
func checkJSONForm(t *testing.T, tc textRedactionCase, got string) {
	t.Helper()
	if secretLeft(got) || strings.Contains(got, "secret a b") {
		t.Errorf("%s: a secret survived in JSON text: %q", tc.name, got)
	}
	if strings.HasPrefix(tc.in, "{") {
		return
	}
	plain, ok := jsonWants[tc.name]
	if !ok {
		plain = tc.want
	}
	want := jsonText(t, plain)
	if got != want && got != strings.TrimSuffix(want, `"`) {
		t.Errorf("%s: JSON form\ngot  %q\nwant %q", tc.name, got, want)
	}
}

// TestTextRedactionsInJSON masks every case as it appears in a row
// image, where every backslash is doubled and every double quote
// escaped.
func TestTextRedactionsInJSON(t *testing.T) {
	for _, tc := range textRedactionCases {
		checkJSONForm(t, tc, applyTextRedactionsInGo(t, jsonText(t, tc.in)))
	}
}

// TestTextRedactionPatterns checks each pattern on its own, so removing
// one fails a test.
func TestTextRedactionPatterns(t *testing.T) {
	samples := map[string]string{
		"URI userinfo":           "postgresql://app:example-secret@192.0.2.1/db",
		"JSON key":               `{"password": "example-secret"}`,
		"SET credential setting": "SET restore_command TO 'example-secret'",
		"quoted name, value":     "('password', 'example-secret')",
		"keyword=value":          "password=example-secret",
		"concatenated PASSWORD":  "PASSWORD ' || '''example-secret'''",
		"PASSWORD 'value'":       "PASSWORD 'example-secret'",
	}
	if len(samples) != len(textRedactions) {
		t.Fatalf("%d samples for %d patterns", len(samples), len(textRedactions))
	}
	for _, r := range textRedactions {
		sample, ok := samples[r.Name]
		if !ok {
			t.Fatalf("no sample for %q", r.Name)
		}
		re := regexp.MustCompile("(?is)" + r.Pattern)
		re.Longest()
		got := re.ReplaceAllString(sample, strings.ReplaceAll(r.Replacement, `\1`, "${1}"))
		if strings.Contains(got, "example-secret") || !strings.Contains(got, RedactedValue) {
			t.Errorf("%s: %q became %q", r.Name, sample, got)
		}
	}
}

func TestRedactedTextSQL(t *testing.T) {
	if got := sqlStringLiteral(`a'b\c`); got != `E'a''b\\c'` {
		t.Errorf("sqlStringLiteral = %s", got)
	}
	// Only group 1 may capture, so the engines cannot differ on which
	// group a replacement names.
	for _, r := range textRedactions {
		if n := regexp.MustCompile(r.Pattern).NumSubexp(); n != 1 {
			t.Errorf("%s has %d capturing groups, want 1", r.Name, n)
		}
	}
	sql := RedactedText(`"t"."query"`)
	if strings.Count(sql, "regexp_replace(") != len(textRedactions) ||
		!strings.Contains(sql, `left("t"."query"::text, 65536)`) ||
		strings.Count(sql, ", 'gi')") != len(textRedactions) {
		t.Errorf("unexpected SQL: %s", sql)
	}
}

// TestTextRedactionKnownLimits pins what the patterns return for the
// forms they do not mask, so a change in what they do is noticed.
func TestTextRedactionKnownLimits(t *testing.T) {
	for _, tc := range textRedactionKnownLimits {
		if got := applyTextRedactionsInGo(t, tc.in); got != tc.want {
			t.Errorf("%s:\ngot  %q\nwant %q", tc.name, got, tc.want)
		}
	}
}

// TestTextRedactionsAdversarialInGo masks 1 MiB inputs built to make a
// regular expression engine backtrack, cut to maxTextLength as
// RedactedText cuts them.
func TestTextRedactionsAdversarialInGo(t *testing.T) {
	for name, in := range adversarialInputs(1 << 20) {
		start := time.Now()
		applyTextRedactionsInGo(t, in)
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Errorf("%s took %s", name, elapsed)
		}
	}
}

func TestTextRedactionsTruncate(t *testing.T) {
	in := "password=example-secret " + strings.Repeat("é", maxTextLength)
	got := applyTextRedactionsInGo(t, in)
	if n := len([]rune(got)); n != maxTextLength-len("example-secret")+len(RedactedValue) {
		t.Errorf("got %d characters", n)
	}
}

// redactTextQuery masks its one parameter with RedactedText. It is
// built once from constants; the text under test is a bound parameter.
var redactTextQuery = "SELECT " + RedactedText("$1::text")

// redactJSONQuery masks its one parameter as a row image holds it, in
// the JSON text of a jsonb string.
var redactJSONQuery = "SELECT " + RedactedText("to_jsonb($1::text)")

// TestRedactedTextIntegration runs RedactedText in PostgreSQL on every
// case, as plain text and as a jsonb row image, so the patterns behave
// there as they do in Go.
func TestRedactedTextIntegration(t *testing.T) {
	pool, cleanup := newLatestRowsTestPool(t)
	defer cleanup()
	for _, tc := range textRedactionCases {
		var got, gotJSON string
		if err := pool.QueryRow(context.Background(), redactTextQuery, tc.in).Scan(&got); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: got  %q\nwant %q", tc.name, got, tc.want)
		}
		if err := pool.QueryRow(context.Background(), redactJSONQuery, tc.in).Scan(&gotJSON); err != nil {
			t.Fatalf("%s as JSON: %v", tc.name, err)
		}
		checkJSONForm(t, tc, gotJSON)
		if inGo := applyTextRedactionsInGo(t, jsonText(t, tc.in)); gotJSON != inGo {
			t.Errorf("%s: JSON form differs between PostgreSQL and Go:\npg %q\ngo %q", tc.name, gotJSON, inGo)
		}
	}
	for _, tc := range textRedactionKnownLimits {
		var got string
		if err := pool.QueryRow(context.Background(), redactTextQuery, tc.in).Scan(&got); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("known limit %s: got  %q\nwant %q", tc.name, got, tc.want)
		}
	}
	var long string
	if err := pool.QueryRow(context.Background(), redactTextQuery, strings.Repeat("é", maxTextLength+10)).Scan(&long); err != nil ||
		len([]rune(long)) != maxTextLength {
		t.Errorf("a long value should be cut to %d characters, got %d (%v)", maxTextLength, len([]rune(long)), err)
	}
	var null *string
	if err := pool.QueryRow(context.Background(), redactTextQuery, nil).Scan(&null); err != nil || null != nil {
		t.Errorf("NULL should stay NULL: %v, %v", null, err)
	}
}

// TestRedactedTextAdversarialIntegration masks inputs of about 1 MiB
// built to make a regular expression engine backtrack, as plain text
// and as a row image, and checks each finishes well within a statement
// timeout.
func TestRedactedTextAdversarialIntegration(t *testing.T) {
	pool, cleanup := newLatestRowsTestPool(t)
	defer cleanup()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET statement_timeout = '30s'"); err != nil {
		t.Fatalf("set statement_timeout: %v", err)
	}
	timed := func(name, form string, run func(*string) error) {
		start := time.Now()
		var got string
		if err := run(&got); err != nil {
			t.Errorf("%s as %s: %v", name, form, err)
			return
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Errorf("%s as %s took %s", name, form, elapsed)
		}
	}
	for name, in := range adversarialInputs(1 << 20) {
		timed(name, "text", func(got *string) error {
			return conn.QueryRow(ctx, redactTextQuery, in).Scan(got)
		})
		timed(name, "JSON", func(got *string) error {
			return conn.QueryRow(ctx, redactJSONQuery, in).Scan(got)
		})
	}
}

// adversarialInputs returns inputs of about size bytes built to make a
// regular expression engine backtrack: runs of quotes, backslashes,
// dollar signs and comments, repeated secret names and long runs of the
// characters that may surround a secret name.
func adversarialInputs(size int) map[string]string {
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
		// Runs of comments after a secret name, which once made every
		// match scan to the end of the text, and comments before
		// another secret name.
		"open comments after equals":   strings.Repeat("password=/* ", size/12),
		"open comments after PASSWORD": strings.Repeat("PASSWORD /* ", size/12),
		"line comments after equals":   strings.Repeat("password=-- ", size/12),
		"comment then name":            strings.Repeat("password /* password='x' ", size/25),
		"line comment then name":       strings.Repeat("password-- password='x' ", size/24),
		"comment then SET":             strings.Repeat("set /* set archive_command='x' ", size/31),
		"comment then quoted name":     strings.Repeat("'password' /* 'password', 'x' ", size/31),
		"comment then unquoted value":  strings.Repeat("password=/* password=x ", size/23),
		"comment then concatenation":   strings.Repeat(`password ' /* password ' || ''\'x'' `, size/36),
	}
}

// TestCredentialSettingLiteralsAreSafe pins the constants that
// credentialSettingCondition writes into SQL as literals.
func TestCredentialSettingLiteralsAreSafe(t *testing.T) {
	safe := regexp.MustCompile(`^[a-z0-9_]+$`)
	for _, v := range slices.Concat(credentialSettings, nonCredentialSettings, secretColumnFragments) {
		if !safe.MatchString(v) {
			t.Errorf("%q is not safe to write as a SQL literal", v)
		}
	}
	safePattern := regexp.MustCompile(`^[a-z_?$]+$`)
	for _, v := range slices.Concat(secretNamePatterns, textSecretNamePatterns) {
		if !safePattern.MatchString(v) {
			t.Errorf("pattern %q is not safe to write as a SQL literal", v)
		}
	}
	if !safe.MatchString(strings.ReplaceAll(RedactedValue, "*", "a")) {
		t.Errorf("RedactedValue %q is not safe to write as a SQL literal", RedactedValue)
	}
	cond := credentialSettingCondition(`t."name"`)
	for _, want := range []string{`lower(t."name") IN ('primary_conninfo', 'ssl_passphrase_command', 'archive_command', 'archive_cleanup_command', 'restore_command', 'recovery_end_command')`, `~ '(password|`, `|pass$|key$|api_?key|access_key|salt)'`,
		`NOT IN ('password_encryption', 'md5_password_warnings', 'ssl_passphrase_command_supports_reload')`} {
		if !strings.Contains(cond, want) {
			t.Errorf("condition %s does not contain %s", cond, want)
		}
	}
}

func TestIsSecretColumnName(t *testing.T) {
	for name, want := range map[string]bool{
		"conninfo":           true,
		"SSLKEY":             true,
		"password_encrypted": true,
		"db_passwd":          true,
		"client_secret":      true,
		"api_token":          true,
		"subconninfo":        true,
		"ssl_passphrase":     true,
		"primary_connstr":    true,
		"connection_string":  true,
		"target_dsn":         true,
		"sslrootcert_path":   true,
		"subscription_name":  false,
		"auto_group_key":     false,
		"xact_commit":        false,
		"datname":            false,
	} {
		if got := IsSecretColumnName(name); got != want {
			t.Errorf("IsSecretColumnName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestSecretNamePatterns(t *testing.T) {
	re := regexp.MustCompile("(" + strings.Join(secretNamePatterns, "|") + ")")
	for name, want := range map[string]bool{
		"ldapbindpasswd":          true,
		"radiussecrets":           true,
		"myext.api_key":           true,
		"myext.apikey":            true,
		"myext.access_key_id":     true,
		"myext.dsn":               true,
		"myext.connection_string": true,
		"myext.salt":              true,
		"myext.pass":              true,
		"ldapserver":              false,
		"work_mem":                false,
		"shared_buffers":          false,
		"ssl_key_file":            false,
	} {
		if got := re.MatchString(name); got != want {
			t.Errorf("%q matches = %v, want %v", name, got, want)
		}
	}
}

func TestProbeColumnMask(t *testing.T) {
	settings := []string{"connection_id", "name", "setting", "unit"}
	for _, tc := range []struct {
		probe, column string
		columns       []string
		want          ColumnMask
	}{
		{"pg_settings", "setting", settings, MaskSetting},
		{"pg_settings", "name", settings, MaskNone},
		{"pg_settings", "setting", []string{"setting", "unit"}, MaskOmit},
		{"pg_hba_file_rules", "options", nil, MaskOptions},
		{"pg_hba_file_rules", "error", nil, MaskWhole},
		{"pg_stat_activity", "query", nil, MaskText},
		{"spock_exception_log", "local_tup", nil, MaskText},
		{"pg_stat_activity", "usename", nil, MaskNone},
		{"pg_stat_wal_receiver", "conninfo", nil, MaskOmit},
		{"any_probe", "api_token", nil, MaskOmit},
		{"any_probe", "setting", nil, MaskNone},
	} {
		if got := ProbeColumnMask(tc.probe, tc.column, tc.columns); got != tc.want {
			t.Errorf("ProbeColumnMask(%s, %s) = %d, want %d", tc.probe, tc.column, got, tc.want)
		}
	}
}

func TestMaskedProbeColumn(t *testing.T) {
	ref := func(c string) string { return `t."` + c + `"` }
	settings := []string{"name", "setting"}
	for _, tc := range []struct {
		probe, column string
		want          string
		ok            bool
	}{
		{"pg_settings", "setting", MaskedSetting(`t."setting"`, `t."name"`), true},
		{"pg_hba_file_rules", "options", MaskedOptions(`t."options"`), true},
		{"pg_hba_file_rules", "error", `CASE WHEN t."error" <> '' THEN '` + RedactedValue + `' ELSE t."error" END`, true},
		{"pg_stat_statements", "query", RedactedText(`t."query"`), true},
		{"pg_stat_statements", "calls", `t."calls"`, true},
		{"any_probe", "sslkey", "", false},
	} {
		got, ok := MaskedProbeColumn(tc.probe, tc.column, settings, ref)
		if got != tc.want || ok != tc.ok {
			t.Errorf("MaskedProbeColumn(%s, %s) = %q, %v; want %q, %v", tc.probe, tc.column, got, ok, tc.want, tc.ok)
		}
	}
	got := ReturnableColumns("pg_settings", []string{"name", "setting", "ssl_passphrase", "unit"})
	if strings.Join(got, ",") != "name,setting,unit" {
		t.Errorf("ReturnableColumns = %v", got)
	}
	if got := ReturnableColumns("pg_settings", []string{"setting", "unit"}); strings.Join(got, ",") != "unit" {
		t.Errorf("a setting without its name column should be left out: %v", got)
	}

	setting := MaskedSetting(`t."setting"`, `t."name"`)
	for _, want := range []string{`CASE WHEN (lower(t."name") IN (`, `AND t."setting" <> '' THEN '` + RedactedValue +
		`' ELSE t."setting" END`} {
		if !strings.Contains(setting, want) {
			t.Errorf("MaskedSetting %s does not contain %s", setting, want)
		}
	}
}

// TestBuildLatestRowsQueryMasks checks the latest-rows query masks in its
// inner query, so DISTINCT ON and the sort see the masked value, and
// selects a column that should have been left out as NULL.
func TestBuildLatestRowsQueryMasks(t *testing.T) {
	query, _ := buildLatestRowsQuery("pg_settings", []string{"name", "setting", "sslkey"},
		map[string]string{"name": "text", "setting": "text", "sslkey": "text"},
		[]int{1}, MetricFilters{}, "setting", "asc", 10)
	for _, want := range []string{
		`SELECT "name", "setting", "sslkey"` + "\n",
		MaskedSetting(`"setting"`, `"name"`) + ` AS "setting", NULL AS "sslkey", connection_id, collected_at`,
		`DISTINCT ON (connection_id, "name", "setting", "sslkey")`,
	} {
		if !strings.Contains(query, want) {
			t.Errorf("query does not contain %s:\n%s", want, query)
		}
	}
}
