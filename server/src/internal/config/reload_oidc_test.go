/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package config

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// oidcEnabledConfig returns the default configuration with federated
// login switched on or off.
func oidcEnabledConfig(enabled bool) *Config {
	cfg := defaultConfig()
	cfg.HTTP.Auth.OIDC.Enabled = boolPtr(enabled)
	return cfg
}

// TestReloadReportsTheOIDCEnabledSwitch covers the one OIDC setting whose
// reload behavior depends on how the server started: switching
// federated login off always applies, since the handler checks it on
// every request, but switching it on applies only when a provider was
// discovered at start-up, and is then warned about since it widens who
// may sign in. The restart warning is judged against start-up, so it
// repeats on every reload until a restart.
func TestReloadReportsTheOIDCEnabledSwitch(t *testing.T) {
	const (
		applied    = "NOTE: http.auth.oidc.enabled changed - applied"
		restart    = "WARNING: http.auth.oidc.enabled changed - requires restart"
		switchedOn = "WARNING: http.auth.oidc.enabled switched on"
	)

	cases := map[string]struct {
		startup *Config
		old     *Config
		reload  *Config
		want    string
		notWant string
	}{
		"switched off after starting on": {
			startup: oidcEnabledConfig(true),
			old:     oidcEnabledConfig(true),
			reload:  oidcEnabledConfig(false),
			want:    applied,
			notWant: restart,
		},
		"switched back on after starting on": {
			startup: oidcEnabledConfig(true),
			old:     oidcEnabledConfig(false),
			reload:  oidcEnabledConfig(true),
			want:    applied,
			notWant: restart,
		},
		"switched back on warns": {
			startup: oidcEnabledConfig(true),
			old:     oidcEnabledConfig(false),
			reload:  oidcEnabledConfig(true),
			want:    switchedOn,
			notWant: restart,
		},
		"switched off does not warn": {
			startup: oidcEnabledConfig(true),
			old:     oidcEnabledConfig(true),
			reload:  oidcEnabledConfig(false),
			want:    applied,
			notWant: switchedOn,
		},
		"still on after starting off": {
			startup: oidcEnabledConfig(false),
			old:     oidcEnabledConfig(true),
			reload:  oidcEnabledConfig(true),
			want:    restart,
			notWant: switchedOn,
		},
		"switched on after starting off": {
			startup: oidcEnabledConfig(false),
			old:     oidcEnabledConfig(false),
			reload:  oidcEnabledConfig(true),
			want:    restart,
			notWant: applied,
		},
		"switched on with no start-up record": {
			old:     oidcEnabledConfig(false),
			reload:  oidcEnabledConfig(true),
			want:    restart,
			notWant: applied,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rc := &ReloadableConfig{config: tc.old, startup: tc.startup}
			out := captureStderr(t, func() {
				rc.logRestartRequiredSettings(tc.reload)
			})
			if !strings.Contains(out, tc.want) {
				t.Errorf("expected stderr to contain %q, got:\n%s", tc.want, out)
			}
			if strings.Contains(out, tc.notWant) {
				t.Errorf("stderr must not contain %q, got:\n%s", tc.notWant, out)
			}
		})
	}
}

// TestReloadReportsNoOIDCChangeWhenNothingChanged guards against a
// reload of an unchanged file announcing OIDC changes it did not make.
func TestReloadReportsNoOIDCChangeWhenNothingChanged(t *testing.T) {
	rc := NewReloadableConfig(oidcEnabledConfig(true), "", CLIFlags{})
	out := captureStderr(t, func() {
		rc.logRestartRequiredSettings(oidcEnabledConfig(true))
	})
	if strings.Contains(out, "http.auth.oidc") {
		t.Errorf("an unchanged OIDC block was reported as changed:\n%s", out)
	}
}

// federatedLoginYAML is a TLS configuration with federated and local
// login set to the given values.
func federatedLoginYAML(oidcEnabled, localEnabled string) string {
	return `
http:
  tls:
    enabled: true
    cert_file: /nonexistent/cert.pem
    key_file: /nonexistent/key.pem
  auth:
    local:
      enabled: ` + localEnabled + `
    oidc:
      enabled: ` + oidcEnabled + `
      issuer: https://idp.example.com
      client_id: workbench
      client_secret: s3cret
      redirect_url: https://workbench.example.com/api/v1/auth/oidc/callback
`
}

// TestReloadRefusesToSwitchOffTheOnlyLoginMethod checks that a server
// started with local login off cannot be left with no way in by a
// reload switching federated login off. The new file switching local
// login back on does not help, because that needs a restart.
func TestReloadRefusesToSwitchOffTheOnlyLoginMethod(t *testing.T) {
	path := writeTempConfig(t, federatedLoginYAML("true", "false"))
	flags := CLIFlags{ConfigFileSet: true, ConfigFile: path}
	initial, err := LoadConfig(path, flags)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	rc := NewReloadableConfig(initial, path, flags)

	if err := os.WriteFile(path, []byte(federatedLoginYAML("false", "true")), 0o600); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	err = rc.Reload()
	if err == nil || !strings.Contains(err.Error(), "http.auth.oidc.enabled cannot be switched off") {
		t.Fatalf("Reload() = %v, want a refusal to switch federated login off", err)
	}
	if rc.Get() != initial {
		t.Error("Reload() swapped the configuration despite refusing it")
	}
}

// TestCheckALoginMethodSurvives covers the cases the reload check lets
// through as well as the one it refuses.
func TestCheckALoginMethodSurvives(t *testing.T) {
	localOff := func(oidcOn bool) *Config {
		cfg := oidcEnabledConfig(oidcOn)
		cfg.HTTP.Auth.Local.Enabled = boolPtr(false)
		return cfg
	}
	cases := map[string]struct {
		startup *Config
		reload  *Config
		refuse  bool
	}{
		"local login on at start-up":     {startup: oidcEnabledConfig(true), reload: oidcEnabledConfig(false)},
		"federated login stays on":       {startup: localOff(true), reload: localOff(true)},
		"no start-up record":             {reload: oidcEnabledConfig(false)},
		"federated login off, local off": {startup: localOff(true), reload: oidcEnabledConfig(false), refuse: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rc := &ReloadableConfig{config: tc.startup, startup: tc.startup}
			err := rc.checkALoginMethodSurvives(tc.reload)
			if (err != nil) != tc.refuse {
				t.Fatalf("checkALoginMethodSurvives() = %v, want refusal %v", err, tc.refuse)
			}
		})
	}
}

// TestReloadWarnsWhenAPolicyChangeWidensAccess checks that the policy
// changes which let more people in, or give them more, are flagged as
// warnings on top of the note saying they applied.
func TestReloadWarnsWhenAPolicyChangeWidensAccess(t *testing.T) {
	old := oidcEnabledConfig(true)
	old.HTTP.Auth.OIDC.ProvisionUsers = boolPtr(false)
	old.HTTP.Auth.OIDC.AllowedEmailDomains = []string{"example.com"}
	old.HTTP.Auth.OIDC.GroupMap = map[string]string{
		"idp-eng": "engineers", "idp-ops": "operators", "idp-ops-2": "operators",
	}

	cur := oidcEnabledConfig(true)
	cur.HTTP.Auth.OIDC.ProvisionUsers = boolPtr(true)
	cur.HTTP.Auth.OIDC.SuperuserGroup = "idp-admins"
	cur.HTTP.Auth.OIDC.GroupMap = map[string]string{"idp-eng": "engineers"}

	rc := &ReloadableConfig{config: old, startup: old}
	out := captureStderr(t, func() { rc.logRestartRequiredSettings(cur) })
	for _, want := range []string{
		"WARNING: http.auth.oidc.provision_users switched on",
		"WARNING: http.auth.oidc.allowed_email_domains emptied",
		"WARNING: http.auth.oidc.superuser_group set: members of idp-admins",
		"WARNING: http.auth.oidc.group_map no longer manages Workbench group operators",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected stderr to contain %q, got:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "Workbench group operators"); n != 1 {
		t.Errorf("operators reported %d times, want once:\n%s", n, out)
	}
}

// TestReloadWarnsWhenTheSuperuserGroupIsCleared checks the warning that
// clearing superuser_group freezes superuser status rather than
// revoking it, and that narrowing changes raise no widening warning.
func TestReloadWarnsWhenTheSuperuserGroupIsCleared(t *testing.T) {
	old := oidcEnabledConfig(true)
	old.HTTP.Auth.OIDC.SuperuserGroup = "idp-admins"
	old.HTTP.Auth.OIDC.ProvisionUsers = boolPtr(true)

	cur := oidcEnabledConfig(true)
	cur.HTTP.Auth.OIDC.ProvisionUsers = boolPtr(false)
	cur.HTTP.Auth.OIDC.AllowedEmailDomains = []string{"example.com"}

	rc := &ReloadableConfig{config: old, startup: old}
	out := captureStderr(t, func() { rc.logRestartRequiredSettings(cur) })
	if !strings.Contains(out, "WARNING: http.auth.oidc.superuser_group cleared") {
		t.Errorf("expected a warning about clearing superuser_group, got:\n%s", out)
	}
	for _, notWant := range []string{"switched on", "emptied"} {
		if strings.Contains(out, notWant) {
			t.Errorf("a narrowing reload warned %q:\n%s", notWant, out)
		}
	}
}

// TestUnmanagedWorkbenchGroups covers the comparison behind the
// group_map warning.
func TestUnmanagedWorkbenchGroups(t *testing.T) {
	cases := map[string]struct {
		old, cur map[string]string
		want     []string
	}{
		"nothing dropped":         {old: map[string]string{"a": "x"}, cur: map[string]string{"b": "x"}},
		"group dropped":           {old: map[string]string{"a": "x", "b": "y"}, cur: map[string]string{"a": "x"}, want: []string{"y"}},
		"sorted and deduplicated": {old: map[string]string{"a": "z", "b": "y", "c": "z"}, want: []string{"y", "z"}},
		"empty target ignored":    {old: map[string]string{"a": "", "b": "x"}, cur: map[string]string{"b": "x"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := unmanagedWorkbenchGroups(tc.old, tc.cur); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("unmanagedWorkbenchGroups() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestReloadWarnsWhenAPolicyChangeWidensAnExistingSetting covers the
// widening changes that leave the setting non-empty: retargeting
// superuser_group, adding an email domain to a list that already
// restricts sign-in, and mapping a provider group to a Workbench group
// it was not mapped to.
func TestReloadWarnsWhenAPolicyChangeWidensAnExistingSetting(t *testing.T) {
	old := oidcEnabledConfig(true)
	old.HTTP.Auth.OIDC.SuperuserGroup = "idp-admins"
	old.HTTP.Auth.OIDC.AllowedEmailDomains = []string{"example.com"}
	old.HTTP.Auth.OIDC.GroupMap = map[string]string{"idp-eng": "engineers"}

	cur := oidcEnabledConfig(true)
	cur.HTTP.Auth.OIDC.SuperuserGroup = "idp-contractors"
	cur.HTTP.Auth.OIDC.AllowedEmailDomains = []string{"Example.com", "@other.example"}
	cur.HTTP.Auth.OIDC.GroupMap = map[string]string{
		"idp-eng": "operators", "idp-ops": "operators",
	}

	rc := &ReloadableConfig{config: old, startup: old}
	out := captureStderr(t, func() { rc.logRestartRequiredSettings(cur) })
	for _, want := range []string{
		"WARNING: http.auth.oidc.superuser_group set: members of idp-contractors",
		"WARNING: http.auth.oidc.allowed_email_domains widened: identities from other.example may now sign in",
		"WARNING: http.auth.oidc.group_map maps provider group idp-eng to Workbench group operators",
		"WARNING: http.auth.oidc.group_map maps provider group idp-ops to Workbench group operators",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected stderr to contain %q, got:\n%s", want, out)
		}
	}
}

// TestSuperuserGroupWarning covers each superuser_group transition.
func TestSuperuserGroupWarning(t *testing.T) {
	cases := map[string]struct {
		old, cur string
		want     string
	}{
		"unchanged":  {old: "a", cur: "a"},
		"both empty": {},
		"set":        {cur: "b", want: "superuser_group set: members of b"},
		"retargeted": {old: "a", cur: "b", want: "superuser_group set: members of b"},
		"cleared":    {old: "a", want: "superuser_group cleared"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := superuserGroupWarning(tc.old, tc.cur)
			if tc.want == "" && got != "" {
				t.Errorf("superuserGroupWarning(%q, %q) = %q, want none", tc.old, tc.cur, got)
			}
			if !strings.HasPrefix(got, tc.want) {
				t.Errorf("superuserGroupWarning(%q, %q) = %q, want prefix %q", tc.old, tc.cur, got, tc.want)
			}
		})
	}
}

// TestAddedEmailDomains covers the comparison behind the
// allowed_email_domains widening warning.
func TestAddedEmailDomains(t *testing.T) {
	cases := map[string]struct {
		old, cur []string
		want     []string
	}{
		"no restriction before":  {cur: []string{"example.com"}},
		"emptied":                {old: []string{"example.com"}},
		"narrowed":               {old: []string{"a.example", "b.example"}, cur: []string{"a.example"}},
		"same domain respelled":  {old: []string{"example.com"}, cur: []string{" @EXAMPLE.com"}},
		"added, sorted, deduped": {old: []string{"a.example"}, cur: []string{"c.example", "a.example", "B.example", "b.example", "@"}, want: []string{"b.example", "c.example"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := addedEmailDomains(tc.old, tc.cur); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("addedEmailDomains() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAddedGroupMappings covers the comparison behind the group_map
// widening warning.
func TestAddedGroupMappings(t *testing.T) {
	cases := map[string]struct {
		old, cur map[string]string
		want     []string
	}{
		"unchanged":            {old: map[string]string{"a": "x"}, cur: map[string]string{"a": "x"}},
		"removed":              {old: map[string]string{"a": "x"}},
		"empty target ignored": {cur: map[string]string{"a": ""}},
		"added and retargeted, sorted": {
			old: map[string]string{"b": "x"},
			cur: map[string]string{"b": "y", "a": "x"},
			want: []string{
				"maps provider group a to Workbench group x: its members join that group at their next login",
				"maps provider group b to Workbench group y: its members join that group at their next login",
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := addedGroupMappings(tc.old, tc.cur); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("addedGroupMappings() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestReloadComparesProviderSettingsWithStartup checks that a provider
// setting changed by one reload is still reported as needing a restart
// by the next, since the provider is still built from the start-up
// value, and that restoring the start-up value stops the warning.
func TestReloadComparesProviderSettingsWithStartup(t *testing.T) {
	startup := oidcEnabledConfig(true)
	startup.HTTP.Auth.OIDC.Issuer = "https://idp.example.com"
	moved := oidcEnabledConfig(true)
	moved.HTTP.Auth.OIDC.Issuer = "https://other-idp.example.com"

	rc := &ReloadableConfig{config: moved, startup: startup}
	out := captureStderr(t, func() { rc.logRestartRequiredSettings(moved) })
	if !strings.Contains(out, "WARNING: http.auth.oidc.issuer changed - requires restart") {
		t.Errorf("a second reload with the moved issuer did not warn, got:\n%s", out)
	}

	restored := oidcEnabledConfig(true)
	restored.HTTP.Auth.OIDC.Issuer = startup.HTTP.Auth.OIDC.Issuer
	out = captureStderr(t, func() { rc.logRestartRequiredSettings(restored) })
	if strings.Contains(out, "issuer") {
		t.Errorf("restoring the start-up issuer still warned, got:\n%s", out)
	}
}
