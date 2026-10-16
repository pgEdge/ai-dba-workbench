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
	"fmt"
	"log"
	"sync"
)

// connectionDeleteStore is the part of the datastore a connection
// delete uses; *database.Datastore satisfies it.
type connectionDeleteStore interface {
	DisableConnectionMonitoring(ctx context.Context, id int) error
	PurgeConnectionMetrics(ctx context.Context, id int) error
	DeleteConnection(ctx context.Context, id int) error
}

// connectionDeleteJob is one connection delete in progress. done is
// closed once the delete has finished, after which err holds its
// outcome.
type connectionDeleteJob struct {
	done chan struct{}
	err  error
}

// connectionDeleter runs connection deletes in the background so that
// they finish however long the connection's metrics history takes to
// clear, rather than failing at a request deadline (issue #669). A
// second request for a connection already being deleted joins the
// running job instead of starting another.
type connectionDeleter struct {
	store connectionDeleteStore

	mu       sync.Mutex
	inflight map[int]*connectionDeleteJob
}

func newConnectionDeleter(store connectionDeleteStore) *connectionDeleter {
	return &connectionDeleter{
		store:    store,
		inflight: make(map[int]*connectionDeleteJob),
	}
}

// start returns the running delete of connection id, starting one if
// none is running. The job runs under ctx with its cancellation
// removed, so neither a client disconnecting nor a handler giving up
// waiting stops it; every step commits in short transactions, so a
// server restart part way through loses no progress either.
func (d *connectionDeleter) start(ctx context.Context, id int) *connectionDeleteJob {
	d.mu.Lock()
	defer d.mu.Unlock()

	if job, ok := d.inflight[id]; ok {
		return job
	}
	job := &connectionDeleteJob{done: make(chan struct{})}
	d.inflight[id] = job

	go func() {
		job.err = d.run(context.WithoutCancel(ctx), id)
		if job.err != nil {
			log.Printf("[ERROR] Failed to delete connection (id=%d): %v", id, job.err)
		}
		d.mu.Lock()
		delete(d.inflight, id)
		d.mu.Unlock()
		close(job.done)
	}()
	return job
}

// run stops collection for the connection, clears its metrics in
// batches and then deletes the row, whose cascade is left with little
// more than whatever the collector wrote before it noticed.
func (d *connectionDeleter) run(ctx context.Context, id int) error {
	if err := d.store.DisableConnectionMonitoring(ctx, id); err != nil {
		return err
	}
	if err := d.store.PurgeConnectionMetrics(ctx, id); err != nil {
		return fmt.Errorf("purging metrics: %w", err)
	}
	return d.store.DeleteConnection(ctx, id)
}
