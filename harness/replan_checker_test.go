package harness

import (
	"errors"
	"strings"
	"testing"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
)

func TestShouldReplan_Disabled(t *testing.T) {
	sr := &execution.StepResult{StepName: "step1"}
	result := ShouldReplan(sr, nil, 0, ReplanConfig{Enabled: false, MaxReplans: 2}, true)
	if result.ShouldReplan {
		t.Error("expected no replan when config disabled")
	}
}

func TestShouldReplan_NoReplannerAvailable(t *testing.T) {
	sr := &execution.StepResult{StepName: "step1", Error: errors.New("boom")}
	result := ShouldReplan(sr, nil, 0, ReplanConfig{Enabled: true, MaxReplans: 2}, false)
	if result.ShouldReplan {
		t.Error("expected no replan when replanner not available")
	}
}

func TestShouldReplan_NoError(t *testing.T) {
	sr := &execution.StepResult{StepName: "step1", Output: "ok"}
	result := ShouldReplan(sr, nil, 0, ReplanConfig{Enabled: true, MaxReplans: 2}, true)
	if result.ShouldReplan {
		t.Error("expected no replan when no error present")
	}
}

func TestShouldReplan_ToolFailure(t *testing.T) {
	sr := &execution.StepResult{StepName: "step1", Error: errors.New("tool failed")}
	result := ShouldReplan(sr, nil, 0, ReplanConfig{Enabled: true, MaxReplans: 2}, true)
	if !result.ShouldReplan {
		t.Fatal("expected replan for step error")
	}
	if result.Trigger != planner.ReplanTriggerToolFailure {
		t.Errorf("expected trigger=tool_failure, got %s", result.Trigger)
	}
	if result.Reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestShouldReplan_ExecError(t *testing.T) {
	sr := &execution.StepResult{StepName: "step1"}
	execErr := errors.New("execution failed")
	result := ShouldReplan(sr, execErr, 0, ReplanConfig{Enabled: true, MaxReplans: 2}, true)
	if !result.ShouldReplan {
		t.Fatal("expected replan for exec error")
	}
	if result.Trigger != planner.ReplanTriggerToolFailure {
		t.Errorf("expected trigger=tool_failure, got %s", result.Trigger)
	}
}

func TestShouldReplan_ExplicitSignal(t *testing.T) {
	sr := &execution.StepResult{
		StepName: "step1",
		Output: map[string]any{
			"needs_replan": true,
			"reason":       "data changed",
		},
	}
	result := ShouldReplan(sr, nil, 0, ReplanConfig{Enabled: true, MaxReplans: 2}, true)
	if !result.ShouldReplan {
		t.Fatal("expected replan for explicit signal")
	}
	if result.Trigger != planner.ReplanTriggerExplicitSignal {
		t.Errorf("expected trigger=explicit_signal, got %s", result.Trigger)
	}
}

func TestShouldReplan_ExplicitSignalString(t *testing.T) {
	jsonOutput := `{"needs_replan": true, "message": "re-evaluate"}`
	sr := &execution.StepResult{
		StepName: "step1",
		Output:   jsonOutput,
	}
	result := ShouldReplan(sr, nil, 0, ReplanConfig{Enabled: true, MaxReplans: 2}, true)
	if !result.ShouldReplan {
		t.Fatal("expected replan for explicit signal in JSON string")
	}
	if result.Trigger != planner.ReplanTriggerExplicitSignal {
		t.Errorf("expected trigger=explicit_signal, got %s", result.Trigger)
	}
}

func TestShouldReplan_MaxReplansReached(t *testing.T) {
	sr := &execution.StepResult{StepName: "step1", Error: errors.New("fail")}
	result := ShouldReplan(sr, nil, 2, ReplanConfig{Enabled: true, MaxReplans: 2}, true)
	if result.ShouldReplan {
		t.Error("expected no replan when max replans reached (replanCount == MaxReplans)")
	}
}

func TestShouldReplan_HardCap(t *testing.T) {
	sr := &execution.StepResult{StepName: "step1", Error: errors.New("fail")}
	// Config allows 5, but hard cap is MaxReplansPerSession(2)
	result := ShouldReplan(sr, nil, 2, ReplanConfig{Enabled: true, MaxReplans: 5}, true)
	if result.ShouldReplan {
		t.Error("expected no replan when hard cap reached even with higher config limit")
	}
}

// --- Unit tests for hasExplicitReplanSignal helper ---

func TestHasExplicitReplanSignal_MapTrue(t *testing.T) {
	sr := &execution.StepResult{
		Output: map[string]any{"needs_replan": true},
	}
	if !hasExplicitReplanSignal(sr) {
		t.Error("expected signal from map with needs_replan=true")
	}
}

func TestHasExplicitReplanSignal_MapFalse(t *testing.T) {
	sr := &execution.StepResult{
		Output: map[string]any{"needs_replan": false},
	}
	if hasExplicitReplanSignal(sr) {
		t.Error("expected no signal from map with needs_replan=false")
	}
}

func TestHasExplicitReplanSignal_JSONString(t *testing.T) {
	sr := &execution.StepResult{
		Output: `{"needs_replan": true}`,
	}
	if !hasExplicitReplanSignal(sr) {
		t.Error("expected signal from JSON string with needs_replan=true")
	}
}

func TestHasExplicitReplanSignal_JSONStringFalse(t *testing.T) {
	sr := &execution.StepResult{
		Output: `{"needs_replan": false}`,
	}
	if hasExplicitReplanSignal(sr) {
		t.Error("expected no signal from JSON string with needs_replan=false")
	}
}

func TestHasExplicitReplanSignal_PlainString(t *testing.T) {
	sr := &execution.StepResult{
		Output: "just a plain string",
	}
	if hasExplicitReplanSignal(sr) {
		t.Error("expected no signal from plain string")
	}
}

func TestHasExplicitReplanSignal_NilOutput(t *testing.T) {
	sr := &execution.StepResult{}
	if hasExplicitReplanSignal(sr) {
		t.Error("expected no signal from nil output")
	}
}

func TestHasExplicitReplanSignal_NonBoolValue(t *testing.T) {
	sr := &execution.StepResult{
		Output: map[string]any{"needs_replan": "yes"},
	}
	if hasExplicitReplanSignal(sr) {
		t.Error("expected no signal when needs_replan is not bool")
	}
}

func TestDefaultReplanConfig(t *testing.T) {
	cfg := DefaultReplanConfig()
	if !cfg.Enabled {
		t.Error("expected Enabled=true by default")
	}
	if cfg.MaxReplans != MaxReplansPerSession {
		t.Errorf("expected MaxReplans=%d, got %d", MaxReplansPerSession, cfg.MaxReplans)
	}
}

func TestShouldReplan_ReasonContainsErrorMessage(t *testing.T) {
	err := errors.New("connection refused")
	sr := &execution.StepResult{StepName: "fetch_data", Error: err}
	result := ShouldReplan(sr, nil, 0, ReplanConfig{Enabled: true, MaxReplans: 2}, true)
	if !result.ShouldReplan {
		t.Fatal("expected replan")
	}
	if result.Reason == "" {
		t.Fatal("expected non-empty reason")
	}
	// Reason must mention the step name
	if !strings.Contains(result.Reason, "fetch_data") {
		t.Errorf("reason should mention step name, got: %s", result.Reason)
	}
	// Reason must mention the error message
	if !strings.Contains(result.Reason, "connection refused") {
		t.Errorf("reason should mention error message, got: %s", result.Reason)
	}
}
