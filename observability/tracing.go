package observability

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "openbotstack"

// StartSkillSpan creates a tracing span for skill execution.
// Returns the new context and span. Caller must defer span.End().
func StartSkillSpan(ctx context.Context, skillID string) (context.Context, trace.Span) {
	return otel.GetTracerProvider().Tracer(tracerName).Start(ctx, "skill.execute",
		trace.WithAttributes(attribute.String("skill.id", skillID)),
	)
}

// StartLLMSpan creates a tracing span for an LLM provider call.
func StartLLMSpan(ctx context.Context, provider, model string) (context.Context, trace.Span) {
	return otel.GetTracerProvider().Tracer(tracerName).Start(ctx, "llm.generate",
		trace.WithAttributes(
			attribute.String("llm.provider", provider),
			attribute.String("llm.model", model),
		),
	)
}
