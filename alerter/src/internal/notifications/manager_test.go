/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package notifications

import (
	"context"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

func TestManager_GetBackoffMinutes(t *testing.T) {
	tests := []struct {
		name            string
		backoffConfig   []int
		attempt         int
		expectedBackoff int
	}{
		{
			name:            "first attempt with defaults",
			backoffConfig:   nil, // Uses defaults: [5, 15, 60]
			attempt:         1,
			expectedBackoff: 5,
		},
		{
			name:            "second attempt with defaults",
			backoffConfig:   nil,
			attempt:         2,
			expectedBackoff: 15,
		},
		{
			name:            "third attempt with defaults",
			backoffConfig:   nil,
			attempt:         3,
			expectedBackoff: 60,
		},
		{
			name:            "beyond max attempts uses last value",
			backoffConfig:   nil,
			attempt:         10,
			expectedBackoff: 60,
		},
		{
			name:            "custom backoff config",
			backoffConfig:   []int{1, 5, 30, 120},
			attempt:         1,
			expectedBackoff: 1,
		},
		{
			name:            "custom backoff - second",
			backoffConfig:   []int{1, 5, 30, 120},
			attempt:         2,
			expectedBackoff: 5,
		},
		{
			name:            "custom backoff - fourth",
			backoffConfig:   []int{1, 5, 30, 120},
			attempt:         4,
			expectedBackoff: 120,
		},
		{
			name:            "custom backoff - beyond max",
			backoffConfig:   []int{1, 5, 30, 120},
			attempt:         100,
			expectedBackoff: 120,
		},
		{
			name:            "zero attempt treated as first",
			backoffConfig:   []int{10, 20, 30},
			attempt:         0,
			expectedBackoff: 10,
		},
		{
			name:            "negative attempt treated as first",
			backoffConfig:   []int{10, 20, 30},
			attempt:         -5,
			expectedBackoff: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create manager with test config (without actual datastore connection)
			m := &Manager{
				config: &config.NotificationsConfig{
					RetryBackoffMinutes: tt.backoffConfig,
				},
			}

			result := m.getBackoffMinutes(tt.attempt)
			if result != tt.expectedBackoff {
				t.Errorf("getBackoffMinutes(%d) = %d, want %d", tt.attempt, result, tt.expectedBackoff)
			}
		})
	}
}

func TestManager_BuildPayload(t *testing.T) {
	triggeredAt := time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC)
	clearedAt := time.Date(2025, 6, 15, 12, 30, 0, 0, time.UTC)

	metricName := "cpu_usage"
	metricValue := 95.5
	thresholdValue := 90.0
	operator := ">"
	dbName := "production"

	alert := &database.Alert{
		ID:             123,
		AlertType:      "metric",
		Title:          "High CPU Alert",
		Description:    "CPU usage is very high",
		Severity:       "critical",
		Status:         "active",
		TriggeredAt:    triggeredAt,
		ClearedAt:      &clearedAt,
		ConnectionID:   5,
		MetricName:     &metricName,
		MetricValue:    &metricValue,
		ThresholdValue: &thresholdValue,
		Operator:       &operator,
		DatabaseName:   &dbName,
	}

	m := &Manager{}

	payload := m.buildPayload(alert, database.NotificationTypeAlertFire, "prod-db-1", "10.0.0.1", 5432)

	// Verify basic fields
	if payload.AlertID != 123 {
		t.Errorf("AlertID = %d, want 123", payload.AlertID)
	}

	if payload.AlertTitle != "High CPU Alert" {
		t.Errorf("AlertTitle = %q, want %q", payload.AlertTitle, "High CPU Alert")
	}

	if payload.AlertDescription != "CPU usage is very high" {
		t.Errorf("AlertDescription = %q", payload.AlertDescription)
	}

	if payload.Severity != "critical" {
		t.Errorf("Severity = %q, want %q", payload.Severity, "critical")
	}

	if payload.ServerName != "prod-db-1" {
		t.Errorf("ServerName = %q, want %q", payload.ServerName, "prod-db-1")
	}

	if payload.ServerHost != "10.0.0.1" {
		t.Errorf("ServerHost = %q, want %q", payload.ServerHost, "10.0.0.1")
	}

	if payload.ServerPort != 5432 {
		t.Errorf("ServerPort = %d, want 5432", payload.ServerPort)
	}

	if payload.NotificationType != string(database.NotificationTypeAlertFire) {
		t.Errorf("NotificationType = %q", payload.NotificationType)
	}

	// Verify optional fields
	if payload.MetricName == nil || *payload.MetricName != "cpu_usage" {
		t.Error("MetricName not set correctly")
	}

	if payload.MetricValue == nil || *payload.MetricValue != 95.5 {
		t.Error("MetricValue not set correctly")
	}

	if payload.ThresholdValue == nil || *payload.ThresholdValue != 90.0 {
		t.Error("ThresholdValue not set correctly")
	}

	if payload.Operator == nil || *payload.Operator != ">" {
		t.Error("Operator not set correctly")
	}

	if payload.DatabaseName == nil || *payload.DatabaseName != "production" {
		t.Error("DatabaseName not set correctly")
	}

	// Verify computed duration
	if payload.Duration == nil {
		t.Error("Duration should be computed for cleared alerts")
	} else if *payload.Duration != "2h 30m" {
		t.Errorf("Duration = %q, want %q", *payload.Duration, "2h 30m")
	}
}

func TestManager_BuildPayload_WithoutOptionalFields(t *testing.T) {
	triggeredAt := time.Now()

	alert := &database.Alert{
		ID:           1,
		AlertType:    "generic",
		Title:        "Generic Alert",
		Description:  "Something happened",
		Severity:     "info",
		Status:       "active",
		TriggeredAt:  triggeredAt,
		ConnectionID: 1,
		// No optional fields set
	}

	m := &Manager{}

	payload := m.buildPayload(alert, database.NotificationTypeAlertFire, "server", "host", 5432)

	// Optional fields should be nil
	if payload.MetricName != nil {
		t.Error("MetricName should be nil")
	}

	if payload.MetricValue != nil {
		t.Error("MetricValue should be nil")
	}

	if payload.ThresholdValue != nil {
		t.Error("ThresholdValue should be nil")
	}

	if payload.Operator != nil {
		t.Error("Operator should be nil")
	}

	if payload.DatabaseName != nil {
		t.Error("DatabaseName should be nil")
	}

	if payload.Duration != nil {
		t.Error("Duration should be nil for active alerts")
	}
}

func TestManager_DecryptChannelSecrets(t *testing.T) {
	// Create a manager with a mock secret manager that tracks calls
	decryptCalls := make(map[string]string)
	secrets := &mockSecretManager{
		decryptFunc: func(ciphertext string) (string, error) {
			decrypted := "decrypted_" + ciphertext
			decryptCalls[ciphertext] = decrypted
			return decrypted, nil
		},
	}

	m := &Manager{
		secrets: secrets,
	}

	webhookURL := "encrypted_webhook"
	authCreds := "encrypted_auth"
	smtpPassword := "encrypted_smtp"

	channel := &database.NotificationChannel{
		WebhookURL:      &webhookURL,
		AuthCredentials: &authCreds,
		SMTPPassword:    &smtpPassword,
	}

	m.decryptChannelSecrets(channel)

	// Verify webhook URL was decrypted
	if *channel.WebhookURL != "decrypted_encrypted_webhook" {
		t.Errorf("WebhookURL = %q, want %q", *channel.WebhookURL, "decrypted_encrypted_webhook")
	}

	// Verify auth credentials were decrypted
	if *channel.AuthCredentials != "decrypted_encrypted_auth" {
		t.Errorf("AuthCredentials = %q, want %q", *channel.AuthCredentials, "decrypted_encrypted_auth")
	}

	// Verify SMTP password was decrypted
	if *channel.SMTPPassword != "decrypted_encrypted_smtp" {
		t.Errorf("SMTPPassword = %q, want %q", *channel.SMTPPassword, "decrypted_encrypted_smtp")
	}

	// Verify all three were decrypted
	if len(decryptCalls) != 3 {
		t.Errorf("Expected 3 decrypt calls, got %d", len(decryptCalls))
	}
}

func TestManager_DecryptChannelSecrets_EmptyFields(t *testing.T) {
	secrets := &mockSecretManager{
		decryptFunc: func(ciphertext string) (string, error) {
			t.Error("Decrypt should not be called for empty fields")
			return "", nil
		},
	}

	m := &Manager{
		secrets: secrets,
	}

	emptyStr := ""
	channel := &database.NotificationChannel{
		WebhookURL:      nil,
		AuthCredentials: &emptyStr,
		SMTPPassword:    nil,
	}

	// Should not panic or call decrypt for nil/empty values
	m.decryptChannelSecrets(channel)
}

func TestManager_DebugLog(t *testing.T) {
	var loggedMessages []string
	logFunc := func(format string, args ...interface{}) {
		loggedMessages = append(loggedMessages, format)
	}

	tests := []struct {
		name         string
		debug        bool
		expectedLogs int
	}{
		{
			name:         "debug enabled",
			debug:        true,
			expectedLogs: 1,
		},
		{
			name:         "debug disabled",
			debug:        false,
			expectedLogs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loggedMessages = nil

			m := &Manager{
				debug: tt.debug,
				log:   logFunc,
			}

			m.debugLog("Test message: %s", "value")

			if len(loggedMessages) != tt.expectedLogs {
				t.Errorf("Expected %d log messages, got %d", tt.expectedLogs, len(loggedMessages))
			}
		})
	}
}

func TestManager_NilManager(t *testing.T) {
	// Test that nil manager methods don't panic and return nil
	var m *Manager
	ctx := context.TODO()

	// These should all handle nil receiver gracefully
	err := m.SendAlertNotification(ctx, nil, database.NotificationTypeAlertFire)
	if err != nil {
		t.Errorf("SendAlertNotification on nil manager should return nil, got %v", err)
	}

	err = m.ProcessPendingNotifications(ctx)
	if err != nil {
		t.Errorf("ProcessPendingNotifications on nil manager should return nil, got %v", err)
	}

	err = m.ProcessReminders(ctx)
	if err != nil {
		t.Errorf("ProcessReminders on nil manager should return nil, got %v", err)
	}
}
