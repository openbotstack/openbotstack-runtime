package observability

import (
	"context"
	"testing"
)

func TestInitAppMetrics(t *testing.T) {
	err := InitAppMetrics()
	if err != nil {
		t.Fatalf("InitAppMetrics failed: %v", err)
	}
	if skillExecCount == nil {
		t.Error("skillExecCount should be initialized")
	}
	if skillExecDuration == nil {
		t.Error("skillExecDuration should be initialized")
	}
	if llmTokenUsage == nil {
		t.Error("llmTokenUsage should be initialized")
	}
	if activeRequestsGauge == nil {
		t.Error("activeRequestsGauge should be initialized")
	}
}

func TestRecordSkillExecution(t *testing.T) {
	if err := InitAppMetrics(); err != nil {
		t.Fatalf("InitAppMetrics: %v", err)
	}
	// Should not panic
	RecordSkillExecution(context.Background(), "core/test", "success", 150.0)
	RecordSkillExecution(context.Background(), "core/test", "error", 50.0)
}

func TestRecordSkillExecution_NilSafe(t *testing.T) {
	origCount := skillExecCount
	origDuration := skillExecDuration
	t.Cleanup(func() {
		skillExecCount = origCount
		skillExecDuration = origDuration
	})
	skillExecCount = nil
	skillExecDuration = nil
	// Should not panic
	RecordSkillExecution(context.Background(), "core/test", "success", 100.0)
}

func TestRecordLLMTokenUsage(t *testing.T) {
	if err := InitAppMetrics(); err != nil {
		t.Fatalf("InitAppMetrics: %v", err)
	}
	RecordLLMTokenUsage(context.Background(), "openai", "gpt-4", 100, 50)
}

func TestRecordLLMTokenUsage_NilSafe(t *testing.T) {
	orig := llmTokenUsage
	t.Cleanup(func() { llmTokenUsage = orig })
	llmTokenUsage = nil
	RecordLLMTokenUsage(context.Background(), "openai", "gpt-4", 100, 50)
}

func TestActiveRequest(t *testing.T) {
	if err := InitAppMetrics(); err != nil {
		t.Fatalf("InitAppMetrics: %v", err)
	}
	ActiveRequestIncrement(context.Background(), "/v1/chat")
	ActiveRequestDecrement(context.Background(), "/v1/chat")
}

func TestActiveRequest_NilSafe(t *testing.T) {
	orig := activeRequestsGauge
	t.Cleanup(func() { activeRequestsGauge = orig })
	activeRequestsGauge = nil
	ActiveRequestIncrement(context.Background(), "/v1/chat")
	ActiveRequestDecrement(context.Background(), "/v1/chat")
}

func TestTracingHelpers(t *testing.T) {
	// These should work even without a real tracer provider (no-op tracer)
	ctx, span := StartSkillSpan(context.Background(), "core/test")
	if span == nil {
		t.Error("StartSkillSpan should return non-nil span")
	}
	span.End()

	ctx, span = StartLLMSpan(context.Background(), "openai", "gpt-4")
	if span == nil {
		t.Error("StartLLMSpan should return non-nil span")
	}
	span.End()

	_ = ctx // use ctx to avoid lint warning
}
