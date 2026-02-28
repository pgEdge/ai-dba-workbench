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
	"context"
	"time"

	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// notificationJob represents a notification to be sent by a worker
type notificationJob struct {
	alert    *database.Alert
	notifTyp database.NotificationType
}

// processNotificationJob handles a single notification job
func (e *Engine) processNotificationJob(job notificationJob) {
	if e.notificationMgr == nil {
		return
	}

	notifCtx, cancel := context.WithTimeout(e.ctx, NotificationTimeout)
	defer cancel()

	if err := e.notificationMgr.SendAlertNotification(notifCtx, job.alert, job.notifTyp); err != nil {
		e.log("ERROR: Failed to send notification: %v", err)
	}

	// For clear notifications, also delete reminder states
	if job.notifTyp == database.NotificationTypeAlertClear {
		if err := e.datastore.DeleteReminderStatesForAlert(notifCtx, job.alert.ID); err != nil {
			e.log("ERROR: Failed to delete reminder states for alert %d: %v", job.alert.ID, err)
		}
	}
}

// queueNotification queues a notification job for async processing by the worker pool
func (e *Engine) queueNotification(alert *database.Alert, notifTyp database.NotificationType) {
	if e.notificationPool == nil {
		return
	}
	if !e.notificationPool.Submit(notificationJob{alert: alert, notifTyp: notifTyp}) {
		// Queue full or pool stopped - log warning but don't block
		e.log("WARNING: Notification queue full, dropping notification for alert %d", alert.ID)
	}
}

// runNotificationWorker processes pending and retry notifications
func (e *Engine) runNotificationWorker(ctx context.Context) {
	cfg := e.getConfig()
	interval := time.Duration(cfg.Notifications.ProcessIntervalSeconds) * time.Second
	if interval == 0 {
		interval = DefaultNotificationProcessInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	e.log("Notification worker started (interval: %v)", interval)

	for {
		select {
		case <-ctx.Done():
			e.log("Notification worker stopping")
			return
		case <-ticker.C:
			if e.notificationMgr != nil {
				if err := e.notificationMgr.ProcessPendingNotifications(ctx); err != nil {
					e.log("ERROR: Failed to process pending notifications: %v", err)
				}
			}

			newCfg := e.getConfig()
			newInterval := time.Duration(newCfg.Notifications.ProcessIntervalSeconds) * time.Second
			if newInterval == 0 {
				newInterval = DefaultNotificationProcessInterval
			}
			if newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
				e.log("Notification worker interval updated to %v", interval)
			}
		}
	}
}

// runReminderWorker sends periodic reminder notifications for active alerts
func (e *Engine) runReminderWorker(ctx context.Context) {
	cfg := e.getConfig()
	interval := time.Duration(cfg.Notifications.ReminderCheckIntervalMinutes) * time.Minute
	if interval == 0 {
		interval = DefaultReminderCheckInterval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	e.log("Reminder worker started (interval: %v)", interval)

	// Process reminders immediately on start
	if e.notificationMgr != nil {
		if err := e.notificationMgr.ProcessReminders(ctx); err != nil {
			e.log("ERROR: Failed to process reminders: %v", err)
		}
	}

	for {
		select {
		case <-ctx.Done():
			e.log("Reminder worker stopping")
			return
		case <-ticker.C:
			if e.notificationMgr != nil {
				if err := e.notificationMgr.ProcessReminders(ctx); err != nil {
					e.log("ERROR: Failed to process reminders: %v", err)
				}
			}

			newCfg := e.getConfig()
			newInterval := time.Duration(newCfg.Notifications.ReminderCheckIntervalMinutes) * time.Minute
			if newInterval == 0 {
				newInterval = DefaultReminderCheckInterval
			}
			if newInterval != interval {
				interval = newInterval
				ticker.Reset(interval)
				e.log("Reminder worker interval updated to %v", interval)
			}
		}
	}
}
