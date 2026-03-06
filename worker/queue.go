// Package worker implements async job queue for skill execution.
package worker

import (
	"context"
	"sync"
)

// Job represents an async work item.
type Job struct {
	ID      string
	Type    string
	Payload map[string]interface{}
}

// JobHandler processes a job.
type JobHandler func(ctx context.Context, job Job) error

// QueueStatus contains queue statistics.
type QueueStatus struct {
	Workers   int
	Pending   int
	Processed int64
}

// JobQueue manages async job processing.
type JobQueue struct {
	workers    int
	jobs       chan Job
	handler    JobHandler
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.RWMutex
	processed  int64
	pendingLen int
}

// NewJobQueue creates a new queue with specified worker count.
func NewJobQueue(workers int) *JobQueue {
	ctx, cancel := context.WithCancel(context.Background())
	q := &JobQueue{
		workers: workers,
		jobs:    make(chan Job, 100),
		ctx:     ctx,
		cancel:  cancel,
	}
	q.startWorkers()
	return q
}

// SetHandler sets the job processing function.
func (q *JobQueue) SetHandler(h JobHandler) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handler = h
}

// Submit adds a job to the queue.
func (q *JobQueue) Submit(ctx context.Context, job Job) error {
	select {
	case q.jobs <- job:
		q.mu.Lock()
		q.pendingLen++
		q.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-q.ctx.Done():
		return q.ctx.Err()
	}
}

// Stop gracefully shuts down the queue.
func (q *JobQueue) Stop() error {
	q.cancel()
	close(q.jobs)
	q.wg.Wait()
	return nil
}

// Status returns current queue status.
func (q *JobQueue) Status() QueueStatus {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return QueueStatus{
		Workers:   q.workers,
		Pending:   len(q.jobs),
		Processed: q.processed,
	}
}

func (q *JobQueue) startWorkers() {
	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}
}

func (q *JobQueue) worker() {
	defer q.wg.Done()
	for {
		select {
		case job, ok := <-q.jobs:
			if !ok {
				return
			}
			q.processJob(job)
		case <-q.ctx.Done():
			return
		}
	}
}

func (q *JobQueue) processJob(job Job) {
	q.mu.RLock()
	handler := q.handler
	q.mu.RUnlock()

	if handler != nil {
		_ = handler(q.ctx, job)
	}

	q.mu.Lock()
	q.processed++
	q.pendingLen--
	q.mu.Unlock()
}
