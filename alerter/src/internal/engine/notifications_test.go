/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package engine

import (
	"testing"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// TestQueueNotificationNilPool tests that queueNotification handles nil pool gracefully
func TestQueueNotificationNilPool(t *testing.T) {
	engine := &Engine{
		notificationPool: nil,
	}

	alert := &database.Alert{
		ID:    1,
		Title: "Test Alert",
	}

	// Should not panic with nil pool
	engine.queueNotification(alert, database.NotificationTypeAlertFire)
}

// TestNotificationJobStruct tests the notificationJob struct
func TestNotificationJobStruct(t *testing.T) {
	alert := &database.Alert{
		ID:    42,
		Title: "Test Alert",
	}

	job := notificationJob{
		alert:    alert,
		notifTyp: database.NotificationTypeAlertFire,
	}

	if job.alert.ID != 42 {
		t.Errorf("job.alert.ID = %d, expected 42", job.alert.ID)
	}

	if job.notifTyp != database.NotificationTypeAlertFire {
		t.Errorf("job.notifTyp = %v, expected %v", job.notifTyp, database.NotificationTypeAlertFire)
	}
}

// TestNotificationJobTypes tests different notification type values
func TestNotificationJobTypes(t *testing.T) {
	tests := []struct {
		name     string
		notifTyp database.NotificationType
	}{
		{"alert fire", database.NotificationTypeAlertFire},
		{"alert clear", database.NotificationTypeAlertClear},
		{"alert reminder", database.NotificationTypeReminder},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := notificationJob{
				alert:    &database.Alert{ID: 1},
				notifTyp: tt.notifTyp,
			}

			if job.alert == nil || job.alert.ID != 1 {
				t.Error("job.alert should be set with ID 1")
			}
			if job.notifTyp != tt.notifTyp {
				t.Errorf("job.notifTyp = %v, expected %v", job.notifTyp, tt.notifTyp)
			}
		})
	}
}

// TestProcessNotificationJobNilManager tests that processNotificationJob handles nil manager
func TestProcessNotificationJobNilManager(t *testing.T) {
	engine := &Engine{
		notificationMgr: nil,
	}

	job := notificationJob{
		alert:    &database.Alert{ID: 1},
		notifTyp: database.NotificationTypeAlertFire,
	}

	// Should not panic with nil notification manager
	engine.processNotificationJob(job)
}
