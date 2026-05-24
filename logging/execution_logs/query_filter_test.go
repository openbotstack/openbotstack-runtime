package execution_logs

import (
	"context"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
)

func TestInMemoryAuditLogger_FilterBySource(t *testing.T) {
	logger := NewInMemoryAuditLogger()

	events := []audit.AuditEvent{
		{ID: "e1", Action: "skills.execute", Source: audit.SourceExecutor, Timestamp: time.Now()},
		{ID: "e2", Action: "policy.enforce", Source: audit.SourcePolicy, Timestamp: time.Now()},
		{ID: "e3", Action: "admin.provider.updated", Source: audit.SourceAdmin, Timestamp: time.Now()},
		{ID: "e4", Action: "skills.execute", Source: audit.SourceExecutor, Timestamp: time.Now()},
	}

	for _, e := range events {
		if err := logger.Log(context.Background(), e); err != nil {
			t.Fatalf("Log failed: %v", err)
		}
	}

	// Filter by executor source
	results, err := logger.Query(context.Background(), QueryFilter{Source: audit.SourceExecutor})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("executor filter: got %d, want 2", len(results))
	}

	// Filter by policy source
	results, err = logger.Query(context.Background(), QueryFilter{Source: audit.SourcePolicy})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("policy filter: got %d, want 1", len(results))
	}

	// Filter by admin source
	results, err = logger.Query(context.Background(), QueryFilter{Source: audit.SourceAdmin})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("admin filter: got %d, want 1", len(results))
	}

	// No filter (empty source)
	results, err = logger.Query(context.Background(), QueryFilter{})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(results) != 4 {
		t.Errorf("no source filter: got %d, want 4", len(results))
	}
}

func TestInMemoryAuditLogger_CountBySource(t *testing.T) {
	logger := NewInMemoryAuditLogger()

	events := []audit.AuditEvent{
		{ID: "e1", Action: "test", Source: audit.SourceExecutor, Timestamp: time.Now()},
		{ID: "e2", Action: "test", Source: audit.SourceExecutor, Timestamp: time.Now()},
		{ID: "e3", Action: "test", Source: audit.SourcePolicy, Timestamp: time.Now()},
	}

	for _, e := range events {
		logger.Log(context.Background(), e)
	}

	count, err := logger.Count(context.Background(), QueryFilter{Source: audit.SourceExecutor})
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 2 {
		t.Errorf("executor count: got %d, want 2", count)
	}

	count, err = logger.Count(context.Background(), QueryFilter{Source: audit.SourcePolicy})
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 1 {
		t.Errorf("policy count: got %d, want 1", count)
	}
}

func TestInMemoryAuditLogger_CombinedSourceAndTenantFilter(t *testing.T) {
	logger := NewInMemoryAuditLogger()

	events := []audit.AuditEvent{
		{ID: "e1", TenantID: "t1", Action: "test", Source: audit.SourceExecutor, Timestamp: time.Now()},
		{ID: "e2", TenantID: "t1", Action: "test", Source: audit.SourcePolicy, Timestamp: time.Now()},
		{ID: "e3", TenantID: "t2", Action: "test", Source: audit.SourceExecutor, Timestamp: time.Now()},
	}

	for _, e := range events {
		logger.Log(context.Background(), e)
	}

	results, err := logger.Query(context.Background(), QueryFilter{
		TenantID: "t1",
		Source:   audit.SourceExecutor,
	})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("t1+executor: got %d, want 1", len(results))
	}
	if results[0].ID != "e1" {
		t.Errorf("expected e1, got %s", results[0].ID)
	}
}

func TestInMemoryAuditLogger_ZeroSourceEvents(t *testing.T) {
	logger := NewInMemoryAuditLogger()

	// Event with no explicit source
	logger.Log(context.Background(), audit.AuditEvent{
		ID: "e1", Action: "test", Timestamp: time.Now(),
	})

	// Source filter should not match empty source
	results, err := logger.Query(context.Background(), QueryFilter{Source: audit.SourceExecutor})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("source filter on zero-source event: got %d, want 0", len(results))
	}

	// No filter should match
	results, err = logger.Query(context.Background(), QueryFilter{})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("no filter: got %d, want 1", len(results))
	}
}
