package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/access/auth"
	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
	"github.com/openbotstack/openbotstack-runtime/persistence"
	rtAudit "github.com/openbotstack/openbotstack-runtime/audit"
)

// replayAuditQuerier is a test double that returns canned events for replay tests.
type replayAuditQuerier struct {
	events []audit.AuditEvent
	err    error
}

func (q *replayAuditQuerier) Query(_ context.Context, filter execution_logs.QueryFilter) ([]audit.AuditEvent, error) {
	if q.err != nil {
		return nil, q.err
	}
	if filter.RequestID != "" {
		var filtered []audit.AuditEvent
		for _, e := range q.events {
			if e.RequestID == filter.RequestID {
				filtered = append(filtered, e)
			}
		}
		return filtered, nil
	}
	return q.events, nil
}

func (q *replayAuditQuerier) Count(_ context.Context, _ execution_logs.QueryFilter) (int, error) {
	return len(q.events), nil
}

// setupReplayTest creates an admin test environment for replay endpoint tests.
func setupReplayTest(t *testing.T, querier execution_logs.AuditLogger) http.Handler {
	t.Helper()
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := db.SeedDefaults(); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	cfg := AdminRouterConfig{DB: db.DB}
	if querier != nil {
		// Wrap AuditLogger to satisfy AuditQuerier interface
		cfg.AuditQuerier = &auditLoggerAsQuerier{querier}
	}
	adminRouter := NewAdminRouter(cfg)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := &auth.User{ID: "admin", TenantID: "default", Name: "Admin"}
		ctx := middleware.WithUser(r.Context(), user)
		ctx = middleware.WithUserRole(ctx, "admin")
		adminRouter.Handler().ServeHTTP(w, r.WithContext(ctx))
	})
}

// auditLoggerAsQuerier adapts an AuditLogger to the AuditQuerier interface for tests.
type auditLoggerAsQuerier struct {
	execution_logs.AuditLogger
}

func (a *auditLoggerAsQuerier) Query(ctx context.Context, filter execution_logs.QueryFilter) ([]audit.AuditEvent, error) {
	return a.AuditLogger.Query(ctx, filter)
}

func (a *auditLoggerAsQuerier) Count(ctx context.Context, filter execution_logs.QueryFilter) (int, error) {
	return a.AuditLogger.Count(ctx, filter)
}

// --- NORMAL ---

func TestReplayEndpoint_FullFlow(t *testing.T) {
	logger := execution_logs.NewInMemoryAuditLogger()
	base := time.Now()

	// Log events for a known execution
	logger.Log(context.Background(), audit.AuditEvent{
		ID: "e1", TenantID: "t1", RequestID: "exec-full",
		StepID: "step-1", StepName: "search", StepType: "tool",
		Status: "started", Outcome: "started", Action: "skills.execute",
		Timestamp: base,
		ToolInput: map[string]any{"query": "hello"},
	})
	logger.Log(context.Background(), audit.AuditEvent{
		ID: "e2", TenantID: "t1", RequestID: "exec-full",
		StepID: "step-1", StepName: "search", StepType: "tool",
		Status: "completed", Outcome: "success", Action: "skills.execute",
		Duration: 100 * time.Millisecond,
		Timestamp: base.Add(100 * time.Millisecond),
		ToolOutput: "result",
	})

	handler := setupReplayTest(t, logger)

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/audit/replay?execution_id=exec-full", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var replay rtAudit.ExecutionReplay
	if err := json.NewDecoder(rec.Body).Decode(&replay); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if replay.ExecutionID != "exec-full" {
		t.Errorf("ExecutionID = %q, want %q", replay.ExecutionID, "exec-full")
	}
	if replay.Status != "completed" {
		t.Errorf("Status = %q, want %q", replay.Status, "completed")
	}
	if len(replay.Steps) != 1 {
		t.Errorf("len(Steps) = %d, want 1", len(replay.Steps))
	}
}

// --- ABNORMAL ---

func TestReplayEndpoint_MissingExecutionID(t *testing.T) {
	handler := setupReplayTest(t, execution_logs.NewInMemoryAuditLogger())

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/audit/replay", nil)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestReplayEndpoint_NotFound(t *testing.T) {
	handler := setupReplayTest(t, execution_logs.NewInMemoryAuditLogger())

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/audit/replay?execution_id=nonexistent", nil)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestReplayEndpoint_NotConfigured(t *testing.T) {
	handler := setupReplayTest(t, nil)

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/audit/replay?execution_id=test", nil)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
}

func TestReplayEndpoint_WrongMethod(t *testing.T) {
	handler := setupReplayTest(t, execution_logs.NewInMemoryAuditLogger())

	rec := doAdminRequest(t, handler, "POST", "/v1/admin/audit/replay?execution_id=test", nil)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestReplayEndpoint_EmptyResult(t *testing.T) {
	logger := execution_logs.NewInMemoryAuditLogger()
	// Log an event for a different execution
	logger.Log(context.Background(), audit.AuditEvent{
		ID: "e1", RequestID: "exec-other", Action: "test", Timestamp: time.Now(),
	})

	handler := setupReplayTest(t, logger)

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/audit/replay?execution_id=exec-missing", nil)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}
