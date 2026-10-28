/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pgedge/ai-workbench/pkg/anomaly"
)

// The anomaly comparison plots a baselined metric's raw values against
// the band the alerter's Tier 1 detector scores them with. Selection,
// warmth and scoring all come from pkg/anomaly, the code the alerter
// itself runs, so the chart cannot drift from detection; this file only
// loads the inputs and lays the result out as a bucketed series.

// Comparison models: as_scored shows, for each point, the row the
// alerter would score it against; the others pin one period type.
const (
	ComparisonModelAsScored = "as_scored"
	ComparisonModelHourly   = "hourly"
	ComparisonModelDaily    = "daily"
	ComparisonModelAll      = "all"
)

// ComparisonModels lists the accepted comparison models.
var ComparisonModels = []string{
	ComparisonModelAsScored, ComparisonModelDaily,
	ComparisonModelHourly, ComparisonModelAll,
}

// DefaultComparisonModel is the model used when none is requested.
const DefaultComparisonModel = ComparisonModelAsScored

// Default and maximum bucket counts for the comparison series.
const (
	DefaultComparisonBuckets = 300
	MaxComparisonBuckets     = 500
)

// MaxComparisonSensitivity is the largest sensitivity, in standard
// deviations, a comparison may request.
const MaxComparisonSensitivity = 10.0

// Scoring sources: the settings the alerter published, or the shipped
// defaults when it has published none that can be read.
const (
	ScoringSourceAlerter  = "alerter"
	ScoringSourceDefaults = "defaults"
)

// candidateIndex maps an explicit model to its position in the slice
// anomaly.Candidates returns.
var candidateIndex = map[string]int{
	ComparisonModelHourly: 0,
	ComparisonModelDaily:  1,
	ComparisonModelAll:    2,
}

// ErrUnknownComparisonMetric is returned for a metric that has no
// baseline definition.
var ErrUnknownComparisonMetric = errors.New("unknown metric: not a baselined metric")

// IsValidComparisonModel reports whether model is an accepted model.
func IsValidComparisonModel(model string) bool {
	for _, m := range ComparisonModels {
		if m == model {
			return true
		}
	}
	return false
}

// ResolveComparisonMetric looks a metric up in the baseline registry and
// checks the database name against its scope: a per-database metric
// needs one and a server-level metric must not have one. The errors are
// safe to return to the caller.
func ResolveComparisonMetric(metric string, databaseName *string) (anomaly.MetricDef, error) {
	def, ok := anomaly.Metric(metric)
	if !ok {
		return anomaly.MetricDef{}, ErrUnknownComparisonMetric
	}
	if def.PerDatabase && databaseName == nil {
		return anomaly.MetricDef{}, fmt.Errorf(
			"database_name is required for the per-database metric %s", metric)
	}
	if !def.PerDatabase && databaseName != nil {
		return anomaly.MetricDef{}, fmt.Errorf(
			"database_name must not be given for the server-level metric %s", metric)
	}
	return def, nil
}

// ComparisonSample is one raw value of the compared metric.
type ComparisonSample struct {
	Time  time.Time
	Value float64
}

// ComparisonPoint is one plotted point. PeriodType, Mean, Stddev and
// ZScore are nil when no baseline row applies; Stddev is the effective,
// floored divisor the z-score is computed with.
type ComparisonPoint struct {
	Time       time.Time `json:"time"`
	Value      float64   `json:"value"`
	PeriodType *string   `json:"period_type"`
	Mean       *float64  `json:"mean"`
	Stddev     *float64  `json:"stddev"`
	Cold       bool      `json:"cold"`
	ZScore     *float64  `json:"z_score"`
	Outside    bool      `json:"outside"`
}

// AnomalyComparison is the response of the anomaly comparison endpoint.
type AnomalyComparison struct {
	Metric             string            `json:"metric"`
	ConnectionID       int               `json:"connection_id"`
	DatabaseName       *string           `json:"database_name"`
	Model              string            `json:"model"`
	Sensitivity        float64           `json:"sensitivity"`
	DefaultSensitivity float64           `json:"default_sensitivity"`
	ScoringSource      string            `json:"scoring_source"`
	BaselinesAsOf      *time.Time        `json:"baselines_as_of"`
	Points             []ComparisonPoint `json:"points"`
}

// ComparisonRequest is a validated comparison request. A nil Sensitivity
// means the scoring settings' default sensitivity.
type ComparisonRequest struct {
	ConnectionID int
	Metric       string
	DatabaseName *string
	Model        string
	Sensitivity  *float64
	Window       TimeWindow
	Buckets      int
}

// BuildComparison scores each sample against the baseline rows the model
// selects, then reduces the series to at most buckets points by splitting
// the window into equal intervals and keeping, per interval, the sample
// with the largest |z| (the largest value when none is scored), so a
// deviation is never averaged away. Samples outside the window are
// dropped. The result is never nil.
//
// Warmth and the hourly and daily keys are evaluated at each sample's own
// time, which is when the alerter would have scored it, against the
// baseline rows as they stand now; metric names the floors to apply.
func BuildComparison(
	metric string,
	samples []ComparisonSample,
	baselines []*anomaly.Baseline,
	settings anomaly.ScoringSettings,
	model string,
	sensitivity float64,
	window TimeWindow,
	buckets int,
) []ComparisonPoint {
	if buckets < 1 {
		buckets = 1
	}
	floor := settings.MetricFloor(metric)
	width := window.End.Sub(window.Start) / time.Duration(buckets)

	kept := make([]*ComparisonPoint, buckets)
	for _, s := range samples {
		if s.Time.Before(window.Start) || s.Time.After(window.End) {
			continue
		}
		idx := 0
		if width > 0 {
			idx = int(s.Time.Sub(window.Start) / width)
		}
		if idx >= buckets {
			idx = buckets - 1
		}
		p := scoreComparisonPoint(s, baselines, settings, floor, model, sensitivity)
		if kept[idx] == nil || outranks(p, *kept[idx]) {
			kept[idx] = &p
		}
	}

	points := make([]ComparisonPoint, 0, buckets)
	for _, p := range kept {
		if p != nil {
			points = append(points, *p)
		}
	}
	return points
}

// outranks reports whether a should replace b as its bucket's point: a
// scored point beats an unscored one, then the larger |z| wins, then the
// larger value. Ties keep the earlier point.
func outranks(a, b ComparisonPoint) bool {
	switch {
	case a.ZScore != nil && b.ZScore == nil:
		return true
	case a.ZScore == nil && b.ZScore != nil:
		return false
	case a.ZScore != nil:
		return math.Abs(*a.ZScore) > math.Abs(*b.ZScore)
	default:
		return a.Value > b.Value
	}
}

// scoreComparisonPoint picks the baseline row for one sample and scores
// the sample against it.
func scoreComparisonPoint(
	s ComparisonSample,
	baselines []*anomaly.Baseline,
	settings anomaly.ScoringSettings,
	floor anomaly.MetricFloor,
	model string,
	sensitivity float64,
) ComparisonPoint {
	p := ComparisonPoint{Time: s.Time.UTC(), Value: s.Value}

	var row *anomaly.Baseline
	if model == ComparisonModelAsScored {
		chosen, skippedCold := anomaly.Select(baselines, s.Time, settings.Warmup)
		row = chosen
		if chosen == nil {
			// Nothing is warm, so the alerter would not score the
			// value; show the most preferred cold row, if any. With
			// no row at all there is nothing to call cold, matching
			// the explicit models.
			row = skippedCold
			p.Cold = skippedCold != nil
		}
	} else {
		row = anomaly.Candidates(baselines, s.Time)[candidateIndex[model]]
		if row != nil {
			p.Cold = !anomaly.IsWarm(*row, settings.Warmup, s.Time)
		}
	}
	if row == nil {
		return p
	}

	periodType := row.PeriodType
	mean := row.Mean
	stddev := anomaly.EffectiveStdDev(*row, settings.VarianceFloor, floor)
	p.PeriodType = &periodType
	p.Mean = &mean
	p.Stddev = &stddev

	z, ok := anomaly.ZScore(s.Value, *row, stddev, settings.MaxZScore)
	if !ok {
		return p
	}
	p.ZScore = &z
	p.Outside = !p.Cold && !anomaly.InBand(s.Value, z, sensitivity, floor)
	return p
}

// ParseScoringSettings decodes the alerter's published Tier 1 settings.
// NULL, malformed JSON, or settings without a positive default
// sensitivity (which no running alerter publishes) fall back to the
// shipped defaults, and the source says which was used.
func ParseScoringSettings(raw []byte) (anomaly.ScoringSettings, string) {
	if len(raw) > 0 {
		var s anomaly.ScoringSettings
		if err := json.Unmarshal(raw, &s); err == nil && s.DefaultSensitivity > 0 {
			return s, ScoringSourceAlerter
		}
	}
	return anomaly.DefaultScoringSettings(), ScoringSourceDefaults
}

// scoringSettingsQuery reads the settings the alerter last published.
const scoringSettingsQuery = `SELECT tier1_scoring FROM alerter_settings WHERE id = 1`

// SQLSTATEs for a column or table the collector's migrations have not
// created yet.
const (
	pgErrCodeUndefinedColumn = "42703"
	pgErrCodeUndefinedTable  = "42P01"
)

// isMissingSchemaError reports whether err is PostgreSQL saying that a
// column or table does not exist, which happens when the server is
// upgraded before the collector has run the migration that adds it.
func isMissingSchemaError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == pgErrCodeUndefinedColumn || pgErr.Code == pgErrCodeUndefinedTable
}

// LoadScoringSettings returns the alerter's published Tier 1 settings,
// or the defaults when there is no settings row, it cannot be decoded,
// or the collector has not yet created the table or column.
func LoadScoringSettings(ctx context.Context, pool *pgxpool.Pool) (anomaly.ScoringSettings, string, error) {
	var raw []byte
	err := pool.QueryRow(ctx, scoringSettingsQuery).Scan(&raw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) && !isMissingSchemaError(err) {
		return anomaly.ScoringSettings{}, "", fmt.Errorf("failed to read scoring settings: %w", err)
	}
	s, source := ParseScoringSettings(raw)
	return s, source, nil
}

// comparisonBaselinesQuery reads every baseline row for one metric on
// one connection and database; NULL database_name is the server-level
// scope.
const comparisonBaselinesQuery = `
    SELECT period_type, hour_of_day, day_of_week, mean, stddev,
           sample_count, earliest_sample_at, last_calculated
    FROM metric_baselines
    WHERE connection_id = $1
      AND metric_name = $2
      AND database_name IS NOT DISTINCT FROM $3::text
`

// QueryComparisonBaselines returns the baseline rows for a metric and
// the time of the most recent recalculation among them, which is nil
// when there are no rows. Rows with a non-finite mean or stddev are
// left out.
func QueryComparisonBaselines(
	ctx context.Context,
	pool *pgxpool.Pool,
	connectionID int,
	metric string,
	databaseName *string,
) ([]*anomaly.Baseline, *time.Time, error) {
	rows, err := pool.Query(ctx, comparisonBaselinesQuery, connectionID, metric, databaseName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query baselines: %w", err)
	}
	defer rows.Close()

	baselines := []*anomaly.Baseline{}
	var asOf *time.Time
	for rows.Next() {
		var (
			b              anomaly.Baseline
			earliest       *time.Time
			lastCalculated time.Time
		)
		if err := rows.Scan(&b.PeriodType, &b.HourOfDay, &b.DayOfWeek,
			&b.Mean, &b.StdDev, &b.SampleCount, &earliest,
			&lastCalculated); err != nil {
			return nil, nil, fmt.Errorf("failed to scan baseline: %w", err)
		}
		// A row with a non-finite mean or stddev cannot be scored or
		// encoded, so it is treated as missing, including for
		// baselines_as_of.
		if !allFinite(b.Mean, b.StdDev) {
			continue
		}
		if earliest != nil {
			b.EarliestSampleAt = earliest.UTC()
		}
		if asOf == nil || lastCalculated.After(*asOf) {
			t := lastCalculated.UTC()
			asOf = &t
		}
		baselines = append(baselines, &b)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("failed to read baselines: %w", err)
	}
	return baselines, asOf, nil
}

// comparisonSeriesQuery wraps a metric's shared historical query, the
// one the baselines are computed from, so the plotted values are exactly
// the values that were baselined. Every registered query yields columns
// named connection_id, database_name, value and collected_at. $1 is the
// historical query's lookback in days; the outer predicates narrow it to
// one connection, one database (NULL for server-level metrics) and the
// window. Only the registry's own SQL is spliced in, never request input.
func comparisonSeriesQuery(def anomaly.MetricDef) string {
	return `SELECT h.value::float8, h.collected_at
        FROM (` + def.HistoricalSQL + `) AS h
        WHERE h.connection_id = $2
          AND h.database_name IS NOT DISTINCT FROM $3::text
          AND h.collected_at BETWEEN $4 AND $5
          AND h.value IS NOT NULL
        ORDER BY h.collected_at`
}

// comparisonLookbackDays returns the historical query's lookback: whole
// days from the window start to now, rounded up, plus one so that clock
// skew between this process and the database cannot clip the window.
func comparisonLookbackDays(window TimeWindow, now time.Time) int {
	days := int(math.Ceil(now.Sub(window.Start).Hours()/24)) + 1
	if days < 1 {
		days = 1
	}
	return days
}

// QueryComparisonSeries returns the metric's finite raw values on one
// connection and database within the window, oldest first. The metric is
// looked up in the registry here, so an unknown name runs no SQL.
func QueryComparisonSeries(
	ctx context.Context,
	pool *pgxpool.Pool,
	connectionID int,
	metric string,
	databaseName *string,
	window TimeWindow,
	now time.Time,
) ([]ComparisonSample, error) {
	def, ok := anomaly.Metric(metric)
	if !ok {
		return nil, ErrUnknownComparisonMetric
	}
	rows, err := pool.Query(ctx, comparisonSeriesQuery(def),
		comparisonLookbackDays(window, now), connectionID, databaseName,
		window.Start.UTC(), window.End.UTC())
	if err != nil {
		return nil, fmt.Errorf("failed to query metric values: %w", err)
	}
	defer rows.Close()

	samples := []ComparisonSample{}
	for rows.Next() {
		var s ComparisonSample
		if err := rows.Scan(&s.Value, &s.Time); err != nil {
			return nil, fmt.Errorf("failed to scan metric value: %w", err)
		}
		// encoding/json cannot marshal NaN or +/-Inf, which a float
		// column can hold, so such samples are dropped.
		if !allFinite(s.Value) {
			continue
		}
		s.Time = s.Time.UTC()
		samples = append(samples, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to read metric values: %w", err)
	}
	return samples, nil
}

// LoadComparison validates the request's metric and database, loads the
// settings, baselines and series, and builds the comparison. now is the
// reference time for the historical query's lookback.
func LoadComparison(
	ctx context.Context,
	pool *pgxpool.Pool,
	req ComparisonRequest,
	now time.Time,
) (*AnomalyComparison, error) {
	if _, err := ResolveComparisonMetric(req.Metric, req.DatabaseName); err != nil {
		return nil, err
	}
	if !IsValidComparisonModel(req.Model) {
		return nil, fmt.Errorf("invalid model %q", req.Model)
	}

	settings, source, err := LoadScoringSettings(ctx, pool)
	if err != nil {
		return nil, err
	}
	sensitivity := settings.DefaultSensitivity
	if req.Sensitivity != nil {
		sensitivity = *req.Sensitivity
	}

	baselines, asOf, err := QueryComparisonBaselines(
		ctx, pool, req.ConnectionID, req.Metric, req.DatabaseName)
	if err != nil {
		return nil, err
	}
	samples, err := QueryComparisonSeries(
		ctx, pool, req.ConnectionID, req.Metric, req.DatabaseName, req.Window, now)
	if err != nil {
		return nil, err
	}

	return &AnomalyComparison{
		Metric:             req.Metric,
		ConnectionID:       req.ConnectionID,
		DatabaseName:       req.DatabaseName,
		Model:              req.Model,
		Sensitivity:        sensitivity,
		DefaultSensitivity: settings.DefaultSensitivity,
		ScoringSource:      source,
		BaselinesAsOf:      asOf,
		Points: BuildComparison(req.Metric, samples, baselines, settings,
			req.Model, sensitivity, req.Window, req.Buckets),
	}, nil
}
