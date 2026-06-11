package audit

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
	_ "modernc.org/sqlite"
)

// setupTestDB creates an in-memory SQLite DB with the audit_logs schema.
func setupSubscriberTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_logs (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL DEFAULT '',
			user_id TEXT NOT NULL DEFAULT '',
			request_id TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			resource TEXT NOT NULL DEFAULT '',
			outcome TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			metadata TEXT NOT NULL DEFAULT '{}',
			timestamp TEXT NOT NULL,
			signature TEXT NOT NULL DEFAULT '',
			step_id TEXT NOT NULL DEFAULT '',
			step_name TEXT NOT NULL DEFAULT '',
			step_type TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			tool_input TEXT NOT NULL DEFAULT '{}',
			tool_output TEXT NOT NULL DEFAULT '{}',
			error TEXT NOT NULL DEFAULT '',
			trace_id TEXT NOT NULL DEFAULT ''
		)
	`)
	if err != nil {
		t.Fatalf("create audit_logs table: %v", err)
	}
	return db
}

func TestSQLiteSubscriber_OnEvent_WritesToDB(t *testing.T) {
	db := setupSubscriberTestDB(t)
	defer db.Close()

	logger := execution_logs.NewSQLiteAuditLogger(db)
	sub := NewSQLiteAuditSubscriber(logger)

	event := audit.AuditEvent{
		ID:        "evt-1",
		TenantID:  "t1",
		UserID:    "u1",
		RequestID: "req-1",
		Action:    "harness.step",
		Resource:  "search",
		Outcome:   "success",
		Source:    audit.SourceExecutor,
		Duration:  100 * time.Millisecond,
		Timestamp: time.Now().UTC(),
		StepID:    "step-1",
		StepName:  "search",
		StepType:  "tool",
		Status:    "completed",
	}

	if err := sub.OnEvent(context.Background(), event); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}

	// Verify the event was written to SQLite
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE id = ?", event.ID).Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

func TestSQLiteSubscriber_ID(t *testing.T) {
	db := setupSubscriberTestDB(t)
	defer db.Close()

	logger := execution_logs.NewSQLiteAuditLogger(db)
	sub := NewSQLiteAuditSubscriber(logger)

	if sub.ID() != "sqlite-audit-subscriber" {
		t.Errorf("ID() = %q, want %q", sub.ID(), "sqlite-audit-subscriber")
	}
}

func TestSQLiteSubscriber_MultipleEvents(t *testing.T) {
	db := setupSubscriberTestDB(t)
	defer db.Close()

	logger := execution_logs.NewSQLiteAuditLogger(db)
	sub := NewSQLiteAuditSubscriber(logger)

	for i := 0; i < 5; i++ {
		event := audit.AuditEvent{
			ID:        "evt-" + string(rune('A'+i)),
			TenantID:  "t1",
			Action:    "harness.step",
			Resource:  "tool",
			Outcome:   "success",
			Source:    audit.SourceExecutor,
			Duration:  time.Duration(i+1) * 10 * time.Millisecond,
			Timestamp: time.Now().UTC(),
			StepID:    "step-" + string(rune('A'+i)),
			StepName:  "tool",
			StepType:  "tool",
			Status:    "completed",
		}
		if err := sub.OnEvent(context.Background(), event); err != nil {
			t.Fatalf("OnEvent(%d): %v", i, err)
		}
	}

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM audit_logs").Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5 rows, got %d", count)
	}
}

func TestEmitterWithSQLiteSubscriber_Integration(t *testing.T) {
	db := setupSubscriberTestDB(t)
	defer db.Close()

	// Create emitter (core)
	emitter := audit.NewEmitter()

	// Create subscriber wrapping SQLite logger
	logger := execution_logs.NewSQLiteAuditLogger(db)
	sub := NewSQLiteAuditSubscriber(logger)

	// Subscribe
	if err := emitter.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Emit events through the emitter
	for i := 0; i < 3; i++ {
		event := audit.AuditEvent{
			ID:        "emit-evt-" + string(rune('A'+i)),
			TenantID:  "t1",
			Action:    "harness.step",
			Resource:  "search",
			Outcome:   "success",
			Source:    audit.SourceExecutor,
			Duration:  50 * time.Millisecond,
			Timestamp: time.Now().UTC(),
			StepID:    "step-" + string(rune('A'+i)),
			StepName:  "search",
			StepType:  "tool",
			Status:    "completed",
		}
		if err := emitter.Emit(context.Background(), event); err != nil {
			t.Fatalf("Emit(%d): %v", i, err)
		}
	}

	// Verify all events arrived in SQLite
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM audit_logs WHERE tenant_id = 't1'").Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 rows, got %d", count)
	}
}

func TestEmitterWithSQLiteSubscriber_WithSigner(t *testing.T) {
	db := setupSubscriberTestDB(t)
	defer db.Close()

	emitter := audit.NewEmitter()
	logger := execution_logs.NewSQLiteAuditLogger(db)
	signer := NewHMACChainSigner([]byte("test-secret-key-at-least-32-bytes!!"))
	logger.SetSigner(signer)
	sub := NewSQLiteAuditSubscriber(logger)

	if err := emitter.Subscribe(sub); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	event := audit.AuditEvent{
		ID:        "signed-evt-1",
		TenantID:  "t1",
		Action:    "harness.step",
		Resource:  "search",
		Outcome:   "success",
		Source:    audit.SourceExecutor,
		Duration:  100 * time.Millisecond,
		Timestamp: time.Now().UTC(),
		StepID:    "step-1",
		StepName:  "search",
		StepType:  "tool",
		Status:    "completed",
	}
	if err := emitter.Emit(context.Background(), event); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	// Verify signature was generated
	var sig string
	err := db.QueryRow("SELECT signature FROM audit_logs WHERE id = ?", event.ID).Scan(&sig)
	if err != nil {
		t.Fatalf("query signature: %v", err)
	}
	if sig == "" {
		t.Error("expected non-empty signature")
	}
}
