package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var (
	skillExecCount    metric.Int64Counter
	skillExecDuration metric.Float64Histogram
	llmTokenUsage     metric.Int64Histogram
	activeRequestsGauge metric.Int64UpDownCounter
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
