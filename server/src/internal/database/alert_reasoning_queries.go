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
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Tier3ReasoningKey is the key of alerts.anomaly_details under which the
// server stores the reasoning it generates on demand for an alert whose
// Tier 3 verdict came from a decision model, which returns a probability
// rather than text (GitHub issue #626). The alerter writes
// anomaly_details when it raises an anomaly alert and never rewrites it,
// so the key is the server's alone.
const Tier3ReasoningKey = "tier3_reasoning"

// AlertTier3Info is what the server needs to explain an alert's Tier 3
// decision: the alert's own fields, the reasoning already stored on it,
// and the values and Tier 3 outcome of the anomaly candidate that raised
// it. The candidate fields are nil when no candidate links to the alert,
// as for a threshold or system alert, or once retention has deleted the
// candidate.
type AlertTier3Info struct {
	AlertID        int64
	AlertType      string
	ConnectionID   *int
	MetricName     *string
	TriggeredAt    time.Time
	AnomalyDetails *string
	// StoredReasoning is the reasoning a previous request generated,
	// read from anomaly_details under Tier3ReasoningKey.
	StoredReasoning *string

	// CandidateMetricValue and CandidateZScore are the values Tier 3
	// judged. The alert's own metric_value and anomaly_score are
	// refreshed while the alert stays open, so they can differ.
	CandidateMetricValue *float64
	CandidateZScore      *float64

	Tier3Source   *string
	Tier3Model    *string
	Tier3Result   *string
	Tier3Pass     *bool
	Tier3Error    *string
	FinalDecision *string
}

// GetAlertTier3Info returns the Tier 3 information for an alert, with
// the anomaly candidate that raised it. It returns ErrAlertNotFound when
// no alert has the ID.
//
// More than one candidate can link to an alert: the alerter links a
// candidate it skips because the alert is still open to that alert,
// with every Tier 3 column NULL. Every candidate that reached Tier 3,
// including one the fail-safe passed, has tier3_pass set, so the query
// takes the oldest of those, which is the one that raised the alert,
// and falls back to the oldest candidate when none reached Tier 3.
func (d *Datastore) GetAlertTier3Info(ctx context.Context, alertID int64) (*AlertTier3Info, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var info AlertTier3Info
	err := d.pool.QueryRow(ctx, `
		SELECT a.id, a.alert_type, a.connection_id,
		       a.metric_name, a.triggered_at,
		       a.anomaly_details::text, a.anomaly_details ->> $2,
		       c.metric_value::float8, c.z_score::float8,
		       c.tier3_source, c.tier3_model, c.tier3_result, c.tier3_pass,
		       c.tier3_error, c.final_decision
		FROM alerts a
		LEFT JOIN LATERAL (
			SELECT metric_value, z_score,
			       tier3_source, tier3_model, tier3_result, tier3_pass,
			       tier3_error, final_decision
			FROM anomaly_candidates
			WHERE alert_id = a.id
			ORDER BY (tier3_pass IS NOT NULL) DESC, id ASC
			LIMIT 1
		) c ON true
		WHERE a.id = $1
	`, alertID, Tier3ReasoningKey).Scan(
		&info.AlertID, &info.AlertType, &info.ConnectionID,
		&info.MetricName, &info.TriggeredAt,
		&info.AnomalyDetails, &info.StoredReasoning,
		&info.CandidateMetricValue, &info.CandidateZScore,
		&info.Tier3Source, &info.Tier3Model, &info.Tier3Result, &info.Tier3Pass,
		&info.Tier3Error, &info.FinalDecision,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: alert %d", ErrAlertNotFound, alertID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get Tier 3 information for alert %d: %w", alertID, err)
	}
	return &info, nil
}

// SaveAlertTier3Reasoning stores reasoning in the alert's
// anomaly_details under Tier3ReasoningKey, unless reasoning is already
// stored there, and returns the reasoning the alert holds afterwards.
// The write is conditional, so when two requests generate reasoning for
// the same alert at once the first to commit wins and the second gets
// the first's text back rather than overwriting it. It returns
// ErrAlertNotFound when the alert has gone.
//
// It takes only the read lock: the mutex guards the Datastore's own
// fields, which this method does not change, and PostgreSQL's row lock
// on the conditional UPDATE makes concurrent writers take turns, so an
// exclusive lock would only stall every other reader for the save.
func (d *Datastore) SaveAlertTier3Reasoning(ctx context.Context, alertID int64, reasoning string) (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var stored string
	err := d.pool.QueryRow(ctx, `
		UPDATE alerts
		SET anomaly_details = COALESCE(anomaly_details, '{}'::jsonb)
		                      || jsonb_build_object($2::text, $3::text)
		WHERE id = $1
		  AND NOT (COALESCE(anomaly_details, '{}'::jsonb) ? $2::text)
		RETURNING anomaly_details ->> $2::text
	`, alertID, Tier3ReasoningKey, reasoning).Scan(&stored)
	if err == nil {
		return stored, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("failed to save Tier 3 reasoning for alert %d: %w", alertID, err)
	}

	// No row was updated: either the alert has gone, or another request
	// stored its reasoning first. The UPDATE waited for that request's
	// row lock, so its text is visible now.
	var existing *string
	err = d.pool.QueryRow(ctx,
		"SELECT anomaly_details ->> $2::text FROM alerts WHERE id = $1",
		alertID, Tier3ReasoningKey).Scan(&existing)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("%w: alert %d", ErrAlertNotFound, alertID)
	}
	if err != nil {
		return "", fmt.Errorf("failed to read Tier 3 reasoning for alert %d: %w", alertID, err)
	}
	if existing == nil {
		// The key exists with a JSON null, which nothing writes; report
		// it rather than loop.
		return "", fmt.Errorf("alert %d holds a null Tier 3 reasoning", alertID)
	}
	return *existing, nil
}
