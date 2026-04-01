package execution_logs_test

import (
	"context"
	"testing"
	"time"

	audit "github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
)

func TestAuditLoggerLog(t *testing.T) {
	logger := audit.NewInMemoryAuditLogger()
	ctx := context.Background()

	event := audit.Event{
		ID:        "evt-1",
		TenantID:  "tenant-1",
		UserID:    "user-1",
		RequestID: "req-1",
		Action:    "skills.execute",
		Resource:  "skill/search",
		Outcome:   "success",
		Timestamp: time.Now(),
	}

	err := logger.Log(ctx, event)
	if err != nil {
		t.Fatalf("Log failed: %v", err)
	}
}

func TestAuditLoggerQuery(t *testing.T) {
	logger := audit.NewInMemoryAuditLogger()
	ctx := context.Background()

	events := []audit.Event{
		{ID: "e1", TenantID: "t1", Action: "skills.execute", Timestamp: time.Now()},
		{ID: "e2", TenantID: "t1", Action: "model.generate", Timestamp: time.Now()},
		{ID: "e3", TenantID: "t2", Action: "skills.execute", Timestamp: time.Now()},
	}

	for _, e := range events {
		_ = logger.Log(ctx, e)
	}

	result, err := logger.Query(ctx, audit.QueryFilter{
		TenantID: "t1",
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 events for t1, got %d", len(result))
	}
}

func TestAuditLoggerQueryByAction(t *testing.T) {
	logger := audit.NewInMemoryAuditLogger()
	ctx := context.Background()

	events := []audit.Event{
		{ID: "a1", TenantID: "t1", Action: "skills.execute", Timestamp: time.Now()},
		{ID: "a2", TenantID: "t1", Action: "skills.execute", Timestamp: time.Now()},
		{ID: "a3", TenantID: "t1", Action: "model.generate", Timestamp: time.Now()},
	}

	for _, e := range events {
		_ = logger.Log(ctx, e)
	}

	result, err := logger.Query(ctx, audit.QueryFilter{
		TenantID: "t1",
		Action:   "skills.execute",
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 skills.execute events, got %d", len(result))
	}
}

func TestAuditLoggerQueryByTimeRange(t *testing.T) {
	logger := audit.NewInMemoryAuditLogger()
	ctx := context.Background()

	now := time.Now()
	events := []audit.Event{
		{ID: "t1", TenantID: "tx", Action: "test", Timestamp: now.Add(-2 * time.Hour)},
		{ID: "t2", TenantID: "tx", Action: "test", Timestamp: now.Add(-30 * time.Minute)},
		{ID: "t3", TenantID: "tx", Action: "test", Timestamp: now},
	}

	for _, e := range events {
		_ = logger.Log(ctx, e)
	}

	result, err := logger.Query(ctx, audit.QueryFilter{
		TenantID: "tx",
		From:     now.Add(-1 * time.Hour),
		To:       now.Add(1 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 events in time range, got %d", len(result))
	}
}

func TestAuditLoggerCount(t *testing.T) {
	logger := audit.NewInMemoryAuditLogger()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_ = logger.Log(ctx, audit.Event{
			ID:        "c" + string(rune('0'+i)),
			TenantID:  "count-tenant",
			Action:    "test",
			Timestamp: time.Now(),
		})
	}

	count, err := logger.Count(ctx, audit.QueryFilter{TenantID: "count-tenant"})
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}

	if count != 5 {
		t.Errorf("Expected count 5, got %d", count)
	}
}
