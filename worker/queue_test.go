package worker_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-runtime/worker"
)

func TestJobQueueSubmit(t *testing.T) {
	queue := worker.NewJobQueue(4)
	defer queue.Stop() //nolint:errcheck // test cleanup

	job := worker.Job{
		ID:   "job-1",
		Type: "skill.execute",
		Payload: map[string]interface{}{
			"skill_id": "search",
		},
	}

	err := queue.Submit(context.Background(), job)
	if err != nil {
		t.Fatalf("Submit failed: %v", err)
	}
}

func TestJobQueueProcess(t *testing.T) {
	queue := worker.NewJobQueue(2)
	defer queue.Stop() //nolint:errcheck // test cleanup

	var processed atomic.Int32

	queue.SetHandler(func(ctx context.Context, job worker.Job) error {
		processed.Add(1)
		return nil
	})

	// Submit jobs
	for i := 0; i < 5; i++ {
		_ = queue.Submit(context.Background(), worker.Job{
			ID:   "job-" + string(rune('0'+i)),
			Type: "test",
		})
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	if processed.Load() != 5 {
		t.Errorf("Expected 5 processed, got %d", processed.Load())
	}
}

func TestJobQueueConcurrency(t *testing.T) {
	queue := worker.NewJobQueue(4) // 4 workers
	defer queue.Stop()             //nolint:errcheck // test cleanup

	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	queue.SetHandler(func(ctx context.Context, job worker.Job) error {
		c := concurrent.Add(1)
		if c > maxConcurrent.Load() {
			maxConcurrent.Store(c)
		}
		time.Sleep(50 * time.Millisecond)
		concurrent.Add(-1)
		return nil
	})

	// Submit many jobs
	for i := 0; i < 10; i++ {
		_ = queue.Submit(context.Background(), worker.Job{ID: "c-" + string(rune('0'+i))})
	}

	time.Sleep(300 * time.Millisecond)

	if maxConcurrent.Load() > 4 {
		t.Errorf("Expected max 4 concurrent, got %d", maxConcurrent.Load())
	}
}

func TestJobQueueStop(t *testing.T) {
	queue := worker.NewJobQueue(2)

	_ = queue.Submit(context.Background(), worker.Job{ID: "stop-test"})

	err := queue.Stop()
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

func TestJobQueueStatus(t *testing.T) {
	queue := worker.NewJobQueue(2)
	defer queue.Stop() //nolint:errcheck // test cleanup

	status := queue.Status()
	if status.Workers != 2 {
		t.Errorf("Expected 2 workers, got %d", status.Workers)
	}
}
