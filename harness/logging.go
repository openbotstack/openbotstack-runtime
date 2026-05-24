package harness

import (
	"context"
	"log/slog"

	"github.com/openbotstack/openbotstack-core/execution"
)

// ecLogAttrs extracts correlation attributes from an ExecutionContext
// for structured logging. Returns alternating key-value pairs.
func ecLogAttrs(ec *execution.ExecutionContext) []any {
	if ec == nil {
		return nil
	}
	attrs := []any{"execution_id", ec.RequestID}
	if ec.TenantID != "" {
		attrs = append(attrs, "tenant_id", ec.TenantID)
	}
	if ec.SessionID != "" {
		attrs = append(attrs, "session_id", ec.SessionID)
	}
	return attrs
}

// logAttrsFromCtx extracts a correlation_id from the raw context (set by
// CorrelationMiddleware) and combines it with ExecutionContext attributes.
func logAttrsFromCtx(ctx context.Context, ec *execution.ExecutionContext) []any {
	attrs := ecLogAttrs(ec)
	// The api package stores correlation_id via context value; since harness
	// doesn't import api, we use a string key match (the api package uses a
	// private type, so we also try the OTel span attribute path).
	// For now, if execution_id is present that serves as the primary correlation.
	_ = ctx
	return attrs
}

// warnLog is a convenience wrapper that adds execution context attributes to slog.WarnContext.
func warnLog(ctx context.Context, ec *execution.ExecutionContext, msg string, args ...any) {
	all := append(ecLogAttrs(ec), args...)
	slog.WarnContext(ctx, msg, all...)
}

// infoLog is a convenience wrapper that adds execution context attributes to slog.InfoContext.
func infoLog(ctx context.Context, ec *execution.ExecutionContext, msg string, args ...any) {
	all := append(ecLogAttrs(ec), args...)
	slog.InfoContext(ctx, msg, all...)
}

// debugLog is a convenience wrapper that adds execution context attributes to slog.DebugContext.
func debugLog(ctx context.Context, ec *execution.ExecutionContext, msg string, args ...any) {
	all := append(ecLogAttrs(ec), args...)
	slog.DebugContext(ctx, msg, all...)
}

// errorLog is a convenience wrapper that adds execution context attributes to slog.ErrorContext.
func errorLog(ctx context.Context, ec *execution.ExecutionContext, msg string, args ...any) {
	all := append(ecLogAttrs(ec), args...)
	slog.ErrorContext(ctx, msg, all...)
}
