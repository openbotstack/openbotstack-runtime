package logging

import (
	"context"
	"log/slog"
	"os"

	"github.com/openbotstack/openbotstack-core/execution"
)

// StructuredLogger implements ExecutionLogger using slog.
type StructuredLogger struct {
	logger *slog.Logger
}

// NewStructuredLogger creates a new JSON logger for execution events.
func NewStructuredLogger() *StructuredLogger {
	return &StructuredLogger{
		logger: slog.New(slog.NewJSONHandler(os.Stdout, nil)),
	}
}

// LogStep implements execution.ExecutionLogger.
func (l *StructuredLogger) LogStep(ctx context.Context, record execution.ExecutionLogRecord) error {
	l.logger.InfoContext(ctx, "execution step completed",
		"request_id", record.RequestID,
		"assistant_id", record.AssistantID,
		"step", record.StepName,
		"type", record.StepType,
		"status", record.Status,
		"duration_ms", record.Duration.Milliseconds(),
		"error", record.Error,
	)
	return nil
}

// LogPlanStart implements execution.ExecutionLogger.
func (l *StructuredLogger) LogPlanStart(ctx context.Context, requestID, assistantID string, plan execution.ExecutionPlan) error {
	l.logger.InfoContext(ctx, "execution plan started",
		"request_id", requestID,
		"assistant_id", assistantID,
		"steps_count", len(plan.Steps),
	)
	return nil
}

// LogPlanEnd implements execution.ExecutionLogger.
func (l *StructuredLogger) LogPlanEnd(ctx context.Context, requestID, assistantID string, err error) error {
	status := "success"
	errorMessage := ""
	if err != nil {
		status = "failed"
		errorMessage = err.Error()
	}
	
	l.logger.InfoContext(ctx, "execution plan completed",
		"request_id", requestID,
		"assistant_id", assistantID,
		"status", status,
		"error", errorMessage,
	)
	return nil
}
