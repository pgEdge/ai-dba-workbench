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
	"fmt"
)

// scanAcknowledgedAnomalyAlert scans all 15 fields from a query row into an
// AcknowledgedAnomalyAlert struct. This helper consolidates the duplicated scan
// pattern across acknowledged anomaly alert queries.
func scanAcknowledgedAnomalyAlert(scanner interface{ Scan(dest ...any) error }, a *AcknowledgedAnomalyAlert) error {
	return scanner.Scan(
		&a.ID, &a.ConnectionID, &a.Title, &a.Severity, &a.MetricName,
		&a.MetricValue, &a.ZScore, &a.AnomalyDetails, &a.TriggeredAt,
		&a.AckMessage, &a.FalsePositive, &a.AcknowledgedBy,
		&a.AcknowledgedAt, &a.LastReevaluatedAt, &a.ReevaluationCount)
}

// float32SliceToVectorString converts a []float32 to a PostgreSQL vector string format
func float32SliceToVectorString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}

	// Pre-allocate approximate size: "[" + (number * ~12 chars) + "]"
	result := make([]byte, 0, 1+len(v)*12+1)
	result = append(result, '[')

	for i, val := range v {
		if i > 0 {
			result = append(result, ',')
		}
		result = append(result, fmt.Sprintf("%g", val)...)
	}

	result = append(result, ']')
	return string(result)
}

// CreateAnomalyCandidate creates a new anomaly candidate for tier 2/3 processing
func (d *Datastore) CreateAnomalyCandidate(ctx context.Context, c *AnomalyCandidate) error {
	return d.pool.QueryRow(ctx, `
		INSERT INTO anomaly_candidates (connection_id, database_name, metric_name,
		                                metric_value, z_score, detected_at, context,
		                                tier1_pass)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id
	`, c.ConnectionID, c.DatabaseName, c.MetricName, c.MetricValue,
		c.ZScore, c.DetectedAt, c.Context, c.Tier1Pass).Scan(&c.ID)
}

// GetUnprocessedAnomalyCandidates retrieves candidates that need tier 2/3 processing
func (d *Datastore) GetUnprocessedAnomalyCandidates(ctx context.Context, limit int) ([]*AnomalyCandidate, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, connection_id, database_name, metric_name, metric_value,
		       z_score, detected_at, context, tier1_pass, tier2_score, tier2_pass,
		       tier3_result, tier3_pass, tier3_error, final_decision, alert_id,
		       processed_at
		FROM anomaly_candidates
		WHERE processed_at IS NULL AND tier1_pass = true
		ORDER BY detected_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get unprocessed candidates: %w", err)
	}
	defer rows.Close()

	var candidates []*AnomalyCandidate
	for rows.Next() {
		var c AnomalyCandidate
		err := rows.Scan(&c.ID, &c.ConnectionID, &c.DatabaseName, &c.MetricName,
			&c.MetricValue, &c.ZScore, &c.DetectedAt, &c.Context, &c.Tier1Pass,
			&c.Tier2Score, &c.Tier2Pass, &c.Tier3Result, &c.Tier3Pass,
			&c.Tier3Error, &c.FinalDecision, &c.AlertID, &c.ProcessedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan anomaly candidate: %w", err)
		}
		candidates = append(candidates, &c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return candidates, nil
}

// UpdateAnomalyCandidate updates a candidate with tier 2/3 results
func (d *Datastore) UpdateAnomalyCandidate(ctx context.Context, c *AnomalyCandidate) error {
	_, err := d.pool.Exec(ctx, `
		UPDATE anomaly_candidates
		SET tier2_score = $2, tier2_pass = $3, tier3_result = $4, tier3_pass = $5,
		    tier3_error = $6, final_decision = $7, alert_id = $8, processed_at = $9
		WHERE id = $1
	`, c.ID, c.Tier2Score, c.Tier2Pass, c.Tier3Result, c.Tier3Pass,
		c.Tier3Error, c.FinalDecision, c.AlertID, c.ProcessedAt)
	return err
}

// SimilarAnomaly is defined in types.go

// StoreAnomalyEmbedding stores an embedding for an anomaly candidate
func (d *Datastore) StoreAnomalyEmbedding(ctx context.Context, candidateID int64, embedding []float32, modelName string) error {
	// Convert []float32 to PostgreSQL vector format
	vectorStr := float32SliceToVectorString(embedding)

	_, err := d.pool.Exec(ctx, `
		INSERT INTO anomaly_embeddings (candidate_id, embedding, model_name)
		VALUES ($1, $2::vector, $3)
		ON CONFLICT (candidate_id) DO UPDATE
		SET embedding = $2::vector, model_name = $3, created_at = CURRENT_TIMESTAMP
	`, candidateID, vectorStr, modelName)

	if err != nil {
		return fmt.Errorf("failed to store embedding: %w", err)
	}

	// Update the anomaly_candidates.embedding_id reference
	_, err = d.pool.Exec(ctx, `
		UPDATE anomaly_candidates
		SET embedding_id = (SELECT id FROM anomaly_embeddings WHERE candidate_id = $1)
		WHERE id = $1
	`, candidateID)

	if err != nil {
		return fmt.Errorf("failed to update embedding reference: %w", err)
	}

	return nil
}

// FindSimilarAnomalies finds past anomalies similar to the given embedding
// Returns candidates with similarity scores above threshold, excluding the current candidate
func (d *Datastore) FindSimilarAnomalies(ctx context.Context, embedding []float32, excludeCandidateID int64, threshold float64, limit int) ([]*SimilarAnomaly, error) {
	// Convert []float32 to PostgreSQL vector format
	vectorStr := float32SliceToVectorString(embedding)

	rows, err := d.pool.Query(ctx, `
		SELECT
			c.id,
			1 - (e.embedding <=> $1::vector) as similarity,
			c.final_decision,
			c.metric_name,
			c.context
		FROM anomaly_embeddings e
		JOIN anomaly_candidates c ON e.candidate_id = c.id
		WHERE c.id != $2
		  AND c.processed_at IS NOT NULL
		  AND c.final_decision IS NOT NULL
		ORDER BY e.embedding <=> $1::vector
		LIMIT $3
	`, vectorStr, excludeCandidateID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to find similar anomalies: %w", err)
	}
	defer rows.Close()

	var results []*SimilarAnomaly
	for rows.Next() {
		var sa SimilarAnomaly
		err := rows.Scan(&sa.CandidateID, &sa.Similarity, &sa.FinalDecision, &sa.MetricName, &sa.Context)
		if err != nil {
			return nil, fmt.Errorf("failed to scan similar anomaly: %w", err)
		}
		// Apply similarity threshold filter in Go so the SQL query
		// can use the HNSW index via ORDER BY <=> without a WHERE
		// clause on the computed similarity value.
		if sa.Similarity >= threshold {
			results = append(results, &sa)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return results, nil
}

// GetAnomalyCandidateByID retrieves an anomaly candidate by ID
func (d *Datastore) GetAnomalyCandidateByID(ctx context.Context, id int64) (*AnomalyCandidate, error) {
	var c AnomalyCandidate
	err := d.pool.QueryRow(ctx, `
		SELECT id, connection_id, database_name, metric_name, metric_value,
		       z_score, detected_at, context, tier1_pass, tier2_score, tier2_pass,
		       tier3_result, tier3_pass, tier3_error, final_decision, alert_id,
		       processed_at
		FROM anomaly_candidates
		WHERE id = $1
	`, id).Scan(&c.ID, &c.ConnectionID, &c.DatabaseName, &c.MetricName,
		&c.MetricValue, &c.ZScore, &c.DetectedAt, &c.Context, &c.Tier1Pass,
		&c.Tier2Score, &c.Tier2Pass, &c.Tier3Result, &c.Tier3Pass,
		&c.Tier3Error, &c.FinalDecision, &c.AlertID, &c.ProcessedAt)

	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetMetricBaselines retrieves baselines for a metric on a connection
func (d *Datastore) GetMetricBaselines(ctx context.Context, connectionID int, metricName string) ([]*MetricBaseline, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, connection_id, database_name, metric_name, period_type,
		       day_of_week, hour_of_day, mean, stddev, min, max,
		       sample_count, last_calculated
		FROM metric_baselines
		WHERE connection_id = $1 AND metric_name = $2
		ORDER BY period_type, day_of_week, hour_of_day
	`, connectionID, metricName)
	if err != nil {
		return nil, fmt.Errorf("failed to get metric baselines: %w", err)
	}
	defer rows.Close()

	var baselines []*MetricBaseline
	for rows.Next() {
		var b MetricBaseline
		err := rows.Scan(&b.ID, &b.ConnectionID, &b.DatabaseName, &b.MetricName,
			&b.PeriodType, &b.DayOfWeek, &b.HourOfDay, &b.Mean, &b.StdDev,
			&b.Min, &b.Max, &b.SampleCount, &b.LastCalculated)
		if err != nil {
			return nil, fmt.Errorf("failed to scan metric baseline: %w", err)
		}
		baselines = append(baselines, &b)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return baselines, nil
}

// UpsertMetricBaseline inserts or updates a metric baseline
func (d *Datastore) UpsertMetricBaseline(ctx context.Context, b *MetricBaseline) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO metric_baselines (connection_id, database_name, metric_name,
		                              period_type, day_of_week, hour_of_day,
		                              mean, stddev, min, max, sample_count,
		                              last_calculated)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (connection_id, COALESCE(database_name, ''), metric_name,
		             period_type, COALESCE(day_of_week, -1), COALESCE(hour_of_day, -1))
		DO UPDATE SET mean = $7, stddev = $8, min = $9, max = $10,
		              sample_count = $11, last_calculated = $12
	`, b.ConnectionID, b.DatabaseName, b.MetricName, b.PeriodType,
		b.DayOfWeek, b.HourOfDay, b.Mean, b.StdDev, b.Min, b.Max,
		b.SampleCount, b.LastCalculated)
	return err
}

// GetAcknowledgedAnomalyAlerts retrieves acknowledged anomaly alerts that are
// due for re-evaluation. An alert is due if it has never been re-evaluated or
// if the last re-evaluation was longer ago than the specified interval.
func (d *Datastore) GetAcknowledgedAnomalyAlerts(ctx context.Context, intervalSeconds int, limit int) ([]*AcknowledgedAnomalyAlert, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT a.id, a.connection_id, a.title, a.severity, a.metric_name,
		       a.metric_value, a.anomaly_score, a.anomaly_details, a.triggered_at,
		       ack.message, ack.false_positive, ack.acknowledged_by,
		       ack.acknowledged_at, a.last_reevaluated_at, a.reevaluation_count
		FROM alerts a
		LEFT JOIN LATERAL (
		    SELECT message, false_positive, acknowledged_by, acknowledged_at
		    FROM alert_acknowledgments
		    WHERE alert_id = a.id
		    ORDER BY acknowledged_at DESC
		    LIMIT 1
		) ack ON true
		WHERE a.status = 'acknowledged' AND a.alert_type = 'anomaly'
		  AND (a.last_reevaluated_at IS NULL
		       OR a.last_reevaluated_at < NOW() - INTERVAL '1 second' * $1)
		ORDER BY a.triggered_at ASC
		LIMIT $2
	`, intervalSeconds, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get acknowledged anomaly alerts: %w", err)
	}
	defer rows.Close()

	var alerts []*AcknowledgedAnomalyAlert
	for rows.Next() {
		var a AcknowledgedAnomalyAlert
		if err := scanAcknowledgedAnomalyAlert(rows, &a); err != nil {
			return nil, fmt.Errorf("failed to scan acknowledged anomaly alert: %w", err)
		}
		alerts = append(alerts, &a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return alerts, nil
}

// GetAcknowledgmentHistoryForMetric retrieves past acknowledgements for the
// same metric and connection across different alert instances. This reveals
// recurring patterns useful for LLM re-evaluation context.
func (d *Datastore) GetAcknowledgmentHistoryForMetric(ctx context.Context, metricName string, connectionID int, excludeAlertID int64, limit int) ([]*AcknowledgedAnomalyAlert, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT a.id, a.connection_id, a.title, a.severity, a.metric_name,
		       a.metric_value, a.anomaly_score, a.anomaly_details, a.triggered_at,
		       ack.message, ack.false_positive, ack.acknowledged_by,
		       ack.acknowledged_at, a.last_reevaluated_at, a.reevaluation_count
		FROM alerts a
		JOIN alert_acknowledgments ack ON ack.alert_id = a.id
		WHERE a.metric_name = $1 AND a.connection_id = $2
		  AND a.id != $3 AND a.alert_type = 'anomaly'
		ORDER BY ack.acknowledged_at DESC
		LIMIT $4
	`, metricName, connectionID, excludeAlertID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get acknowledgment history: %w", err)
	}
	defer rows.Close()

	var alerts []*AcknowledgedAnomalyAlert
	for rows.Next() {
		var a AcknowledgedAnomalyAlert
		if err := scanAcknowledgedAnomalyAlert(rows, &a); err != nil {
			return nil, fmt.Errorf("failed to scan acknowledgment history: %w", err)
		}
		alerts = append(alerts, &a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return alerts, nil
}
