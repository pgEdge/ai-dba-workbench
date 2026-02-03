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
	"strings"
	"time"
)

// Sentinel errors for blackout operations
var (
	// ErrBlackoutNotFound is returned when a blackout is not found
	ErrBlackoutNotFound = errors.New("blackout not found")

	// ErrBlackoutScheduleNotFound is returned when a blackout schedule is not found
	ErrBlackoutScheduleNotFound = errors.New("blackout schedule not found")
)

// BlackoutScope represents the hierarchy level of a blackout
type BlackoutScope string

const (
	BlackoutScopeEstate  BlackoutScope = "estate"
	BlackoutScopeGroup   BlackoutScope = "group"
	BlackoutScopeCluster BlackoutScope = "cluster"
	BlackoutScopeServer  BlackoutScope = "server"
)

// ValidBlackoutScopes contains all valid blackout scope values
var ValidBlackoutScopes = map[string]bool{
	string(BlackoutScopeEstate):  true,
	string(BlackoutScopeGroup):   true,
	string(BlackoutScopeCluster): true,
	string(BlackoutScopeServer):  true,
}

// Blackout represents a blackout period during which alerts are suppressed
type Blackout struct {
	ID           int64     `json:"id"`
	Scope        string    `json:"scope"`
	GroupID      *int      `json:"group_id,omitempty"`
	ClusterID    *int      `json:"cluster_id,omitempty"`
	ConnectionID *int      `json:"connection_id,omitempty"`
	DatabaseName *string   `json:"database_name,omitempty"`
	Reason       string    `json:"reason"`
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	IsActive     bool      `json:"is_active"`
}

// BlackoutSchedule represents a recurring blackout schedule
type BlackoutSchedule struct {
	ID              int64     `json:"id"`
	Scope           string    `json:"scope"`
	GroupID         *int      `json:"group_id,omitempty"`
	ClusterID       *int      `json:"cluster_id,omitempty"`
	ConnectionID    *int      `json:"connection_id,omitempty"`
	DatabaseName    *string   `json:"database_name,omitempty"`
	Name            string    `json:"name"`
	CronExpression  string    `json:"cron_expression"`
	DurationMinutes int       `json:"duration_minutes"`
	Timezone        string    `json:"timezone"`
	Reason          string    `json:"reason"`
	Enabled         bool      `json:"enabled"`
	CreatedBy       string    `json:"created_by"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// BlackoutFilter holds filter options for querying blackouts
type BlackoutFilter struct {
	Scope        *string
	GroupID      *int
	ClusterID    *int
	ConnectionID *int
	Active       *bool
	Limit        int
	Offset       int
}

// BlackoutListResult holds paginated blackout results
type BlackoutListResult struct {
	Blackouts  []Blackout `json:"blackouts"`
	TotalCount int        `json:"total_count"`
}

// BlackoutScheduleListResult holds paginated blackout schedule results
type BlackoutScheduleListResult struct {
	Schedules  []BlackoutSchedule `json:"schedules"`
	TotalCount int                `json:"total_count"`
}

// ListBlackouts returns blackouts matching the filter with pagination
func (d *Datastore) ListBlackouts(ctx context.Context, filter BlackoutFilter) (*BlackoutListResult, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	conditions := []string{}
	args := []interface{}{}
	argNum := 1

	if filter.Scope != nil {
		conditions = append(conditions, fmt.Sprintf("b.scope = $%d", argNum))
		args = append(args, *filter.Scope)
		argNum++
	}

	if filter.GroupID != nil {
		conditions = append(conditions, fmt.Sprintf("b.group_id = $%d", argNum))
		args = append(args, *filter.GroupID)
		argNum++
	}

	if filter.ClusterID != nil {
		conditions = append(conditions, fmt.Sprintf("b.cluster_id = $%d", argNum))
		args = append(args, *filter.ClusterID)
		argNum++
	}

	if filter.ConnectionID != nil {
		conditions = append(conditions, fmt.Sprintf("b.connection_id = $%d", argNum))
		args = append(args, *filter.ConnectionID)
		argNum++
	}

	if filter.Active != nil {
		if *filter.Active {
			conditions = append(conditions, "b.start_time <= NOW() AND b.end_time >= NOW()")
		} else {
			conditions = append(conditions, "(b.start_time > NOW() OR b.end_time < NOW())")
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM blackouts b %s", whereClause)
	var totalCount int
	if err := d.pool.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("failed to count blackouts: %w", err)
	}

	// Get paginated results
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	query := fmt.Sprintf(`
        SELECT b.id, b.scope, b.group_id, b.cluster_id, b.connection_id,
               b.database_name, b.reason, b.start_time, b.end_time,
               b.created_by, b.created_at,
               (b.start_time <= NOW() AND b.end_time >= NOW()) AS is_active
        FROM blackouts b
        %s
        ORDER BY b.start_time DESC
        LIMIT $%d OFFSET $%d
    `, whereClause, argNum, argNum+1)
	args = append(args, limit, offset)

	rows, err := d.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query blackouts: %w", err)
	}
	defer rows.Close()

	blackouts := []Blackout{}
	for rows.Next() {
		var b Blackout
		if err := rows.Scan(
			&b.ID, &b.Scope, &b.GroupID, &b.ClusterID, &b.ConnectionID,
			&b.DatabaseName, &b.Reason, &b.StartTime, &b.EndTime,
			&b.CreatedBy, &b.CreatedAt, &b.IsActive,
		); err != nil {
			return nil, fmt.Errorf("failed to scan blackout: %w", err)
		}
		blackouts = append(blackouts, b)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating blackouts: %w", err)
	}

	return &BlackoutListResult{
		Blackouts:  blackouts,
		TotalCount: totalCount,
	}, nil
}

// GetBlackout returns a single blackout by ID
func (d *Datastore) GetBlackout(ctx context.Context, id int64) (*Blackout, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT b.id, b.scope, b.group_id, b.cluster_id, b.connection_id,
               b.database_name, b.reason, b.start_time, b.end_time,
               b.created_by, b.created_at,
               (b.start_time <= NOW() AND b.end_time >= NOW()) AS is_active
        FROM blackouts b
        WHERE b.id = $1
    `

	var b Blackout
	err := d.pool.QueryRow(ctx, query, id).Scan(
		&b.ID, &b.Scope, &b.GroupID, &b.ClusterID, &b.ConnectionID,
		&b.DatabaseName, &b.Reason, &b.StartTime, &b.EndTime,
		&b.CreatedBy, &b.CreatedAt, &b.IsActive,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrBlackoutNotFound
		}
		return nil, fmt.Errorf("failed to get blackout: %w", err)
	}

	return &b, nil
}

// CreateBlackout creates a new blackout and sets its ID via RETURNING
func (d *Datastore) CreateBlackout(ctx context.Context, b *Blackout) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
        INSERT INTO blackouts (scope, group_id, cluster_id, connection_id,
                               database_name, reason, start_time, end_time,
                               created_by)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
        RETURNING id, created_at
    `

	err := d.pool.QueryRow(ctx, query,
		b.Scope, b.GroupID, b.ClusterID, b.ConnectionID,
		b.DatabaseName, b.Reason, b.StartTime, b.EndTime,
		b.CreatedBy,
	).Scan(&b.ID, &b.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create blackout: %w", err)
	}

	return nil
}

// UpdateBlackout updates the reason and end time of an existing blackout
func (d *Datastore) UpdateBlackout(ctx context.Context, id int64, reason string, endTime time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
        UPDATE blackouts
        SET reason = $1, end_time = $2
        WHERE id = $3
    `

	tag, err := d.pool.Exec(ctx, query, reason, endTime, id)
	if err != nil {
		return fmt.Errorf("failed to update blackout: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrBlackoutNotFound
	}

	return nil
}

// DeleteBlackout deletes a blackout by ID
func (d *Datastore) DeleteBlackout(ctx context.Context, id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `DELETE FROM blackouts WHERE id = $1`
	tag, err := d.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete blackout: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrBlackoutNotFound
	}

	return nil
}

// StopBlackout stops an active blackout by setting its end time to now
func (d *Datastore) StopBlackout(ctx context.Context, id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
        UPDATE blackouts
        SET end_time = NOW()
        WHERE id = $1 AND end_time >= NOW()
    `

	tag, err := d.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to stop blackout: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrBlackoutNotFound
	}

	return nil
}

// GetActiveBlackoutsForEntity returns active blackouts for a given scope and entity
func (d *Datastore) GetActiveBlackoutsForEntity(ctx context.Context, scope string, entityID int) ([]Blackout, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var condition string
	switch BlackoutScope(scope) {
	case BlackoutScopeEstate:
		condition = "b.scope = 'estate'"
	case BlackoutScopeGroup:
		condition = "b.scope = 'group' AND b.group_id = $2"
	case BlackoutScopeCluster:
		condition = "b.scope = 'cluster' AND b.cluster_id = $2"
	case BlackoutScopeServer:
		condition = "b.scope = 'server' AND b.connection_id = $2"
	default:
		return nil, fmt.Errorf("invalid blackout scope: %s", scope)
	}

	query := fmt.Sprintf(`
        SELECT b.id, b.scope, b.group_id, b.cluster_id, b.connection_id,
               b.database_name, b.reason, b.start_time, b.end_time,
               b.created_by, b.created_at, TRUE AS is_active
        FROM blackouts b
        WHERE %s AND b.start_time <= NOW() AND b.end_time >= NOW()
        ORDER BY b.start_time DESC
    `, condition)

	var args []interface{}
	if scope != string(BlackoutScopeEstate) {
		args = append(args, entityID)
	}

	rows, err := d.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query active blackouts: %w", err)
	}
	defer rows.Close()

	var blackouts []Blackout
	for rows.Next() {
		var b Blackout
		if err := rows.Scan(
			&b.ID, &b.Scope, &b.GroupID, &b.ClusterID, &b.ConnectionID,
			&b.DatabaseName, &b.Reason, &b.StartTime, &b.EndTime,
			&b.CreatedBy, &b.CreatedAt, &b.IsActive,
		); err != nil {
			return nil, fmt.Errorf("failed to scan blackout: %w", err)
		}
		blackouts = append(blackouts, b)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating blackouts: %w", err)
	}

	return blackouts, nil
}

// ListBlackoutSchedules returns blackout schedules matching the filter with pagination
func (d *Datastore) ListBlackoutSchedules(ctx context.Context, filter BlackoutFilter) (*BlackoutScheduleListResult, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	conditions := []string{}
	args := []interface{}{}
	argNum := 1

	if filter.Scope != nil {
		conditions = append(conditions, fmt.Sprintf("s.scope = $%d", argNum))
		args = append(args, *filter.Scope)
		argNum++
	}

	if filter.GroupID != nil {
		conditions = append(conditions, fmt.Sprintf("s.group_id = $%d", argNum))
		args = append(args, *filter.GroupID)
		argNum++
	}

	if filter.ClusterID != nil {
		conditions = append(conditions, fmt.Sprintf("s.cluster_id = $%d", argNum))
		args = append(args, *filter.ClusterID)
		argNum++
	}

	if filter.ConnectionID != nil {
		conditions = append(conditions, fmt.Sprintf("s.connection_id = $%d", argNum))
		args = append(args, *filter.ConnectionID)
		argNum++
	}

	if filter.Active != nil {
		conditions = append(conditions, fmt.Sprintf("s.enabled = $%d", argNum))
		args = append(args, *filter.Active)
		argNum++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM blackout_schedules s %s", whereClause)
	var totalCount int
	if err := d.pool.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("failed to count blackout schedules: %w", err)
	}

	// Get paginated results
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	query := fmt.Sprintf(`
        SELECT s.id, s.scope, s.group_id, s.cluster_id, s.connection_id,
               s.database_name, s.name, s.cron_expression, s.duration_minutes,
               s.timezone, s.reason, s.enabled, s.created_by,
               s.created_at, s.updated_at
        FROM blackout_schedules s
        %s
        ORDER BY s.created_at DESC
        LIMIT $%d OFFSET $%d
    `, whereClause, argNum, argNum+1)
	args = append(args, limit, offset)

	rows, err := d.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query blackout schedules: %w", err)
	}
	defer rows.Close()

	schedules := []BlackoutSchedule{}
	for rows.Next() {
		var s BlackoutSchedule
		if err := rows.Scan(
			&s.ID, &s.Scope, &s.GroupID, &s.ClusterID, &s.ConnectionID,
			&s.DatabaseName, &s.Name, &s.CronExpression, &s.DurationMinutes,
			&s.Timezone, &s.Reason, &s.Enabled, &s.CreatedBy,
			&s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan blackout schedule: %w", err)
		}
		schedules = append(schedules, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating blackout schedules: %w", err)
	}

	return &BlackoutScheduleListResult{
		Schedules:  schedules,
		TotalCount: totalCount,
	}, nil
}

// GetBlackoutSchedule returns a single blackout schedule by ID
func (d *Datastore) GetBlackoutSchedule(ctx context.Context, id int64) (*BlackoutSchedule, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	query := `
        SELECT s.id, s.scope, s.group_id, s.cluster_id, s.connection_id,
               s.database_name, s.name, s.cron_expression, s.duration_minutes,
               s.timezone, s.reason, s.enabled, s.created_by,
               s.created_at, s.updated_at
        FROM blackout_schedules s
        WHERE s.id = $1
    `

	var s BlackoutSchedule
	err := d.pool.QueryRow(ctx, query, id).Scan(
		&s.ID, &s.Scope, &s.GroupID, &s.ClusterID, &s.ConnectionID,
		&s.DatabaseName, &s.Name, &s.CronExpression, &s.DurationMinutes,
		&s.Timezone, &s.Reason, &s.Enabled, &s.CreatedBy,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, ErrBlackoutScheduleNotFound
		}
		return nil, fmt.Errorf("failed to get blackout schedule: %w", err)
	}

	return &s, nil
}

// CreateBlackoutSchedule creates a new blackout schedule and sets its ID via RETURNING
func (d *Datastore) CreateBlackoutSchedule(ctx context.Context, s *BlackoutSchedule) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
        INSERT INTO blackout_schedules (scope, group_id, cluster_id, connection_id,
                                        database_name, name, cron_expression,
                                        duration_minutes, timezone, reason,
                                        enabled, created_by)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
        RETURNING id, created_at, updated_at
    `

	err := d.pool.QueryRow(ctx, query,
		s.Scope, s.GroupID, s.ClusterID, s.ConnectionID,
		s.DatabaseName, s.Name, s.CronExpression,
		s.DurationMinutes, s.Timezone, s.Reason,
		s.Enabled, s.CreatedBy,
	).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create blackout schedule: %w", err)
	}

	return nil
}

// UpdateBlackoutSchedule updates an existing blackout schedule
func (d *Datastore) UpdateBlackoutSchedule(ctx context.Context, s *BlackoutSchedule) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `
        UPDATE blackout_schedules
        SET scope = $1, group_id = $2, cluster_id = $3, connection_id = $4,
            database_name = $5, name = $6, cron_expression = $7,
            duration_minutes = $8, timezone = $9, reason = $10,
            enabled = $11, updated_at = NOW()
        WHERE id = $12
        RETURNING updated_at
    `

	err := d.pool.QueryRow(ctx, query,
		s.Scope, s.GroupID, s.ClusterID, s.ConnectionID,
		s.DatabaseName, s.Name, s.CronExpression,
		s.DurationMinutes, s.Timezone, s.Reason,
		s.Enabled, s.ID,
	).Scan(&s.UpdatedAt)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return ErrBlackoutScheduleNotFound
		}
		return fmt.Errorf("failed to update blackout schedule: %w", err)
	}

	return nil
}

// DeleteBlackoutSchedule deletes a blackout schedule by ID
func (d *Datastore) DeleteBlackoutSchedule(ctx context.Context, id int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	query := `DELETE FROM blackout_schedules WHERE id = $1`
	tag, err := d.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete blackout schedule: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return ErrBlackoutScheduleNotFound
	}

	return nil
}
