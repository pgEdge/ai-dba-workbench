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
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/alerter/internal/config"
	"github.com/pgedge/ai-workbench/alerter/internal/database"
)

// These tests cover the re-evaluation worker's reuse of a "keep" answer
// while the prompt inputs are unchanged (GitHub issue #575).

// scriptedReasoningProvider returns a fixed response or error and counts
// the calls it receives.
type scriptedReasoningProvider struct {
	mu       sync.Mutex
	response string
	err      error
	model    string
	calls    int
}

func (p *scriptedReasoningProvider) Classify(_ context.Context, _ string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.response, p.err
}

func (p *scriptedReasoningProvider) ModelName() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.model
}

func (p *scriptedReasoningProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *scriptedReasoningProvider) set(response string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.response = response
	p.err = err
}

const (
	keepResponse  = `{"decision": "keep", "confidence": 0.8, "reasoning": "still relevant"}`
	clearResponse = `{"decision": "clear", "confidence": 0.9, "reasoning": "explained by the user"}`
)

// nodeRoleSchema provides the metrics.pg_node_role table GetClusterPeers
// reads, so the cluster context query succeeds as it does in production.
const nodeRoleSchema = `
CREATE SCHEMA IF NOT EXISTS metrics;
DROP TABLE IF EXISTS metrics.pg_node_role;
CREATE TABLE metrics.pg_node_role (
    connection_id INTEGER NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL,
    primary_role TEXT
);
`

// reevaluationTestEnv holds the engine, pool and provider for one test.
type reevaluationTestEnv struct {
	engine   *Engine
	pool     *pgxpool.Pool
	provider *scriptedReasoningProvider
	connID   int
}

func newReevaluationTestEnv(t *testing.T) *reevaluationTestEnv {
	t.Helper()
	ds, pool, cleanup := newEngineIntegrationTestEnv(t)
	t.Cleanup(cleanup)

	ctx := context.Background()
	if _, err := pool.Exec(ctx, nodeRoleSchema); err != nil {
		t.Fatalf("create metrics.pg_node_role: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(),
			`DROP SCHEMA IF EXISTS metrics CASCADE`); err != nil {
			t.Logf("drop metrics schema: %v", err)
		}
	})

	cfg := config.NewConfig()
	cfg.Anomaly.Reevaluation.Enabled = true
	cfg.Anomaly.Reevaluation.IntervalSeconds = 300
	cfg.Anomaly.Reevaluation.MaxPerCycle = 10

	provider := &scriptedReasoningProvider{response: keepResponse, model: "model-a"}
	return &reevaluationTestEnv{
		engine:   &Engine{config: cfg, datastore: ds, reasoningProvider: provider},
		pool:     pool,
		provider: provider,
		connID:   insertTestConnection(t, pool, "reeval-conn"),
	}
}

// insertAcknowledgedAnomaly stores an acknowledged anomaly alert with a
// value and baseline, and its acknowledgement, and returns its ID.
func (env *reevaluationTestEnv) insertAcknowledgedAnomaly(t *testing.T, metric *string) int64 {
	t.Helper()
	ctx := context.Background()
	var id int64
	if err := env.pool.QueryRow(ctx, `
		INSERT INTO alerts (alert_type, connection_id, severity, title, description,
		    status, metric_name, metric_value, anomaly_score, anomaly_details, triggered_at)
		VALUES ('anomaly', $1, 'warning', 'Anomaly', 'd', 'acknowledged', $2, 42.5, 4.2,
		    '{"z_score": 4.2, "baseline_context": {"baseline_mean": 10.0, "baseline_stddev": 2.0}}',
		    NOW() - INTERVAL '2 hours')
		RETURNING id
	`, env.connID, metric).Scan(&id); err != nil {
		t.Fatalf("insert acknowledged anomaly: %v", err)
	}
	insertTestAcknowledgment(t, env.pool, id, "acknowledge", false)
	return id
}

// makeDue backdates every alert's last re-evaluation so the next cycle
// picks it up again.
func (env *reevaluationTestEnv) makeDue(t *testing.T) {
	t.Helper()
	if _, err := env.pool.Exec(context.Background(), `
		UPDATE alerts SET last_reevaluated_at = NOW() - INTERVAL '1 hour'
		WHERE last_reevaluated_at IS NOT NULL
	`); err != nil {
		t.Fatalf("backdate last_reevaluated_at: %v", err)
	}
}

// state returns the stored re-evaluation count, fingerprint and status.
func (env *reevaluationTestEnv) state(t *testing.T, id int64) (int, *string, string) {
	t.Helper()
	var count int
	var fp *string
	var status string
	if err := env.pool.QueryRow(context.Background(), `
		SELECT reevaluation_count, reevaluation_fingerprint, status
		FROM alerts WHERE id = $1
	`, id).Scan(&count, &fp, &status); err != nil {
		t.Fatalf("read alert %d: %v", id, err)
	}
	return count, fp, status
}

func (env *reevaluationTestEnv) run() {
	env.engine.reevaluateAcknowledgedAlerts(context.Background())
}

// TestReevaluationSkipsUnchangedKeep checks that a "keep" is reused while
// nothing the prompt shows has changed, and that the LLM is asked again
// once the other alerts on the server, the reasoning model, or the
// acknowledgement change.
func TestReevaluationSkipsUnchangedKeep(t *testing.T) {
	env := newReevaluationTestEnv(t)
	metric := "pg_stat_database.xact_commit"
	id := env.insertAcknowledgedAnomaly(t, &metric)

	env.run()
	count, fp, status := env.state(t, id)
	if env.provider.callCount() != 1 || count != 1 || fp == nil || status != "acknowledged" {
		t.Fatalf("first cycle: calls=%d count=%d fingerprint=%v status=%s, want 1, 1, set, acknowledged",
			env.provider.callCount(), count, fp, status)
	}
	first := *fp

	// Not yet due: nothing happens at all.
	env.run()
	if env.provider.callCount() != 1 {
		t.Fatalf("alert not yet due was sent to the LLM: calls=%d", env.provider.callCount())
	}

	// Due again with unchanged inputs: the call is skipped, the count is
	// kept, and the alert is pushed back rather than picked up again.
	env.makeDue(t)
	env.run()
	count, fp, _ = env.state(t, id)
	if env.provider.callCount() != 1 {
		t.Errorf("unchanged inputs were sent to the LLM again: calls=%d", env.provider.callCount())
	}
	if count != 1 || fp == nil || *fp != first {
		t.Errorf("skip changed state: count=%d fingerprint=%v, want 1, %s", count, fp, first)
	}
	env.run()
	if env.provider.callCount() != 1 {
		t.Errorf("skipped alert was not deferred: calls=%d", env.provider.callCount())
	}

	// A new alert on the same server changes the prompt.
	insertTestAlert(t, env.pool, "threshold", nil, env.connID, "critical", "active", "Replication lag")
	env.makeDue(t)
	env.run()
	count, fp, _ = env.state(t, id)
	if env.provider.callCount() != 2 {
		t.Fatalf("changed server alerts did not trigger a call: calls=%d", env.provider.callCount())
	}
	if count != 2 || fp == nil || *fp == first {
		t.Errorf("after changed inputs: count=%d fingerprint=%v, want 2 and a new fingerprint", count, fp)
	}

	// A different reasoning model gets asked afresh.
	env.provider.mu.Lock()
	env.provider.model = "model-b"
	env.provider.mu.Unlock()
	env.makeDue(t)
	env.run()
	if env.provider.callCount() != 3 {
		t.Errorf("model change did not trigger a call: calls=%d", env.provider.callCount())
	}

	// A fresh acknowledgement with a new message changes the prompt too.
	if _, err := env.pool.Exec(context.Background(), `
		INSERT INTO alert_acknowledgments (alert_id, acknowledged_by, acknowledged_at,
		    acknowledge_type, message, false_positive)
		VALUES ($1, 'tester', NOW(), 'acknowledge', 'planned batch load', FALSE)
	`, id); err != nil {
		t.Fatalf("insert second acknowledgement: %v", err)
	}
	env.makeDue(t)
	env.run()
	if env.provider.callCount() != 4 {
		t.Errorf("new acknowledgement did not trigger a call: calls=%d", env.provider.callCount())
	}
}

// TestReevaluationDoesNotReuseUnreliableAnswers checks that a failed
// call, a response with no decision, and an answer given without the full
// context store no fingerprint, so the alert is asked about again.
func TestReevaluationDoesNotReuseUnreliableAnswers(t *testing.T) {
	env := newReevaluationTestEnv(t)
	metric := "pg_stat_database.xact_commit"
	id := env.insertAcknowledgedAnomaly(t, &metric)

	// A failed call records the attempt without a fingerprint.
	env.provider.set("", errors.New("provider unavailable"))
	env.run()
	count, fp, _ := env.state(t, id)
	if count != 1 || fp != nil {
		t.Fatalf("failed call: count=%d fingerprint=%v, want 1, nil", count, fp)
	}

	// A response the parser cannot read falls back to keep, but is not
	// an answer worth reusing.
	env.provider.set("I am not sure what to say.", nil)
	env.makeDue(t)
	env.run()
	count, fp, _ = env.state(t, id)
	if env.provider.callCount() != 2 || count != 2 || fp != nil {
		t.Fatalf("undecided response: calls=%d count=%d fingerprint=%v, want 2, 2, nil",
			env.provider.callCount(), count, fp)
	}

	// A keep given while the cluster context could not be read is not
	// stored, and nor is a stored fingerprint trusted to skip the call.
	env.provider.set(keepResponse, nil)
	if _, err := env.pool.Exec(context.Background(),
		`ALTER TABLE metrics.pg_node_role RENAME TO pg_node_role_hidden`); err != nil {
		t.Fatalf("hide pg_node_role: %v", err)
	}
	env.makeDue(t)
	env.run()
	count, fp, _ = env.state(t, id)
	if env.provider.callCount() != 3 || count != 3 || fp != nil {
		t.Fatalf("incomplete context: calls=%d count=%d fingerprint=%v, want 3, 3, nil",
			env.provider.callCount(), count, fp)
	}

	// With the context back, a keep is stored and then reused.
	if _, err := env.pool.Exec(context.Background(),
		`ALTER TABLE metrics.pg_node_role_hidden RENAME TO pg_node_role`); err != nil {
		t.Fatalf("restore pg_node_role: %v", err)
	}
	env.makeDue(t)
	env.run()
	_, fp, _ = env.state(t, id)
	if env.provider.callCount() != 4 || fp == nil {
		t.Fatalf("restored context: calls=%d fingerprint=%v, want 4, set", env.provider.callCount(), fp)
	}

	// The stored fingerprint is not trusted while the context is
	// incomplete, even when the degraded prompt would match it.
	if _, err := env.pool.Exec(context.Background(),
		`ALTER TABLE metrics.pg_node_role RENAME TO pg_node_role_hidden`); err != nil {
		t.Fatalf("hide pg_node_role again: %v", err)
	}
	env.makeDue(t)
	env.run()
	if env.provider.callCount() != 5 {
		t.Errorf("stored fingerprint skipped a call made without full context: calls=%d",
			env.provider.callCount())
	}
	if _, err := env.pool.Exec(context.Background(),
		`ALTER TABLE metrics.pg_node_role_hidden RENAME TO pg_node_role`); err != nil {
		t.Fatalf("restore pg_node_role again: %v", err)
	}
}

// TestReevaluationClearAndMissingMetric covers the clear decision, which
// clears the alert without storing a fingerprint, and an alert with no
// metric name, which is recorded without a call.
func TestReevaluationClearAndMissingMetric(t *testing.T) {
	env := newReevaluationTestEnv(t)
	metric := "pg_stat_database.xact_commit"
	cleared := env.insertAcknowledgedAnomaly(t, &metric)
	noMetric := env.insertAcknowledgedAnomaly(t, nil)

	env.provider.set(clearResponse, nil)
	env.run()

	count, fp, status := env.state(t, cleared)
	if status != "cleared" || count != 1 || fp != nil {
		t.Errorf("cleared alert: status=%s count=%d fingerprint=%v, want cleared, 1, nil",
			status, count, fp)
	}
	count, fp, status = env.state(t, noMetric)
	if status != "acknowledged" || count != 1 || fp != nil {
		t.Errorf("alert without metric: status=%s count=%d fingerprint=%v, want acknowledged, 1, nil",
			status, count, fp)
	}
	if env.provider.callCount() != 1 {
		t.Errorf("calls = %d, want 1 (the alert without a metric is not sent)", env.provider.callCount())
	}
}

// TestReevaluationFingerprintIgnoresCount checks that the fingerprint does
// not change with the re-evaluation count but does with the alert value.
func TestReevaluationFingerprintIgnoresCount(t *testing.T) {
	metric := "m"
	value := 1.0
	e := &Engine{reasoningProvider: &scriptedReasoningProvider{model: "x"}}
	a := &database.AcknowledgedAnomalyAlert{ID: 1, MetricName: &metric, MetricValue: &value}

	base := e.reevaluationFingerprint(a, nil, nil, nil, nil)
	a.ReevaluationCount = 7
	if got := e.reevaluationFingerprint(a, nil, nil, nil, nil); got != base {
		t.Errorf("fingerprint changed with the count: %s != %s", got, base)
	}
	if a.ReevaluationCount != 7 {
		t.Errorf("fingerprinting modified the alert's count to %d", a.ReevaluationCount)
	}
	other := 2.0
	a.MetricValue = &other
	if got := e.reevaluationFingerprint(a, nil, nil, nil, nil); got == base {
		t.Error("fingerprint did not change with the metric value")
	}

	// Without a provider the fingerprint is still computed.
	if got := (&Engine{}).reevaluationFingerprint(a, nil, nil, nil, nil); len(got) != 64 {
		t.Errorf("fingerprint without a provider = %q, want 64 hex characters", got)
	}
}

// TestReevaluationSkipsWhenDisabledOrCancelled covers the early returns.
func TestReevaluationSkipsWhenDisabledOrCancelled(t *testing.T) {
	env := newReevaluationTestEnv(t)
	metric := "m"
	env.insertAcknowledgedAnomaly(t, &metric)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	env.engine.reevaluateAcknowledgedAlerts(ctx)

	env.engine.config.Anomaly.Reevaluation.Enabled = false
	env.run()
	if env.provider.callCount() != 0 {
		t.Errorf("calls = %d, want 0", env.provider.callCount())
	}
}

// blockAlertUpdatesSQL makes every UPDATE of alerts fail whilst leaving
// reads working, so the write paths of a re-evaluation can be failed on
// their own.
const blockAlertUpdatesSQL = `
CREATE OR REPLACE FUNCTION reeval_block_updates() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'alerts are read-only in this test';
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER reeval_block_updates BEFORE UPDATE ON alerts
    FOR EACH ROW EXECUTE FUNCTION reeval_block_updates();
`

// TestReevaluationWriteFailures checks that failures to defer, clear or
// record a re-evaluation are logged and leave the alert as it was.
func TestReevaluationWriteFailures(t *testing.T) {
	env := newReevaluationTestEnv(t)
	metric := "pg_stat_database.xact_commit"
	id := env.insertAcknowledgedAnomaly(t, &metric)

	env.run()
	env.makeDue(t)
	ctx := context.Background()
	due, err := env.engine.datastore.GetAcknowledgedAnomalyAlerts(ctx, 300, 10)
	if err != nil || len(due) != 1 || due[0].ReevaluationFingerprint == nil {
		t.Fatalf("due alerts = %v, %v; want one with a fingerprint", due, err)
	}

	if _, err := env.pool.Exec(ctx, blockAlertUpdatesSQL); err != nil {
		t.Fatalf("block alert updates: %v", err)
	}
	t.Cleanup(func() {
		if _, err := env.pool.Exec(context.Background(), `
			DROP TRIGGER IF EXISTS reeval_block_updates ON alerts;
			DROP FUNCTION IF EXISTS reeval_block_updates();
		`); err != nil {
			t.Logf("drop update block: %v", err)
		}
	})

	// Unchanged inputs: the deferral fails, and no call is made.
	env.engine.reevaluateAlert(ctx, env.engine.config.Anomaly.Reevaluation, due[0])
	if env.provider.callCount() != 1 {
		t.Errorf("failed deferral made a call: calls=%d", env.provider.callCount())
	}

	// Changed inputs and a clear: the clear and the record both fail.
	due[0].ReevaluationFingerprint = nil
	env.provider.set(clearResponse, nil)
	env.engine.reevaluateAlert(ctx, env.engine.config.Anomaly.Reevaluation, due[0])
	if env.provider.callCount() != 2 {
		t.Errorf("changed inputs were not sent: calls=%d", env.provider.callCount())
	}
	count, fp, status := env.state(t, id)
	if count != 1 || fp == nil || status != "acknowledged" {
		t.Errorf("failed writes changed the alert: count=%d fingerprint=%v status=%s",
			count, fp, status)
	}
}

// TestClearReevaluatedAlertUnreadable checks that an alert cleared but then
// unreadable is logged without a notification, using a trigger that
// deletes the alert once it is cleared.
func TestClearReevaluatedAlertUnreadable(t *testing.T) {
	env := newReevaluationTestEnv(t)
	metric := "m"
	id := env.insertAcknowledgedAnomaly(t, &metric)

	ctx := context.Background()
	if _, err := env.pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION reeval_delete_cleared() RETURNS trigger AS $$
		BEGIN
		    DELETE FROM alert_acknowledgments WHERE alert_id = NEW.id;
		    DELETE FROM alerts WHERE id = NEW.id;
		    RETURN NULL;
		END;
		$$ LANGUAGE plpgsql;
		CREATE TRIGGER reeval_delete_cleared AFTER UPDATE ON alerts
		    FOR EACH ROW EXECUTE FUNCTION reeval_delete_cleared();
	`); err != nil {
		t.Fatalf("create delete trigger: %v", err)
	}
	t.Cleanup(func() {
		if _, err := env.pool.Exec(context.Background(), `
			DROP TRIGGER IF EXISTS reeval_delete_cleared ON alerts;
			DROP FUNCTION IF EXISTS reeval_delete_cleared();
		`); err != nil {
			t.Logf("drop delete trigger: %v", err)
		}
	})

	env.engine.clearReevaluatedAlert(ctx, &database.AcknowledgedAnomalyAlert{ID: id, MetricName: &metric}, 0.9)

	var remaining int
	if err := env.pool.QueryRow(ctx, `SELECT COUNT(*) FROM alerts WHERE id = $1`, id).Scan(&remaining); err != nil {
		t.Fatalf("count alerts: %v", err)
	}
	if remaining != 0 {
		t.Errorf("alert %d still present; the trigger did not run", id)
	}
}

// TestReevaluationWithoutContext checks that an alert is still sent to the
// LLM when every context query fails, using the default timeout when none
// is configured, and that a stored fingerprint is not trusted.
func TestReevaluationWithoutContext(t *testing.T) {
	_, pool, cleanup := newEngineIntegrationTestEnv(t)
	cleanup()

	provider := &scriptedReasoningProvider{response: keepResponse, model: "model-a"}
	e := &Engine{
		config:            config.NewConfig(),
		datastore:         database.NewTestDatastore(pool),
		reasoningProvider: provider,
	}
	metric := "m"
	stored := "stale"
	e.reevaluateAlert(context.Background(), config.ReevaluationConfig{},
		&database.AcknowledgedAnomalyAlert{ID: 1, MetricName: &metric, ReevaluationFingerprint: &stored})
	if provider.callCount() != 1 {
		t.Errorf("calls = %d, want 1", provider.callCount())
	}
}
