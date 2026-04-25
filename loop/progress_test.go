package loop

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
)

// =============================================================================
// ProgressEvent serialization
// =============================================================================

func TestProgressEvent_JSONSerialization(t *testing.T) {
	event := ProgressEvent{
		Type:    "thought",
		Content: "Analyzing the user request",
		Turn:    1,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal ProgressEvent: %v", err)
	}

	var decoded ProgressEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal ProgressEvent: %v", err)
	}

	if decoded.Type != "thought" {
		t.Errorf("expected type 'thought', got %q", decoded.Type)
	}
	if decoded.Content != "Analyzing the user request" {
		t.Errorf("expected content 'Analyzing the user request', got %q", decoded.Content)
	}
	if decoded.Turn != 1 {
		t.Errorf("expected turn 1, got %d", decoded.Turn)
	}
}

func TestProgressEvent_OmitsEmptyFields(t *testing.T) {
	event := ProgressEvent{
		Type:    "checkpoint",
		Content: "task 0 started",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// "turn" and "tool" should be omitted because they are zero-valued with omitempty
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if _, ok := raw["turn"]; ok {
		t.Error("expected 'turn' to be omitted when zero, but it was present")
	}
	if _, ok := raw["tool"]; ok {
		t.Error("expected 'tool' to be omitted when empty, but it was present")
	}
}

func TestProgressEvent_ToolCallEvent(t *testing.T) {
	event := ProgressEvent{
		Type:    "tool_call",
		Content: "executing web_search",
		Turn:    2,
		Tool:    "web_search",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ProgressEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Tool != "web_search" {
		t.Errorf("expected tool 'web_search', got %q", decoded.Tool)
	}
	if decoded.Turn != 2 {
		t.Errorf("expected turn 2, got %d", decoded.Turn)
	}
}

// =============================================================================
// progressCollector — test helper
// =============================================================================

// progressCollector is a thread-safe collector for ProgressEvent values.
type progressCollector struct {
	mu     sync.Mutex
	events []ProgressEvent
}

func newProgressCollector() *progressCollector {
	return &progressCollector{events: make([]ProgressEvent, 0)}
}

func (c *progressCollector) callback(event ProgressEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func (c *progressCollector) collected() []ProgressEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]ProgressEvent, len(c.events))
	copy(cp, c.events)
	return cp
}

// =============================================================================
// DefaultInnerLoop progress callback tests
// =============================================================================

func TestInnerLoop_ProgressCallback_EmitsEvents(t *testing.T) {
	// Planner produces one tool step on turn 1, then an empty plan (stop) on turn 2.
	mockPlanner := &mockExecutionPlanner{
		plans: []*execution.ExecutionPlan{
			{Steps: []execution.ExecutionStep{
				{Type: execution.StepTypeTool, Name: "search"},
			}},
			{Steps: []execution.ExecutionStep{}}, // stop signal
		},
	}
	mockRunner := &mockToolRunner{
		results: []any{"search result"},
	}
	collector := newProgressCollector()

	loop := NewDefaultInnerLoop(DefaultInnerConfig(), mockPlanner, mockRunner, &NoOpCompactor{}, &mockLogger{})
	loop.SetProgressCallback(collector.callback)

	ctx := context.Background()
	task := TaskInput{
		TaskDescription: "test progress",
		PlannerContext:  &planner.PlannerContext{},
	}
	ec := execution.NewExecutionContext(ctx, "req1", "asst1", "sess1", "ten1", "user1")

	_, err := loop.Run(ctx, task, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := collector.collected()
	if len(events) == 0 {
		t.Fatal("expected progress events to be emitted, got none")
	}

	// Verify event types appear: thought (turn start), tool_call, tool_result, turn_complete
	var hasThought, hasToolCall, hasToolResult, hasTurnComplete bool
	for _, e := range events {
		switch e.Type {
		case "thought":
			hasThought = true
		case "tool_call":
			hasToolCall = true
			if e.Tool != "search" {
				t.Errorf("expected tool_call tool 'search', got %q", e.Tool)
			}
		case "tool_result":
			hasToolResult = true
		case "turn_complete":
			hasTurnComplete = true
		}
	}

	if !hasThought {
		t.Error("expected 'thought' event to be emitted")
	}
	if !hasToolCall {
		t.Error("expected 'tool_call' event to be emitted")
	}
	if !hasToolResult {
		t.Error("expected 'tool_result' event to be emitted")
	}
	if !hasTurnComplete {
		t.Error("expected 'turn_complete' event to be emitted")
	}
}

func TestInnerLoop_ProgressCallback_TurnNumbersCorrect(t *testing.T) {
	// Two turns: turn 1 has a tool step, turn 2 is empty (stop).
	mockPlanner := &mockExecutionPlanner{
		plans: []*execution.ExecutionPlan{
			{Steps: []execution.ExecutionStep{
				{Type: execution.StepTypeTool, Name: "t1"},
			}},
			{Steps: []execution.ExecutionStep{}},
		},
	}
	mockRunner := &mockToolRunner{results: []any{"ok"}}
	collector := newProgressCollector()

	loop := NewDefaultInnerLoop(DefaultInnerConfig(), mockPlanner, mockRunner, &NoOpCompactor{}, &mockLogger{})
	loop.SetProgressCallback(collector.callback)

	ctx := context.Background()
	_, err := loop.Run(ctx, TaskInput{PlannerContext: &planner.PlannerContext{}}, &execution.ExecutionContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := collector.collected()

	// Events for turn 1 should have Turn=1
	turn1Events := filterEvents(events, "thought", 1)
	if len(turn1Events) == 0 {
		t.Fatal("expected at least one 'thought' event for turn 1")
	}

	// All turn 1 events should have Turn=1
	for _, e := range events {
		if e.Turn == 0 {
			t.Errorf("expected non-zero turn number, got event %+v", e)
		}
	}
}

func TestInnerLoop_ProgressCallback_NilCallback_NoPanics(t *testing.T) {
	// Ensure the loop works normally when no callback is set.
	mockPlanner := &mockExecutionPlanner{
		plans: []*execution.ExecutionPlan{
			{Steps: []execution.ExecutionStep{}},
		},
	}
	loop := NewDefaultInnerLoop(DefaultInnerConfig(), mockPlanner, &mockToolRunner{}, &NoOpCompactor{}, &mockLogger{})
	// Do NOT call SetProgressCallback — callback is nil

	ctx := context.Background()
	result, err := loop.Run(ctx, TaskInput{PlannerContext: &planner.PlannerContext{}}, &execution.ExecutionContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.StopReason != StopReasonPlannerStopped {
		t.Errorf("expected planner_stopped, got %s", result.StopReason)
	}
}

func TestInnerLoop_ProgressCallback_StillWorksNormally(t *testing.T) {
	// Verify the loop still returns correct results even with callback active.
	mockPlanner := &mockExecutionPlanner{
		plans: []*execution.ExecutionPlan{
			{Steps: []execution.ExecutionStep{
				{Type: execution.StepTypeTool, Name: "t1"},
			}},
			{Steps: []execution.ExecutionStep{
				{Type: execution.StepTypeTool, Name: "t2"},
			}},
			{Steps: []execution.ExecutionStep{}},
		},
	}
	mockRunner := &mockToolRunner{results: []any{"r1", "r2"}}
	collector := newProgressCollector()

	loop := NewDefaultInnerLoop(DefaultInnerConfig(), mockPlanner, mockRunner, &NoOpCompactor{}, &mockLogger{})
	loop.SetProgressCallback(collector.callback)

	result, err := loop.Run(context.Background(), TaskInput{PlannerContext: &planner.PlannerContext{}}, &execution.ExecutionContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TurnCount != 3 {
		t.Errorf("expected 3 turns, got %d", result.TurnCount)
	}
	if result.ToolCallsUsed != 2 {
		t.Errorf("expected 2 tool calls, got %d", result.ToolCallsUsed)
	}
	if result.StopReason != StopReasonPlannerStopped {
		t.Errorf("expected planner_stopped, got %s", result.StopReason)
	}
}

// =============================================================================
// DefaultOuterLoop progress callback tests
// =============================================================================

func TestOuterLoop_ProgressCallback_EmitsCheckpointEvents(t *testing.T) {
	mockInner := &mockInnerLoop{
		results: []*TaskResult{
			{TurnCount: 1, StopReason: StopReasonPlannerStopped},
			{TurnCount: 2, StopReason: StopReasonPlannerStopped},
		},
	}
	collector := newProgressCollector()

	loop := NewDefaultOuterLoop(DefaultOuterConfig(), mockInner, &NoOpCheckpoint{}, &NoOpPolicyCheckpoint{}, &mockLogger{})
	loop.SetProgressCallback(collector.callback)

	ctx := context.Background()
	ec := execution.NewExecutionContext(ctx, "r", "a", "s", "t", "u")
	tasks := []TaskInput{
		{TaskDescription: "task 1"},
		{TaskDescription: "task 2"},
	}

	result, err := loop.Run(ctx, tasks, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}

	events := collector.collected()
	if len(events) == 0 {
		t.Fatal("expected progress events from outer loop, got none")
	}

	// Should have checkpoint events: task started + task completed for each task
	var startCount, completeCount int
	for _, e := range events {
		if e.Type == "checkpoint" {
			if contains(e.Content, "started") {
				startCount++
			}
			if contains(e.Content, "completed") {
				completeCount++
			}
		}
	}

	if startCount != 2 {
		t.Errorf("expected 2 'task started' checkpoint events, got %d", startCount)
	}
	if completeCount != 2 {
		t.Errorf("expected 2 'task completed' checkpoint events, got %d", completeCount)
	}
}

func TestOuterLoop_ProgressCallback_NilCallback_NoPanics(t *testing.T) {
	mockInner := &mockInnerLoop{
		results: []*TaskResult{
			{TurnCount: 1, StopReason: StopReasonPlannerStopped},
		},
	}
	loop := NewDefaultOuterLoop(DefaultOuterConfig(), mockInner, &NoOpCheckpoint{}, &NoOpPolicyCheckpoint{}, &mockLogger{})
	// Do NOT set callback — should work normally

	ctx := context.Background()
	ec := execution.NewExecutionContext(ctx, "r", "a", "s", "t", "u")
	result, err := loop.Run(ctx, []TaskInput{{TaskDescription: "task 1"}}, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopCondition.Reason != StopReasonGoalAchieved {
		t.Errorf("expected goal_achieved, got %s", result.StopCondition.Reason)
	}
}

func TestOuterLoop_ProgressCallback_StillWorksNormally(t *testing.T) {
	mockInner := &mockInnerLoop{
		results: []*TaskResult{
			{TurnCount: 2, ToolCallsUsed: 1, StopReason: StopReasonPlannerStopped},
			{TurnCount: 1, ToolCallsUsed: 0, StopReason: StopReasonPlannerStopped},
		},
	}
	collector := newProgressCollector()

	loop := NewDefaultOuterLoop(DefaultOuterConfig(), mockInner, &NoOpCheckpoint{}, &NoOpPolicyCheckpoint{}, &mockLogger{})
	loop.SetProgressCallback(collector.callback)

	ctx := context.Background()
	ec := execution.NewExecutionContext(ctx, "r", "a", "s", "t", "u")
	tasks := []TaskInput{
		{TaskDescription: "task 1"},
		{TaskDescription: "task 2"},
	}

	result, err := loop.Run(ctx, tasks, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Metrics.WorkflowSteps != 2 {
		t.Errorf("expected 2 steps, got %d", result.Metrics.WorkflowSteps)
	}
}

func TestOuterLoop_ProgressCallback_TaskIndexCorrect(t *testing.T) {
	mockInner := &mockInnerLoop{
		results: []*TaskResult{
			{TurnCount: 1, StopReason: StopReasonPlannerStopped},
			{TurnCount: 1, StopReason: StopReasonPlannerStopped},
			{TurnCount: 1, StopReason: StopReasonPlannerStopped},
		},
	}
	collector := newProgressCollector()

	loop := NewDefaultOuterLoop(DefaultOuterConfig(), mockInner, &NoOpCheckpoint{}, &NoOpPolicyCheckpoint{}, &mockLogger{})
	loop.SetProgressCallback(collector.callback)

	ctx := context.Background()
	ec := execution.NewExecutionContext(ctx, "r", "a", "s", "t", "u")
	tasks := []TaskInput{
		{TaskDescription: "task 0"},
		{TaskDescription: "task 1"},
		{TaskDescription: "task 2"},
	}

	_, err := loop.Run(ctx, tasks, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := collector.collected()

	// Check that checkpoint events mention the correct task indices (0-based)
	for _, e := range events {
		if e.Type == "checkpoint" {
			// Events should mention "task 0", "task 1", "task 2"
			if !contains(e.Content, "task 0") && !contains(e.Content, "task 1") && !contains(e.Content, "task 2") {
				t.Errorf("checkpoint event doesn't reference a valid task index: %q", e.Content)
			}
		}
	}
}

// =============================================================================
// Helpers
// =============================================================================

func filterEvents(events []ProgressEvent, eventType string, turn int) []ProgressEvent {
	var result []ProgressEvent
	for _, e := range events {
		if e.Type == eventType && (turn == 0 || e.Turn == turn) {
			result = append(result, e)
		}
	}
	return result
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
