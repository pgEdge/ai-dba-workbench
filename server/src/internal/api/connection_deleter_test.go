/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */
package api

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

// fakeConnectionDeleteStore records the steps connectionDeleter runs
// and fails whichever step it is told to. release, when set, holds the
// purge until it is closed.
type fakeConnectionDeleteStore struct {
	disableErr, purgeErr, deleteErr error
	release                         chan struct{}

	mu    sync.Mutex
	calls []string
	ctxs  []context.Context
}

func (f *fakeConnectionDeleteStore) record(ctx context.Context, step string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, step)
	f.ctxs = append(f.ctxs, ctx)
}

func (f *fakeConnectionDeleteStore) steps() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeConnectionDeleteStore) DisableConnectionMonitoring(ctx context.Context, _ int) error {
	f.record(ctx, "disable")
	return f.disableErr
}

func (f *fakeConnectionDeleteStore) PurgeConnectionMetrics(ctx context.Context, _ int) error {
	f.record(ctx, "purge")
	if f.release != nil {
		<-f.release
	}
	return f.purgeErr
}

func (f *fakeConnectionDeleteStore) DeleteConnection(ctx context.Context, _ int) error {
	f.record(ctx, "delete")
	return f.deleteErr
}

func waitForJob(t *testing.T, job *connectionDeleteJob) {
	t.Helper()
	select {
	case <-job.done:
	case <-time.After(5 * time.Second):
		t.Fatal("delete job did not finish")
	}
}

// TestConnectionDeleter_Steps checks the order of the steps, that a
// failing step stops the ones after it, and the error each produces.
func TestConnectionDeleter_Steps(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name      string
		store     *fakeConnectionDeleteStore
		wantSteps []string
		wantErr   error
	}{
		{"success", &fakeConnectionDeleteStore{},
			[]string{"disable", "purge", "delete"}, nil},
		{"disable fails", &fakeConnectionDeleteStore{disableErr: boom},
			[]string{"disable"}, boom},
		{"purge fails", &fakeConnectionDeleteStore{purgeErr: boom},
			[]string{"disable", "purge"}, boom},
		{"delete fails", &fakeConnectionDeleteStore{deleteErr: boom},
			[]string{"disable", "purge", "delete"}, boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newConnectionDeleter(tt.store)
			job := d.start(context.Background(), 1)
			waitForJob(t, job)

			if !errors.Is(job.err, tt.wantErr) || (tt.wantErr == nil && job.err != nil) {
				t.Errorf("err = %v, want %v", job.err, tt.wantErr)
			}
			if got := tt.store.steps(); !reflect.DeepEqual(got, tt.wantSteps) {
				t.Errorf("steps = %v, want %v", got, tt.wantSteps)
			}
			d.mu.Lock()
			left := len(d.inflight)
			d.mu.Unlock()
			if left != 0 {
				t.Errorf("%d jobs left in flight", left)
			}
		})
	}
}

// TestConnectionDeleter_JoinsRunningJob checks that a second request
// for the same connection joins the running delete, that a different
// connection gets its own, and that canceling the starting context does
// not stop the job.
func TestConnectionDeleter_JoinsRunningJob(t *testing.T) {
	store := &fakeConnectionDeleteStore{release: make(chan struct{})}
	d := newConnectionDeleter(store)

	ctx, cancel := context.WithCancel(context.Background())
	first := d.start(ctx, 1)
	cancel()
	if second := d.start(context.Background(), 1); second != first {
		t.Fatal("second request for the same connection started a new job")
	}
	if other := d.start(context.Background(), 2); other == first {
		t.Fatal("a different connection joined the first job")
	}

	close(store.release)
	waitForJob(t, first)
	if first.err != nil {
		t.Fatalf("job failed: %v", first.err)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	for i, c := range store.ctxs {
		if c.Err() != nil {
			t.Errorf("step %s ran under a canceled context", store.calls[i])
		}
	}
}

// TestConnectionHandler_ConnectionDeleterDefault checks a handler
// builds its deleter on first use and keeps it.
func TestConnectionHandler_ConnectionDeleterDefault(t *testing.T) {
	h := &ConnectionHandler{}
	d := h.connectionDeleter()
	if d == nil || d.store == nil {
		t.Fatal("no deleter built")
	}
	if h.connectionDeleter() != d {
		t.Fatal("deleter rebuilt on second use")
	}
}
