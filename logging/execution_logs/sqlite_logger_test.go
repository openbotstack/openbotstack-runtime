package execution_logs

import (
	"context"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-runtime/persistence"
)

func setupAuditTestDB(t *testing.T) *persistence.DB {
	t.Helper()
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestLogAndQuery(t *testing.T) {
	db := setupAuditTestDB(t)
	defer db.Close()
	ctx := context.Background()

	logger := NewSQLiteAuditLogger(db.DB)

	events := []Event{
		{ID: "e1", TenantID: "t1", Action: "execute", Timestamp: time.Now()},
		{ID: "e2", TenantID: "t1", Action: "query", Timestamp: time.Now()},
		{ID: "e3", TenantID: "t2", Action: "execute", Timestamp: time.Now()},
	}
	for _, e := range events {
		if err := logger.Log(ctx, e); err != nil {
			t.Fatalf("Log %s: %v", e.ID, err)
		}
	}

	results, err := logger.Query(ctx, QueryFilter{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Query tenant t1 returned %d events, want 2", len(results))
	}
}

func TestQueryByTimeRange(t *testing.T) {
	db := setupAuditTestDB(t)
	defer db.Close()
	ctx := context.Background()

	logger := NewSQLiteAuditLogger(db.DB)

	past := time.Now().Add(-2 * time.Hour)
	recent := time.Now()

	if err := logger.Log(ctx, Event{ID: "old", TenantID: "t1", Action: "test", Timestamp: past}); err != nil {
		t.Fatalf("Log old: %v", err)
	}
	if err := logger.Log(ctx, Event{ID: "new", TenantID: "t1", Action: "test", Timestamp: recent}); err != nil {
		t.Fatalf("Log new: %v", err)
	}

	from := time.Now().Add(-1 * time.Hour)
	results, err := logger.Query(ctx, QueryFilter{TenantID: "t1", From: from})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Query with time range returned %d events, want 1", len(results))
	}
	if len(results) > 0 && results[0].ID != "new" {
		t.Errorf("got event %q, want new", results[0].ID)
	}
}

func TestQueryByRequestID(t *testing.T) {
	db := setupAuditTestDB(t)
	defer db.Close()
	ctx := context.Background()

	logger := NewSQLiteAuditLogger(db.DB)
	if err := logger.Log(ctx, Event{ID: "e1", TenantID: "t1", RequestID: "req-123", Action: "test", Timestamp: time.Now()}); err != nil {
		t.Fatalf("Log e1: %v", err)
	}
	if err := logger.Log(ctx, Event{ID: "e2", TenantID: "t1", RequestID: "req-456", Action: "test", Timestamp: time.Now()}); err != nil {
		t.Fatalf("Log e2: %v", err)
	}

	results, err := logger.Query(ctx, QueryFilter{RequestID: "req-123"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Query by RequestID returned %d events, want 1", len(results))
	}
}

func TestCount(t *testing.T) {
	db := setupAuditTestDB(t)
	defer db.Close()
	ctx := context.Background()

	logger := NewSQLiteAuditLogger(db.DB)
	logger.Log(ctx, Event{ID: "e1", TenantID: "t1", Action: "a", Timestamp: time.Now()})
	logger.Log(ctx, Event{ID: "e2", TenantID: "t1", Action: "b", Timestamp: time.Now()})
	logger.Log(ctx, Event{ID: "e3", TenantID: "t2", Action: "a", Timestamp: time.Now()})

	count, err := logger.Count(ctx, QueryFilter{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 2 {
		t.Errorf("Count for t1 = %d, want 2", count)
	}
}

func TestQueryEmptyResult(t *testing.T) {
	db := setupAuditTestDB(t)
	defer db.Close()

	logger := NewSQLiteAuditLogger(db.DB)
	results, err := logger.Query(context.Background(), QueryFilter{TenantID: "nonexistent"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Query returned %d events, want 0", len(results))
	}
}
