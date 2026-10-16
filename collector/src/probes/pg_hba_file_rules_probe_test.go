/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package probes

import (
	"reflect"
	"strings"
	"testing"
)

func TestPgHbaFileRulesProbe_GetName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgHbaFileRules,
	}
	probe := NewPgHbaFileRulesProbe(config)

	if probe.GetName() != ProbeNamePgHbaFileRules {
		t.Errorf("GetName() = %v, want %v", probe.GetName(), ProbeNamePgHbaFileRules)
	}
}

func TestPgHbaFileRulesProbe_GetTableName(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgHbaFileRules,
	}
	probe := NewPgHbaFileRulesProbe(config)

	if probe.GetTableName() != ProbeNamePgHbaFileRules {
		t.Errorf("GetTableName() = %v, want %v", probe.GetTableName(), ProbeNamePgHbaFileRules)
	}
}

func TestPgHbaFileRulesProbe_IsDatabaseScoped(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgHbaFileRules,
	}
	probe := NewPgHbaFileRulesProbe(config)

	if probe.IsDatabaseScoped() {
		t.Error("IsDatabaseScoped() = true, want false (pg_hba_file_rules is server-scoped)")
	}
}

func TestPgHbaFileRulesProbe_GetQuery(t *testing.T) {
	config := &ProbeConfig{
		Name: ProbeNamePgHbaFileRules,
	}
	probe := NewPgHbaFileRulesProbe(config)

	query := probe.GetQuery()
	if query == "" {
		t.Error("GetQuery() returned empty string")
	}

	// Verify the query includes key columns from pg_hba_file_rules
	expectedColumns := []string{
		"rule_number",
		"file_name",
		"line_number",
		"type",
		"database",
		"user_name",
		"address",
		"netmask",
		"auth_method",
		"options",
		"error",
	}

	for _, column := range expectedColumns {
		if !containsColumn(query, column) {
			t.Errorf("GetQuery() missing expected column: %s", column)
		}
	}
}

func TestPgHbaFileRulesProbe_ComputeMetricsHash(t *testing.T) {
	// Test with identical metrics
	metrics1 := []map[string]any{
		{
			"rule_number": 1,
			"auth_method": "trust",
			"database":    []string{"all"},
		},
		{
			"rule_number": 2,
			"auth_method": "md5",
			"database":    []string{"postgres"},
		},
	}

	metrics2 := []map[string]any{
		{
			"rule_number": 1,
			"auth_method": "trust",
			"database":    []string{"all"},
		},
		{
			"rule_number": 2,
			"auth_method": "md5",
			"database":    []string{"postgres"},
		},
	}

	hash1, err := ComputeMetricsHash(metrics1)
	if err != nil {
		t.Fatalf("ComputeMetricsHash(metrics1) error = %v", err)
	}

	hash2, err := ComputeMetricsHash(metrics2)
	if err != nil {
		t.Fatalf("ComputeMetricsHash(metrics2) error = %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("Identical metrics produced different hashes: %s vs %s", hash1, hash2)
	}

	// Test with different metrics
	metrics3 := []map[string]any{
		{
			"rule_number": 1,
			"auth_method": "scram-sha-256", // Different value
			"database":    []string{"all"},
		},
		{
			"rule_number": 2,
			"auth_method": "md5",
			"database":    []string{"postgres"},
		},
	}

	hash3, err := ComputeMetricsHash(metrics3)
	if err != nil {
		t.Fatalf("ComputeMetricsHash(metrics3) error = %v", err)
	}

	if hash1 == hash3 {
		t.Errorf("Different metrics produced same hash: %s", hash1)
	}
}

func TestPgHbaFileRulesProbe_ComputeMetricsHash_EmptyMetrics(t *testing.T) {
	metrics := []map[string]any{}

	hash, err := ComputeMetricsHash(metrics)
	if err != nil {
		t.Fatalf("ComputeMetricsHash(empty) error = %v", err)
	}

	if hash == "" {
		t.Error("ComputeMetricsHash(empty) returned empty hash")
	}
}

func TestPgHbaFileRulesProbe_GetConfig(t *testing.T) {
	config := &ProbeConfig{
		Name:                      ProbeNamePgHbaFileRules,
		Description:               "Test probe",
		CollectionIntervalSeconds: 3600,
		RetentionDays:             365,
		IsEnabled:                 true,
	}
	probe := NewPgHbaFileRulesProbe(config)

	returnedConfig := probe.GetConfig()
	if returnedConfig != config {
		t.Error("GetConfig() did not return the same config instance")
	}

	if returnedConfig.Name != ProbeNamePgHbaFileRules {
		t.Errorf("GetConfig().Name = %v, want %v", returnedConfig.Name, ProbeNamePgHbaFileRules)
	}

	if returnedConfig.CollectionIntervalSeconds != 3600 {
		t.Errorf("GetConfig().CollectionIntervalSeconds = %v, want 3600", returnedConfig.CollectionIntervalSeconds)
	}

	if returnedConfig.RetentionDays != 365 {
		t.Errorf("GetConfig().RetentionDays = %v, want 365", returnedConfig.RetentionDays)
	}
}

func TestMaskHBAError(t *testing.T) {
	// The messages are PostgreSQL 18's, as pg_hba_file_rules reports
	// them for lines it cannot parse.
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "unquoted token after a colon",
			in:   "authentication option not in name=value format: ldapbindpasswdS3cret",
			want: "authentication option not in name=value format: ********",
		},
		{
			name: "quoted token",
			in:   `invalid IP address "ldapbindpasswd=S3cret": Name or service not known`,
			want: `invalid IP address "********"`,
		},
		{
			name: "quoted token holding a quote",
			in:   `invalid connection type "a"S3cret"`,
			want: `invalid connection type "********"`,
		},
		{
			name: "several quoted names",
			in:   `authentication method "ldap" requires argument "ldapbasedn", "ldapprefix", or "ldapsuffix" to be set`,
			want: `authentication method "********"`,
		},
		{
			name: "message quoting nothing is kept",
			in:   "end-of-line before IP address specification",
			want: "end-of-line before IP address specification",
		},
		{
			name: "numbers are kept",
			in:   "the number of RADIUS secrets (2) must be 1 or the same as the number of RADIUS servers (3)",
			want: "the number of RADIUS secrets (2) must be 1 or the same as the number of RADIUS servers (3)",
		},
		{
			name: "errors from several included files",
			in: "could not open file \"/etc/postgresql/a.conf\": No such file or directory\n" +
				"authentication option not in name=value format: radiussecretsS3cret",
			want: "could not open file \"********\"\n" +
				"authentication option not in name=value format: ********",
		},
		{
			name: "empty",
			in:   "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskHBAError(tt.in)
			if got != tt.want {
				t.Errorf("maskHBAError(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if again := maskHBAError(got); again != got {
				t.Errorf("masking twice gave %q, want %q", again, got)
			}
		})
	}
}

func TestMaskHBASecrets(t *testing.T) {
	t.Run("no errors returns the input", func(t *testing.T) {
		in := []map[string]any{
			{"rule_number": int32(1), "error": nil},
			{"rule_number": int32(2), "error": ""},
		}
		out := maskHBASecrets(in)
		if &out[0] != &in[0] {
			t.Error("expected the input slice back when nothing is masked")
		}
	})

	t.Run("nil input", func(t *testing.T) {
		if out := maskHBASecrets(nil); out != nil {
			t.Errorf("maskHBASecrets(nil) = %v, want nil", out)
		}
	})

	t.Run("masks a copy", func(t *testing.T) {
		valid := map[string]any{"rule_number": int32(1), "error": nil}
		broken := map[string]any{
			"rule_number": int32(-1),
			"line_number": int32(3),
			"error":       "authentication option not in name=value format: ldapbindpasswdS3cret",
		}
		in := []map[string]any{valid, broken}
		out := maskHBASecrets(in)

		if len(out) != 2 {
			t.Fatalf("got %d rows, want 2", len(out))
		}
		if got := out[1]["error"]; got != "authentication option not in name=value format: ********" {
			t.Errorf("masked error = %v", got)
		}
		if out[1]["line_number"] != int32(3) || out[1]["rule_number"] != int32(-1) {
			t.Errorf("other columns changed: %v", out[1])
		}
		if msg, ok := broken["error"].(string); !ok || !strings.Contains(msg, "S3cret") {
			t.Error("the caller's row was changed")
		}
		if in[1]["error"] != broken["error"] {
			t.Error("the caller's slice was changed")
		}
		if out[0]["rule_number"] != int32(1) {
			t.Errorf("valid row changed: %v", out[0])
		}
	})

	t.Run("masks secret options in a copy", func(t *testing.T) {
		ldap := map[string]any{
			"rule_number": int32(1),
			"auth_method": "ldap",
			"options": []any{"ldapserver=ldap.example.com",
				"ldapbinddn=cn=reader", "ldapbindpasswd=S3cret"},
		}
		radius := map[string]any{
			"rule_number": int32(2),
			"auth_method": "radius",
			"options": []any{"radiusservers=radius.example.com",
				"radiussecrets=S3cret,0ther"},
		}
		in := []map[string]any{ldap, radius}
		out := maskHBASecrets(in)

		wantLDAP := []any{"ldapserver=ldap.example.com",
			"ldapbinddn=cn=reader", "ldapbindpasswd=********"}
		if !reflect.DeepEqual(out[0]["options"], wantLDAP) {
			t.Errorf("LDAP options = %v, want %v", out[0]["options"], wantLDAP)
		}
		wantRADIUS := []any{"radiusservers=radius.example.com",
			"radiussecrets=********"}
		if !reflect.DeepEqual(out[1]["options"], wantRADIUS) {
			t.Errorf("RADIUS options = %v, want %v", out[1]["options"], wantRADIUS)
		}
		if out[0]["error"] != nil {
			t.Errorf("error = %v, want it absent", out[0]["error"])
		}
		if opts, ok := ldap["options"].([]any); !ok || opts[2] != "ldapbindpasswd=S3cret" {
			t.Error("the caller's options were changed")
		}
	})
}

func TestMaskHBAOptions(t *testing.T) {
	tests := []struct {
		name        string
		in          any
		want        any
		wantChanged bool
	}{
		{
			name:        "ldapbindpasswd",
			in:          []any{"ldapserver=ldap.example.com", "ldapbindpasswd=S3cret"},
			want:        []any{"ldapserver=ldap.example.com", "ldapbindpasswd=********"},
			wantChanged: true,
		},
		{
			name:        "radiussecrets holding an equals sign",
			in:          []any{"radiussecrets=a=b,S3cret"},
			want:        []any{"radiussecrets=********"},
			wantChanged: true,
		},
		{
			name:        "string slice",
			in:          []string{"radiusservers=radius.example.com", "radiussecrets=S3cret"},
			want:        []string{"radiusservers=radius.example.com", "radiussecrets=********"},
			wantChanged: true,
		},
		{
			name: "already masked",
			in:   []any{"ldapbindpasswd=********"},
			want: []any{"ldapbindpasswd=********"},
		},
		{
			name: "already masked string slice",
			in:   []string{"radiussecrets=********"},
			want: []string{"radiussecrets=********"},
		},
		{
			name: "no secret options",
			in:   []any{"map=office", "include_realm=false", nil, int32(3)},
			want: []any{"map=office", "include_realm=false", nil, int32(3)},
		},
		{
			name: "name without a value is kept",
			in:   []any{"ldapbindpasswd"},
			want: []any{"ldapbindpasswd"},
		},
		{
			name: "nil",
			in:   nil,
			want: nil,
		},
		{
			name: "not an array",
			in:   "ldapbindpasswd=S3cret",
			want: "ldapbindpasswd=S3cret",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := maskHBAOptions(tt.in)
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tt.wantChanged)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("maskHBAOptions(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
