/*-------------------------------------------------------------------------
 *
 * pgEdge AI DBA Workbench
 *
 * Copyright (c) 2025 - 2026, pgEdge, Inc.
 * This software is released under The PostgreSQL License
 *
 *-------------------------------------------------------------------------
 */

package worker

import (
    "sync"
    "sync/atomic"
    "testing"
    "time"
)

func TestWorkerPool_Basic(t *testing.T) {
    var processed atomic.Int32

    pool := NewWorkerPool(2, 10, func(job int) {
        processed.Add(1)
    })
    pool.Start()

    // Submit jobs
    for i := 0; i < 5; i++ {
        if !pool.Submit(i) {
            t.Error("Expected job to be submitted")
        }
    }

    // Give workers time to process
    time.Sleep(100 * time.Millisecond)

    pool.Stop()

    if got := processed.Load(); got != 5 {
        t.Errorf("Expected 5 jobs processed, got %d", got)
    }
}

func TestWorkerPool_Backpressure(t *testing.T) {
    // Create a slow worker with small queue
    pool := NewWorkerPool(1, 2, func(job int) {
        time.Sleep(100 * time.Millisecond)
    })
    pool.Start()
    defer pool.Stop()

    // Submit more jobs than the queue can hold
    successCount := 0
    for i := 0; i < 10; i++ {
        if pool.Submit(i) {
            successCount++
        }
    }

    // We should have filled the queue (2) plus one in-flight
    // Some submissions should have failed
    if successCount >= 10 {
        t.Errorf("Expected some submissions to fail due to backpressure, but all %d succeeded", successCount)
    }
}

func TestWorkerPool_SubmitWait(t *testing.T) {
    var processed atomic.Int32

    pool := NewWorkerPool(1, 1, func(job int) {
        time.Sleep(10 * time.Millisecond)
        processed.Add(1)
    })
    pool.Start()

    // Submit jobs that block
    var wg sync.WaitGroup
    for i := 0; i < 5; i++ {
        wg.Add(1)
        go func(job int) {
            defer wg.Done()
            pool.SubmitWait(job)
        }(i)
    }

    // Wait for all submissions to complete
    wg.Wait()

    // Give the last job time to be processed before stopping
    time.Sleep(50 * time.Millisecond)
    pool.Stop()

    if got := processed.Load(); got != 5 {
        t.Errorf("Expected 5 jobs processed, got %d", got)
    }
}

func TestWorkerPool_GracefulShutdown(t *testing.T) {
    var processed atomic.Int32

    pool := NewWorkerPool(2, 10, func(job int) {
        time.Sleep(50 * time.Millisecond)
        processed.Add(1)
    })
    pool.Start()

    // Submit jobs
    for i := 0; i < 4; i++ {
        pool.Submit(i)
    }

    // Give workers time to pick up jobs before stopping
    time.Sleep(10 * time.Millisecond)

    // Stop should wait for in-flight jobs
    pool.Stop()

    // Jobs that were already picked up should complete
    // With 2 workers and giving time to start, at least 2 should be in-flight
    if got := processed.Load(); got < 2 {
        t.Errorf("Expected at least 2 jobs to complete before shutdown, got %d", got)
    }
}

func TestWorkerPool_SubmitAfterStop(t *testing.T) {
    pool := NewWorkerPool(1, 10, func(job int) {})
    pool.Start()
    pool.Stop()

    // After Stop, Submit should cleanly return false without panicking.
    // The pool's select statement checks stopChan first, which is closed
    // before the queue channel is closed.
    result := pool.Submit(1)
    if result {
        t.Error("Expected Submit to return false after Stop")
    }

    result = pool.SubmitWait(1)
    if result {
        t.Error("Expected SubmitWait to return false after Stop")
    }
}

func TestWorkerPool_MultipleStartCalls(t *testing.T) {
    var workerCount atomic.Int32

    pool := NewWorkerPool(3, 10, func(job int) {
        workerCount.Add(1)
        time.Sleep(50 * time.Millisecond)
        workerCount.Add(-1)
    })

    // Call Start multiple times - should only create workers once
    pool.Start()
    pool.Start()
    pool.Start()

    // Submit jobs
    for i := 0; i < 3; i++ {
        pool.Submit(i)
    }

    time.Sleep(25 * time.Millisecond)

    // Should have at most 3 workers active (not 9)
    if got := workerCount.Load(); got > 3 {
        t.Errorf("Expected at most 3 workers, but %d are active", got)
    }

    pool.Stop()
}

func TestWorkerPool_MultipleStopCalls(t *testing.T) {
    pool := NewWorkerPool(2, 10, func(job int) {})
    pool.Start()

    // Call Stop multiple times - should not panic
    pool.Stop()
    pool.Stop()
    pool.Stop()
}

func TestWorkerPool_QueueMetrics(t *testing.T) {
    pool := NewWorkerPool(1, 100, func(job int) {
        time.Sleep(50 * time.Millisecond)
    })
    pool.Start()
    defer pool.Stop()

    // Check capacity
    if got := pool.QueueCapacity(); got != 100 {
        t.Errorf("Expected queue capacity 100, got %d", got)
    }

    // Submit some jobs
    for i := 0; i < 10; i++ {
        pool.Submit(i)
    }

    // Queue length should be > 0 (minus one being processed)
    if got := pool.QueueLength(); got < 5 {
        t.Errorf("Expected queue length >= 5, got %d", got)
    }
}

func TestWorkerPool_DefaultValues(t *testing.T) {
    // Zero values should be corrected
    pool := NewWorkerPool(0, 0, func(job int) {})

    if pool.size != 1 {
        t.Errorf("Expected default size 1, got %d", pool.size)
    }
    if cap(pool.queue) != 1 {
        t.Errorf("Expected default queue capacity 1, got %d", cap(pool.queue))
    }
}

func TestWorkerPool_ConcurrentSubmit(t *testing.T) {
    var processed atomic.Int32

    pool := NewWorkerPool(4, 100, func(job int) {
        processed.Add(1)
    })
    pool.Start()

    // Submit from multiple goroutines
    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(start int) {
            defer wg.Done()
            for j := 0; j < 10; j++ {
                pool.Submit(start*10 + j)
            }
        }(i)
    }

    wg.Wait()
    time.Sleep(100 * time.Millisecond)
    pool.Stop()

    if got := processed.Load(); got != 100 {
        t.Errorf("Expected 100 jobs processed, got %d", got)
    }
}
