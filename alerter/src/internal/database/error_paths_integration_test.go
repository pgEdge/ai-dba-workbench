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
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newClosedPoolDatastore returns a Datastore wrapping a pool that has
// been immediately closed. Every Query/Exec/QueryRow on this datastore
// returns "closed pool" - exactly the error path that defensive code
// in the query helpers wraps and returns. This is the lightest-weight
// way to drive coverage of the post-Query error-handling branches
// without standing up a mock of the pgxpool interface.
func newClosedPoolDatastore(t *testing.T) (*Datastore, func()) {
	t.Helper()
	if os.Getenv("SKIP_DB_TESTS") != "" {
		t.Skip("Skipping database test (SKIP_DB_TESTS is set)")
	}
	connStr := os.Getenv("TEST_AI_WORKBENCH_SERVER")
	if connStr == "" {
		t.Skip("TEST_AI_WORKBENCH_SERVER not set, skipping closed-pool error test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("Could not connect: %v", err)
	}
	pool.Close()
	return &Datastore{pool: pool, config: nil}, func() {}
}

// TestQueriesReturnErrorOnClosedPool exercises the post-Query/Exec
// error-handling branches in queries.go. Every error wrapper in the
// file should fire when the pool is closed.
func TestQueriesReturnErrorOnClosedPool(t *testing.T) {
	ds, cleanup := newClosedPoolDatastore(t)
	defer cleanup()

	ctx := context.Background()

	if _, err := ds.GetMonitoredConnectionErrors(ctx); err == nil {
		t.Errorf("GetMonitoredConnectionErrors should error on closed pool")
	}
	if _, _, _, err := ds.GetActiveConnectionAlert(ctx, 1); err == nil {
		t.Errorf("GetActiveConnectionAlert should error on closed pool")
	}
	if _, err := ds.CreateConnectionAlert(ctx, 1, "n", "msg"); err == nil {
		t.Errorf("CreateConnectionAlert should error on closed pool")
	}
	if err := ds.UpdateConnectionAlertDescription(ctx, 1, "x"); err == nil {
		t.Errorf("UpdateConnectionAlertDescription should error on closed pool")
	}
	if _, err := ds.GetAlerterSettings(ctx); err == nil {
		t.Errorf("GetAlerterSettings should error on closed pool")
	}
	if _, err := ds.GetEnabledAlertRules(ctx); err == nil {
		t.Errorf("GetEnabledAlertRules should error on closed pool")
	}
	thr, op, sev, en := ds.GetEffectiveThreshold(ctx, 1, 1, nil)
	if thr != 0 || op != "" || sev != "" || en {
		t.Errorf("GetEffectiveThreshold should return zero values on closed pool")
	}
	if _, err := ds.IsBlackoutActive(ctx, nil, nil); err == nil {
		t.Errorf("IsBlackoutActive should error on closed pool")
	}
	if _, err := ds.DeleteOldAlerts(ctx, time.Now()); err == nil {
		t.Errorf("DeleteOldAlerts should error on closed pool")
	}
	if _, err := ds.DeleteOldAnomalyCandidates(ctx, time.Now()); err == nil {
		t.Errorf("DeleteOldAnomalyCandidates should error on closed pool")
	}
	if _, err := ds.GetProbeAvailability(ctx, 1, "x"); err == nil {
		t.Errorf("GetProbeAvailability should error on closed pool")
	}
	if _, err := ds.GetEnabledBlackoutSchedules(ctx); err == nil {
		t.Errorf("GetEnabledBlackoutSchedules should error on closed pool")
	}
	if err := ds.CreateBlackout(ctx, &Blackout{Scope: "estate"}); err == nil {
		t.Errorf("CreateBlackout should error on closed pool")
	}
	if _, err := ds.GetActiveConnections(ctx); err == nil {
		t.Errorf("GetActiveConnections should error on closed pool")
	}
	if _, err := ds.GetProbeStalenessByConnection(ctx); err == nil {
		t.Errorf("GetProbeStalenessByConnection should error on closed pool")
	}
	if _, err := ds.GetAlertRuleByName(ctx, "x"); err == nil {
		t.Errorf("GetAlertRuleByName should error on closed pool")
	}
	if _, err := ds.GetClusterPeers(ctx, 1); err == nil {
		t.Errorf("GetClusterPeers should error on closed pool")
	}
}

// TestAlertQueriesReturnErrorOnClosedPool exercises the error
// branches in alert_queries.go.
func TestAlertQueriesReturnErrorOnClosedPool(t *testing.T) {
	ds, cleanup := newClosedPoolDatastore(t)
	defer cleanup()

	ctx := context.Background()

	if _, err := ds.GetActiveThresholdAlert(ctx, 1, 1, nil); err == nil {
		t.Errorf("GetActiveThresholdAlert should error on closed pool")
	}
	if _, err := ds.GetActiveAnomalyAlert(ctx, "x", 1, nil); err == nil {
		t.Errorf("GetActiveAnomalyAlert should error on closed pool")
	}
	if _, err := ds.GetRecentlyClearedAlert(ctx, 1, 1, nil, time.Second); err == nil {
		t.Errorf("GetRecentlyClearedAlert should error on closed pool")
	}
	if _, err := ds.GetReevaluationSuppressedAlert(ctx, "x", 1, nil, time.Second); err == nil {
		t.Errorf("GetReevaluationSuppressedAlert should error on closed pool")
	}
	if _, err := ds.GetFalsePositiveSuppressedAlert(ctx, "x", 1, nil, time.Second); err == nil {
		t.Errorf("GetFalsePositiveSuppressedAlert should error on closed pool")
	}
	if err := ds.UpdateAlertValues(ctx, 1, 1, 1, ">", "warning"); err == nil {
		t.Errorf("UpdateAlertValues should error on closed pool")
	}
	a := &Alert{AlertType: "threshold", ConnectionID: 1, Severity: "warning", Title: "t", Description: "d", Status: "active", TriggeredAt: time.Now()}
	if err := ds.CreateAlert(ctx, a); err == nil {
		t.Errorf("CreateAlert should error on closed pool")
	}
	if _, err := ds.GetActiveAlerts(ctx); err == nil {
		t.Errorf("GetActiveAlerts should error on closed pool")
	}
	if _, err := ds.GetAlert(ctx, 1); err == nil {
		t.Errorf("GetAlert should error on closed pool")
	}
	if err := ds.ClearAlert(ctx, 1); err == nil {
		t.Errorf("ClearAlert should error on closed pool")
	}
	if err := ds.ReactivateAlert(ctx, 1); err == nil {
		t.Errorf("ReactivateAlert should error on closed pool")
	}
	if _, err := ds.GetAlertsByCluster(ctx, 1); err == nil {
		t.Errorf("GetAlertsByCluster should error on closed pool")
	}
	if _, err := ds.GetAlertsByConnection(ctx, 1); err == nil {
		t.Errorf("GetAlertsByConnection should error on closed pool")
	}
	if err := ds.UpdateAlertReevaluation(ctx, 1); err == nil {
		t.Errorf("UpdateAlertReevaluation should error on closed pool")
	}
}

// TestAnomalyQueriesReturnErrorOnClosedPool exercises the error
// branches in anomaly_queries.go.
func TestAnomalyQueriesReturnErrorOnClosedPool(t *testing.T) {
	ds, cleanup := newClosedPoolDatastore(t)
	defer cleanup()

	ctx := context.Background()

	c := &AnomalyCandidate{ConnectionID: 1, MetricName: "x", DetectedAt: time.Now(), Context: "{}"}
	if err := ds.CreateAnomalyCandidate(ctx, c); err == nil {
		t.Errorf("CreateAnomalyCandidate should error on closed pool")
	}
	if _, err := ds.GetUnprocessedAnomalyCandidates(ctx, 10); err == nil {
		t.Errorf("GetUnprocessedAnomalyCandidates should error on closed pool")
	}
	if err := ds.UpdateAnomalyCandidate(ctx, c); err == nil {
		t.Errorf("UpdateAnomalyCandidate should error on closed pool")
	}
	if err := ds.StoreAnomalyEmbedding(ctx, 1, []float32{1, 0}, "m"); err == nil {
		t.Errorf("StoreAnomalyEmbedding should error on closed pool")
	}
	if _, err := ds.FindSimilarAnomalies(ctx, []float32{1, 0}, 1, 0.5, 10); err == nil {
		t.Errorf("FindSimilarAnomalies should error on closed pool")
	}
	if _, err := ds.GetAnomalyCandidateByID(ctx, 1); err == nil {
		t.Errorf("GetAnomalyCandidateByID should error on closed pool")
	}
	if _, err := ds.GetMetricBaselines(ctx, 1, "x"); err == nil {
		t.Errorf("GetMetricBaselines should error on closed pool")
	}
	if err := ds.UpsertMetricBaseline(ctx, &MetricBaseline{ConnectionID: 1, MetricName: "x", PeriodType: "all", LastCalculated: time.Now()}); err == nil {
		t.Errorf("UpsertMetricBaseline should error on closed pool")
	}
	if _, err := ds.GetAcknowledgedAnomalyAlerts(ctx, 60, 10); err == nil {
		t.Errorf("GetAcknowledgedAnomalyAlerts should error on closed pool")
	}
	if _, err := ds.GetAcknowledgmentHistoryForMetric(ctx, "x", 1, 0, 10); err == nil {
		t.Errorf("GetAcknowledgmentHistoryForMetric should error on closed pool")
	}
}

// TestNotificationQueriesReturnErrorOnClosedPool exercises the
// error branches in notification_queries.go.
func TestNotificationQueriesReturnErrorOnClosedPool(t *testing.T) {
	ds, cleanup := newClosedPoolDatastore(t)
	defer cleanup()

	ctx := context.Background()
	owner := "tester"
	ch := &NotificationChannel{
		OwnerUsername:         &owner,
		Enabled:               true,
		ChannelType:           ChannelTypeWebhook,
		Name:                  "x",
		HTTPMethod:            "POST",
		Headers:               map[string]string{},
		SMTPPort:              587,
		SMTPUseTLS:            true,
		ReminderEnabled:       true,
		ReminderIntervalHours: 1,
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
	}
	if _, err := ds.GetNotificationChannel(ctx, 1); err == nil {
		t.Errorf("GetNotificationChannel should error on closed pool")
	}
	if _, err := ds.GetNotificationChannelsForConnection(ctx, 1); err == nil {
		t.Errorf("GetNotificationChannelsForConnection should error on closed pool")
	}
	if err := ds.CreateNotificationChannel(ctx, ch); err == nil {
		t.Errorf("CreateNotificationChannel should error on closed pool")
	}
	if err := ds.UpdateNotificationChannel(ctx, ch); err == nil {
		t.Errorf("UpdateNotificationChannel should error on closed pool")
	}
	if err := ds.DeleteNotificationChannel(ctx, 1); err == nil {
		t.Errorf("DeleteNotificationChannel should error on closed pool")
	}
	if _, err := ds.GetEmailRecipients(ctx, 1); err == nil {
		t.Errorf("GetEmailRecipients should error on closed pool")
	}
	if err := ds.CreateEmailRecipient(ctx, &EmailRecipient{}); err == nil {
		t.Errorf("CreateEmailRecipient should error on closed pool")
	}
	if err := ds.DeleteEmailRecipient(ctx, 1); err == nil {
		t.Errorf("DeleteEmailRecipient should error on closed pool")
	}
	if err := ds.LinkConnectionToChannel(ctx, &ConnectionNotificationChannel{}); err == nil {
		t.Errorf("LinkConnectionToChannel should error on closed pool")
	}
	if err := ds.UnlinkConnectionFromChannel(ctx, 1, 1); err == nil {
		t.Errorf("UnlinkConnectionFromChannel should error on closed pool")
	}
	if _, err := ds.GetConnectionChannelLinks(ctx, 1); err == nil {
		t.Errorf("GetConnectionChannelLinks should error on closed pool")
	}
	if err := ds.CreateNotificationHistory(ctx, &NotificationHistory{}); err == nil {
		t.Errorf("CreateNotificationHistory should error on closed pool")
	}
	if err := ds.UpdateNotificationHistory(ctx, &NotificationHistory{}); err == nil {
		t.Errorf("UpdateNotificationHistory should error on closed pool")
	}
	if _, err := ds.GetPendingNotifications(ctx); err == nil {
		t.Errorf("GetPendingNotifications should error on closed pool")
	}
	if _, err := ds.GetNotificationHistoryForAlert(ctx, 1); err == nil {
		t.Errorf("GetNotificationHistoryForAlert should error on closed pool")
	}
	if _, err := ds.GetReminderState(ctx, 1, 1); err == nil {
		t.Errorf("GetReminderState should error on closed pool")
	}
	if err := ds.UpsertReminderState(ctx, &NotificationReminderState{}); err == nil {
		t.Errorf("UpsertReminderState should error on closed pool")
	}
	if err := ds.DeleteReminderStatesForAlert(ctx, 1); err == nil {
		t.Errorf("DeleteReminderStatesForAlert should error on closed pool")
	}
	if _, err := ds.GetDueReminders(ctx); err == nil {
		t.Errorf("GetDueReminders should error on closed pool")
	}
	if _, _, _, err := ds.GetConnectionInfo(ctx, 1); err == nil {
		t.Errorf("GetConnectionInfo should error on closed pool")
	}
}
