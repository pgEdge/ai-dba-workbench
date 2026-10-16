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
	"encoding/json"

	"github.com/pgedge/ai-workbench/pkg/pgsettings"
)

// The collector redacts credential-bearing settings such as
// primary_conninfo before storing them, and its schema migration 25
// rewrites rows stored earlier (GitHub issue #643). The functions here
// redact the same values again as they leave the server, which covers a
// datastore the upgraded collector has not migrated yet.

// redactTimelineEvents redacts the setting values in the details of
// every configuration change event.
func redactTimelineEvents(events []TimelineEvent) {
	for i := range events {
		if events[i].EventType == EventTypeConfigChange && events[i].Details != nil {
			events[i].Details = redactConfigChangeDetails(events[i].Details)
		}
	}
}

// configChangeValueKeys are the keys of a configuration change entry
// that hold a setting value.
var configChangeValueKeys = []string{"old_value", "new_value"}

// redactConfigChangeDetails redacts old_value and new_value in each
// change entry whose setting can hold a credential, leaving the rest of
// the details as stored. Details that cannot be parsed are dropped
// rather than passed through, since they could hold a value that has not
// been checked.
func redactConfigChangeDetails(details json.RawMessage) json.RawMessage {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(details, &top); err != nil {
		return nil
	}
	rawChanges, ok := top["changes"]
	if !ok {
		return details
	}
	var changes []map[string]json.RawMessage
	if err := json.Unmarshal(rawChanges, &changes); err != nil {
		return nil
	}

	changed := false
	for _, change := range changes {
		var name string
		if err := json.Unmarshal(change["name"], &name); err != nil ||
			!pgsettings.IsCredentialSetting(name) {
			continue
		}
		for _, key := range configChangeValueKeys {
			raw, present := change[key]
			if !present {
				continue
			}
			change[key] = redactSettingJSONValue(name, raw)
			changed = true
		}
	}
	if !changed {
		return details
	}

	// Every value here was parsed from JSON or produced by
	// redactSettingJSONValue, so marshaling cannot fail; a failure would
	// still drop the details rather than return them unredacted.
	out, err := json.Marshal(changes)
	if err == nil {
		top["changes"] = out
		out, err = json.Marshal(top)
	}
	if err != nil {
		return nil
	}
	return out
}

// redactSettingJSONValue redacts one JSON-encoded value of the named
// setting. null stays null; a string is redacted; anything else is
// replaced outright.
func redactSettingJSONValue(name string, raw json.RawMessage) json.RawMessage {
	var value any
	if err := json.Unmarshal(raw, &value); err == nil {
		switch v := value.(type) {
		case nil:
			return json.RawMessage("null")
		case string:
			return jsonString(pgsettings.RedactValue(name, v))
		}
	}
	return jsonString(pgsettings.RedactedValue)
}

// jsonString encodes s as a JSON string. Marshaling a Go string cannot
// fail: invalid UTF-8 is replaced rather than rejected.
func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s) //nolint:errcheck // cannot fail for a string
	return b
}
