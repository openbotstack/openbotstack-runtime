package audit

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
)

// ExecutionReplay is a reconstructed execution trace built from audit events.
type ExecutionReplay struct {
	ExecutionID   string       `json:"execution_id"`
	TenantID      string       `json:"tenant_id"`
	Steps         []ReplayStep `json:"steps"`
	TotalDuration int64        `json:"total_duration_ms"`
	StartedAt     time.Time    `json:"started_at"`
	CompletedAt   time.Time    `json:"completed_at,omitempty"`
	Status        string       `json:"status"` // completed, failed, partial
}

// ReplayStep is a single step in the replay, reconstructed from matched
// started/completed/failed audit events.
type ReplayStep struct {
	StepID    string          `json:"step_id"`
	StepName  string          `json:"step_name"`
	StepType  string          `json:"step_type"`
	StepIndex int             `json:"step_index"`
	Status    string          `json:"status"` // started, completed, failed
	Input     json.RawMessage `json:"input,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
	Duration  int64           `json:"duration_ms"`
	Timestamp time.Time       `json:"timestamp"`
	Error     string          `json:"error,omitempty"`
}

var (
	// ErrEmptyExecutionID is returned when the execution ID is empty.
	ErrEmptyExecutionID = errors.New("execution_id is required")

	// ErrNilProvider is returned when the audit querier is nil.
	ErrNilProvider = errors.New("audit querier is nil")

	// ErrExecutionNotFound is returned when no events are found for the execution ID.
	ErrExecutionNotFound = errors.New("execution not found")
)

// ReplayQuerier queries audit events for replay construction.
// This matches the existing AuditQuerier interface in the api package.
type ReplayQuerier interface {
	Query(ctx context.Context, filter execution_logs.QueryFilter) ([]audit.AuditEvent, error)
}

// ReplayBuilder reconstructs execution traces from audit events.
type ReplayBuilder struct {
	querier ReplayQuerier
}

// NewReplayBuilder creates a new ReplayBuilder with the given audit querier.
func NewReplayBuilder(querier ReplayQuerier) *ReplayBuilder {
	return &ReplayBuilder{querier: querier}
}

// Build reconstructs an execution replay from audit events for the given execution ID.
// It queries all events with the matching RequestID, sorts them by timestamp,
// groups them into steps, and calculates durations.
func (b *ReplayBuilder) Build(ctx context.Context, executionID string) (*ExecutionReplay, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if executionID == "" {
		return nil, ErrEmptyExecutionID
	}

	if b.querier == nil {
		return nil, ErrNilProvider
	}

	// Query all events for this execution ID (mapped to RequestID in the audit log)
	events, err := b.querier.Query(ctx, execution_logs.QueryFilter{
		RequestID: executionID,
		Limit:     10000, // Reasonable upper bound for replay
	})
	if err != nil {
		return nil, err
	}

	if len(events) == 0 {
		return nil, ErrExecutionNotFound
	}

	// Sort events by timestamp for deterministic ordering
	sort.Slice(events, func(i, j int) bool {
		if events[i].Timestamp.Equal(events[j].Timestamp) {
			return events[i].ID < events[j].ID
		}
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	// Derive tenant from first event
	tenantID := events[0].TenantID

	// Group events by StepID to reconstruct steps
	steps := buildSteps(events)

	// Determine overall status
	status := determineOverallStatus(events, steps)

	// Calculate timing
	startedAt := events[0].Timestamp
	completedAt := events[len(events)-1].Timestamp
	totalDuration := completedAt.Sub(startedAt).Milliseconds()

	// If the last event is not a terminal state, don't report a completion time
	if status == "partial" {
		completedAt = time.Time{}
	}

	return &ExecutionReplay{
		ExecutionID:   executionID,
		TenantID:      tenantID,
		Steps:         steps,
		TotalDuration: totalDuration,
		StartedAt:     startedAt,
		CompletedAt:   completedAt,
		Status:        status,
	}, nil
}

// buildSteps groups audit events by StepID and reconstructs ReplayStep entries.
// Events without a StepID are skipped (they are execution-level events, not step events).
func buildSteps(events []audit.AuditEvent) []ReplayStep {
	type stepAccumulator struct {
		started  *audit.AuditEvent
		terminal *audit.AuditEvent // completed or failed
	}

	// Group events by StepID
	stepMap := make(map[string]*stepAccumulator)
	var stepOrder []string

	for _, e := range events {
		if e.StepID == "" {
			continue // Skip execution-level events
		}

		acc, exists := stepMap[e.StepID]
		if !exists {
			acc = &stepAccumulator{}
			stepMap[e.StepID] = acc
			stepOrder = append(stepOrder, e.StepID)
		}

		switch e.Status {
		case "started":
			acc.started = &e
		case "completed", "failed":
			acc.terminal = &e
		}
	}

	// Build ReplayStep entries in order of first appearance
	steps := make([]ReplayStep, 0, len(stepOrder))
	for i, stepID := range stepOrder {
		acc := stepMap[stepID]
		step := ReplayStep{
			StepID:    stepID,
			StepIndex: i,
		}

		// Determine timestamp from earliest event
		switch {
		case acc.started != nil:
			step.StepName = acc.started.StepName
			step.StepType = acc.started.StepType
			step.Timestamp = acc.started.Timestamp
			step.Input = serializeToolInput(acc.started.ToolInput)
		case acc.terminal != nil:
			step.StepName = acc.terminal.StepName
			step.StepType = acc.terminal.StepType
			step.Timestamp = acc.terminal.Timestamp
		}

		// Determine status and duration from terminal event
		switch {
		case acc.terminal != nil:
			step.Status = acc.terminal.Status
			step.Duration = acc.terminal.Duration.Milliseconds()
			step.Error = acc.terminal.Error
			step.Output = serializeToolOutput(acc.terminal.ToolOutput)
		case acc.started != nil:
			step.Status = "started" // Never completed
		}

		steps = append(steps, step)
	}

	return steps
}

// determineOverallStatus returns the overall execution status based on events.
func determineOverallStatus(events []audit.AuditEvent, steps []ReplayStep) string {
	// Check if any step failed
	for _, s := range steps {
		if s.Status == "failed" {
			return "failed"
		}
	}

	// Check if any event has a failure outcome
	for _, e := range events {
		if e.Outcome == "failure" || e.Outcome == "timeout" {
			return "failed"
		}
	}

	// Check if all steps are completed
	for _, s := range steps {
		if s.Status != "completed" {
			return "partial"
		}
	}

	// If there are no steps but the last event is a success, it's completed
	if len(steps) == 0 {
		last := events[len(events)-1]
		if last.Outcome == "success" {
			return "completed"
		}
		return "partial"
	}

	return "completed"
}

// serializeToolInput converts ToolInput map to JSON RawMessage.
func serializeToolInput(input map[string]any) json.RawMessage {
	if len(input) == 0 {
		return nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	return data
}

// serializeToolOutput converts ToolOutput to JSON RawMessage.
func serializeToolOutput(output any) json.RawMessage {
	if output == nil {
		return nil
	}
	data, err := json.Marshal(output)
	if err != nil {
		return nil
	}
	return data
}
