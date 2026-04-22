/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

// Package worker provides reusable concurrency primitives for background
// task processing. It includes a generic bounded worker pool for parallel
// job execution and a periodic task runner for scheduled background work.
package worker

import (
	"sync"
)

// WorkerPool is a generic bounded worker pool that processes jobs of type T.
// It provides non-blocking job submission with backpressure (returns false
// when the queue is full) and graceful shutdown with wait for completion.
//
// Example usage:
//
//	pool := worker.NewWorkerPool(4, 100, func(job MyJob) {
//	    // Process job
//	})
//	pool.Start()
//	defer pool.Stop()
//
//	if !pool.Submit(myJob) {
//	    // Queue full, handle backpressure
//	}
type WorkerPool[T any] struct {
	size      int
	queue     chan T
	handler   func(T)
	stopChan  chan struct{}
	wg        sync.WaitGroup
	startOnce sync.Once
	stopOnce  sync.Once
}

// NewWorkerPool creates a new worker pool with the specified configuration.
//
// Parameters:
//   - size: Number of worker goroutines to spawn
//   - queueSize: Size of the buffered job queue (determines backpressure threshold)
//   - handler: Function called to process each job
//
// The pool must be started with Start() before jobs can be processed.
func NewWorkerPool[T any](size int, queueSize int, handler func(T)) *WorkerPool[T] {
	if size <= 0 {
		size = 1
	}
	if queueSize <= 0 {
		queueSize = 1
	}
	return &WorkerPool[T]{
		size:     size,
		queue:    make(chan T, queueSize),
		handler:  handler,
		stopChan: make(chan struct{}),
	}
}

// Start spawns the worker goroutines. It is safe to call multiple times;
// only the first call has any effect.
func (p *WorkerPool[T]) Start() {
	p.startOnce.Do(func() {
		for i := 0; i < p.size; i++ {
			p.wg.Add(1)
			go p.worker()
		}
	})
}

// worker is the main loop for each worker goroutine.
func (p *WorkerPool[T]) worker() {
	defer p.wg.Done()

	for {
		select {
		case <-p.stopChan:
			return
		case job, ok := <-p.queue:
			if !ok {
				return
			}
			p.handler(job)
		}
	}
}

// Submit adds a job to the queue for processing. It is non-blocking and
// returns true if the job was queued successfully, or false if the queue
// is full. This allows callers to implement their own backpressure handling.
//
// Returns false if the pool has been stopped.
func (p *WorkerPool[T]) Submit(job T) bool {
	// Check if stopped first to avoid select's pseudo-random case selection
	// when both stopChan and queue are ready.
	select {
	case <-p.stopChan:
		return false
	default:
	}

	select {
	case <-p.stopChan:
		return false
	case p.queue <- job:
		return true
	default:
		// Queue is full
		return false
	}
}

// SubmitWait adds a job to the queue, blocking until space is available
// or the pool is stopped. Returns true if the job was queued, false if
// the pool was stopped before the job could be queued.
func (p *WorkerPool[T]) SubmitWait(job T) bool {
	// Check if stopped first to avoid select's pseudo-random case selection
	// when both stopChan and queue are ready.
	select {
	case <-p.stopChan:
		return false
	default:
	}

	select {
	case <-p.stopChan:
		return false
	case p.queue <- job:
		return true
	}
}

// Stop gracefully shuts down the worker pool. It signals all workers to
// stop and waits for in-flight jobs to complete.
// It is safe to call multiple times; only the first call has any effect.
//
// After Stop returns, all workers have exited and no more jobs will be
// processed.
func (p *WorkerPool[T]) Stop() {
	p.stopOnce.Do(func() {
		close(p.stopChan)
		p.wg.Wait()
		// Don't close p.queue - workers already exited via stopChan,
		// and closing it causes panic in post-Stop Submit/SubmitWait calls.
	})
}

// QueueLength returns the current number of jobs waiting in the queue.
// This can be used for monitoring or adaptive backpressure.
func (p *WorkerPool[T]) QueueLength() int {
	return len(p.queue)
}

// QueueCapacity returns the maximum queue size.
func (p *WorkerPool[T]) QueueCapacity() int {
	return cap(p.queue)
}
