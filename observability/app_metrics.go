package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	skillExecCount      metric.Int64Counter
	skillExecDuration   metric.Float64Histogram
	llmTokenUsage       metric.Int64Histogram
	activeRequestsGauge metric.Int64UpDownCounter
	plannerLatency      metric.Float64Histogram
	providerLatency     metric.Float64Histogram
	timeoutCount        metric.Int64Counter
	retryCount          metric.Int64Counter
	wasmFailureTotal    metric.Int64Counter
)

// InitAppMetrics creates application-specific metric instruments.
// Must be called after Setup() and InitMetrics().
func InitAppMetrics() error {
	meter := otel.GetMeterProvider().Meter(meterName)

	var err error

	skillExecCount, err = meter.Int64Counter("skill_execution_count",
		metric.WithDescription("Total number of skill executions"),
	)
	if err != nil {
		return err
	}

	skillExecDuration, err = meter.Float64Histogram("skill_execution_duration_ms",
		metric.WithDescription("Skill execution duration in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return err
	}

	llmTokenUsage, err = meter.Int64Histogram("llm_token_usage",
		metric.WithDescription("LLM token consumption by type"),
	)
	if err != nil {
		return err
	}

	activeRequestsGauge, err = meter.Int64UpDownCounter("active_requests",
		metric.WithDescription("Currently active requests"),
	)
	if err != nil {
		return err
	}

	plannerLatency, err = meter.Float64Histogram("planner_latency_ms",
		metric.WithDescription("Execution planner latency in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return err
	}

	providerLatency, err = meter.Float64Histogram("provider_latency_ms",
		metric.WithDescription("LLM provider response latency in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return err
	}

	timeoutCount, err = meter.Int64Counter("timeout_count",
		metric.WithDescription("Total number of step/session timeouts"),
	)
	if err != nil {
		return err
	}

	retryCount, err = meter.Int64Counter("retry_count",
		metric.WithDescription("Total number of step retry attempts"),
	)
	if err != nil {
		return err
	}

	wasmFailureTotal, err = meter.Int64Counter("wasm_failure_total",
		metric.WithDescription("Total number of Wasm skill execution failures"),
	)
	if err != nil {
		return err
	}

	return nil
}

// RecordSkillExecution increments the skill_execution_count counter and records duration.
func RecordSkillExecution(ctx context.Context, skillID, status string, durationMs float64) {
	attrs := []attribute.KeyValue{
		attribute.String("skill_id", skillID),
		attribute.String("status", status),
	}
	if skillExecCount != nil {
		skillExecCount.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if skillExecDuration != nil {
		skillExecDuration.Record(ctx, durationMs, metric.WithAttributes(attrs...))
	}
}

// RecordLLMTokenUsage records token consumption from an LLM call.
func RecordLLMTokenUsage(ctx context.Context, provider, model string, promptTokens, completionTokens int) {
	baseAttrs := []attribute.KeyValue{
		attribute.String("provider", provider),
		attribute.String("model", model),
	}
	if llmTokenUsage != nil {
		llmTokenUsage.Record(ctx, int64(promptTokens),
			metric.WithAttributes(append(baseAttrs, attribute.String("type", "prompt"))...))
		llmTokenUsage.Record(ctx, int64(completionTokens),
			metric.WithAttributes(append(baseAttrs, attribute.String("type", "completion"))...))
	}
}

// ActiveRequestIncrement increases the active_requests gauge.
func ActiveRequestIncrement(ctx context.Context, endpoint string) {
	if activeRequestsGauge != nil {
		activeRequestsGauge.Add(ctx, 1, metric.WithAttributes(
			attribute.String("endpoint", endpoint),
		))
	}
}

// ActiveRequestDecrement decreases the active_requests gauge.
func ActiveRequestDecrement(ctx context.Context, endpoint string) {
	if activeRequestsGauge != nil {
		activeRequestsGauge.Add(ctx, -1, metric.WithAttributes(
			attribute.String("endpoint", endpoint),
		))
	}
}

// RecordPlannerLatency records execution planner latency.
func RecordPlannerLatency(ctx context.Context, durationMs float64, success bool) {
	if plannerLatency != nil {
		plannerLatency.Record(ctx, durationMs, metric.WithAttributes(
			attribute.Bool("success", success),
		))
	}
}

// RecordProviderLatency records LLM provider response latency.
func RecordProviderLatency(ctx context.Context, provider, model string, durationMs float64) {
	if providerLatency != nil {
		providerLatency.Record(ctx, durationMs, metric.WithAttributes(
			attribute.String("provider", provider),
			attribute.String("model", model),
		))
	}
}

// RecordTimeout increments the timeout counter.
func RecordTimeout(ctx context.Context, scope string) {
	if timeoutCount != nil {
		timeoutCount.Add(ctx, 1, metric.WithAttributes(
			attribute.String("scope", scope),
		))
	}
}

// RecordRetry increments the retry counter.
func RecordRetry(ctx context.Context, stepName string) {
	if retryCount != nil {
		retryCount.Add(ctx, 1, metric.WithAttributes(
			attribute.String("step", stepName),
		))
	}
}

// RecordWasmFailure increments the Wasm failure counter.
func RecordWasmFailure(ctx context.Context, skillID, errorType string) {
	if wasmFailureTotal != nil {
		wasmFailureTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("skill_id", skillID),
			attribute.String("error_type", errorType),
		))
	}
}
