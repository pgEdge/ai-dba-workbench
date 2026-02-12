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
	"strings"
	"testing"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

func TestNewTemplateRenderer(t *testing.T) {
	renderer := NewTemplateRenderer()
	if renderer == nil {
		t.Error("NewTemplateRenderer() returned nil")
	}
}

func TestTemplateRenderer_Render_BasicFields(t *testing.T) {
	renderer := NewTemplateRenderer()

	triggeredAt := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	payload := &database.NotificationPayload{
		AlertID:          123,
		AlertType:        "metric",
		AlertTitle:       "High CPU Usage",
		AlertDescription: "CPU usage exceeds 90%",
		Severity:         "warning",
		Status:           "active",
		TriggeredAt:      triggeredAt,
		ConnectionID:     1,
		ServerName:       "prod-db-1",
		ServerHost:       "192.168.1.100",
		ServerPort:       5432,
		NotificationType: "alert_fire",
		ReminderCount:    0,
		Timestamp:        time.Now(),
	}

	template := "Alert: {{.AlertTitle}} - Server: {{.ServerName}} - Severity: {{.Severity}}"
	result, err := renderer.Render(template, payload, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	expected := "Alert: High CPU Usage - Server: prod-db-1 - Severity: warning"
	if result != expected {
		t.Errorf("Render() = %q, want %q", result, expected)
	}
}

func TestTemplateRenderer_Render_DefaultTemplate(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertTitle: "Test Alert",
	}

	defaultTemplate := "Default: {{.AlertTitle}}"

	// Empty template string should use default
	result, err := renderer.Render("", payload, defaultTemplate)
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	if result != "Default: Test Alert" {
		t.Errorf("Render() = %q, want %q", result, "Default: Test Alert")
	}
}

func TestTemplateRenderer_Render_NoTemplateError(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertTitle: "Test",
	}

	// Both template and default are empty
	_, err := renderer.Render("", payload, "")
	if err == nil {
		t.Error("Render() expected error for empty templates")
	}

	if err.Error() != "no template provided" {
		t.Errorf("Render() error = %q, want %q", err.Error(), "no template provided")
	}
}

func TestTemplateRenderer_Render_InvalidTemplate(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertTitle: "Test",
	}

	// Invalid template syntax
	template := "{{.AlertTitle"
	_, err := renderer.Render(template, payload, "")
	if err == nil {
		t.Error("Render() expected error for invalid template syntax")
	}

	if !strings.Contains(err.Error(), "failed to compile template") {
		t.Errorf("Error should mention template compilation: %v", err)
	}
}

func TestTemplateRenderer_Render_OptionalFields(t *testing.T) {
	renderer := NewTemplateRenderer()

	metricName := "cpu_usage"
	metricValue := 95.5
	thresholdValue := 90.0
	operator := ">"
	databaseName := "mydb"

	payload := &database.NotificationPayload{
		AlertTitle:     "Test Alert",
		MetricName:     &metricName,
		MetricValue:    &metricValue,
		ThresholdValue: &thresholdValue,
		Operator:       &operator,
		DatabaseName:   &databaseName,
	}

	template := "Metric: {{.MetricName}} = {{.MetricValue}} ({{.Operator}} {{.ThresholdValue}}) in {{.DatabaseName}}"
	result, err := renderer.Render(template, payload, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	expected := "Metric: cpu_usage = 95.5 (> 90) in mydb"
	if result != expected {
		t.Errorf("Render() = %q, want %q", result, expected)
	}
}

func TestTemplateRenderer_Render_ComputedFields(t *testing.T) {
	renderer := NewTemplateRenderer()

	tests := []struct {
		name     string
		severity string
		expected string
	}{
		{"critical", "critical", SeverityColorCritical},
		{"warning", "warning", SeverityColorWarning},
		{"info", "info", SeverityColorInfo},
		{"unknown defaults to info", "unknown", SeverityColorInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := &database.NotificationPayload{
				Severity: tt.severity,
			}

			template := "{{.SeverityColor}}"
			result, err := renderer.Render(template, payload, "")
			if err != nil {
				t.Errorf("Render() unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("SeverityColor for %s = %q, want %q", tt.severity, result, tt.expected)
			}
		})
	}
}

func TestTemplateRenderer_Render_SeverityEmoji(t *testing.T) {
	renderer := NewTemplateRenderer()

	tests := []struct {
		name     string
		severity string
		expected string
	}{
		{"critical", "critical", SeverityEmojiCritical},
		{"warning", "warning", SeverityEmojiWarning},
		{"info", "info", SeverityEmojiInfo},
		{"unknown defaults to info", "other", SeverityEmojiInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := &database.NotificationPayload{
				Severity: tt.severity,
			}

			template := "{{.SeverityEmoji}}"
			result, err := renderer.Render(template, payload, "")
			if err != nil {
				t.Errorf("Render() unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("SeverityEmoji for %s = %q, want %q", tt.severity, result, tt.expected)
			}
		})
	}
}

func TestTemplateRenderer_Render_Duration(t *testing.T) {
	renderer := NewTemplateRenderer()

	triggeredAt := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	clearedAt := time.Date(2025, 1, 15, 12, 30, 45, 0, time.UTC)

	payload := &database.NotificationPayload{
		TriggeredAt: triggeredAt,
		ClearedAt:   &clearedAt,
	}

	template := "{{.Duration}}"
	result, err := renderer.Render(template, payload, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	// Duration should be 2h 30m
	if result != "2h 30m" {
		t.Errorf("Duration = %q, want %q", result, "2h 30m")
	}
}

func TestTemplateRenderer_Render_DurationFromField(t *testing.T) {
	renderer := NewTemplateRenderer()

	duration := "1h 15m"
	payload := &database.NotificationPayload{
		TriggeredAt: time.Now(),
		ClearedAt:   nil,
		Duration:    &duration,
	}

	template := "{{.Duration}}"
	result, err := renderer.Render(template, payload, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	if result != "1h 15m" {
		t.Errorf("Duration = %q, want %q", result, "1h 15m")
	}
}

func TestTemplateRenderer_Render_EmptyDuration(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		TriggeredAt: time.Now(),
		ClearedAt:   nil,
		Duration:    nil,
	}

	template := "Duration: [{{.Duration}}]"
	result, err := renderer.Render(template, payload, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	if result != "Duration: []" {
		t.Errorf("Duration = %q, want %q", result, "Duration: []")
	}
}

func TestTemplateRenderer_Render_TemplateCaching(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertTitle: "Test",
	}

	template := "Cached: {{.AlertTitle}}"

	// Render the same template multiple times
	for i := 0; i < 5; i++ {
		result, err := renderer.Render(template, payload, "")
		if err != nil {
			t.Errorf("Render() iteration %d unexpected error: %v", i, err)
		}

		if result != "Cached: Test" {
			t.Errorf("Render() iteration %d = %q, want %q", i, result, "Cached: Test")
		}
	}
}

func TestTemplateRenderer_RenderJSON_ValidJSON(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertID:    123,
		AlertTitle: "Test Alert",
		Severity:   "warning",
	}

	template := `{"alert_id": {{.AlertID}}, "title": "{{.AlertTitle}}", "severity": "{{.Severity}}"}`
	result, err := renderer.RenderJSON(template, payload, "")
	if err != nil {
		t.Errorf("RenderJSON() unexpected error: %v", err)
	}

	expected := `{"alert_id": 123, "title": "Test Alert", "severity": "warning"}`
	if result != expected {
		t.Errorf("RenderJSON() = %q, want %q", result, expected)
	}
}

func TestTemplateRenderer_RenderJSON_InvalidJSON(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertTitle: "Test",
	}

	// Template produces invalid JSON (missing closing brace)
	template := `{"title": "{{.AlertTitle}}"`
	_, err := renderer.RenderJSON(template, payload, "")
	if err == nil {
		t.Error("RenderJSON() expected error for invalid JSON")
	}

	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("Error should mention invalid JSON: %v", err)
	}
}

func TestTemplateRenderer_RenderJSON_TemplateError(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertTitle: "Test",
	}

	// Invalid template syntax
	template := `{"title": "{{.AlertTitle"`
	_, err := renderer.RenderJSON(template, payload, "")
	if err == nil {
		t.Error("RenderJSON() expected error for template syntax error")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"seconds only", 45 * time.Second, "45s"},
		{"minutes and seconds", 5*time.Minute + 30*time.Second, "5m 30s"},
		{"hours and minutes", 3*time.Hour + 15*time.Minute, "3h 15m"},
		{"days and hours", 2*24*time.Hour + 6*time.Hour, "2d 6h"},
		{"zero", 0, "0s"},
		{"negative duration", -5 * time.Minute, "5m 0s"},
		{"exactly one hour", 1 * time.Hour, "1h 0m"},
		{"exactly one day", 24 * time.Hour, "1d 0h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.duration)
			if result != tt.expected {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestGetSeverityColor(t *testing.T) {
	tests := []struct {
		severity string
		expected string
	}{
		{"critical", SeverityColorCritical},
		{"warning", SeverityColorWarning},
		{"info", SeverityColorInfo},
		{"unknown", SeverityColorInfo},
		{"", SeverityColorInfo},
	}

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			result := getSeverityColor(tt.severity)
			if result != tt.expected {
				t.Errorf("getSeverityColor(%q) = %q, want %q", tt.severity, result, tt.expected)
			}
		})
	}
}

func TestGetSeverityEmoji(t *testing.T) {
	tests := []struct {
		severity string
		expected string
	}{
		{"critical", SeverityEmojiCritical},
		{"warning", SeverityEmojiWarning},
		{"info", SeverityEmojiInfo},
		{"unknown", SeverityEmojiInfo},
		{"", SeverityEmojiInfo},
	}

	for _, tt := range tests {
		t.Run(tt.severity, func(t *testing.T) {
			result := getSeverityEmoji(tt.severity)
			if result != tt.expected {
				t.Errorf("getSeverityEmoji(%q) = %q, want %q", tt.severity, result, tt.expected)
			}
		})
	}
}

func TestTemplateRenderer_Render_DateFormatting(t *testing.T) {
	renderer := NewTemplateRenderer()

	triggeredAt := time.Date(2025, 6, 15, 14, 30, 45, 0, time.UTC)

	payload := &database.NotificationPayload{
		TriggeredAt: triggeredAt,
	}

	template := `{{.TriggeredAt.Format "2006-01-02 15:04:05"}}`
	result, err := renderer.Render(template, payload, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	expected := "2025-06-15 14:30:45"
	if result != expected {
		t.Errorf("Render() = %q, want %q", result, expected)
	}
}

func TestTemplateRenderer_Render_ConditionalFields(t *testing.T) {
	renderer := NewTemplateRenderer()

	// With database name
	dbName := "production"
	payloadWithDB := &database.NotificationPayload{
		AlertTitle:   "Test",
		DatabaseName: &dbName,
	}

	template := `Alert: {{.AlertTitle}}{{if .DatabaseName}} in {{.DatabaseName}}{{end}}`
	result, err := renderer.Render(template, payloadWithDB, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	if result != "Alert: Test in production" {
		t.Errorf("Render() = %q, want %q", result, "Alert: Test in production")
	}

	// Without database name
	payloadWithoutDB := &database.NotificationPayload{
		AlertTitle:   "Test",
		DatabaseName: nil,
	}

	result2, err := renderer.Render(template, payloadWithoutDB, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	if result2 != "Alert: Test" {
		t.Errorf("Render() = %q, want %q", result2, "Alert: Test")
	}
}

func TestDefaultTemplatesAreValid(t *testing.T) {
	renderer := NewTemplateRenderer()

	triggeredAt := time.Now()
	clearedAt := triggeredAt.Add(1 * time.Hour)
	metricName := "cpu_usage"
	metricValue := 95.0
	thresholdValue := 90.0
	operator := ">"
	dbName := "testdb"

	payload := &database.NotificationPayload{
		AlertID:          1,
		AlertType:        "metric",
		AlertTitle:       "High CPU Usage",
		AlertDescription: "CPU usage exceeds threshold",
		Severity:         "warning",
		Status:           "active",
		TriggeredAt:      triggeredAt,
		ClearedAt:        &clearedAt,
		MetricName:       &metricName,
		MetricValue:      &metricValue,
		ThresholdValue:   &thresholdValue,
		Operator:         &operator,
		ConnectionID:     1,
		ServerName:       "prod-db",
		ServerHost:       "10.0.0.1",
		ServerPort:       5432,
		DatabaseName:     &dbName,
		NotificationType: "alert_fire",
		ReminderCount:    2,
		Timestamp:        time.Now(),
	}

	templates := []struct {
		name     string
		template string
		isJSON   bool
	}{
		{"Slack Alert Fire", DefaultSlackAlertFireTemplate, true},
		{"Slack Alert Clear", DefaultSlackAlertClearTemplate, true},
		{"Slack Reminder", DefaultSlackReminderTemplate, true},
		{"Email Alert Fire", DefaultEmailAlertFireTemplate, false},
		{"Email Alert Clear", DefaultEmailAlertClearTemplate, false},
		{"Email Reminder", DefaultEmailReminderTemplate, false},
		{"Webhook Alert Fire", DefaultWebhookAlertFireTemplate, true},
		{"Webhook Alert Clear", DefaultWebhookAlertClearTemplate, true},
		{"Webhook Reminder", DefaultWebhookReminderTemplate, true},
	}

	for _, tt := range templates {
		t.Run(tt.name, func(t *testing.T) {
			var result string
			var err error

			if tt.isJSON {
				result, err = renderer.RenderJSON(tt.template, payload, "")
			} else {
				result, err = renderer.Render(tt.template, payload, "")
			}

			if err != nil {
				t.Errorf("Template %s failed to render: %v", tt.name, err)
			}

			if result == "" {
				t.Errorf("Template %s produced empty result", tt.name)
			}
		})
	}
}

func TestTemplateRenderer_RenderJSON_EscapesSpecialCharacters(t *testing.T) {
	renderer := NewTemplateRenderer()

	tests := []struct {
		name        string
		description string
		wantSubstr  string
	}{
		{
			"double quotes",
			`password authentication failed for user "postgres"`,
			`password authentication failed for user \"postgres\"`,
		},
		{
			"backticks and quotes",
			"connection error: failed to connect to `user=postgres database=postgres`: failed SASL auth: FATAL: password authentication failed for user \"postgres\"",
			`failed for user \"postgres\"`,
		},
		{
			"newlines",
			"line one\nline two\nline three",
			`line one\nline two\nline three`,
		},
		{
			"backslashes",
			`path is C:\Users\admin`,
			`path is C:\\Users\\admin`,
		},
		{
			"tabs",
			"col1\tcol2\tcol3",
			`col1\tcol2\tcol3`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := &database.NotificationPayload{
				AlertID:          1,
				AlertTitle:       "Test Alert",
				AlertDescription: tt.description,
				Severity:         "warning",
			}

			tmpl := `{"title":"{{.AlertTitle}}","description":"{{.AlertDescription}}"}`
			result, err := renderer.RenderJSON(tmpl, payload, "")
			if err != nil {
				t.Fatalf("RenderJSON() unexpected error: %v", err)
			}

			if !strings.Contains(result, tt.wantSubstr) {
				t.Errorf("RenderJSON() result should contain %q, got %q", tt.wantSubstr, result)
			}
		})
	}
}

func TestTemplateRenderer_RenderJSON_DoesNotDoubleEscapeSafeStrings(t *testing.T) {
	renderer := NewTemplateRenderer()

	payload := &database.NotificationPayload{
		AlertID:          123,
		AlertTitle:       "High CPU Usage",
		AlertDescription: "CPU usage exceeds 90%",
		Severity:         "warning",
	}

	tmpl := `{"alert_id": {{.AlertID}}, "title": "{{.AlertTitle}}", "description": "{{.AlertDescription}}"}`
	result, err := renderer.RenderJSON(tmpl, payload, "")
	if err != nil {
		t.Fatalf("RenderJSON() unexpected error: %v", err)
	}

	expected := `{"alert_id": 123, "title": "High CPU Usage", "description": "CPU usage exceeds 90%"}`
	if result != expected {
		t.Errorf("RenderJSON() = %q, want %q", result, expected)
	}
}

func TestTemplateRenderer_RenderJSON_DefaultTemplatesWithSpecialChars(t *testing.T) {
	renderer := NewTemplateRenderer()

	triggeredAt := time.Now()
	clearedAt := triggeredAt.Add(1 * time.Hour)
	metricName := "cpu_usage"
	metricValue := 95.0
	thresholdValue := 90.0
	operator := ">"
	dbName := "testdb"

	// Use a description that contains characters problematic for JSON
	payload := &database.NotificationPayload{
		AlertID:          1,
		AlertType:        "metric",
		AlertTitle:       "Connection Failed",
		AlertDescription: "connection error: failed to connect to `user=postgres database=postgres`: FATAL: password authentication failed for user \"postgres\"",
		Severity:         "critical",
		Status:           "active",
		TriggeredAt:      triggeredAt,
		ClearedAt:        &clearedAt,
		MetricName:       &metricName,
		MetricValue:      &metricValue,
		ThresholdValue:   &thresholdValue,
		Operator:         &operator,
		ConnectionID:     1,
		ServerName:       "prod-db",
		ServerHost:       "10.0.0.1",
		ServerPort:       5432,
		DatabaseName:     &dbName,
		NotificationType: "alert_fire",
		ReminderCount:    2,
		Timestamp:        time.Now(),
	}

	jsonTemplates := []struct {
		name     string
		template string
	}{
		{"Slack Alert Fire", DefaultSlackAlertFireTemplate},
		{"Slack Alert Clear", DefaultSlackAlertClearTemplate},
		{"Slack Reminder", DefaultSlackReminderTemplate},
		{"Webhook Alert Fire", DefaultWebhookAlertFireTemplate},
		{"Webhook Alert Clear", DefaultWebhookAlertClearTemplate},
		{"Webhook Reminder", DefaultWebhookReminderTemplate},
	}

	for _, tt := range jsonTemplates {
		t.Run(tt.name, func(t *testing.T) {
			result, err := renderer.RenderJSON(tt.template, payload, "")
			if err != nil {
				t.Errorf("RenderJSON(%s) with special chars failed: %v", tt.name, err)
			}
			if result == "" {
				t.Errorf("RenderJSON(%s) produced empty result", tt.name)
			}
		})
	}
}

func TestJsonEscapeString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"plain text", "hello world", "hello world"},
		{"double quotes", `say "hello"`, `say \"hello\"`},
		{"backslash", `C:\path`, `C:\\path`},
		{"newline", "line1\nline2", `line1\nline2`},
		{"tab", "col1\tcol2", `col1\tcol2`},
		{"backtick", "`code`", "`code`"},
		{"mixed special", "error: \"fail\"\nnext", `error: \"fail\"\nnext`},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := jsonEscapeString(tt.input)
			if result != tt.expected {
				t.Errorf("jsonEscapeString(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestTemplateRenderer_Render_AllPayloadFields(t *testing.T) {
	renderer := NewTemplateRenderer()

	metricName := "connections"
	metricValue := 150.0
	thresholdValue := 100.0
	operator := ">="
	dbName := "maindb"
	duration := "2h 30m"
	clearedAt := time.Now()

	payload := &database.NotificationPayload{
		AlertID:          42,
		AlertType:        "threshold",
		AlertTitle:       "Connection Limit",
		AlertDescription: "Too many connections",
		Severity:         "critical",
		Status:           "cleared",
		TriggeredAt:      time.Now().Add(-3 * time.Hour),
		ClearedAt:        &clearedAt,
		MetricName:       &metricName,
		MetricValue:      &metricValue,
		ThresholdValue:   &thresholdValue,
		Operator:         &operator,
		ConnectionID:     5,
		ServerName:       "primary",
		ServerHost:       "db.example.com",
		ServerPort:       5433,
		DatabaseName:     &dbName,
		NotificationType: "alert_clear",
		ReminderCount:    0,
		Duration:         &duration,
		Timestamp:        time.Now(),
	}

	template := `ID:{{.AlertID}} Type:{{.AlertType}} Title:{{.AlertTitle}} Desc:{{.AlertDescription}} ` +
		`Sev:{{.Severity}} Status:{{.Status}} Conn:{{.ConnectionID}} Server:{{.ServerName}} ` +
		`Host:{{.ServerHost}} Port:{{.ServerPort}} NotifType:{{.NotificationType}} ` +
		`Remind:{{.ReminderCount}} Metric:{{.MetricName}} Val:{{.MetricValue}} ` +
		`Thresh:{{.ThresholdValue}} Op:{{.Operator}} DB:{{.DatabaseName}}`

	result, err := renderer.Render(template, payload, "")
	if err != nil {
		t.Errorf("Render() unexpected error: %v", err)
	}

	// Verify key fields are present
	if !strings.Contains(result, "ID:42") {
		t.Error("Result should contain AlertID")
	}
	if !strings.Contains(result, "Title:Connection Limit") {
		t.Error("Result should contain AlertTitle")
	}
	if !strings.Contains(result, "Sev:critical") {
		t.Error("Result should contain Severity")
	}
	if !strings.Contains(result, "DB:maindb") {
		t.Error("Result should contain DatabaseName")
	}
}
