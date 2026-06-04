package audit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
)

// mockComplianceQuerier implements AuditQuerier for compliance report tests.
type mockComplianceQuerier struct {
	events []audit.AuditEvent
	count  int
	err    error
}

func (m *mockComplianceQuerier) Query(ctx context.Context, filter execution_logs.QueryFilter) ([]audit.AuditEvent, error) {
	if m.err != nil {
		return nil, m.err
	}
	// Simple tenant filtering for tests
	if filter.TenantID != "" {
		var filtered []audit.AuditEvent
		for _, e := range m.events {
			if e.TenantID == filter.TenantID {
				filtered = append(filtered, e)
			}
		}
		return filtered, nil
	}
	return m.events, nil
}

func (m *mockComplianceQuerier) Count(ctx context.Context, filter execution_logs.QueryFilter) (int, error) {
	if m.err != nil {
		return 0, m.err
	}
	if filter.TenantID != "" {
		n := 0
		for _, e := range m.events {
			if e.TenantID == filter.TenantID {
				n++
			}
		}
		return n, nil
	}
	return len(m.events), nil
}

// makeEvents creates N test events with configurable fields.
func makeEvents(n int, fn func(i int) audit.AuditEvent) []audit.AuditEvent {
	events := make([]audit.AuditEvent, n)
	for i := range events {
		events[i] = fn(i)
	}
	return events
}

// === Summary statistics ===

func TestComplianceReport_EmptyEvents(t *testing.T) {
	querier := &mockComplianceQuerier{events: nil}
	generator := NewComplianceReportGenerator(querier, nil)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("Generate with empty events: %v", err)
	}
	if report.Summary.TotalEvents != 0 {
		t.Errorf("TotalEvents = %d, want 0", report.Summary.TotalEvents)
	}
	if report.Summary.ErrorRate != 0 {
		t.Errorf("ErrorRate = %f, want 0", report.Summary.ErrorRate)
	}
	if report.Summary.DenialRate != 0 {
		t.Errorf("DenialRate = %f, want 0", report.Summary.DenialRate)
	}
}

func TestComplianceReport_SummaryCounts(t *testing.T) {
	events := makeEvents(10, func(i int) audit.AuditEvent {
		e := audit.AuditEvent{
			ID:        fmt.Sprintf("evt-%d", i),
			TenantID:  "t1",
			Timestamp: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			Action:    "skills.execute",
			Outcome:   "success",
			Duration:  100 * time.Millisecond,
		}
		if i < 3 {
			e.Outcome = "failure"
			e.Error = "timeout"
		}
		return e
	})

	querier := &mockComplianceQuerier{events: events}
	generator := NewComplianceReportGenerator(querier, nil)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{TenantID: "t1"},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if report.Summary.TotalEvents != 10 {
		t.Errorf("TotalEvents = %d, want 10", report.Summary.TotalEvents)
	}
	// 3 failures out of 10 = 30%
	if report.Summary.ErrorRate != 30.0 {
		t.Errorf("ErrorRate = %f, want 30.0", report.Summary.ErrorRate)
	}
}

func TestComplianceReport_UniqueExecutions(t *testing.T) {
	events := []audit.AuditEvent{
		{ID: "e1", RequestID: "exec-a", TenantID: "t1", Timestamp: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)},
		{ID: "e2", RequestID: "exec-a", TenantID: "t1", Timestamp: time.Date(2026, 1, 15, 0, 0, 1, 0, time.UTC)},
		{ID: "e3", RequestID: "exec-b", TenantID: "t1", Timestamp: time.Date(2026, 1, 15, 0, 0, 2, 0, time.UTC)},
		{ID: "e4", RequestID: "", TenantID: "t1", Timestamp: time.Date(2026, 1, 15, 0, 0, 3, 0, time.UTC)},
	}

	querier := &mockComplianceQuerier{events: events}
	generator := NewComplianceReportGenerator(querier, nil)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{TenantID: "t1"},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if report.Summary.TotalExecutions != 2 {
		t.Errorf("TotalExecutions = %d, want 2 (unique exec-a, exec-b; empty RequestID excluded)", report.Summary.TotalExecutions)
	}
}

// === Policy compliance ===

func TestComplianceReport_PolicyCompliance(t *testing.T) {
	events := makeEvents(20, func(i int) audit.AuditEvent {
		e := audit.AuditEvent{
			ID:        fmt.Sprintf("evt-%d", i),
			TenantID:  "t1",
			Timestamp: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		}
		if i < 12 {
			e.Action = "policy.check"
			e.Outcome = "allowed"
		} else if i < 16 {
			e.Action = "policy.enforce"
			e.Outcome = "denied"
			e.Resource = fmt.Sprintf("skill/restricted-%d", i)
		} else {
			e.Action = "skills.execute"
			e.Outcome = "success"
		}
		return e
	})

	querier := &mockComplianceQuerier{events: events}
	generator := NewComplianceReportGenerator(querier, nil)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{TenantID: "t1"},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	pc := report.PolicyCompliance
	if pc.TotalChecks != 16 {
		t.Errorf("TotalChecks = %d, want 16", pc.TotalChecks)
	}
	if pc.Allowed != 12 {
		t.Errorf("Allowed = %d, want 12", pc.Allowed)
	}
	if pc.Denied != 4 {
		t.Errorf("Denied = %d, want 4", pc.Denied)
	}
	// 4 denied out of 16 = 25%
	if pc.DenialRate != 25.0 {
		t.Errorf("DenialRate = %f, want 25.0", pc.DenialRate)
	}
}

func TestComplianceReport_PolicyCompliance_NoPolicyEvents(t *testing.T) {
	events := makeEvents(5, func(i int) audit.AuditEvent {
		return audit.AuditEvent{
			ID:        fmt.Sprintf("evt-%d", i),
			TenantID:  "t1",
			Action:    "skills.execute",
			Outcome:   "success",
			Timestamp: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		}
	})

	querier := &mockComplianceQuerier{events: events}
	generator := NewComplianceReportGenerator(querier, nil)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{TenantID: "t1"},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if report.PolicyCompliance.TotalChecks != 0 {
		t.Errorf("TotalChecks = %d, want 0", report.PolicyCompliance.TotalChecks)
	}
	if report.PolicyCompliance.DenialRate != 0 {
		t.Errorf("DenialRate = %f, want 0 when no policy events", report.PolicyCompliance.DenialRate)
	}
}

// === Execution health ===

func TestComplianceReport_ExecutionHealth(t *testing.T) {
	events := makeEvents(6, func(i int) audit.AuditEvent {
		e := audit.AuditEvent{
			ID:        fmt.Sprintf("evt-%d", i),
			TenantID:  "t1",
			Timestamp: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			StepID:    fmt.Sprintf("step-%d", i),
			Duration:  time.Duration(100+i*50) * time.Millisecond,
		}
		if i < 4 {
			e.Status = "completed"
		} else {
			e.Status = "failed"
			e.Error = "timeout"
			e.Source = audit.SourceExecutor
		}
		return e
	})

	querier := &mockComplianceQuerier{events: events}
	generator := NewComplianceReportGenerator(querier, nil)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{TenantID: "t1"},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	eh := report.ExecutionHealth
	if eh.StepsTotal != 6 {
		t.Errorf("StepsTotal = %d, want 6", eh.StepsTotal)
	}
	if eh.StepsCompleted != 4 {
		t.Errorf("StepsCompleted = %d, want 4", eh.StepsCompleted)
	}
	if eh.StepsFailed != 2 {
		t.Errorf("StepsFailed = %d, want 2", eh.StepsFailed)
	}
	// 4/6 * 100 = 66.67%
	if eh.SuccessRate < 66.0 || eh.SuccessRate > 67.0 {
		t.Errorf("SuccessRate = %f, want ~66.67", eh.SuccessRate)
	}
	// Avg duration: (100+150+200+250+300+350)/6 = 225ms
	if eh.AvgDurationMs != 225 {
		t.Errorf("AvgDurationMs = %d, want 225", eh.AvgDurationMs)
	}
}

func TestComplianceReport_ExecutionHealth_P99(t *testing.T) {
	// 100 events with linearly increasing duration
	events := makeEvents(100, func(i int) audit.AuditEvent {
		return audit.AuditEvent{
			ID:        fmt.Sprintf("evt-%d", i),
			TenantID:  "t1",
			Timestamp: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			StepID:    fmt.Sprintf("step-%d", i),
			Status:    "completed",
			Duration:  time.Duration(i+1) * time.Millisecond,
		}
	})

	querier := &mockComplianceQuerier{events: events}
	generator := NewComplianceReportGenerator(querier, nil)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{TenantID: "t1"},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// P99 of [1..100] = 99
	if report.ExecutionHealth.P99DurationMs != 99 {
		t.Errorf("P99DurationMs = %d, want 99", report.ExecutionHealth.P99DurationMs)
	}
}

func TestComplianceReport_ExecutionHealth_FailureBreakdown(t *testing.T) {
	events := []audit.AuditEvent{
		{ID: "e1", TenantID: "t1", StepID: "s1", Status: "failed", Source: audit.SourceExecutor, Error: "err", Timestamp: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)},
		{ID: "e2", TenantID: "t1", StepID: "s2", Status: "failed", Source: audit.SourceReasoning, Error: "err", Timestamp: time.Date(2026, 1, 15, 0, 0, 1, 0, time.UTC)},
		{ID: "e3", TenantID: "t1", StepID: "s3", Status: "failed", Source: audit.SourceExecutor, Error: "err", Timestamp: time.Date(2026, 1, 15, 0, 0, 2, 0, time.UTC)},
		{ID: "e4", TenantID: "t1", StepID: "s4", Status: "completed", Timestamp: time.Date(2026, 1, 15, 0, 0, 3, 0, time.UTC)},
	}

	querier := &mockComplianceQuerier{events: events}
	generator := NewComplianceReportGenerator(querier, nil)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{TenantID: "t1"},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	fb := report.ExecutionHealth.FailureBreakdown
	if fb[string(audit.SourceExecutor)] != 2 {
		t.Errorf("FailureBreakdown[executor] = %d, want 2", fb[string(audit.SourceExecutor)])
	}
	if fb[string(audit.SourceReasoning)] != 1 {
		t.Errorf("FailureBreakdown[reasoning_loop] = %d, want 1", fb[string(audit.SourceReasoning)])
	}
}

// === Chain integrity ===

func TestComplianceReport_ChainIntegrity_Intact(t *testing.T) {
	key := []byte("test-key")
	signer := NewHMACChainSigner(key)
	events := makeEvents(5, func(i int) audit.AuditEvent {
		return audit.AuditEvent{
			ID:        fmt.Sprintf("evt-%d", i),
			TenantID:  "t1",
			Action:    "test",
			Timestamp: time.Date(2026, 1, 15, 0, 0, i, 0, time.UTC),
		}
	})

	// Sign the chain
	signatures := make([]string, len(events))
	for i, e := range events {
		prevSig := ""
		if i > 0 {
			prevSig = signatures[i-1]
		}
		sig, err := signer.Sign(e, prevSig)
		if err != nil {
			t.Fatalf("Sign: %v", err)
		}
		signatures[i] = sig
	}

	querier := &mockComplianceQuerier{events: events}
	generator := NewComplianceReportGenerator(querier, key)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{TenantID: "t1"},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
		Signatures: signatures,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !report.ChainIntegrity.Verified {
		t.Error("ChainIntegrity.Verified should be true for intact chain")
	}
	if report.ChainIntegrity.TotalEvents != 5 {
		t.Errorf("TotalEvents = %d, want 5", report.ChainIntegrity.TotalEvents)
	}
	if report.ChainIntegrity.FirstBreakIndex != -1 {
		t.Errorf("FirstBreakIndex = %d, want -1", report.ChainIntegrity.FirstBreakIndex)
	}
}

func TestComplianceReport_ChainIntegrity_Broken(t *testing.T) {
	key := []byte("test-key")
	events := makeEvents(3, func(i int) audit.AuditEvent {
		return audit.AuditEvent{
			ID:        fmt.Sprintf("evt-%d", i),
			TenantID:  "t1",
			Action:    "test",
			Timestamp: time.Date(2026, 1, 15, 0, 0, i, 0, time.UTC),
		}
	})

	// Provide tampered signatures
	signatures := []string{"sig-1", "tampered", "sig-3"}

	querier := &mockComplianceQuerier{events: events}
	generator := NewComplianceReportGenerator(querier, key)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{TenantID: "t1"},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
		Signatures: signatures,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if report.ChainIntegrity.Verified {
		t.Error("ChainIntegrity.Verified should be false for broken chain")
	}
	if report.ChainIntegrity.BreakCount < 1 {
		t.Errorf("BreakCount = %d, want >= 1", report.ChainIntegrity.BreakCount)
	}
}

func TestComplianceReport_ChainIntegrity_NoKey(t *testing.T) {
	events := makeEvents(3, func(i int) audit.AuditEvent {
		return audit.AuditEvent{
			ID:        fmt.Sprintf("evt-%d", i),
			TenantID:  "t1",
			Timestamp: time.Date(2026, 1, 15, 0, 0, i, 0, time.UTC),
		}
	})

	querier := &mockComplianceQuerier{events: events}
	generator := NewComplianceReportGenerator(querier, nil) // no key

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{TenantID: "t1"},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
		Signatures: []string{"sig-1", "sig-2", "sig-3"},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Without a key, chain integrity should report skipped
	if report.ChainIntegrity.Verified {
		t.Error("without key, chain integrity should not be verified")
	}
}

// === Retention compliance ===

func TestComplianceReport_RetentionCompliance(t *testing.T) {
	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)

	events := []audit.AuditEvent{
		{ID: "e1", TenantID: "t1", Timestamp: now.AddDate(0, 0, -5)},  // within 30-day window
		{ID: "e2", TenantID: "t1", Timestamp: now.AddDate(0, 0, -10)}, // within 30-day window
		{ID: "e3", TenantID: "t1", Timestamp: now.AddDate(0, 0, -45)}, // outside 30-day window (expired)
	}

	querier := &mockComplianceQuerier{events: events}
	retention := NewRetentionPolicy(RetentionConfig{
		Enabled:     true,
		DefaultDays: 30,
	}, nil)
	generator := NewComplianceReportGenerator(querier, nil)
	generator.SetRetentionPolicy(retention)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:      audit.ComplianceScope{TenantID: "t1"},
		Period:     audit.TimeRange{From: now.AddDate(-1, 0, 0), To: now},
		RetentionNow: now,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	rc := report.RetentionCompliance
	if !rc.PolicyEnabled {
		t.Error("PolicyEnabled should be true")
	}
	if rc.DefaultDays != 30 {
		t.Errorf("DefaultDays = %d, want 30", rc.DefaultDays)
	}
	if rc.EventsInRange != 2 {
		t.Errorf("EventsInRange = %d, want 2", rc.EventsInRange)
	}
	if rc.EventsExpired != 1 {
		t.Errorf("EventsExpired = %d, want 1", rc.EventsExpired)
	}
	if rc.Compliant {
		t.Error("Compliant should be false (1 expired event remaining)")
	}
}

func TestComplianceReport_RetentionCompliance_Compliant(t *testing.T) {
	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)

	events := []audit.AuditEvent{
		{ID: "e1", TenantID: "t1", Timestamp: now.AddDate(0, 0, -5)},
		{ID: "e2", TenantID: "t1", Timestamp: now.AddDate(0, 0, -10)},
	}

	querier := &mockComplianceQuerier{events: events}
	retention := NewRetentionPolicy(RetentionConfig{
		Enabled:     true,
		DefaultDays: 30,
	}, nil)
	generator := NewComplianceReportGenerator(querier, nil)
	generator.SetRetentionPolicy(retention)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:      audit.ComplianceScope{TenantID: "t1"},
		Period:     audit.TimeRange{From: now.AddDate(-1, 0, 0), To: now},
		RetentionNow: now,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !report.RetentionCompliance.Compliant {
		t.Error("should be compliant when no expired events")
	}
}

func TestComplianceReport_RetentionCompliance_Disabled(t *testing.T) {
	events := []audit.AuditEvent{
		{ID: "e1", TenantID: "t1", Timestamp: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)},
	}

	querier := &mockComplianceQuerier{events: events}
	retention := NewRetentionPolicy(RetentionConfig{
		Enabled:     false,
		DefaultDays: 30,
	}, nil)
	generator := NewComplianceReportGenerator(querier, nil)
	generator.SetRetentionPolicy(retention)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{TenantID: "t1"},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	rc := report.RetentionCompliance
	if rc.PolicyEnabled {
		t.Error("PolicyEnabled should be false")
	}
	// When disabled, still compliant (no enforcement)
	if !rc.Compliant {
		t.Error("when retention disabled, should report compliant")
	}
}

// === Error patterns ===

func TestComplianceReport_TopErrors(t *testing.T) {
	events := makeEvents(20, func(i int) audit.AuditEvent {
		e := audit.AuditEvent{
			ID:        fmt.Sprintf("evt-%d", i),
			TenantID:  "t1",
			Timestamp: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
		}
		if i < 8 {
			e.Error = "connection refused"
			e.Source = audit.SourceExecutor
		} else if i < 12 {
			e.Error = "timeout exceeded"
			e.Source = audit.SourceReasoning
		}
		return e
	})

	querier := &mockComplianceQuerier{events: events}
	generator := NewComplianceReportGenerator(querier, nil)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{TenantID: "t1"},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if len(report.TopErrors) < 2 {
		t.Fatalf("TopErrors len = %d, want >= 2", len(report.TopErrors))
	}
	if report.TopErrors[0].Error != "connection refused" {
		t.Errorf("TopErrors[0].Error = %q, want 'connection refused'", report.TopErrors[0].Error)
	}
	if report.TopErrors[0].Count != 8 {
		t.Errorf("TopErrors[0].Count = %d, want 8", report.TopErrors[0].Count)
	}
}

func TestComplianceReport_TopErrors_Max10(t *testing.T) {
	events := makeEvents(20, func(i int) audit.AuditEvent {
		return audit.AuditEvent{
			ID:        fmt.Sprintf("evt-%d", i),
			TenantID:  "t1",
			Timestamp: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
			Error:     fmt.Sprintf("unique-error-%d", i),
			Source:    audit.SourceExecutor,
		}
	})

	querier := &mockComplianceQuerier{events: events}
	generator := NewComplianceReportGenerator(querier, nil)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{TenantID: "t1"},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if len(report.TopErrors) > 10 {
		t.Errorf("TopErrors len = %d, want <= 10", len(report.TopErrors))
	}
}

// === Edge cases ===

func TestComplianceReport_NilContext(t *testing.T) {
	querier := &mockComplianceQuerier{}
	generator := NewComplianceReportGenerator(querier, nil)

	_, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err == nil {
		t.Error("nil context should return error")
	}
}

func TestComplianceReport_QueryError(t *testing.T) {
	querier := &mockComplianceQuerier{err: fmt.Errorf("db connection lost")}
	generator := NewComplianceReportGenerator(querier, nil)

	_, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err == nil {
		t.Error("query error should propagate")
	}
}

func TestComplianceReport_ReportID(t *testing.T) {
	querier := &mockComplianceQuerier{}
	generator := NewComplianceReportGenerator(querier, nil)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if report.ID == "" {
		t.Error("report ID should be auto-generated")
	}
	if report.GeneratedAt.IsZero() {
		t.Error("GeneratedAt should be set")
	}
}

func TestComplianceReport_ScopeReflected(t *testing.T) {
	querier := &mockComplianceQuerier{}
	generator := NewComplianceReportGenerator(querier, nil)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{TenantID: "t1", UserID: "u1"},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if report.Scope.TenantID != "t1" {
		t.Errorf("Scope.TenantID = %q, want 't1'", report.Scope.TenantID)
	}
	if report.Scope.UserID != "u1" {
		t.Errorf("Scope.UserID = %q, want 'u1'", report.Scope.UserID)
	}
}

// === Denied actions tracking ===

func TestComplianceReport_TopDeniedActions(t *testing.T) {
	events := []audit.AuditEvent{
		{ID: "e1", TenantID: "t1", Action: "policy.enforce", Outcome: "denied", Resource: "skill/admin-delete", Timestamp: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)},
		{ID: "e2", TenantID: "t1", Action: "policy.enforce", Outcome: "denied", Resource: "skill/admin-delete", Timestamp: time.Date(2026, 1, 15, 0, 0, 1, 0, time.UTC)},
		{ID: "e3", TenantID: "t1", Action: "policy.enforce", Outcome: "denied", Resource: "skill/sudo", Timestamp: time.Date(2026, 1, 15, 0, 0, 2, 0, time.UTC)},
		{ID: "e4", TenantID: "t1", Action: "policy.check", Outcome: "allowed", Resource: "skill/read", Timestamp: time.Date(2026, 1, 15, 0, 0, 3, 0, time.UTC)},
	}

	querier := &mockComplianceQuerier{events: events}
	generator := NewComplianceReportGenerator(querier, nil)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{TenantID: "t1"},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if len(report.PolicyCompliance.TopDeniedActions) < 1 {
		t.Fatal("TopDeniedActions should not be empty")
	}
	if report.PolicyCompliance.TopDeniedActions[0].Action != "skill/admin-delete" {
		t.Errorf("TopDeniedActions[0].Action = %q, want 'skill/admin-delete'", report.PolicyCompliance.TopDeniedActions[0].Action)
	}
	if report.PolicyCompliance.TopDeniedActions[0].Count != 2 {
		t.Errorf("TopDeniedActions[0].Count = %d, want 2", report.PolicyCompliance.TopDeniedActions[0].Count)
	}
}

// === No step events ===

func TestComplianceReport_NoStepEvents(t *testing.T) {
	events := []audit.AuditEvent{
		{ID: "e1", TenantID: "t1", Action: "system.started", Outcome: "success", Timestamp: time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)},
		{ID: "e2", TenantID: "t1", Action: "system.stopped", Outcome: "success", Timestamp: time.Date(2026, 1, 15, 0, 0, 1, 0, time.UTC)},
	}

	querier := &mockComplianceQuerier{events: events}
	generator := NewComplianceReportGenerator(querier, nil)

	report, err := generator.Generate(context.Background(), ComplianceReportRequest{
		Scope:  audit.ComplianceScope{TenantID: "t1"},
		Period: audit.TimeRange{From: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if report.ExecutionHealth.StepsTotal != 0 {
		t.Errorf("StepsTotal = %d, want 0 when no step events", report.ExecutionHealth.StepsTotal)
	}
	if report.ExecutionHealth.SuccessRate != 0 {
		t.Errorf("SuccessRate = %f, want 0 when no step events", report.ExecutionHealth.SuccessRate)
	}
}
