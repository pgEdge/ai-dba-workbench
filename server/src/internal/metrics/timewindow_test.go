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
	"strings"
	"testing"
	"time"
)

// TestResolveTimeWindow_Presets confirms that every preset still resolves
// through ParseTimeRange and that the supplied timestamps are ignored on
// that path, so an odd pair of timestamps cannot influence a preset query.
func TestResolveTimeWindow_Presets(t *testing.T) {
	for rangeName, wantDur := range ValidTimeRanges {
		t.Run(rangeName, func(t *testing.T) {
			window, err := ResolveTimeWindow(rangeName,
				"not-a-timestamp", "also-not-a-timestamp")
			if err != nil {
				t.Fatalf("ResolveTimeWindow(%q) unexpected error: %v",
					rangeName, err)
			}

			gotDur := window.End.Sub(window.Start)
			diff := gotDur - wantDur
			if diff < 0 {
				diff = -diff
			}
			if diff > 2*time.Second {
				t.Errorf("span = %v, want ~%v", gotDur, wantDur)
			}
			if window.End.After(time.Now().UTC().Add(2 * time.Second)) {
				t.Errorf("preset end %v is in the future", window.End)
			}
		})
	}
}

// TestResolveTimeWindow_Custom is the table-driven check over each
// validation rule for the custom path.
func TestResolveTimeWindow_Custom(t *testing.T) {
	now := time.Now().UTC()
	iso := func(t time.Time) string { return t.Format(time.RFC3339) }

	tests := []struct {
		name      string
		timeRange string
		startISO  string
		endISO    string
		wantErr   string
		// wantEnd, when non-zero, is the end the resolver must produce
		// after clamping.
		checkClamp bool
	}{
		{
			name:      "unknown preset rejected and advertises custom",
			timeRange: "99z",
			wantErr:   `invalid time range "99z": must be one of 1h, 6h, 24h, 7d, 30d, custom`,
		},
		{
			name:      "empty time range rejected",
			timeRange: "",
			wantErr:   "must be one of",
		},
		{
			name:      "custom without either timestamp rejected",
			timeRange: "custom",
			wantErr:   "time_start and time_end are both required",
		},
		{
			name:      "custom without end rejected",
			timeRange: "custom",
			startISO:  iso(now.Add(-2 * time.Hour)),
			wantErr:   "time_start and time_end are both required",
		},
		{
			name:      "custom without start rejected",
			timeRange: "custom",
			endISO:    iso(now),
			wantErr:   "time_start and time_end are both required",
		},
		{
			name:      "unparsable start rejected",
			timeRange: "custom",
			startISO:  "yesterday",
			endISO:    iso(now),
			wantErr:   `invalid time_start "yesterday": must be an RFC 3339 timestamp`,
		},
		{
			name:      "unparsable end rejected",
			timeRange: "custom",
			startISO:  iso(now.Add(-time.Hour)),
			endISO:    "2026-07-29 12:00:00",
			wantErr:   `invalid time_end "2026-07-29 12:00:00": must be an RFC 3339 timestamp`,
		},
		{
			name:      "end before start rejected",
			timeRange: "custom",
			startISO:  iso(now.Add(-time.Hour)),
			endISO:    iso(now.Add(-2 * time.Hour)),
			wantErr:   "time_end must be after time_start",
		},
		{
			name:      "end equal to start rejected",
			timeRange: "custom",
			startISO:  iso(now.Add(-time.Hour)),
			endISO:    iso(now.Add(-time.Hour)),
			wantErr:   "time_end must be after time_start",
		},
		{
			name:      "start in the future rejected",
			timeRange: "custom",
			startISO:  iso(now.Add(time.Hour)),
			endISO:    iso(now.Add(2 * time.Hour)),
			wantErr:   "invalid time_start: must not be in the future",
		},
		{
			name:      "span beyond 366 days rejected",
			timeRange: "custom",
			startISO:  iso(now.Add(-367 * 24 * time.Hour)),
			endISO:    iso(now),
			wantErr:   "span must not exceed 366 days",
		},
		{
			name:      "span of exactly 366 days accepted",
			timeRange: "custom",
			startISO:  iso(now.Add(-366 * 24 * time.Hour)),
			endISO:    iso(now),
		},
		{
			name:      "valid past window accepted",
			timeRange: "custom",
			startISO:  iso(now.Add(-4 * time.Hour)),
			endISO:    iso(now.Add(-2 * time.Hour)),
		},
		{
			name:       "end in the future is clamped to now",
			timeRange:  "custom",
			startISO:   iso(now.Add(-2 * time.Hour)),
			endISO:     iso(now.Add(90 * time.Minute)),
			checkClamp: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			window, err := ResolveTimeWindow(
				tt.timeRange, tt.startISO, tt.endISO)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil",
						tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want it to contain %q",
						err.Error(), tt.wantErr)
				}
				if window != (TimeWindow{}) {
					t.Errorf("expected zero window on error, got %+v", window)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !window.End.After(window.Start) {
				t.Errorf("window %+v has a non-positive span", window)
			}
			if window.Start.Location() != time.UTC ||
				window.End.Location() != time.UTC {
				t.Errorf("window %+v is not in UTC", window)
			}

			if tt.checkClamp {
				// The clamp pins the end to the resolver's own idea of
				// now, which is a moment after the test captured its own.
				if window.End.After(time.Now().UTC()) {
					t.Errorf("clamped end %v is still in the future",
						window.End)
				}
				if window.End.Before(now) {
					t.Errorf("clamped end %v is before now %v",
						window.End, now)
				}
			} else if want := tt.endISO; want != "" {
				wantEnd, perr := time.Parse(time.RFC3339, want)
				if perr != nil {
					t.Fatalf("bad fixture end %q: %v", want, perr)
				}
				if !window.End.Equal(wantEnd) {
					t.Errorf("end = %v, want %v", window.End, wantEnd)
				}
			}
		})
	}
}

// TestResolveTimeWindow_NonUTCOffsetNormalised confirms that a timestamp
// carrying a non-zero UTC offset resolves to the same instant expressed in
// UTC, which is what the query layer's bucketing expects.
func TestResolveTimeWindow_NonUTCOffsetNormalised(t *testing.T) {
	start := "2026-01-02T10:00:00+05:30"
	end := "2026-01-02T12:00:00+05:30"

	window, err := ResolveTimeWindow(CustomTimeRange, start, end)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantStart := time.Date(2026, 1, 2, 4, 30, 0, 0, time.UTC)
	if !window.Start.Equal(wantStart) {
		t.Errorf("start = %v, want %v", window.Start, wantStart)
	}
	if got := window.End.Sub(window.Start); got != 2*time.Hour {
		t.Errorf("span = %v, want 2h", got)
	}
}
