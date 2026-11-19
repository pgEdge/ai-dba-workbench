/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package actions stores proposed actions: writes that Ellie or a person
// proposes for a monitored database, with the review verdict, the approval
// and the execution outcome. The store is the only code that touches the
// proposed_actions table.
package actions

import (
	"encoding/json"
	"time"
)

// Verdict is the reviewer's decision about an action.
type Verdict struct {
	Decision string   `json:"decision"`
	Danger   string   `json:"danger"`
	Reasons  []string `json:"reasons"`
	Reviewer string   `json:"reviewer"`
}

// StatementResult records what happened when one statement was executed.
type StatementResult struct {
	Statement    string     `json:"statement"`
	CommandTag   string     `json:"command_tag"`
	RowsAffected int64      `json:"rows_affected"`
	DurationMS   int64      `json:"duration_ms"`
	Error        string     `json:"error"`
	Applied      bool       `json:"applied"`
	Columns      []string   `json:"columns,omitempty"`
	Rows         [][]string `json:"rows,omitempty"`
	Truncated    bool       `json:"truncated,omitempty"`
	// Note warns of an effect the outcome does not show, such as a
	// sequence change a rollback kept or an invalid index a failed
	// concurrent build left behind.
	Note string `json:"note,omitempty"`
}

// Action mirrors a row of the proposed_actions table.
type Action struct {
	ID             int64             `json:"id"`
	ConnectionID   int               `json:"connection_id"`
	DatabaseName   *string           `json:"database_name"`
	Statements     []string          `json:"statements"`
	Source         string            `json:"source"`
	ProposedBy     string            `json:"proposed_by"`
	ProposedVia    string            `json:"proposed_via"`
	ConversationID *string           `json:"conversation_id"`
	AlertID        *int64            `json:"alert_id"`
	ParentActionID *int64            `json:"parent_action_id"`
	Attempt        int               `json:"attempt"`
	Rationale      *string           `json:"rationale"`
	Evidence       json.RawMessage   `json:"evidence"`
	ReviewStatus   string            `json:"review_status"`
	Review         *Verdict          `json:"review"`
	Approval       string            `json:"approval"`
	ApprovedBy     *string           `json:"approved_by"`
	ApprovedVia    *string           `json:"approved_via"`
	ApprovedAt     *time.Time        `json:"approved_at"`
	ExecStatus     string            `json:"exec_status"`
	Transactional  *bool             `json:"transactional"`
	Results        []StatementResult `json:"results"`
	Preview        json.RawMessage   `json:"preview"`
	Undo           json.RawMessage   `json:"undo"`
	Verification   json.RawMessage   `json:"verification"`
	Outcome        json.RawMessage   `json:"outcome"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	ExecutedAt     *time.Time        `json:"executed_at"`
}

// NewAction is the input to Store.Create.
type NewAction struct {
	ConnectionID   int
	DatabaseName   *string
	Statements     []string
	Source         string
	ProposedBy     string
	ProposedVia    string
	ConversationID *string
	AlertID        *int64
	ParentActionID *int64
	// Attempt is the position in a veto chain; zero means the first.
	Attempt   int
	Rationale string
	Evidence  json.RawMessage
}
