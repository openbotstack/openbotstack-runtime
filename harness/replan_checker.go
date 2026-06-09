package harness

import (
	"encoding/json"
	"fmt"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
)

// MaxReplansPerSession is the hard cap on replans per session, regardless of
// what ReplanConfig.MaxReplans says. This is a safety bound.
const MaxReplansPerSession = 2

// ReplanConfig controls whether and how replanning is allowed.
type ReplanConfig struct {
	Enabled    bool
	MaxReplans int
}

// DefaultReplanConfig returns the default replan configuration.
func DefaultReplanConfig() ReplanConfig {
	return ReplanConfig{Enabled: true, MaxReplans: MaxReplansPerSession}
}

// ReplanCheckResult is the outcome of a ShouldReplan check.
type ReplanCheckResult struct {
	ShouldReplan bool
	Trigger      planner.ReplanTrigger
	Reason       string
}

// ShouldReplan determines whether the harness should replan after a step
// completes. It checks: config enabled, replanner availability, replan count
// caps (both config and hard cap), and inspects the step result for errors or
// explicit replan signals.
func ShouldReplan(
	stepResult *execution.StepResult,
	execErr error,
	replanCount int,
	config ReplanConfig,
	replannerAvailable bool,
) ReplanCheckResult {
	// Gate: feature disabled or no replanner plugged in.
	if !config.Enabled || !replannerAvailable {
		return ReplanCheckResult{}
	}

	// Cap: config limit.
	if replanCount >= config.MaxReplans {
		return ReplanCheckResult{}
	}

	// Cap: hard session limit (cannot be overridden by config).
	if replanCount >= MaxReplansPerSession {
		return ReplanCheckResult{}
	}

	// Check for explicit replan signal first (takes priority over errors).
	if stepResult != nil && hasExplicitReplanSignal(stepResult) {
		return ReplanCheckResult{
			ShouldReplan: true,
			Trigger:      planner.ReplanTriggerExplicitSignal,
			Reason:       fmt.Sprintf("step %q requested explicit replan", stepResult.StepName),
		}
	}

	// Check for errors.
	var err error
	switch {
	case execErr != nil:
		err = execErr
	case stepResult != nil && stepResult.Error != nil:
		err = stepResult.Error
	}

	if err != nil {
		stepName := "unknown"
		if stepResult != nil {
			stepName = stepResult.StepName
		}
		return ReplanCheckResult{
			ShouldReplan: true,
			Trigger:      planner.ReplanTriggerToolFailure,
			Reason:       fmt.Sprintf("step %q failed: %s", stepName, err.Error()),
		}
	}

	// No error, no signal — continue as planned.
	return ReplanCheckResult{}
}

// hasExplicitReplanSignal checks whether a step result contains a needs_replan
// signal. It handles both map[string]any and JSON string outputs.
func hasExplicitReplanSignal(sr *execution.StepResult) bool {
	if sr == nil || sr.Output == nil {
		return false
	}

	switch v := sr.Output.(type) {
	case map[string]any:
		val, ok := v["needs_replan"]
		if !ok {
			return false
		}
		boolVal, ok := val.(bool)
		return ok && boolVal

	case string:
		var m map[string]any
		if err := json.Unmarshal([]byte(v), &m); err != nil {
			return false
		}
		val, ok := m["needs_replan"]
		if !ok {
			return false
		}
		boolVal, ok := val.(bool)
		return ok && boolVal

	default:
		return false
	}
}
