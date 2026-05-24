package telemetry

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
	coretelemetry "github.com/openbotstack/openbotstack-core/telemetry"
	"github.com/openbotstack/openbotstack-runtime/telemetry/store"
	"go.opentelemetry.io/otel/trace"
)

type inflightKey struct {
	executionID string
	stepIndex   int
}

// Instrumentor emits telemetry from runtime hooks.
// It is the sole bridge between runtime execution and the telemetry system.
type Instrumentor struct {
	spanStore  store.SpanStore
	eventStore store.EventStore
	meter      coretelemetry.Meter

	mu      sync.Mutex
	inflight map[inflightKey]coretelemetry.Span
}

// NewInstrumentor creates a telemetry instrumentor.
// Any argument may be nil — emission is silently skipped.
func NewInstrumentor(spanStore store.SpanStore, eventStore store.EventStore, meter coretelemetry.Meter) *Instrumentor {
	return &Instrumentor{
		spanStore:  spanStore,
		eventStore: eventStore,
		meter:      meter,
		inflight:   make(map[inflightKey]coretelemetry.Span),
	}
}

// ActiveExecutions returns a map of execution_id to count of in-flight steps.
func (inst *Instrumentor) ActiveExecutions() map[string]int {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	counts := make(map[string]int)
	for k := range inst.inflight {
		counts[k.executionID]++
	}
	return counts
}

func (inst *Instrumentor) traceID(ctx context.Context, hctx *execution.HookContext) coretelemetry.TraceID {
	// 1. Prefer W3C TraceID from OTel span context — bridges infra tracing with product events
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		return coretelemetry.TraceID(span.SpanContext().TraceID().String())
	}
	// 2. Fallback to execution RequestID
	if hctx.EC != nil && hctx.EC.RequestID != "" {
		return coretelemetry.TraceID(hctx.EC.RequestID)
	}
	// 3. Last resort: generate a new ID
	return coretelemetry.NewTraceID()
}

func (inst *Instrumentor) executionID(hctx *execution.HookContext) string {
	if hctx.EC != nil {
		return hctx.EC.RequestID
	}
	return ""
}

func (inst *Instrumentor) emitEvent(ctx context.Context, hctx *execution.HookContext, level coretelemetry.EventLevel, operation, message string) {
	if inst.eventStore == nil {
		return
	}
	ev := coretelemetry.TelemetryEvent{
		Timestamp:  time.Now(),
		TraceID:    inst.traceID(ctx, hctx),
		Component:  "harness",
		Operation:  operation,
		Level:      level,
		Message:    message,
		Attributes: map[string]string{},
	}
	if hctx.Step != nil {
		ev.Attributes["step_name"] = hctx.Step.Name
		ev.SpanID = coretelemetry.NewSpanID()
	}
	if eid := inst.executionID(hctx); eid != "" {
		ev.Attributes["execution_id"] = eid
	}
	if err := inst.eventStore.RecordEvent(ctx, ev); err != nil {
		slog.Warn("telemetry: failed to record event", "error", err)
	}
}

// OnPreStepExecute is a PreStepExecuteHook that starts an in-flight span and emits an event.
// It never denies — telemetry is observation-only.
func (inst *Instrumentor) OnPreStepExecute(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
	if hctx.Step == nil {
		return nil, nil
	}

	traceID := inst.traceID(ctx, hctx)
	kind := stepTypeToKind(hctx.Step.Type)
	span := coretelemetry.Span{
		TraceID:   traceID,
		SpanID:    coretelemetry.NewSpanID(),
		Name:      hctx.Step.Name,
		Kind:      kind,
		StartTime: time.Now(),
		Status:    coretelemetry.SpanStatusOK,
		Attributes: map[string]string{
			"step_index": strconv.Itoa(hctx.StepIndex),
		},
	}
	if hctx.EC != nil {
		span.Attributes["execution_id"] = hctx.EC.RequestID
		span.Attributes["tenant_id"] = hctx.EC.TenantID
	}

	key := inflightKey{executionID: inst.executionID(hctx), stepIndex: hctx.StepIndex}
	inst.mu.Lock()
	inst.inflight[key] = span
	inst.mu.Unlock()

	if inst.meter != nil {
		inst.meter.Counter("step_started_total", 1, coretelemetry.Labels{
			"span_kind": string(kind),
		})
	}

	inst.emitEvent(ctx, hctx, coretelemetry.EventLevelInfo, "pre_step", "step starting")

	return nil, nil
}

// OnPostStepExecute is a PostStepExecuteHook that records a span with timing and emits metrics.
func (inst *Instrumentor) OnPostStepExecute(ctx context.Context, hctx *execution.HookContext) error {
	if hctx.Step == nil {
		return nil
	}

	kind := stepTypeToKind(hctx.Step.Type)
	status := coretelemetry.SpanStatusOK
	if hctx.ToolOutput != nil {
		if _, ok := hctx.ToolOutput.(error); ok {
			status = coretelemetry.SpanStatusError
		}
	}

	key := inflightKey{executionID: inst.executionID(hctx), stepIndex: hctx.StepIndex}
	now := time.Now()

	inst.mu.Lock()
	span, found := inst.inflight[key]
	if found {
		delete(inst.inflight, key)
	}
	inst.mu.Unlock()

	if found {
		span.EndTime = now
		span.Status = status
	} else {
		traceID := inst.traceID(ctx, hctx)
		span = coretelemetry.Span{
			TraceID:   traceID,
			SpanID:    coretelemetry.NewSpanID(),
			Name:      hctx.Step.Name,
			Kind:      kind,
			StartTime: now,
			EndTime:   now,
			Status:    status,
			Attributes: map[string]string{
				"step_index":  strconv.Itoa(hctx.StepIndex),
				"execution_id": inst.executionID(hctx),
			},
		}
	}

	if inst.spanStore != nil {
		if err := inst.spanStore.RecordSpan(ctx, span); err != nil {
			slog.Warn("telemetry: failed to record span", "error", err)
		}
	}

	if inst.meter != nil {
		statusStr := "ok"
		if status == coretelemetry.SpanStatusError {
			statusStr = "error"
		}
		inst.meter.Counter("step_completed_total", 1, coretelemetry.Labels{
			"status":    statusStr,
			"span_kind": string(kind),
		})
		if dur := span.Duration(); dur > 0 {
			inst.meter.Histogram("step_duration_ms", float64(dur.Milliseconds()), coretelemetry.Labels{
				"span_kind": string(kind),
				"status":    statusStr,
			})
		}
	}

	eventLevel := coretelemetry.EventLevelInfo
	if status == coretelemetry.SpanStatusError {
		eventLevel = coretelemetry.EventLevelError
	}
	inst.emitEvent(ctx, hctx, eventLevel, "post_step", "step completed")

	return nil
}

// OnPreToolUse is a PreToolUseHook that records tool invocation start.
// It never denies — telemetry is observation-only.
func (inst *Instrumentor) OnPreToolUse(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
	inst.emitEvent(ctx, hctx, coretelemetry.EventLevelInfo, "pre_tool", "tool invocation starting")
	return nil, nil
}

// OnPostToolUse is a PostToolUseHook that records tool invocation result.
func (inst *Instrumentor) OnPostToolUse(ctx context.Context, hctx *execution.HookContext) error {
	level := coretelemetry.EventLevelInfo
	if hctx.ToolOutput != nil {
		if _, ok := hctx.ToolOutput.(error); ok {
			level = coretelemetry.EventLevelError
		}
	}
	inst.emitEvent(ctx, hctx, level, "post_tool", "tool invocation completed")

	if inst.meter != nil {
		statusStr := "ok"
		if level == coretelemetry.EventLevelError {
			statusStr = "error"
		}
		inst.meter.Counter("tool_invocation_completed_total", 1, coretelemetry.Labels{
			"status": statusStr,
		})
	}
	return nil
}

// OnStop is an OnStopHook that records execution-level metrics and cleans up orphaned spans.
func (inst *Instrumentor) OnStop(ctx context.Context, hctx *execution.HookContext) {
	execID := inst.executionID(hctx)

	// Clean up any orphaned in-flight spans for this execution
	inst.mu.Lock()
	for key, span := range inst.inflight {
		if key.executionID == execID {
			span.EndTime = time.Now()
			span.Status = coretelemetry.SpanStatusCancelled
			if inst.spanStore != nil {
				if err := inst.spanStore.RecordSpan(ctx, span); err != nil {
					slog.Warn("telemetry: failed to record orphaned span", "error", err)
				}
			}
			delete(inst.inflight, key)
		}
	}
	inst.mu.Unlock()

	if inst.meter != nil {
		inst.meter.Counter("execution_completed_total", 1, coretelemetry.Labels{})
	}

	inst.emitEvent(ctx, hctx, coretelemetry.EventLevelInfo, "execution_stop", "execution completed")
}

// HookRegistrar is implemented by harness.HookManager.
type HookRegistrar interface {
	RegisterPreStepExecute(h execution.PreStepExecuteHook)
	RegisterPostStepExecute(h execution.PostStepExecuteHook)
	RegisterPreToolUse(h execution.PreToolUseHook)
	RegisterPostToolUse(h execution.PostToolUseHook)
	RegisterOnStop(h execution.OnStopHook)
}

// RegisterHooks registers all 5 telemetry hooks.
func (inst *Instrumentor) RegisterHooks(hm HookRegistrar) {
	hm.RegisterPreStepExecute(inst.OnPreStepExecute)
	hm.RegisterPostStepExecute(inst.OnPostStepExecute)
	hm.RegisterPreToolUse(inst.OnPreToolUse)
	hm.RegisterPostToolUse(inst.OnPostToolUse)
	hm.RegisterOnStop(inst.OnStop)
}

func stepTypeToKind(t execution.StepType) coretelemetry.SpanKind {
	switch t {
	case execution.StepTypeTool:
		return coretelemetry.SpanKindToolCall
	case execution.StepTypeSkill:
		return coretelemetry.SpanKindSkill
	case execution.StepTypeLLM:
		return coretelemetry.SpanKindPlanner
	default:
		return coretelemetry.SpanKindExecution
	}
}
