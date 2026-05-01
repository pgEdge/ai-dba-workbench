/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package database

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestIsSecretSet verifies the helper that drives the `*Set` boolean
// flags exposed to API clients. The helper must distinguish three
// states: nil pointer (not set), pointer to empty string (not set),
// and pointer to a non-empty value (set).
func TestIsSecretSet(t *testing.T) {
	empty := ""
	value := "hunter2"

	tests := []struct {
		name string
		in   *string
		want bool
	}{
		{"nil pointer is not set", nil, false},
		{"empty string is not set", &empty, false},
		{"non-empty string is set", &value, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSecretSet(tt.in); got != tt.want {
				t.Errorf("isSecretSet(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestDecryptNotificationChannelSecrets_FlagsOnly exercises the flag
// computation path of decryptNotificationChannelSecrets. The datastore
// is built with an empty server secret so the helper's decryption path
// is a no-op; that lets us assert just the `*Set` flag behavior for
// every combination of nil, empty, and populated inputs.
func TestDecryptNotificationChannelSecrets_FlagsOnly(t *testing.T) {
	d := &Datastore{} // empty serverSecret -> decrypt is a no-op
	empty := ""
	webhook := "https://hooks.example.com/abcd"
	authCreds := "Bearer secret-token"
	smtpUser := "mailer@example.com"
	smtpPass := "p@ssw0rd"

	tests := []struct {
		name    string
		channel NotificationChannel
		want    NotificationChannel
	}{
		{
			name:    "all secrets nil",
			channel: NotificationChannel{},
			want:    NotificationChannel{},
		},
		{
			name: "all secrets empty strings",
			channel: NotificationChannel{
				WebhookURL:      &empty,
				AuthCredentials: &empty,
				SMTPUsername:    &empty,
				SMTPPassword:    &empty,
			},
			want: NotificationChannel{
				WebhookURL:      &empty,
				AuthCredentials: &empty,
				SMTPUsername:    &empty,
				SMTPPassword:    &empty,
			},
		},
		{
			name: "all secrets populated",
			channel: NotificationChannel{
				WebhookURL:      &webhook,
				AuthCredentials: &authCreds,
				SMTPUsername:    &smtpUser,
				SMTPPassword:    &smtpPass,
			},
			want: NotificationChannel{
				WebhookURL:         &webhook,
				WebhookURLSet:      true,
				AuthCredentials:    &authCreds,
				AuthCredentialsSet: true,
				SMTPUsername:       &smtpUser,
				SMTPUsernameSet:    true,
				SMTPPassword:       &smtpPass,
				SMTPPasswordSet:    true,
			},
		},
		{
			name: "only smtp_username set",
			channel: NotificationChannel{
				SMTPUsername: &smtpUser,
			},
			want: NotificationChannel{
				SMTPUsername:    &smtpUser,
				SMTPUsernameSet: true,
			},
		},
		{
			name: "only webhook url set",
			channel: NotificationChannel{
				WebhookURL: &webhook,
			},
			want: NotificationChannel{
				WebhookURL:    &webhook,
				WebhookURLSet: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.channel
			d.decryptNotificationChannelSecrets(&c)
			if c.WebhookURLSet != tt.want.WebhookURLSet {
				t.Errorf("WebhookURLSet = %v, want %v", c.WebhookURLSet, tt.want.WebhookURLSet)
			}
			if c.AuthCredentialsSet != tt.want.AuthCredentialsSet {
				t.Errorf("AuthCredentialsSet = %v, want %v",
					c.AuthCredentialsSet, tt.want.AuthCredentialsSet)
			}
			if c.SMTPUsernameSet != tt.want.SMTPUsernameSet {
				t.Errorf("SMTPUsernameSet = %v, want %v",
					c.SMTPUsernameSet, tt.want.SMTPUsernameSet)
			}
			if c.SMTPPasswordSet != tt.want.SMTPPasswordSet {
				t.Errorf("SMTPPasswordSet = %v, want %v",
					c.SMTPPasswordSet, tt.want.SMTPPasswordSet)
			}
		})
	}
}

// TestNotificationChannel_SecretsNeverSerialized is a guard-rail test
// for issue #187. It encodes a fully-populated channel as JSON and
// asserts that none of the four redacted fields appear in the wire
// representation. If a future refactor reintroduces a serialized
// secret, this test fails loudly.
func TestNotificationChannel_SecretsNeverSerialized(t *testing.T) {
	webhook := "https://hooks.slack.com/should-not-leak"
	authCreds := "Bearer should-not-leak"
	smtpUser := "leak@example.com"
	smtpPass := "leaky-password"

	ch := NotificationChannel{
		ID:                 1,
		ChannelType:        ChannelTypeEmail,
		Name:               "secrets",
		HTTPMethod:         "POST",
		WebhookURL:         &webhook,
		WebhookURLSet:      true,
		AuthCredentials:    &authCreds,
		AuthCredentialsSet: true,
		SMTPUsername:       &smtpUser,
		SMTPUsernameSet:    true,
		SMTPPassword:       &smtpPass,
		SMTPPasswordSet:    true,
	}

	data, err := json.Marshal(ch)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	body := string(data)

	// The literal secret values must not appear anywhere.
	for _, leaked := range []string{webhook, authCreds, smtpUser, smtpPass} {
		if strings.Contains(body, leaked) {
			t.Errorf("secret value %q leaked in JSON: %s", leaked, body)
		}
	}

	// The redacted JSON keys must not appear, but the indicator keys
	// must be present.
	for _, redactedKey := range []string{
		`"webhook_url"`,
		`"auth_credentials"`,
		`"smtp_username"`,
		`"smtp_password"`,
	} {
		if strings.Contains(body, redactedKey) {
			t.Errorf("redacted key %s appeared in JSON: %s", redactedKey, body)
		}
	}
	for _, indicatorKey := range []string{
		`"webhook_url_set":true`,
		`"auth_credentials_set":true`,
		`"smtp_username_set":true`,
		`"smtp_password_set":true`,
	} {
		if !strings.Contains(body, indicatorKey) {
			t.Errorf("indicator %s missing from JSON: %s", indicatorKey, body)
		}
	}
}

// TestNotificationChannel_SetFlagsFalseInJSON makes sure the indicator
// flags are still emitted (as `false`) when no secret is configured.
// The UI relies on the field always being present so that `undefined`
// vs `false` does not become a third state in the client model.
func TestNotificationChannel_SetFlagsFalseInJSON(t *testing.T) {
	ch := NotificationChannel{
		ID:          2,
		ChannelType: ChannelTypeSlack,
		Name:        "no-secrets",
		HTTPMethod:  "POST",
	}

	data, err := json.Marshal(ch)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	body := string(data)
	for _, want := range []string{
		`"webhook_url_set":false`,
		`"auth_credentials_set":false`,
		`"smtp_username_set":false`,
		`"smtp_password_set":false`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %s in JSON, got: %s", want, body)
		}
	}
}
