package telemetry

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/telemetry"
	"github.com/openbotstack/openbotstack-runtime/telemetry/store"
	"go.opentelemetry.io/otel/trace"
)

func makeHookContext(stepName string, stepType execution.StepType, ec *execution.ExecutionContext) *execution.HookContext {
	return &execution.HookContext{
		Step: &execution.ExecutionStep{
			Name:    stepName,
			Type:    stepType,
			Timeout: int64(60 * time.Second),
		},
		StepIndex: 0,
		EC:        ec,
	}
}

func makeHookContextWithIndex(stepName string, stepType execution.StepType, ec *execution.ExecutionContext, idx int) *execution.HookContext {
	hctx := makeHookContext(stepName, stepType, ec)
	hctx.StepIndex = idx
	return hctx
}

func makeEC() *execution.ExecutionContext {
	ctx := context.Background()
	return execution.NewExecutionContext(ctx, "req-1", "asst-1", "sess-1", "tenant-1", "user-1")
}

func makeECWithID(reqID string) *execution.ExecutionContext {
	ctx := context.Background()
	return execution.NewExecutionContext(ctx, reqID, "asst-1", "sess-1", "tenant-1", "user-1")
}

func TestInstrumentor_EmitsSpanOnPostStep(t *testing.T) {
	spanStore := store.NewRingBufferSpanStore(100)
	eventStore := store.NewRingBufferEventStore(100)
	meter := telemetry.NewMemoryMeter()
	inst := NewInstrumentor(spanStore, eventStore, meter)

	ec := makeEC()
	hctx := makeHookContext("tool.query_data", execution.StepTypeTool, ec)

	inst.OnPostStepExecute(context.Background(), hctx)

	spans, err := spanStore.QuerySpans(context.Background(), store.SpanQuery{})
	if err != nil {
		t.Fatalf("QuerySpans: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("spans count = %d, want 1", len(spans))
	}
	s := spans[0]
	if s.Name != "tool.query_data" {
		t.Fatalf("span name = %q, want %q", s.Name, "tool.query_data")
	}
	if s.Kind != telemetry.SpanKindToolCall {
		t.Fatalf("span kind = %q, want %q", s.Kind, telemetry.SpanKindToolCall)
	}
}

func TestInstrumentor_EmitsMetricsOnPostStep(t *testing.T) {
	spanStore := store.NewRingBufferSpanStore(100)
	eventStore := store.NewRingBufferEventStore(100)
	meter := telemetry.NewMemoryMeter()
	inst := NewInstrumentor(spanStore, eventStore, meter)

	ec := makeEC()
	hctx := makeHookContext("skill.summarize", execution.StepTypeSkill, ec)

	inst.OnPostStepExecute(context.Background(), hctx)

	snap := meter.Snapshot()
	entries := snap.Counters["step_completed_total"]
	if entries == nil {
		t.Fatal("expected step_completed_total counter")
	}
	found := false
	for _, e := range entries {
		if e.Value >= 1 {
			found = true
		}
	}
	if !found {
		t.Fatal("expected step_completed_total >= 1")
	}
}

func TestInstrumentor_SkillSpanKind(t *testing.T) {
	spanStore := store.NewRingBufferSpanStore(100)
	eventStore := store.NewRingBufferEventStore(100)
	meter := telemetry.NewMemoryMeter()
	inst := NewInstrumentor(spanStore, eventStore, meter)

	ec := makeEC()
	hctx := makeHookContext("skill.summarize", execution.StepTypeSkill, ec)
	inst.OnPostStepExecute(context.Background(), hctx)

	spans, _ := spanStore.QuerySpans(context.Background(), store.SpanQuery{})
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if spans[0].Kind != telemetry.SpanKindSkill {
		t.Fatalf("kind = %q, want %q", spans[0].Kind, telemetry.SpanKindSkill)
	}
}

func TestInstrumentor_TelemetryFailureIsolation(t *testing.T) {
	inst := NewInstrumentor(nil, nil, nil)
	ec := makeEC()
	hctx := makeHookContext("test", execution.StepTypeTool, ec)
	inst.OnPreStepExecute(context.Background(), hctx)
	inst.OnPostStepExecute(context.Background(), hctx)
	inst.OnPreToolUse(context.Background(), hctx)
	inst.OnPostToolUse(context.Background(), hctx)
	inst.OnStop(context.Background(), hctx)
}

func TestInstrumentor_OnStopEmitsExecutionMetrics(t *testing.T) {
	spanStore := store.NewRingBufferSpanStore(100)
	eventStore := store.NewRingBufferEventStore(100)
	meter := telemetry.NewMemoryMeter()
	inst := NewInstrumentor(spanStore, eventStore, meter)

	ec := makeEC()
	hctx := &execution.HookContext{EC: ec}
	inst.OnStop(context.Background(), hctx)

	snap := meter.Snapshot()
	if snap.Counters["execution_completed_total"] == nil {
		t.Fatal("expected execution_completed_total counter")
	}
}

func TestInstrumentor_SpanTimingFromInFlight(t *testing.T) {
	spanStore := store.NewRingBufferSpanStore(100)
	eventStore := store.NewRingBufferEventStore(100)
	meter := telemetry.NewMemoryMeter()
	inst := NewInstrumentor(spanStore, eventStore, meter)

	ec := makeEC()
	hctx := makeHookContext("skill.test", execution.StepTypeSkill, ec)

	inst.OnPreStepExecute(context.Background(), hctx)
	time.Sleep(20 * time.Millisecond)
	inst.OnPostStepExecute(context.Background(), hctx)

	spans, _ := spanStore.QuerySpans(context.Background(), store.SpanQuery{})
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	s := spans[0]
	if s.EndTime.Before(s.StartTime) {
		t.Fatalf("EndTime %v before StartTime %v", s.EndTime, s.StartTime)
	}
	if s.Duration() < 15*time.Millisecond {
		t.Fatalf("duration = %v, want >= 15ms", s.Duration())
	}

	// Verify histogram recorded
	snap := meter.Snapshot()
	if snap.Histograms["step_duration_ms"] == nil {
		t.Fatal("expected step_duration_ms histogram")
	}
}

func TestInstrumentor_PreStepCreatesInFlight(t *testing.T) {
	spanStore := store.NewRingBufferSpanStore(100)
	eventStore := store.NewRingBufferEventStore(100)
	meter := telemetry.NewMemoryMeter()
	inst := NewInstrumentor(spanStore, eventStore, meter)

	ec := makeEC()
	hctx := makeHookContext("tool.x", execution.StepTypeTool, ec)

	inst.OnPreStepExecute(context.Background(), hctx)
	active := inst.ActiveExecutions()
	if active["req-1"] != 1 {
		t.Fatalf("active steps = %d, want 1", active["req-1"])
	}

	inst.OnPostStepExecute(context.Background(), hctx)
	active = inst.ActiveExecutions()
	if len(active) != 0 {
		t.Fatalf("active after completion = %v, want empty", active)
	}
}

func TestInstrumentor_EventEmission(t *testing.T) {
	spanStore := store.NewRingBufferSpanStore(100)
	eventStore := store.NewRingBufferEventStore(100)
	meter := telemetry.NewMemoryMeter()
	inst := NewInstrumentor(spanStore, eventStore, meter)

	ec := makeEC()
	hctx := makeHookContext("tool.x", execution.StepTypeTool, ec)

	inst.OnPreStepExecute(context.Background(), hctx)
	inst.OnPostStepExecute(context.Background(), hctx)

	events, _ := eventStore.QueryEvents(context.Background(), store.EventQuery{})
	if len(events) < 2 {
		t.Fatalf("events = %d, want >= 2", len(events))
	}
}

func TestInstrumentor_OnStopCleansUpOrphans(t *testing.T) {
	spanStore := store.NewRingBufferSpanStore(100)
	eventStore := store.NewRingBufferEventStore(100)
	meter := telemetry.NewMemoryMeter()
	inst := NewInstrumentor(spanStore, eventStore, meter)

	ec := makeEC()
	hctx := makeHookContext("tool.x", execution.StepTypeTool, ec)

	// Start step but never complete it
	inst.OnPreStepExecute(context.Background(), hctx)
	active := inst.ActiveExecutions()
	if active["req-1"] != 1 {
		t.Fatalf("active before stop = %d, want 1", active["req-1"])
	}

	// OnStop should clean up
	inst.OnStop(context.Background(), &execution.HookContext{EC: ec})
	active = inst.ActiveExecutions()
	if len(active) != 0 {
		t.Fatalf("active after stop = %v, want empty", active)
	}

	// Orphaned span should be recorded as cancelled
	spans, _ := spanStore.QuerySpans(context.Background(), store.SpanQuery{})
	found := false
	for _, s := range spans {
		if s.Name == "tool.x" && s.Status == telemetry.SpanStatusCancelled {
			found = true
		}
	}
	if !found {
		t.Fatal("expected cancelled span for orphaned step")
	}
}

func TestInstrumentor_AllFiveHooksRegistered(t *testing.T) {
	spanStore := store.NewRingBufferSpanStore(100)
	eventStore := store.NewRingBufferEventStore(100)
	meter := telemetry.NewMemoryMeter()
	inst := NewInstrumentor(spanStore, eventStore, meter)

	mgr := &mockRegistrar{}
	inst.RegisterHooks(mgr)

	if !mgr.preStep {
		t.Error("PreStepExecute not registered")
	}
	if !mgr.postStep {
		t.Error("PostStepExecute not registered")
	}
	if !mgr.preTool {
		t.Error("PreToolUse not registered")
	}
	if !mgr.postTool {
		t.Error("PostToolUse not registered")
	}
	if !mgr.onStop {
		t.Error("OnStop not registered")
	}
}

type mockRegistrar struct {
	preStep  bool
	postStep bool
	preTool  bool
	postTool bool
	onStop   bool
}

func (m *mockRegistrar) RegisterPreStepExecute(h execution.PreStepExecuteHook)  { m.preStep = true }
func (m *mockRegistrar) RegisterPostStepExecute(h execution.PostStepExecuteHook) { m.postStep = true }
func (m *mockRegistrar) RegisterPreToolUse(h execution.PreToolUseHook)          { m.preTool = true }
func (m *mockRegistrar) RegisterPostToolUse(h execution.PostToolUseHook)         { m.postTool = true }
func (m *mockRegistrar) RegisterOnStop(h execution.OnStopHook)                  { m.onStop = true }

func TestInstrumentor_ConcurrentExecutions(t *testing.T) {
	spanStore := store.NewRingBufferSpanStore(10000)
	eventStore := store.NewRingBufferEventStore(10000)
	meter := telemetry.NewMemoryMeter()
	inst := NewInstrumentor(spanStore, eventStore, meter)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ec := makeECWithID(fmt.Sprintf("req-%d", idx))
			hctx := makeHookContext("tool.x", execution.StepTypeTool, ec)
			inst.OnPreStepExecute(context.Background(), hctx)
			time.Sleep(time.Millisecond)
			inst.OnPostStepExecute(context.Background(), hctx)
		}(i)
	}
	wg.Wait()

	spans, _ := spanStore.QuerySpans(context.Background(), store.SpanQuery{Limit: 10000})
	if len(spans) != 100 {
		t.Errorf("spans = %d, want 100", len(spans))
	}

	active := inst.ActiveExecutions()
	if len(active) != 0 {
		t.Errorf("active after completion = %v, want empty", active)
	}
}

func TestInstrumentor_TraceIDFromOTelContext(t *testing.T) {
	spanStore := store.NewRingBufferSpanStore(100)
	eventStore := store.NewRingBufferEventStore(100)
	meter := telemetry.NewMemoryMeter()
	inst := NewInstrumentor(spanStore, eventStore, meter)

	// Create a context carrying a valid W3C TraceID via OTel span context
	w3cTraceID, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    w3cTraceID,
		SpanID:     trace.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}),
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

	ec := makeEC()
	hctx := makeHookContext("tool.test", execution.StepTypeTool, ec)

	inst.OnPreStepExecute(ctx, hctx)
	inst.OnPostStepExecute(ctx, hctx)

	// Verify the span captured the W3C TraceID (not the EC RequestID)
	spans, err := spanStore.QuerySpans(context.Background(), store.SpanQuery{})
	if err != nil {
		t.Fatalf("QuerySpans: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	got := string(spans[0].TraceID)
	want := "0af7651916cd43dd8448eb211c80319c"
	if got != want {
		t.Fatalf("TraceID = %q, want %q (W3C from OTel context)", got, want)
	}
}

func TestInstrumentor_TraceIDFallbackToRequestID(t *testing.T) {
	spanStore := store.NewRingBufferSpanStore(100)
	eventStore := store.NewRingBufferEventStore(100)
	meter := telemetry.NewMemoryMeter()
	inst := NewInstrumentor(spanStore, eventStore, meter)

	// context.Background() has no OTel span — should fall back to RequestID
	ec := makeECWithID("req-fallback-42")
	hctx := makeHookContext("tool.test", execution.StepTypeTool, ec)

	inst.OnPreStepExecute(context.Background(), hctx)
	inst.OnPostStepExecute(context.Background(), hctx)

	spans, err := spanStore.QuerySpans(context.Background(), store.SpanQuery{})
	if err != nil {
		t.Fatalf("QuerySpans: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	got := string(spans[0].TraceID)
	want := "req-fallback-42"
	if got != want {
		t.Fatalf("TraceID = %q, want %q (fallback to RequestID)", got, want)
	}
}
