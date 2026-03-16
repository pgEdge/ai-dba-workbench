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
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// config_queries.go tests
// ---------------------------------------------------------------------------

func TestProbeConfigStruct(t *testing.T) {
	now := time.Now()
	connID := 42
	pc := ProbeConfig{
		ID:                        1,
		ConnectionID:              &connID,
		IsEnabled:                 true,
		Name:                      "pg_stat_activity",
		Description:               "Collects pg_stat_activity data",
		CollectionIntervalSeconds: 30,
		RetentionDays:             7,
		CreatedAt:                 now,
		UpdatedAt:                 now,
	}

	if pc.ID != 1 {
		t.Errorf("expected ID 1, got %d", pc.ID)
	}
	if pc.ConnectionID == nil || *pc.ConnectionID != 42 {
		t.Errorf("expected ConnectionID 42, got %v", pc.ConnectionID)
	}
	if !pc.IsEnabled {
		t.Error("expected IsEnabled true")
	}
	if pc.Name != "pg_stat_activity" {
		t.Errorf("expected Name 'pg_stat_activity', got %q", pc.Name)
	}
	if pc.Description != "Collects pg_stat_activity data" {
		t.Errorf("expected Description 'Collects pg_stat_activity data', got %q", pc.Description)
	}
	if pc.CollectionIntervalSeconds != 30 {
		t.Errorf("expected CollectionIntervalSeconds 30, got %d", pc.CollectionIntervalSeconds)
	}
	if pc.RetentionDays != 7 {
		t.Errorf("expected RetentionDays 7, got %d", pc.RetentionDays)
	}
	if pc.CreatedAt != now {
		t.Errorf("expected CreatedAt %v, got %v", now, pc.CreatedAt)
	}
	if pc.UpdatedAt != now {
		t.Errorf("expected UpdatedAt %v, got %v", now, pc.UpdatedAt)
	}
}

func TestProbeConfigNilConnectionID(t *testing.T) {
	pc := ProbeConfig{
		ID:           2,
		ConnectionID: nil,
		Name:         "pg_stat_database",
	}

	if pc.ID != 2 {
		t.Errorf("expected ID 2, got %d", pc.ID)
	}
	if pc.Name != "pg_stat_database" {
		t.Errorf("expected Name 'pg_stat_database', got %q", pc.Name)
	}
	if pc.ConnectionID != nil {
		t.Errorf("expected nil ConnectionID, got %v", pc.ConnectionID)
	}
}

func TestProbeConfigUpdateStruct(t *testing.T) {
	enabled := true
	interval := 60
	retention := 14

	update := ProbeConfigUpdate{
		IsEnabled:                 &enabled,
		CollectionIntervalSeconds: &interval,
		RetentionDays:             &retention,
	}

	if update.IsEnabled == nil || !*update.IsEnabled {
		t.Error("expected IsEnabled to be true")
	}
	if update.CollectionIntervalSeconds == nil || *update.CollectionIntervalSeconds != 60 {
		t.Errorf("expected CollectionIntervalSeconds 60, got %v", update.CollectionIntervalSeconds)
	}
	if update.RetentionDays == nil || *update.RetentionDays != 14 {
		t.Errorf("expected RetentionDays 14, got %v", update.RetentionDays)
	}
}

func TestProbeConfigUpdatePartial(t *testing.T) {
	// Only set one field; the others remain nil
	enabled := false
	update := ProbeConfigUpdate{
		IsEnabled: &enabled,
	}

	if update.IsEnabled == nil || *update.IsEnabled {
		t.Error("expected IsEnabled to be false")
	}
	if update.CollectionIntervalSeconds != nil {
		t.Error("expected CollectionIntervalSeconds to be nil")
	}
	if update.RetentionDays != nil {
		t.Error("expected RetentionDays to be nil")
	}
}

func TestProbeConfigJSON(t *testing.T) {
	connID := 5
	pc := ProbeConfig{
		ID:                        10,
		ConnectionID:              &connID,
		IsEnabled:                 true,
		Name:                      "pg_locks",
		Description:               "Lock monitoring",
		CollectionIntervalSeconds: 15,
		RetentionDays:             30,
		CreatedAt:                 time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:                 time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(pc)
	if err != nil {
		t.Fatalf("failed to marshal ProbeConfig: %v", err)
	}

	var decoded ProbeConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal ProbeConfig: %v", err)
	}

	if decoded.ID != pc.ID {
		t.Errorf("expected ID %d, got %d", pc.ID, decoded.ID)
	}
	if decoded.ConnectionID == nil || *decoded.ConnectionID != *pc.ConnectionID {
		t.Errorf("expected ConnectionID %d, got %v", *pc.ConnectionID, decoded.ConnectionID)
	}
	if decoded.Name != pc.Name {
		t.Errorf("expected Name %q, got %q", pc.Name, decoded.Name)
	}
}

func TestAlertRuleStruct(t *testing.T) {
	unit := "MB"
	ext := "pg_stat_statements"
	ar := AlertRule{
		ID:                1,
		Name:              "high_cpu",
		Description:       "CPU usage is high",
		Category:          "performance",
		MetricName:        "cpu_usage_percent",
		MetricUnit:        &unit,
		DefaultOperator:   ">",
		DefaultThreshold:  90.0,
		DefaultSeverity:   "critical",
		DefaultEnabled:    true,
		RequiredExtension: &ext,
		IsBuiltIn:         true,
		CreatedAt:         time.Now(),
	}

	if ar.ID != 1 {
		t.Errorf("expected ID 1, got %d", ar.ID)
	}
	if ar.Name != "high_cpu" {
		t.Errorf("expected Name 'high_cpu', got %q", ar.Name)
	}
	if ar.Description != "CPU usage is high" {
		t.Errorf("expected Description 'CPU usage is high', got %q", ar.Description)
	}
	if ar.Category != "performance" {
		t.Errorf("expected Category 'performance', got %q", ar.Category)
	}
	if ar.MetricName != "cpu_usage_percent" {
		t.Errorf("expected MetricName 'cpu_usage_percent', got %q", ar.MetricName)
	}
	if ar.MetricUnit == nil || *ar.MetricUnit != "MB" {
		t.Errorf("expected MetricUnit 'MB', got %v", ar.MetricUnit)
	}
	if ar.DefaultOperator != ">" {
		t.Errorf("expected DefaultOperator '>', got %q", ar.DefaultOperator)
	}
	if ar.DefaultThreshold != 90.0 {
		t.Errorf("expected DefaultThreshold 90.0, got %f", ar.DefaultThreshold)
	}
	if ar.DefaultSeverity != "critical" {
		t.Errorf("expected DefaultSeverity 'critical', got %q", ar.DefaultSeverity)
	}
	if !ar.DefaultEnabled {
		t.Error("expected DefaultEnabled true")
	}
	if ar.RequiredExtension == nil || *ar.RequiredExtension != "pg_stat_statements" {
		t.Errorf("expected RequiredExtension 'pg_stat_statements', got %v", ar.RequiredExtension)
	}
	if !ar.IsBuiltIn {
		t.Error("expected IsBuiltIn true")
	}
	if ar.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestAlertRuleNilOptionalFields(t *testing.T) {
	ar := AlertRule{
		ID:                2,
		Name:              "connections",
		MetricUnit:        nil,
		RequiredExtension: nil,
	}

	if ar.ID != 2 {
		t.Errorf("expected ID 2, got %d", ar.ID)
	}
	if ar.Name != "connections" {
		t.Errorf("expected Name 'connections', got %q", ar.Name)
	}
	if ar.MetricUnit != nil {
		t.Error("expected nil MetricUnit")
	}
	if ar.RequiredExtension != nil {
		t.Error("expected nil RequiredExtension")
	}
}

func TestAlertRuleJSON(t *testing.T) {
	ar := AlertRule{
		ID:               5,
		Name:             "disk_space",
		Description:      "Disk space low",
		Category:         "storage",
		MetricName:       "disk_free_bytes",
		DefaultOperator:  "<",
		DefaultThreshold: 1073741824,
		DefaultSeverity:  "warning",
		DefaultEnabled:   true,
		IsBuiltIn:        true,
		CreatedAt:        time.Date(2025, 3, 15, 0, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(ar)
	if err != nil {
		t.Fatalf("failed to marshal AlertRule: %v", err)
	}

	var decoded AlertRule
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal AlertRule: %v", err)
	}

	if decoded.Name != ar.Name {
		t.Errorf("expected Name %q, got %q", ar.Name, decoded.Name)
	}
	if decoded.DefaultOperator != "<" {
		t.Errorf("expected DefaultOperator '<', got %q", decoded.DefaultOperator)
	}
}

func TestAlertRuleUpdateStruct(t *testing.T) {
	op := ">="
	threshold := 95.5
	sev := "critical"
	enabled := false

	update := AlertRuleUpdate{
		DefaultOperator:  &op,
		DefaultThreshold: &threshold,
		DefaultSeverity:  &sev,
		DefaultEnabled:   &enabled,
	}

	if update.DefaultOperator == nil || *update.DefaultOperator != ">=" {
		t.Errorf("expected DefaultOperator '>=', got %v", update.DefaultOperator)
	}
	if update.DefaultThreshold == nil || *update.DefaultThreshold != 95.5 {
		t.Errorf("expected DefaultThreshold 95.5, got %v", update.DefaultThreshold)
	}
	if update.DefaultSeverity == nil || *update.DefaultSeverity != "critical" {
		t.Errorf("expected DefaultSeverity 'critical', got %v", update.DefaultSeverity)
	}
	if update.DefaultEnabled == nil || *update.DefaultEnabled {
		t.Error("expected DefaultEnabled false")
	}
}

func TestValidOperators(t *testing.T) {
	expected := []string{">", ">=", "<", "<=", "==", "!="}
	for _, op := range expected {
		if !validOperators[op] {
			t.Errorf("expected operator %q to be valid", op)
		}
	}

	invalid := []string{"LIKE", "IN", "BETWEEN", "~", "&&", ""}
	for _, op := range invalid {
		if validOperators[op] {
			t.Errorf("expected operator %q to be invalid", op)
		}
	}
}

func TestValidSeverities(t *testing.T) {
	expected := []string{"info", "warning", "critical"}
	for _, sev := range expected {
		if !validSeverities[sev] {
			t.Errorf("expected severity %q to be valid", sev)
		}
	}

	invalid := []string{"error", "fatal", "debug", "notice", ""}
	for _, sev := range invalid {
		if validSeverities[sev] {
			t.Errorf("expected severity %q to be invalid", sev)
		}
	}
}

func TestAlertOverrideStruct(t *testing.T) {
	op := ">"
	thresh := 85.0
	sev := "warning"
	enabled := true

	ao := AlertOverride{
		RuleID:            1,
		Name:              "cpu_alert",
		Description:       "High CPU",
		Category:          "performance",
		MetricName:        "cpu_pct",
		DefaultOperator:   ">",
		DefaultThreshold:  90.0,
		DefaultSeverity:   "critical",
		DefaultEnabled:    true,
		HasOverride:       true,
		OverrideOperator:  &op,
		OverrideThreshold: &thresh,
		OverrideSeverity:  &sev,
		OverrideEnabled:   &enabled,
	}

	if ao.RuleID != 1 {
		t.Errorf("expected RuleID 1, got %d", ao.RuleID)
	}
	if ao.Name != "cpu_alert" {
		t.Errorf("expected Name 'cpu_alert', got %q", ao.Name)
	}
	if ao.Description != "High CPU" {
		t.Errorf("expected Description 'High CPU', got %q", ao.Description)
	}
	if ao.Category != "performance" {
		t.Errorf("expected Category 'performance', got %q", ao.Category)
	}
	if ao.MetricName != "cpu_pct" {
		t.Errorf("expected MetricName 'cpu_pct', got %q", ao.MetricName)
	}
	if ao.DefaultOperator != ">" {
		t.Errorf("expected DefaultOperator '>', got %q", ao.DefaultOperator)
	}
	if ao.DefaultThreshold != 90.0 {
		t.Errorf("expected DefaultThreshold 90.0, got %f", ao.DefaultThreshold)
	}
	if ao.DefaultSeverity != "critical" {
		t.Errorf("expected DefaultSeverity 'critical', got %q", ao.DefaultSeverity)
	}
	if !ao.DefaultEnabled {
		t.Error("expected DefaultEnabled true")
	}
	if !ao.HasOverride {
		t.Error("expected HasOverride true")
	}
	if ao.OverrideSeverity == nil || *ao.OverrideSeverity != "warning" {
		t.Errorf("expected OverrideSeverity 'warning', got %v", ao.OverrideSeverity)
	}
	if ao.OverrideEnabled == nil || !*ao.OverrideEnabled {
		t.Error("expected OverrideEnabled true")
	}
	if ao.OverrideOperator == nil || *ao.OverrideOperator != ">" {
		t.Errorf("expected OverrideOperator '>', got %v", ao.OverrideOperator)
	}
	if ao.OverrideThreshold == nil || *ao.OverrideThreshold != 85.0 {
		t.Errorf("expected OverrideThreshold 85.0, got %v", ao.OverrideThreshold)
	}
}

func TestAlertOverrideNoOverride(t *testing.T) {
	ao := AlertOverride{
		RuleID:           2,
		Name:             "disk_space",
		DefaultOperator:  "<",
		DefaultThreshold: 1024,
		DefaultSeverity:  "warning",
		DefaultEnabled:   true,
		HasOverride:      false,
	}

	if ao.RuleID != 2 {
		t.Errorf("expected RuleID 2, got %d", ao.RuleID)
	}
	if ao.Name != "disk_space" {
		t.Errorf("expected Name 'disk_space', got %q", ao.Name)
	}
	if ao.DefaultOperator != "<" {
		t.Errorf("expected DefaultOperator '<', got %q", ao.DefaultOperator)
	}
	if ao.DefaultThreshold != 1024 {
		t.Errorf("expected DefaultThreshold 1024, got %f", ao.DefaultThreshold)
	}
	if ao.DefaultSeverity != "warning" {
		t.Errorf("expected DefaultSeverity 'warning', got %q", ao.DefaultSeverity)
	}
	if !ao.DefaultEnabled {
		t.Error("expected DefaultEnabled true")
	}
	if ao.HasOverride {
		t.Error("expected HasOverride false")
	}
	if ao.OverrideOperator != nil {
		t.Error("expected nil OverrideOperator")
	}
	if ao.OverrideThreshold != nil {
		t.Error("expected nil OverrideThreshold")
	}
}

func TestAlertThresholdUpdateStruct(t *testing.T) {
	update := AlertThresholdUpdate{
		Operator:  ">=",
		Threshold: 80.0,
		Severity:  "warning",
		Enabled:   true,
	}

	if update.Operator != ">=" {
		t.Errorf("expected Operator '>=', got %q", update.Operator)
	}
	if update.Threshold != 80.0 {
		t.Errorf("expected Threshold 80.0, got %f", update.Threshold)
	}
	if update.Severity != "warning" {
		t.Errorf("expected Severity 'warning', got %q", update.Severity)
	}
	if !update.Enabled {
		t.Error("expected Enabled true")
	}
}

func TestProbeOverrideStruct(t *testing.T) {
	overrideEnabled := true
	overrideInterval := 15
	overrideRetention := 3

	po := ProbeOverride{
		Name:                    "pg_stat_activity",
		Description:             "Activity stats",
		DefaultEnabled:          true,
		DefaultIntervalSeconds:  30,
		DefaultRetentionDays:    7,
		HasOverride:             true,
		OverrideEnabled:         &overrideEnabled,
		OverrideIntervalSeconds: &overrideInterval,
		OverrideRetentionDays:   &overrideRetention,
	}

	if po.Name != "pg_stat_activity" {
		t.Errorf("expected Name 'pg_stat_activity', got %q", po.Name)
	}
	if po.Description != "Activity stats" {
		t.Errorf("expected Description 'Activity stats', got %q", po.Description)
	}
	if !po.DefaultEnabled {
		t.Error("expected DefaultEnabled true")
	}
	if po.DefaultIntervalSeconds != 30 {
		t.Errorf("expected DefaultIntervalSeconds 30, got %d", po.DefaultIntervalSeconds)
	}
	if po.DefaultRetentionDays != 7 {
		t.Errorf("expected DefaultRetentionDays 7, got %d", po.DefaultRetentionDays)
	}
	if !po.HasOverride {
		t.Error("expected HasOverride true")
	}
	if po.OverrideEnabled == nil || !*po.OverrideEnabled {
		t.Error("expected OverrideEnabled true")
	}
	if po.OverrideIntervalSeconds == nil || *po.OverrideIntervalSeconds != 15 {
		t.Errorf("expected OverrideIntervalSeconds 15, got %v", po.OverrideIntervalSeconds)
	}
	if po.OverrideRetentionDays == nil || *po.OverrideRetentionDays != 3 {
		t.Errorf("expected OverrideRetentionDays 3, got %v", po.OverrideRetentionDays)
	}
}

func TestProbeOverrideNoOverride(t *testing.T) {
	po := ProbeOverride{
		Name:                   "pg_stat_database",
		DefaultEnabled:         true,
		DefaultIntervalSeconds: 60,
		DefaultRetentionDays:   14,
		HasOverride:            false,
	}

	if po.Name != "pg_stat_database" {
		t.Errorf("expected Name 'pg_stat_database', got %q", po.Name)
	}
	if !po.DefaultEnabled {
		t.Error("expected DefaultEnabled true")
	}
	if po.DefaultIntervalSeconds != 60 {
		t.Errorf("expected DefaultIntervalSeconds 60, got %d", po.DefaultIntervalSeconds)
	}
	if po.DefaultRetentionDays != 14 {
		t.Errorf("expected DefaultRetentionDays 14, got %d", po.DefaultRetentionDays)
	}
	if po.HasOverride {
		t.Error("expected HasOverride false")
	}
	if po.OverrideEnabled != nil {
		t.Error("expected nil OverrideEnabled")
	}
}

func TestProbeOverrideUpdateStruct(t *testing.T) {
	update := ProbeOverrideUpdate{
		IsEnabled:                 true,
		CollectionIntervalSeconds: 10,
		RetentionDays:             5,
	}

	if !update.IsEnabled {
		t.Error("expected IsEnabled true")
	}
	if update.CollectionIntervalSeconds != 10 {
		t.Errorf("expected CollectionIntervalSeconds 10, got %d", update.CollectionIntervalSeconds)
	}
	if update.RetentionDays != 5 {
		t.Errorf("expected RetentionDays 5, got %d", update.RetentionDays)
	}
}

func TestSentinelErrors_Config(t *testing.T) {
	if ErrProbeConfigNotFound == nil {
		t.Fatal("ErrProbeConfigNotFound is nil")
	}
	if ErrAlertRuleNotFound == nil {
		t.Fatal("ErrAlertRuleNotFound is nil")
	}
	if ErrProbeConfigNotFound.Error() != "probe config not found" {
		t.Errorf("unexpected error message: %q", ErrProbeConfigNotFound.Error())
	}
	if ErrAlertRuleNotFound.Error() != "alert rule not found" {
		t.Errorf("unexpected error message: %q", ErrAlertRuleNotFound.Error())
	}
}

// ---------------------------------------------------------------------------
// notification_queries.go tests
// ---------------------------------------------------------------------------

func TestNotificationChannelTypeConstants(t *testing.T) {
	tests := []struct {
		constant NotificationChannelType
		value    string
	}{
		{ChannelTypeSlack, "slack"},
		{ChannelTypeMattermost, "mattermost"},
		{ChannelTypeWebhook, "webhook"},
		{ChannelTypeEmail, "email"},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if string(tt.constant) != tt.value {
				t.Errorf("expected %q, got %q", tt.value, string(tt.constant))
			}
		})
	}
}

func TestValidChannelTypes(t *testing.T) {
	valid := []string{"slack", "mattermost", "webhook", "email"}
	for _, ct := range valid {
		if !ValidChannelTypes[ct] {
			t.Errorf("expected channel type %q to be valid", ct)
		}
	}

	invalid := []string{"sms", "pager", "teams", "discord", ""}
	for _, ct := range invalid {
		if ValidChannelTypes[ct] {
			t.Errorf("expected channel type %q to be invalid", ct)
		}
	}
}

func TestSentinelErrors_Notification(t *testing.T) {
	if ErrNotificationChannelNotFound == nil {
		t.Fatal("ErrNotificationChannelNotFound is nil")
	}
	if ErrEmailRecipientNotFound == nil {
		t.Fatal("ErrEmailRecipientNotFound is nil")
	}
	if ErrNotificationChannelNotFound.Error() != "notification channel not found" {
		t.Errorf("unexpected error message: %q", ErrNotificationChannelNotFound.Error())
	}
	if ErrEmailRecipientNotFound.Error() != "email recipient not found" {
		t.Errorf("unexpected error message: %q", ErrEmailRecipientNotFound.Error())
	}
}

func TestNotificationChannelStruct(t *testing.T) {
	username := "admin"
	desc := "Main Slack channel"
	webhookURL := "https://hooks.slack.com/services/xxx"

	ch := NotificationChannel{
		ID:                    1,
		OwnerUsername:         &username,
		Enabled:               true,
		ChannelType:           ChannelTypeSlack,
		Name:                  "ops-alerts",
		Description:           &desc,
		WebhookURL:            &webhookURL,
		HTTPMethod:            "POST",
		SMTPPort:              0,
		ReminderEnabled:       false,
		ReminderIntervalHours: 4,
		IsEstateDefault:       true,
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}

	if ch.ID != 1 {
		t.Errorf("expected ID 1, got %d", ch.ID)
	}
	if ch.Name != "ops-alerts" {
		t.Errorf("expected Name 'ops-alerts', got %q", ch.Name)
	}
	if ch.Description == nil || *ch.Description != "Main Slack channel" {
		t.Errorf("expected Description 'Main Slack channel', got %v", ch.Description)
	}
	if !ch.Enabled {
		t.Error("expected Enabled true")
	}
	if ch.ChannelType != ChannelTypeSlack {
		t.Errorf("expected ChannelType 'slack', got %q", ch.ChannelType)
	}
	if ch.WebhookURL == nil || *ch.WebhookURL != "https://hooks.slack.com/services/xxx" {
		t.Errorf("expected WebhookURL 'https://hooks.slack.com/services/xxx', got %v", ch.WebhookURL)
	}
	if ch.HTTPMethod != "POST" {
		t.Errorf("expected HTTPMethod 'POST', got %q", ch.HTTPMethod)
	}
	if ch.SMTPPort != 0 {
		t.Errorf("expected SMTPPort 0, got %d", ch.SMTPPort)
	}
	if ch.ReminderEnabled {
		t.Error("expected ReminderEnabled false")
	}
	if ch.ReminderIntervalHours != 4 {
		t.Errorf("expected ReminderIntervalHours 4, got %d", ch.ReminderIntervalHours)
	}
	if ch.IsEstateDefault != true {
		t.Error("expected IsEstateDefault true")
	}
	if ch.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
	if ch.UpdatedAt.IsZero() {
		t.Error("expected non-zero UpdatedAt")
	}
	if ch.OwnerUsername == nil || *ch.OwnerUsername != "admin" {
		t.Errorf("expected OwnerUsername 'admin', got %v", ch.OwnerUsername)
	}
	if !ch.IsEstateDefault {
		t.Error("expected IsEstateDefault true")
	}
}

func TestNotificationChannelJSON(t *testing.T) {
	ch := NotificationChannel{
		ID:          2,
		Enabled:     true,
		ChannelType: ChannelTypeWebhook,
		Name:        "custom-webhook",
		HTTPMethod:  "POST",
		Headers:     map[string]string{"Authorization": "Bearer token123"},
		SMTPPort:    0,
		CreatedAt:   time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	if ch.ID != 2 {
		t.Errorf("expected ID 2, got %d", ch.ID)
	}
	if ch.Description != nil {
		t.Errorf("expected nil Description, got %v", ch.Description)
	}
	if ch.CreatedAt != time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("expected CreatedAt 2025-06-01, got %v", ch.CreatedAt)
	}
	if ch.UpdatedAt != time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("expected UpdatedAt 2025-06-01, got %v", ch.UpdatedAt)
	}

	data, err := json.Marshal(ch)
	if err != nil {
		t.Fatalf("failed to marshal NotificationChannel: %v", err)
	}

	var decoded NotificationChannel
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal NotificationChannel: %v", err)
	}

	if decoded.Name != "custom-webhook" {
		t.Errorf("expected Name 'custom-webhook', got %q", decoded.Name)
	}
	if decoded.ChannelType != ChannelTypeWebhook {
		t.Errorf("expected ChannelType 'webhook', got %q", decoded.ChannelType)
	}
	if decoded.Headers["Authorization"] != "Bearer token123" {
		t.Errorf("expected Authorization header, got %v", decoded.Headers)
	}
}

func TestEmailRecipientStruct(t *testing.T) {
	displayName := "Dave Page"
	r := EmailRecipient{
		ID:           1,
		ChannelID:    10,
		EmailAddress: "dave@example.com",
		DisplayName:  &displayName,
		Enabled:      true,
		CreatedAt:    time.Now(),
	}

	if r.ID != 1 {
		t.Errorf("expected ID 1, got %d", r.ID)
	}
	if r.ChannelID != 10 {
		t.Errorf("expected ChannelID 10, got %d", r.ChannelID)
	}
	if r.EmailAddress != "dave@example.com" {
		t.Errorf("expected EmailAddress 'dave@example.com', got %q", r.EmailAddress)
	}
	if r.DisplayName == nil || *r.DisplayName != "Dave Page" {
		t.Errorf("expected DisplayName 'Dave Page', got %v", r.DisplayName)
	}
	if !r.Enabled {
		t.Error("expected Enabled true")
	}
	if r.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestEmailRecipientJSON(t *testing.T) {
	r := EmailRecipient{
		ID:           3,
		ChannelID:    7,
		EmailAddress: "test@example.com",
		Enabled:      true,
		CreatedAt:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("failed to marshal EmailRecipient: %v", err)
	}

	var decoded EmailRecipient
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal EmailRecipient: %v", err)
	}

	if decoded.EmailAddress != "test@example.com" {
		t.Errorf("expected EmailAddress 'test@example.com', got %q", decoded.EmailAddress)
	}
}

func TestMarshalHeaders(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		wantNil  bool
		wantKeys []string
	}{
		{
			name:    "nil map returns nil",
			headers: nil,
			wantNil: true,
		},
		{
			name:    "empty map returns nil",
			headers: map[string]string{},
			wantNil: true,
		},
		{
			name: "single header",
			headers: map[string]string{
				"Content-Type": "application/json",
			},
			wantNil:  false,
			wantKeys: []string{"Content-Type"},
		},
		{
			name: "multiple headers",
			headers: map[string]string{
				"Authorization": "Bearer token",
				"X-Custom":      "value",
			},
			wantNil:  false,
			wantKeys: []string{"Authorization", "X-Custom"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := marshalHeaders(tt.headers)
			if err != nil {
				t.Fatalf("marshalHeaders() returned error: %v", err)
			}

			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %s", string(result))
				}
				return
			}

			if result == nil {
				t.Fatal("expected non-nil result")
			}

			// Verify the JSON is valid and contains expected keys
			var decoded map[string]string
			if err := json.Unmarshal(result, &decoded); err != nil {
				t.Fatalf("failed to unmarshal result: %v", err)
			}

			for _, key := range tt.wantKeys {
				if _, ok := decoded[key]; !ok {
					t.Errorf("expected key %q in result", key)
				}
			}

			// Verify round-trip fidelity
			for k, v := range tt.headers {
				if decoded[k] != v {
					t.Errorf("expected %q=%q, got %q=%q", k, v, k, decoded[k])
				}
			}
		})
	}
}

func TestChannelOverrideStruct(t *testing.T) {
	desc := "Primary alerts channel"
	enabled := true

	co := ChannelOverride{
		ChannelID:       1,
		ChannelName:     "ops-alerts",
		ChannelType:     "slack",
		Description:     &desc,
		IsEstateDefault: true,
		HasOverride:     true,
		OverrideEnabled: &enabled,
	}

	if co.ChannelID != 1 {
		t.Errorf("expected ChannelID 1, got %d", co.ChannelID)
	}
	if co.ChannelName != "ops-alerts" {
		t.Errorf("expected ChannelName 'ops-alerts', got %q", co.ChannelName)
	}
	if co.ChannelType != "slack" {
		t.Errorf("expected ChannelType 'slack', got %q", co.ChannelType)
	}
	if !co.IsEstateDefault {
		t.Error("expected IsEstateDefault true")
	}
	if !co.HasOverride {
		t.Error("expected HasOverride true")
	}
	if co.OverrideEnabled == nil || !*co.OverrideEnabled {
		t.Error("expected OverrideEnabled true")
	}
	if co.Description == nil || *co.Description != "Primary alerts channel" {
		t.Errorf("expected Description 'Primary alerts channel', got %v", co.Description)
	}
}

func TestChannelOverrideNoOverride(t *testing.T) {
	co := ChannelOverride{
		ChannelID:   2,
		ChannelName: "backup-channel",
		ChannelType: "email",
		HasOverride: false,
	}

	if co.ChannelID != 2 {
		t.Errorf("expected ChannelID 2, got %d", co.ChannelID)
	}
	if co.ChannelName != "backup-channel" {
		t.Errorf("expected ChannelName 'backup-channel', got %q", co.ChannelName)
	}
	if co.ChannelType != "email" {
		t.Errorf("expected ChannelType 'email', got %q", co.ChannelType)
	}
	if co.HasOverride {
		t.Error("expected HasOverride false")
	}
	if co.OverrideEnabled != nil {
		t.Error("expected nil OverrideEnabled")
	}
}

func TestChannelOverrideUpdateStruct(t *testing.T) {
	update := ChannelOverrideUpdate{Enabled: true}
	if !update.Enabled {
		t.Error("expected Enabled true")
	}

	update2 := ChannelOverrideUpdate{Enabled: false}
	if update2.Enabled {
		t.Error("expected Enabled false")
	}
}

// ---------------------------------------------------------------------------
// timeline_queries.go tests
// ---------------------------------------------------------------------------

func TestEventTypeConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		value    string
	}{
		{"config_change", EventTypeConfigChange, "config_change"},
		{"hba_change", EventTypeHBAChange, "hba_change"},
		{"ident_change", EventTypeIdentChange, "ident_change"},
		{"restart", EventTypeRestart, "restart"},
		{"alert_fired", EventTypeAlertFired, "alert_fired"},
		{"alert_cleared", EventTypeAlertCleared, "alert_cleared"},
		{"alert_acknowledged", EventTypeAlertAcknowledged, "alert_acknowledged"},
		{"extension_change", EventTypeExtensionChange, "extension_change"},
		{"blackout_started", EventTypeBlackoutStarted, "blackout_started"},
		{"blackout_ended", EventTypeBlackoutEnded, "blackout_ended"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.value {
				t.Errorf("expected %q, got %q", tt.value, tt.constant)
			}
		})
	}
}

func TestTimelineEventStruct(t *testing.T) {
	now := time.Now()
	details := json.RawMessage(`{"key":"value"}`)

	event := TimelineEvent{
		ID:           "config-1-2025-01-01",
		EventType:    EventTypeConfigChange,
		ConnectionID: 42,
		ServerName:   "pg-primary",
		OccurredAt:   now,
		Severity:     "info",
		Title:        "Configuration Changed",
		Summary:      "Updated 3 PostgreSQL settings",
		Details:      details,
	}

	if event.ID != "config-1-2025-01-01" {
		t.Errorf("expected ID 'config-1-2025-01-01', got %q", event.ID)
	}
	if event.EventType != EventTypeConfigChange {
		t.Errorf("expected EventType %q, got %q", EventTypeConfigChange, event.EventType)
	}
	if event.ConnectionID != 42 {
		t.Errorf("expected ConnectionID 42, got %d", event.ConnectionID)
	}
	if event.ServerName != "pg-primary" {
		t.Errorf("expected ServerName 'pg-primary', got %q", event.ServerName)
	}
	if event.OccurredAt != now {
		t.Errorf("expected OccurredAt %v, got %v", now, event.OccurredAt)
	}
	if event.Severity != "info" {
		t.Errorf("expected Severity 'info', got %q", event.Severity)
	}
	if event.Title != "Configuration Changed" {
		t.Errorf("expected Title 'Configuration Changed', got %q", event.Title)
	}
	if event.Summary != "Updated 3 PostgreSQL settings" {
		t.Errorf("expected Summary 'Updated 3 PostgreSQL settings', got %q", event.Summary)
	}
	if string(event.Details) != `{"key":"value"}` {
		t.Errorf("unexpected Details: %s", string(event.Details))
	}
}

func TestTimelineEventJSON(t *testing.T) {
	event := TimelineEvent{
		ID:           "alert-fired-5-2025-01-15",
		EventType:    EventTypeAlertFired,
		ConnectionID: 10,
		ServerName:   "db-server-1",
		OccurredAt:   time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC),
		Severity:     "critical",
		Title:        "Alert Fired: High CPU",
		Summary:      "CPU at 95%",
	}

	if event.ID != "alert-fired-5-2025-01-15" {
		t.Errorf("expected ID 'alert-fired-5-2025-01-15', got %q", event.ID)
	}
	if event.ServerName != "db-server-1" {
		t.Errorf("expected ServerName 'db-server-1', got %q", event.ServerName)
	}
	if event.OccurredAt != time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC) {
		t.Errorf("expected OccurredAt 2025-01-15 12:00:00, got %v", event.OccurredAt)
	}
	if event.Title != "Alert Fired: High CPU" {
		t.Errorf("expected Title 'Alert Fired: High CPU', got %q", event.Title)
	}
	if event.Summary != "CPU at 95%" {
		t.Errorf("expected Summary 'CPU at 95%%', got %q", event.Summary)
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal TimelineEvent: %v", err)
	}

	var decoded TimelineEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal TimelineEvent: %v", err)
	}

	if decoded.EventType != EventTypeAlertFired {
		t.Errorf("expected EventType %q, got %q", EventTypeAlertFired, decoded.EventType)
	}
	if decoded.ServerName != "db-server-1" {
		t.Errorf("expected ServerName 'db-server-1', got %q", decoded.ServerName)
	}
}

func TestTimelineFilterDefaults(t *testing.T) {
	filter := TimelineFilter{}

	if filter.ConnectionID != nil {
		t.Error("expected nil ConnectionID")
	}
	if len(filter.ConnectionIDs) != 0 {
		t.Errorf("expected empty ConnectionIDs, got %d", len(filter.ConnectionIDs))
	}
	if !filter.StartTime.IsZero() {
		t.Error("expected zero StartTime")
	}
	if !filter.EndTime.IsZero() {
		t.Error("expected zero EndTime")
	}
	if len(filter.EventTypes) != 0 {
		t.Errorf("expected empty EventTypes, got %d", len(filter.EventTypes))
	}
	if filter.Limit != 0 {
		t.Errorf("expected Limit 0, got %d", filter.Limit)
	}
}

func TestTimelineResultStruct(t *testing.T) {
	result := TimelineResult{
		Events:     []TimelineEvent{},
		TotalCount: 0,
	}

	if len(result.Events) != 0 {
		t.Errorf("expected 0 events, got %d", len(result.Events))
	}
	if result.TotalCount != 0 {
		t.Errorf("expected TotalCount 0, got %d", result.TotalCount)
	}
}

func TestBuildConnectionFilter(t *testing.T) {
	tests := []struct {
		name          string
		filter        TimelineFilter
		wantCondition string
		wantArgCount  int
		wantArgNum    int
	}{
		{
			name:          "no filters",
			filter:        TimelineFilter{},
			wantCondition: "",
			wantArgCount:  0,
			wantArgNum:    1,
		},
		{
			name: "single connection ID",
			filter: TimelineFilter{
				ConnectionID: intPtr(42),
			},
			wantCondition: "connection_id = $1",
			wantArgCount:  1,
			wantArgNum:    2,
		},
		{
			name: "multiple connection IDs",
			filter: TimelineFilter{
				ConnectionIDs: []int{1, 2, 3},
			},
			wantCondition: "connection_id IN ($1, $2, $3)",
			wantArgCount:  3,
			wantArgNum:    4,
		},
		{
			name: "both single and multiple IDs",
			filter: TimelineFilter{
				ConnectionID:  intPtr(10),
				ConnectionIDs: []int{20, 30},
			},
			wantCondition: "connection_id = $1 AND connection_id IN ($2, $3)",
			wantArgCount:  3,
			wantArgNum:    4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			condition, args, argNum := buildConnectionFilter(tt.filter)

			if condition != tt.wantCondition {
				t.Errorf("expected condition %q, got %q", tt.wantCondition, condition)
			}
			if len(args) != tt.wantArgCount {
				t.Errorf("expected %d args, got %d", tt.wantArgCount, len(args))
			}
			if argNum != tt.wantArgNum {
				t.Errorf("expected argNum %d, got %d", tt.wantArgNum, argNum)
			}
		})
	}
}

func TestBuildTimeFilter(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 12, 31, 23, 59, 59, 0, time.UTC)

	tests := []struct {
		name          string
		filter        TimelineFilter
		startArgNum   int
		wantCondition string
		wantArgCount  int
		wantArgNum    int
	}{
		{
			name:          "no time filters",
			filter:        TimelineFilter{},
			startArgNum:   1,
			wantCondition: "",
			wantArgCount:  0,
			wantArgNum:    1,
		},
		{
			name:          "start time only",
			filter:        TimelineFilter{StartTime: start},
			startArgNum:   1,
			wantCondition: "event_time >= $1",
			wantArgCount:  1,
			wantArgNum:    2,
		},
		{
			name:          "end time only",
			filter:        TimelineFilter{EndTime: end},
			startArgNum:   1,
			wantCondition: "event_time <= $1",
			wantArgCount:  1,
			wantArgNum:    2,
		},
		{
			name:          "both start and end",
			filter:        TimelineFilter{StartTime: start, EndTime: end},
			startArgNum:   1,
			wantCondition: "event_time >= $1 AND event_time <= $2",
			wantArgCount:  2,
			wantArgNum:    3,
		},
		{
			name:          "continued from previous args",
			filter:        TimelineFilter{StartTime: start, EndTime: end},
			startArgNum:   3,
			wantCondition: "event_time >= $3 AND event_time <= $4",
			wantArgCount:  2,
			wantArgNum:    5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			condition, args, argNum := buildTimeFilter(tt.filter, tt.startArgNum)

			if condition != tt.wantCondition {
				t.Errorf("expected condition %q, got %q", tt.wantCondition, condition)
			}
			if len(args) != tt.wantArgCount {
				t.Errorf("expected %d args, got %d", tt.wantArgCount, len(args))
			}
			if argNum != tt.wantArgNum {
				t.Errorf("expected argNum %d, got %d", tt.wantArgNum, argNum)
			}
		})
	}
}

func TestBuildWhereClause(t *testing.T) {
	tests := []struct {
		name           string
		connCondition  string
		timeCondition  string
		extraCondition string
		want           string
	}{
		{
			name: "all empty",
			want: "",
		},
		{
			name:          "connection only",
			connCondition: "connection_id = $1",
			want:          "WHERE connection_id = $1",
		},
		{
			name:          "time only",
			timeCondition: "event_time >= $1",
			want:          "WHERE event_time >= $1",
		},
		{
			name:           "extra only",
			extraCondition: "severity = 'critical'",
			want:           "WHERE severity = 'critical'",
		},
		{
			name:          "connection and time",
			connCondition: "connection_id = $1",
			timeCondition: "event_time >= $2",
			want:          "WHERE connection_id = $1 AND event_time >= $2",
		},
		{
			name:           "all three conditions",
			connCondition:  "connection_id = $1",
			timeCondition:  "event_time >= $2",
			extraCondition: "severity = 'critical'",
			want:           "WHERE connection_id = $1 AND event_time >= $2 AND severity = 'critical'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildWhereClause(tt.connCondition, tt.timeCondition, tt.extraCondition)
			if result != tt.want {
				t.Errorf("expected %q, got %q", tt.want, result)
			}
		})
	}
}

func TestBuildUnionQueryIncludesAllTypesByDefault(t *testing.T) {
	filter := TimelineFilter{}
	query := buildUnionQuery("", "", "", filter, 500, 1)

	// The default query should include subqueries for all event types
	expectedFragments := []string{
		"config_change",
		"hba_change",
		"ident_change",
		"restart",
		"alert_fired",
		"alert_cleared",
		"alert_acknowledged",
		"extension_change",
		"blackout_started",
		"blackout_ended",
	}

	for _, fragment := range expectedFragments {
		if !strings.Contains(query, fragment) {
			t.Errorf("expected query to contain %q", fragment)
		}
	}

	// Should contain LIMIT
	if !strings.Contains(query, "LIMIT 500") {
		t.Error("expected query to contain 'LIMIT 500'")
	}
}

func TestBuildUnionQueryFilteredTypes(t *testing.T) {
	filter := TimelineFilter{
		EventTypes: []string{EventTypeAlertFired, EventTypeAlertCleared},
	}
	query := buildUnionQuery("", "", "", filter, 100, 1)

	// Should include alert types
	if !strings.Contains(query, "alert_fired") {
		t.Error("expected query to contain 'alert_fired'")
	}
	if !strings.Contains(query, "alert_cleared") {
		t.Error("expected query to contain 'alert_cleared'")
	}

	// Should not include config_change or restart
	if strings.Contains(query, "'config_change'") {
		t.Error("did not expect query to contain config_change type literal")
	}
	if strings.Contains(query, "'restart'") {
		t.Error("did not expect query to contain restart type literal")
	}
}

func TestBuildUnionQueryEmptyTypes(t *testing.T) {
	// If filter has an explicit empty event types list, that means include all
	filter := TimelineFilter{
		EventTypes: []string{},
	}
	query := buildUnionQuery("", "", "", filter, 500, 1)

	// An empty EventTypes slice should include all event types (same as default)
	if !strings.Contains(query, "config_change") {
		t.Error("expected query to contain config_change for empty EventTypes")
	}
}

func TestBuildUnionQueryNoTypes(t *testing.T) {
	// An unrecognized event type means zero subqueries
	filter := TimelineFilter{
		EventTypes: []string{"nonexistent_type"},
	}
	query := buildUnionQuery("", "", "", filter, 100, 1)

	// With no matching types, should get the empty result query
	if !strings.Contains(query, "WHERE FALSE") {
		t.Error("expected empty result query with WHERE FALSE")
	}
}

func TestBuildCountQuery(t *testing.T) {
	filter := TimelineFilter{}
	query := buildCountQuery("", "", "", filter)

	// Should be a SELECT with addition of count subqueries
	if !strings.Contains(query, "SELECT") {
		t.Error("expected query to start with SELECT")
	}
	if !strings.Contains(query, "total_count") {
		t.Error("expected query to contain 'total_count'")
	}
}

func TestBuildCountQueryNoTypes(t *testing.T) {
	filter := TimelineFilter{
		EventTypes: []string{"nonexistent_type"},
	}
	query := buildCountQuery("", "", "", filter)

	if query != "SELECT 0" {
		t.Errorf("expected 'SELECT 0' for empty count query, got %q", query)
	}
}

func TestBuildCountQueryWithConditions(t *testing.T) {
	filter := TimelineFilter{
		EventTypes: []string{EventTypeAlertFired},
	}
	query := buildCountQuery("connection_id = $1", "event_time >= $2", "", filter)

	// Should contain a count subquery for alerts
	if !strings.Contains(query, "COUNT(*)") {
		t.Error("expected query to contain COUNT(*)")
	}
	if !strings.Contains(query, "total_count") {
		t.Error("expected query to contain 'total_count'")
	}
}

func TestBuildBlackoutScopeFilter(t *testing.T) {
	tests := []struct {
		name          string
		connCondition string
		wantEmpty     bool
		wantContains  []string
	}{
		{
			name:          "empty condition returns empty",
			connCondition: "",
			wantEmpty:     true,
		},
		{
			name:          "single connection filter",
			connCondition: "connection_id = $1",
			wantEmpty:     false,
			wantContains: []string{
				"b.scope = 'server'",
				"b.scope = 'cluster'",
				"b.scope = 'group'",
				"b.scope = 'estate'",
				"b.connection_id = $1",
			},
		},
		{
			name:          "IN clause filter",
			connCondition: "connection_id IN ($1, $2)",
			wantEmpty:     false,
			wantContains: []string{
				"b.connection_id IN ($1, $2)",
				"sc.id IN ($1, $2)",
				"gc.id IN ($1, $2)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildBlackoutScopeFilter(tt.connCondition)

			if tt.wantEmpty {
				if result != "" {
					t.Errorf("expected empty result, got %q", result)
				}
				return
			}

			for _, expected := range tt.wantContains {
				if !strings.Contains(result, expected) {
					t.Errorf("expected result to contain %q, got %q", expected, result)
				}
			}
		})
	}
}

func TestBuildBlackoutStartedQuery(t *testing.T) {
	query := buildBlackoutStartedQuery("", "")

	if !strings.Contains(query, "blackout_started") {
		t.Error("expected query to contain 'blackout_started'")
	}
	if !strings.Contains(query, "FROM blackouts b") {
		t.Error("expected query to select from blackouts")
	}
	if !strings.Contains(query, "Blackout Started") {
		t.Error("expected query to contain 'Blackout Started' title")
	}
}

func TestBuildBlackoutStartedQueryWithTimeFilter(t *testing.T) {
	query := buildBlackoutStartedQuery("", "event_time >= $1")

	// Time filter should be translated to start_time
	if !strings.Contains(query, "b.start_time >= $1") {
		t.Error("expected time filter translated to b.start_time >= $1")
	}
}

func TestBuildBlackoutEndedQuery(t *testing.T) {
	query := buildBlackoutEndedQuery("", "")

	if !strings.Contains(query, "blackout_ended") {
		t.Error("expected query to contain 'blackout_ended'")
	}
	if !strings.Contains(query, "b.end_time IS NOT NULL") {
		t.Error("expected query to filter on end_time IS NOT NULL")
	}
	if !strings.Contains(query, "Blackout Ended") {
		t.Error("expected query to contain 'Blackout Ended' title")
	}
}

func TestBuildBlackoutEndedQueryWithTimeFilter(t *testing.T) {
	query := buildBlackoutEndedQuery("", "event_time <= $1")

	// Time filter should be translated to end_time
	if !strings.Contains(query, "b.end_time <= $1") {
		t.Error("expected time filter translated to b.end_time <= $1")
	}
}

func TestBuildConfigChangeQuery(t *testing.T) {
	query := buildConfigChangeQuery("")

	if !strings.Contains(query, "config_change") {
		t.Error("expected query to contain 'config_change'")
	}
	if !strings.Contains(query, "pg_settings") {
		t.Error("expected query to reference pg_settings")
	}
	if !strings.Contains(query, "Configuration Changed") {
		t.Error("expected query to contain title 'Configuration Changed'")
	}
}

func TestBuildConfigChangeQueryWithWhere(t *testing.T) {
	query := buildConfigChangeQuery("WHERE connection_id = $1 AND event_time >= $2")

	// Column names should be rewritten to table-qualified names for the
	// outer filter (changes alias) and inner filter (collected_at)
	if !strings.Contains(query, "changes.connection_id = $1") {
		t.Error("expected connection_id rewritten to changes.connection_id")
	}
	if !strings.Contains(query, "changes.collected_at >= $2") {
		t.Error("expected event_time rewritten to changes.collected_at")
	}
	// Inner subquery should use collected_at without alias
	if !strings.Contains(query, "collected_at >= $2") {
		t.Error("expected inner filter to use collected_at >= $2")
	}
}

func TestBuildHBAChangeQuery(t *testing.T) {
	query := buildHBAChangeQuery("")

	if !strings.Contains(query, "hba_change") {
		t.Error("expected query to contain 'hba_change'")
	}
	if !strings.Contains(query, "pg_hba_file_rules") {
		t.Error("expected query to reference pg_hba_file_rules")
	}
	if !strings.Contains(query, "HBA Configuration Changed") {
		t.Error("expected query to contain 'HBA Configuration Changed'")
	}
}

func TestBuildIdentChangeQuery(t *testing.T) {
	query := buildIdentChangeQuery("")

	if !strings.Contains(query, "ident_change") {
		t.Error("expected query to contain 'ident_change'")
	}
	if !strings.Contains(query, "pg_ident_file_mappings") {
		t.Error("expected query to reference pg_ident_file_mappings")
	}
	if !strings.Contains(query, "Ident Mappings Changed") {
		t.Error("expected query to contain 'Ident Mappings Changed'")
	}
}

func TestBuildRestartQuery(t *testing.T) {
	query := buildRestartQuery("")

	if !strings.Contains(query, "restart") {
		t.Error("expected query to contain 'restart'")
	}
	if !strings.Contains(query, "pg_node_role") {
		t.Error("expected query to reference pg_node_role")
	}
	if !strings.Contains(query, "Server Restart Detected") {
		t.Error("expected query to contain 'Server Restart Detected'")
	}
	if !strings.Contains(query, "LAG(postmaster_start_time)") {
		t.Error("expected query to use LAG window function")
	}
}

func TestBuildAlertFiredQuery(t *testing.T) {
	query := buildAlertFiredQuery("")

	if !strings.Contains(query, "alert_fired") {
		t.Error("expected query to contain 'alert_fired'")
	}
	if !strings.Contains(query, "FROM alerts") {
		t.Error("expected query to select from alerts table")
	}
	if !strings.Contains(query, "Alert Fired:") {
		t.Error("expected query to contain 'Alert Fired:' in title")
	}
}

func TestBuildAlertClearedQuery(t *testing.T) {
	query := buildAlertClearedQuery("")

	if !strings.Contains(query, "alert_cleared") {
		t.Error("expected query to contain 'alert_cleared'")
	}
	if !strings.Contains(query, "cleared_at IS NOT NULL") {
		t.Error("expected query to filter on cleared_at IS NOT NULL")
	}
	if !strings.Contains(query, "Alert Cleared:") {
		t.Error("expected query to contain 'Alert Cleared:' in title")
	}
}

func TestBuildAlertAcknowledgedQuery(t *testing.T) {
	query := buildAlertAcknowledgedQuery("")

	if !strings.Contains(query, "alert_acknowledged") {
		t.Error("expected query to contain 'alert_acknowledged'")
	}
	if !strings.Contains(query, "alert_acknowledgments") {
		t.Error("expected query to reference alert_acknowledgments table")
	}
}

func TestBuildExtensionChangeQuery(t *testing.T) {
	query := buildExtensionChangeQuery("")

	if !strings.Contains(query, "extension_change") {
		t.Error("expected query to contain 'extension_change'")
	}
	if !strings.Contains(query, "pg_extension") {
		t.Error("expected query to reference pg_extension")
	}
	if !strings.Contains(query, "Extensions Changed") {
		t.Error("expected query to contain 'Extensions Changed'")
	}
}

// ---------------------------------------------------------------------------
// blackout_queries.go tests
// ---------------------------------------------------------------------------

func TestBlackoutScopeConstants(t *testing.T) {
	tests := []struct {
		constant BlackoutScope
		value    string
	}{
		{BlackoutScopeEstate, "estate"},
		{BlackoutScopeGroup, "group"},
		{BlackoutScopeCluster, "cluster"},
		{BlackoutScopeServer, "server"},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if string(tt.constant) != tt.value {
				t.Errorf("expected %q, got %q", tt.value, string(tt.constant))
			}
		})
	}
}

func TestValidBlackoutScopes(t *testing.T) {
	valid := []string{"estate", "group", "cluster", "server"}
	for _, scope := range valid {
		if !ValidBlackoutScopes[scope] {
			t.Errorf("expected scope %q to be valid", scope)
		}
	}

	invalid := []string{"database", "schema", "table", "global", ""}
	for _, scope := range invalid {
		if ValidBlackoutScopes[scope] {
			t.Errorf("expected scope %q to be invalid", scope)
		}
	}
}

func TestSentinelErrors_Blackout(t *testing.T) {
	if ErrBlackoutNotFound == nil {
		t.Fatal("ErrBlackoutNotFound is nil")
	}
	if ErrBlackoutScheduleNotFound == nil {
		t.Fatal("ErrBlackoutScheduleNotFound is nil")
	}
	if ErrBlackoutNotFound.Error() != "blackout not found" {
		t.Errorf("unexpected error message: %q", ErrBlackoutNotFound.Error())
	}
	if ErrBlackoutScheduleNotFound.Error() != "blackout schedule not found" {
		t.Errorf("unexpected error message: %q", ErrBlackoutScheduleNotFound.Error())
	}
}

func TestBlackoutStruct(t *testing.T) {
	now := time.Now()
	groupID := 1
	clusterID := 2
	connID := 3
	dbName := "mydb"

	b := Blackout{
		ID:           100,
		Scope:        "server",
		GroupID:      &groupID,
		ClusterID:    &clusterID,
		ConnectionID: &connID,
		DatabaseName: &dbName,
		Reason:       "Planned maintenance",
		StartTime:    now,
		EndTime:      now.Add(2 * time.Hour),
		CreatedBy:    "admin",
		CreatedAt:    now,
		IsActive:     true,
	}

	if b.ID != 100 {
		t.Errorf("expected ID 100, got %d", b.ID)
	}
	if b.Scope != "server" {
		t.Errorf("expected Scope 'server', got %q", b.Scope)
	}
	if b.GroupID == nil || *b.GroupID != 1 {
		t.Errorf("expected GroupID 1, got %v", b.GroupID)
	}
	if b.ClusterID == nil || *b.ClusterID != 2 {
		t.Errorf("expected ClusterID 2, got %v", b.ClusterID)
	}
	if b.ConnectionID == nil || *b.ConnectionID != 3 {
		t.Errorf("expected ConnectionID 3, got %v", b.ConnectionID)
	}
	if b.DatabaseName == nil || *b.DatabaseName != "mydb" {
		t.Errorf("expected DatabaseName 'mydb', got %v", b.DatabaseName)
	}
	if !b.IsActive {
		t.Error("expected IsActive true")
	}
	if b.Reason != "Planned maintenance" {
		t.Errorf("expected Reason 'Planned maintenance', got %q", b.Reason)
	}
	if b.StartTime != now {
		t.Errorf("expected StartTime %v, got %v", now, b.StartTime)
	}
	if b.EndTime != now.Add(2*time.Hour) {
		t.Errorf("expected EndTime %v, got %v", now.Add(2*time.Hour), b.EndTime)
	}
	if b.CreatedBy != "admin" {
		t.Errorf("expected CreatedBy 'admin', got %q", b.CreatedBy)
	}
	if b.CreatedAt != now {
		t.Errorf("expected CreatedAt %v, got %v", now, b.CreatedAt)
	}
}

func TestBlackoutEstateScope(t *testing.T) {
	b := Blackout{
		ID:    1,
		Scope: string(BlackoutScopeEstate),
	}

	if b.ID != 1 {
		t.Errorf("expected ID 1, got %d", b.ID)
	}
	if b.Scope != "estate" {
		t.Errorf("expected Scope 'estate', got %q", b.Scope)
	}
	if b.GroupID != nil {
		t.Error("estate-scope blackout should have nil GroupID")
	}
	if b.ClusterID != nil {
		t.Error("estate-scope blackout should have nil ClusterID")
	}
	if b.ConnectionID != nil {
		t.Error("estate-scope blackout should have nil ConnectionID")
	}
}

func TestBlackoutJSON(t *testing.T) {
	connID := 5
	b := Blackout{
		ID:           1,
		Scope:        "server",
		ConnectionID: &connID,
		Reason:       "Testing",
		StartTime:    time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		EndTime:      time.Date(2025, 6, 1, 2, 0, 0, 0, time.UTC),
		CreatedBy:    "admin",
		CreatedAt:    time.Date(2025, 5, 31, 0, 0, 0, 0, time.UTC),
		IsActive:     false,
	}

	if b.ID != 1 {
		t.Errorf("expected ID 1, got %d", b.ID)
	}
	if b.Scope != "server" {
		t.Errorf("expected Scope 'server', got %q", b.Scope)
	}
	if b.ConnectionID == nil || *b.ConnectionID != 5 {
		t.Errorf("expected ConnectionID 5, got %v", b.ConnectionID)
	}
	if b.Reason != "Testing" {
		t.Errorf("expected Reason 'Testing', got %q", b.Reason)
	}
	if b.StartTime != time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("expected StartTime 2025-06-01 00:00:00, got %v", b.StartTime)
	}
	if b.EndTime != time.Date(2025, 6, 1, 2, 0, 0, 0, time.UTC) {
		t.Errorf("expected EndTime 2025-06-01 02:00:00, got %v", b.EndTime)
	}
	if b.CreatedBy != "admin" {
		t.Errorf("expected CreatedBy 'admin', got %q", b.CreatedBy)
	}
	if b.CreatedAt != time.Date(2025, 5, 31, 0, 0, 0, 0, time.UTC) {
		t.Errorf("expected CreatedAt 2025-05-31 00:00:00, got %v", b.CreatedAt)
	}

	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("failed to marshal Blackout: %v", err)
	}

	var decoded Blackout
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal Blackout: %v", err)
	}

	if decoded.Scope != "server" {
		t.Errorf("expected Scope 'server', got %q", decoded.Scope)
	}
	if decoded.ConnectionID == nil || *decoded.ConnectionID != 5 {
		t.Errorf("expected ConnectionID 5, got %v", decoded.ConnectionID)
	}
	if decoded.Reason != "Testing" {
		t.Errorf("expected Reason 'Testing', got %q", decoded.Reason)
	}
}

func TestBlackoutScheduleStruct(t *testing.T) {
	now := time.Now()
	connID := 10

	s := BlackoutSchedule{
		ID:              1,
		Scope:           "server",
		ConnectionID:    &connID,
		Name:            "Nightly maintenance",
		CronExpression:  "0 2 * * *",
		DurationMinutes: 60,
		Timezone:        "America/New_York",
		Reason:          "Nightly vacuum",
		Enabled:         true,
		CreatedBy:       "admin",
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if s.ID != 1 {
		t.Errorf("expected ID 1, got %d", s.ID)
	}
	if s.Scope != "server" {
		t.Errorf("expected Scope 'server', got %q", s.Scope)
	}
	if s.ConnectionID == nil || *s.ConnectionID != 10 {
		t.Errorf("expected ConnectionID 10, got %v", s.ConnectionID)
	}
	if s.Name != "Nightly maintenance" {
		t.Errorf("expected Name 'Nightly maintenance', got %q", s.Name)
	}
	if s.CronExpression != "0 2 * * *" {
		t.Errorf("expected CronExpression '0 2 * * *', got %q", s.CronExpression)
	}
	if s.DurationMinutes != 60 {
		t.Errorf("expected DurationMinutes 60, got %d", s.DurationMinutes)
	}
	if s.Timezone != "America/New_York" {
		t.Errorf("expected Timezone 'America/New_York', got %q", s.Timezone)
	}
	if s.Reason != "Nightly vacuum" {
		t.Errorf("expected Reason 'Nightly vacuum', got %q", s.Reason)
	}
	if !s.Enabled {
		t.Error("expected Enabled true")
	}
	if s.CreatedBy != "admin" {
		t.Errorf("expected CreatedBy 'admin', got %q", s.CreatedBy)
	}
	if s.CreatedAt != now {
		t.Errorf("expected CreatedAt %v, got %v", now, s.CreatedAt)
	}
	if s.UpdatedAt != now {
		t.Errorf("expected UpdatedAt %v, got %v", now, s.UpdatedAt)
	}
}

func TestBlackoutFilterDefaults(t *testing.T) {
	filter := BlackoutFilter{}

	if filter.Scope != nil {
		t.Error("expected nil Scope")
	}
	if filter.GroupID != nil {
		t.Error("expected nil GroupID")
	}
	if filter.ClusterID != nil {
		t.Error("expected nil ClusterID")
	}
	if filter.ConnectionID != nil {
		t.Error("expected nil ConnectionID")
	}
	if filter.Active != nil {
		t.Error("expected nil Active")
	}
	if filter.Limit != 0 {
		t.Errorf("expected Limit 0, got %d", filter.Limit)
	}
	if filter.Offset != 0 {
		t.Errorf("expected Offset 0, got %d", filter.Offset)
	}
}

func TestBlackoutListResultStruct(t *testing.T) {
	result := BlackoutListResult{
		Blackouts:  []Blackout{},
		TotalCount: 0,
	}

	if len(result.Blackouts) != 0 {
		t.Errorf("expected 0 blackouts, got %d", len(result.Blackouts))
	}
	if result.TotalCount != 0 {
		t.Errorf("expected TotalCount 0, got %d", result.TotalCount)
	}
}

func TestBlackoutScheduleListResultStruct(t *testing.T) {
	result := BlackoutScheduleListResult{
		Schedules:  []BlackoutSchedule{},
		TotalCount: 0,
	}

	if len(result.Schedules) != 0 {
		t.Errorf("expected 0 schedules, got %d", len(result.Schedules))
	}
	if result.TotalCount != 0 {
		t.Errorf("expected TotalCount 0, got %d", result.TotalCount)
	}
}

// ---------------------------------------------------------------------------
// datastore.go tests
// ---------------------------------------------------------------------------

func TestSentinelErrors_Datastore(t *testing.T) {
	tests := []struct {
		name string
		err  error
		msg  string
	}{
		{"ErrConnectionNotFound", ErrConnectionNotFound, "connection not found"},
		{"ErrClusterGroupNotFound", ErrClusterGroupNotFound, "cluster group not found"},
		{"ErrClusterNotFound", ErrClusterNotFound, "cluster not found"},
		{"ErrAlertNotFound", ErrAlertNotFound, "alert not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Fatalf("%s is nil", tt.name)
			}
			if tt.err.Error() != tt.msg {
				t.Errorf("expected %q, got %q", tt.msg, tt.err.Error())
			}
		})
	}
}

func TestMonitoredConnectionStruct(t *testing.T) {
	conn := MonitoredConnection{
		ID:           1,
		Name:         "prod-primary",
		Host:         "db.example.com",
		Port:         5432,
		DatabaseName: "appdb",
		Username:     "postgres",
		IsMonitored:  true,
		IsShared:     false,
	}

	if conn.ID != 1 {
		t.Errorf("expected ID 1, got %d", conn.ID)
	}
	if conn.Name != "prod-primary" {
		t.Errorf("expected Name 'prod-primary', got %q", conn.Name)
	}
	if conn.Host != "db.example.com" {
		t.Errorf("expected Host 'db.example.com', got %q", conn.Host)
	}
	if conn.Port != 5432 {
		t.Errorf("expected Port 5432, got %d", conn.Port)
	}
	if conn.DatabaseName != "appdb" {
		t.Errorf("expected DatabaseName 'appdb', got %q", conn.DatabaseName)
	}
	if conn.Username != "postgres" {
		t.Errorf("expected Username 'postgres', got %q", conn.Username)
	}
	if !conn.IsMonitored {
		t.Error("expected IsMonitored true")
	}
	if conn.IsShared {
		t.Error("expected IsShared false")
	}
}

func TestMonitoredConnectionNullableFields(t *testing.T) {
	conn := MonitoredConnection{
		ID:   1,
		Name: "test",
		HostAddr: sql.NullString{
			String: "192.168.1.10",
			Valid:  true,
		},
		PasswordEncrypted: sql.NullString{
			String: "encrypted_data",
			Valid:  true,
		},
		SSLMode: sql.NullString{
			String: "require",
			Valid:  true,
		},
		OwnerUsername: sql.NullString{
			String: "admin",
			Valid:  true,
		},
	}

	if conn.ID != 1 {
		t.Errorf("expected ID 1, got %d", conn.ID)
	}
	if conn.Name != "test" {
		t.Errorf("expected Name 'test', got %q", conn.Name)
	}
	if !conn.HostAddr.Valid {
		t.Error("expected HostAddr to be valid")
	}
	if conn.HostAddr.String != "192.168.1.10" {
		t.Errorf("expected HostAddr '192.168.1.10', got %q", conn.HostAddr.String)
	}
	if !conn.SSLMode.Valid {
		t.Error("expected SSLMode to be valid")
	}
	if conn.SSLMode.String != "require" {
		t.Errorf("expected SSLMode 'require', got %q", conn.SSLMode.String)
	}
}

func TestConnectionListItemStruct(t *testing.T) {
	item := ConnectionListItem{
		ID:            5,
		Name:          "staging-db",
		Host:          "staging.example.com",
		Port:          5433,
		DatabaseName:  "stagingdb",
		IsMonitored:   true,
		IsShared:      true,
		OwnerUsername: "alice",
	}

	if item.ID != 5 {
		t.Errorf("expected ID 5, got %d", item.ID)
	}
	if item.Name != "staging-db" {
		t.Errorf("expected Name 'staging-db', got %q", item.Name)
	}
	if item.Host != "staging.example.com" {
		t.Errorf("expected Host 'staging.example.com', got %q", item.Host)
	}
	if item.Port != 5433 {
		t.Errorf("expected Port 5433, got %d", item.Port)
	}
	if item.DatabaseName != "stagingdb" {
		t.Errorf("expected DatabaseName 'stagingdb', got %q", item.DatabaseName)
	}
	if !item.IsMonitored {
		t.Error("expected IsMonitored true")
	}
	if !item.IsShared {
		t.Error("expected IsShared true")
	}
	if item.OwnerUsername != "alice" {
		t.Errorf("expected OwnerUsername 'alice', got %q", item.OwnerUsername)
	}
}

func TestDatabaseInfoStruct(t *testing.T) {
	db := DatabaseInfo{
		Name:     "mydb",
		Owner:    "postgres",
		Encoding: "UTF8",
		Size:     "150 MB",
	}

	if db.Name != "mydb" {
		t.Errorf("expected Name 'mydb', got %q", db.Name)
	}
	if db.Owner != "postgres" {
		t.Errorf("expected Owner 'postgres', got %q", db.Owner)
	}
	if db.Encoding != "UTF8" {
		t.Errorf("expected Encoding 'UTF8', got %q", db.Encoding)
	}
	if db.Size != "150 MB" {
		t.Errorf("expected Size '150 MB', got %q", db.Size)
	}
}

func TestBuildConnectionString(t *testing.T) {
	ds := &Datastore{}

	tests := []struct {
		name             string
		conn             *MonitoredConnection
		password         string
		databaseOverride string
		wantContains     []string
		wantNotContains  []string
	}{
		{
			name: "basic connection",
			conn: &MonitoredConnection{
				Host:         "localhost",
				Port:         5432,
				DatabaseName: "mydb",
				Username:     "user",
			},
			password:         "secret",
			databaseOverride: "",
			wantContains:     []string{"postgres://user:secret@localhost:5432/mydb"},
		},
		{
			name: "no password",
			conn: &MonitoredConnection{
				Host:         "localhost",
				Port:         5432,
				DatabaseName: "mydb",
				Username:     "user",
			},
			password:         "",
			databaseOverride: "",
			wantContains:     []string{"postgres://user@localhost:5432/mydb"},
			wantNotContains:  []string{":@"},
		},
		{
			name: "database override",
			conn: &MonitoredConnection{
				Host:         "localhost",
				Port:         5432,
				DatabaseName: "mydb",
				Username:     "user",
			},
			password:         "",
			databaseOverride: "otherdb",
			wantContains:     []string{"postgres://user@localhost:5432/otherdb"},
			wantNotContains:  []string{"mydb"},
		},
		{
			name: "with hostaddr",
			conn: &MonitoredConnection{
				Host: "db.example.com",
				HostAddr: sql.NullString{
					String: "192.168.1.10",
					Valid:  true,
				},
				Port:         5432,
				DatabaseName: "mydb",
				Username:     "user",
			},
			password:         "",
			databaseOverride: "",
			wantContains:     []string{"192.168.1.10:5432"},
			wantNotContains:  []string{"db.example.com"},
		},
		{
			name: "with invalid hostaddr falls back to host",
			conn: &MonitoredConnection{
				Host: "db.example.com",
				HostAddr: sql.NullString{
					String: "",
					Valid:  false,
				},
				Port:         5432,
				DatabaseName: "mydb",
				Username:     "user",
			},
			password:         "",
			databaseOverride: "",
			wantContains:     []string{"db.example.com:5432"},
		},
		{
			name: "with ssl mode",
			conn: &MonitoredConnection{
				Host:         "localhost",
				Port:         5432,
				DatabaseName: "mydb",
				Username:     "user",
				SSLMode: sql.NullString{
					String: "require",
					Valid:  true,
				},
			},
			password:         "",
			databaseOverride: "",
			wantContains:     []string{"?sslmode=require"},
		},
		{
			name: "without ssl mode",
			conn: &MonitoredConnection{
				Host:         "localhost",
				Port:         5432,
				DatabaseName: "mydb",
				Username:     "user",
			},
			password:         "",
			databaseOverride: "",
			wantNotContains:  []string{"sslmode"},
		},
		{
			name: "special characters in password",
			conn: &MonitoredConnection{
				Host:         "localhost",
				Port:         5432,
				DatabaseName: "mydb",
				Username:     "user",
			},
			password:         "p@ss:w/ord",
			databaseOverride: "",
			wantContains:     []string{"postgres://user:p%40ss%3Aw%2Ford@localhost:5432/mydb"},
		},
		{
			name: "special characters in username",
			conn: &MonitoredConnection{
				Host:         "localhost",
				Port:         5432,
				DatabaseName: "mydb",
				Username:     "user@domain",
			},
			password:         "",
			databaseOverride: "",
			wantContains:     []string{"postgres://user%40domain@localhost:5432/mydb"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ds.BuildConnectionString(tt.conn, tt.password, tt.databaseOverride)

			for _, expected := range tt.wantContains {
				if !strings.Contains(result, expected) {
					t.Errorf("expected result to contain %q, got %q", expected, result)
				}
			}
			for _, unexpected := range tt.wantNotContains {
				if strings.Contains(result, unexpected) {
					t.Errorf("expected result not to contain %q, got %q", unexpected, result)
				}
			}
		})
	}
}

func TestConnectionCreateParamsStruct(t *testing.T) {
	sslMode := "require"
	params := ConnectionCreateParams{
		Name:          "new-conn",
		Host:          "db.example.com",
		Port:          5432,
		DatabaseName:  "appdb",
		Username:      "admin",
		Password:      "secret",
		SSLMode:       &sslMode,
		IsShared:      true,
		IsMonitored:   true,
		OwnerUsername: "dave",
	}

	if params.Name != "new-conn" {
		t.Errorf("expected Name 'new-conn', got %q", params.Name)
	}
	if params.Host != "db.example.com" {
		t.Errorf("expected Host 'db.example.com', got %q", params.Host)
	}
	if params.Port != 5432 {
		t.Errorf("expected Port 5432, got %d", params.Port)
	}
	if params.DatabaseName != "appdb" {
		t.Errorf("expected DatabaseName 'appdb', got %q", params.DatabaseName)
	}
	if params.Username != "admin" {
		t.Errorf("expected Username 'admin', got %q", params.Username)
	}
	if params.Password != "secret" {
		t.Errorf("expected Password 'secret', got %q", params.Password)
	}
	if params.SSLMode == nil || *params.SSLMode != "require" {
		t.Errorf("expected SSLMode 'require', got %v", params.SSLMode)
	}
	if !params.IsShared {
		t.Error("expected IsShared true")
	}
	if !params.IsMonitored {
		t.Error("expected IsMonitored true")
	}
	if params.OwnerUsername != "dave" {
		t.Errorf("expected OwnerUsername 'dave', got %q", params.OwnerUsername)
	}
}

func TestConnectionUpdateParamsStruct(t *testing.T) {
	name := "updated-name"
	port := 5433
	shared := false

	params := ConnectionUpdateParams{
		Name:     &name,
		Port:     &port,
		IsShared: &shared,
	}

	if params.Name == nil || *params.Name != "updated-name" {
		t.Errorf("expected Name 'updated-name', got %v", params.Name)
	}
	if params.Port == nil || *params.Port != 5433 {
		t.Errorf("expected Port 5433, got %v", params.Port)
	}
	if params.IsShared == nil || *params.IsShared {
		t.Errorf("expected IsShared false, got %v", params.IsShared)
	}
	if params.Host != nil {
		t.Error("expected nil Host")
	}
	if params.Password != nil {
		t.Error("expected nil Password")
	}
}

func TestClusterGroupStruct(t *testing.T) {
	desc := "Production servers"
	g := ClusterGroup{
		ID:          1,
		Name:        "production",
		Description: &desc,
		IsShared:    true,
		IsDefault:   false,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if g.ID != 1 {
		t.Errorf("expected ID 1, got %d", g.ID)
	}
	if g.Name != "production" {
		t.Errorf("expected Name 'production', got %q", g.Name)
	}
	if g.Description == nil || *g.Description != "Production servers" {
		t.Errorf("expected Description 'Production servers', got %v", g.Description)
	}
	if !g.IsShared {
		t.Error("expected IsShared true")
	}
	if g.IsDefault {
		t.Error("expected IsDefault false")
	}
	if g.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
	if g.UpdatedAt.IsZero() {
		t.Error("expected non-zero UpdatedAt")
	}
}

func TestClusterStruct(t *testing.T) {
	desc := "Primary cluster"
	c := Cluster{
		ID:   1,
		Name: "pg17-cluster",
		GroupID: sql.NullInt32{
			Int32: 5,
			Valid: true,
		},
		Description: &desc,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if c.ID != 1 {
		t.Errorf("expected ID 1, got %d", c.ID)
	}
	if c.Name != "pg17-cluster" {
		t.Errorf("expected Name 'pg17-cluster', got %q", c.Name)
	}
	if !c.GroupID.Valid || c.GroupID.Int32 != 5 {
		t.Errorf("expected GroupID 5, got %v", c.GroupID)
	}
	if c.Description == nil || *c.Description != "Primary cluster" {
		t.Errorf("expected Description 'Primary cluster', got %v", c.Description)
	}
	if c.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
	if c.UpdatedAt.IsZero() {
		t.Error("expected non-zero UpdatedAt")
	}
}

func TestServerInfoStruct(t *testing.T) {
	role := "primary"
	s := ServerInfo{
		ID:       1,
		Name:     "pg-node1",
		Host:     "192.168.1.10",
		Port:     5432,
		Status:   "online",
		Role:     &role,
		Database: "mydb",
	}

	if s.ID != 1 {
		t.Errorf("expected ID 1, got %d", s.ID)
	}
	if s.Name != "pg-node1" {
		t.Errorf("expected Name 'pg-node1', got %q", s.Name)
	}
	if s.Host != "192.168.1.10" {
		t.Errorf("expected Host '192.168.1.10', got %q", s.Host)
	}
	if s.Port != 5432 {
		t.Errorf("expected Port 5432, got %d", s.Port)
	}
	if s.Status != "online" {
		t.Errorf("expected Status 'online', got %q", s.Status)
	}
	if s.Role == nil || *s.Role != "primary" {
		t.Errorf("expected Role 'primary', got %v", s.Role)
	}
	if s.Database != "mydb" {
		t.Errorf("expected Database 'mydb', got %q", s.Database)
	}
}

func TestExtractClusterPrefix(t *testing.T) {
	ds := &Datastore{}

	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"standard format", "pg17-node1", "pg17"},
		{"with numbers", "pg18-spock1", "pg18"},
		{"no separator", "standalone", "standalone"},
		{"multiple dashes", "my-app-server-1", "my"},
		{"dash at start", "-leading", "-leading"},
		{"single char prefix", "a-server", "a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ds.extractClusterPrefix(tt.input)
			if result != tt.expect {
				t.Errorf("extractClusterPrefix(%q) = %q, want %q", tt.input, result, tt.expect)
			}
		})
	}
}

func TestMapPrimaryRoleToDisplayRole(t *testing.T) {
	ds := &Datastore{}

	tests := []struct {
		primaryRole string
		displayRole string
	}{
		{"binary_primary", "primary"},
		{"binary_standby", "standby"},
		{"binary_cascading", "standby"},
		{"spock_node", "spock"},
		{"spock_standby", "spock_standby"},
		{"logical_publisher", "publisher"},
		{"logical_subscriber", "subscriber"},
		{"logical_bidirectional", "bidirectional"},
		{"standalone", "standalone"},
		{"unknown_role", "unknown_role"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s->%s", tt.primaryRole, tt.displayRole), func(t *testing.T) {
			result := ds.mapPrimaryRoleToDisplayRole(tt.primaryRole)
			if result != tt.displayRole {
				t.Errorf("mapPrimaryRoleToDisplayRole(%q) = %q, want %q",
					tt.primaryRole, result, tt.displayRole)
			}
		})
	}
}

func TestAlertStruct(t *testing.T) {
	ruleID := int64(5)
	dbName := "appdb"
	metricName := "cpu_percent"
	metricValue := 95.2
	metricUnit := "%"
	threshold := 90.0
	operator := ">"
	now := time.Now()
	clearedAt := now.Add(time.Hour)

	a := Alert{
		ID:             1,
		AlertType:      "threshold",
		RuleID:         &ruleID,
		ConnectionID:   42,
		DatabaseName:   &dbName,
		MetricName:     &metricName,
		MetricValue:    &metricValue,
		MetricUnit:     &metricUnit,
		ThresholdValue: &threshold,
		Operator:       &operator,
		Severity:       "critical",
		Title:          "High CPU Usage",
		Description:    "CPU usage exceeded threshold",
		Status:         "cleared",
		TriggeredAt:    now,
		ClearedAt:      &clearedAt,
	}

	if a.ID != 1 {
		t.Errorf("expected ID 1, got %d", a.ID)
	}
	if a.ConnectionID != 42 {
		t.Errorf("expected ConnectionID 42, got %d", a.ConnectionID)
	}
	if a.DatabaseName == nil || *a.DatabaseName != "appdb" {
		t.Errorf("expected DatabaseName 'appdb', got %v", a.DatabaseName)
	}
	if a.MetricName == nil || *a.MetricName != "cpu_percent" {
		t.Errorf("expected MetricName 'cpu_percent', got %v", a.MetricName)
	}
	if a.MetricValue == nil || *a.MetricValue != 95.2 {
		t.Errorf("expected MetricValue 95.2, got %v", a.MetricValue)
	}
	if a.MetricUnit == nil || *a.MetricUnit != "%" {
		t.Errorf("expected MetricUnit '%%', got %v", a.MetricUnit)
	}
	if a.ThresholdValue == nil || *a.ThresholdValue != 90.0 {
		t.Errorf("expected ThresholdValue 90.0, got %v", a.ThresholdValue)
	}
	if a.Operator == nil || *a.Operator != ">" {
		t.Errorf("expected Operator '>', got %v", a.Operator)
	}
	if a.AlertType != "threshold" {
		t.Errorf("expected AlertType 'threshold', got %q", a.AlertType)
	}
	if a.Title != "High CPU Usage" {
		t.Errorf("expected Title 'High CPU Usage', got %q", a.Title)
	}
	if a.Description != "CPU usage exceeded threshold" {
		t.Errorf("expected Description 'CPU usage exceeded threshold', got %q", a.Description)
	}
	if a.Status != "cleared" {
		t.Errorf("expected Status 'cleared', got %q", a.Status)
	}
	if a.TriggeredAt != now {
		t.Errorf("expected TriggeredAt %v, got %v", now, a.TriggeredAt)
	}
	if a.RuleID == nil || *a.RuleID != 5 {
		t.Errorf("expected RuleID 5, got %v", a.RuleID)
	}
	if a.Severity != "critical" {
		t.Errorf("expected Severity 'critical', got %q", a.Severity)
	}
	if a.ClearedAt == nil {
		t.Error("expected non-nil ClearedAt")
	}
}

func TestAlertListFilterDefaults(t *testing.T) {
	filter := AlertListFilter{}

	if filter.ConnectionID != nil {
		t.Error("expected nil ConnectionID")
	}
	if len(filter.ConnectionIDs) != 0 {
		t.Errorf("expected empty ConnectionIDs, got %d", len(filter.ConnectionIDs))
	}
	if filter.Status != nil {
		t.Error("expected nil Status")
	}
	if filter.Severity != nil {
		t.Error("expected nil Severity")
	}
	if filter.ExcludeCleared {
		t.Error("expected ExcludeCleared false")
	}
	if filter.Limit != 0 {
		t.Errorf("expected Limit 0, got %d", filter.Limit)
	}
	if filter.Offset != 0 {
		t.Errorf("expected Offset 0, got %d", filter.Offset)
	}
}

func TestAlertListResultStruct(t *testing.T) {
	result := AlertListResult{
		Alerts: []Alert{},
		Total:  0,
	}

	if len(result.Alerts) != 0 {
		t.Errorf("expected 0 alerts, got %d", len(result.Alerts))
	}
	if result.Total != 0 {
		t.Errorf("expected Total 0, got %d", result.Total)
	}
}

func TestClusterWithServersStruct(t *testing.T) {
	cws := ClusterWithServers{
		ID:   "cluster-1",
		Name: "prod-cluster",
		Servers: []ServerInfo{
			{ID: 1, Name: "node1", Status: "online"},
			{ID: 2, Name: "node2", Status: "online"},
		},
	}

	if cws.ID != "cluster-1" {
		t.Errorf("expected ID 'cluster-1', got %q", cws.ID)
	}
	if cws.Name != "prod-cluster" {
		t.Errorf("expected Name 'prod-cluster', got %q", cws.Name)
	}
	if len(cws.Servers) != 2 {
		t.Errorf("expected 2 servers, got %d", len(cws.Servers))
	}
}

func TestClusterGroupWithClustersStruct(t *testing.T) {
	gwc := ClusterGroupWithClusters{
		ID:   "group-1",
		Name: "production",
		Clusters: []ClusterWithServers{
			{
				ID:      "cluster-1",
				Name:    "main-cluster",
				Servers: []ServerInfo{},
			},
		},
	}

	if gwc.ID != "group-1" {
		t.Errorf("expected ID 'group-1', got %q", gwc.ID)
	}
	if gwc.Name != "production" {
		t.Errorf("expected Name 'production', got %q", gwc.Name)
	}
	if len(gwc.Clusters) != 1 {
		t.Errorf("expected 1 cluster, got %d", len(gwc.Clusters))
	}
	if gwc.Clusters[0].Name != "main-cluster" {
		t.Errorf("expected cluster Name 'main-cluster', got %q", gwc.Clusters[0].Name)
	}
}

func TestDatastoreClose(t *testing.T) {
	// Closing a Datastore with nil pool should not panic
	ds := &Datastore{}
	ds.Close() // Should not panic
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

func intPtr(v int) *int {
	return &v
}
