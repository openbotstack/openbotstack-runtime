package harness

import (
	"fmt"
	"strings"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-runtime/harness/reasoning"
)

// BuildExecutionTrace converts HarnessResult to ExecutionTraceData for visualization.
func BuildExecutionTrace(result *HarnessResult, executionID, tenantID string) *ExecutionTraceData {
	trace := &ExecutionTraceData{
		ExecutionID: executionID,
		PlanID:      result.PlanID,
		TenantID:    tenantID,
		DurationMs:  int(result.Duration.Milliseconds()),
		StopReason:  string(result.StopCondition.Reason),
		StopDetail:  result.StopCondition.Detail,
		ReplanCount: result.ReplanCount,
		PlanIDs:     result.PlanIDs,
		Metrics: TraceMetricsData{
			TotalSteps:     result.Metrics.TotalSteps,
			TotalToolCalls: result.Metrics.TotalToolCalls,
			TotalLLMTurns:  result.Metrics.TotalLLMTurns,
			TotalRuntimeMs: int(result.Metrics.TotalRuntime.Milliseconds()),
		},
	}

	for _, sr := range result.StepResults {
		stepTrace := StepTraceData{
			StepID:     sr.StepID,
			StepName:   sr.StepName,
			StepType:   sr.Type,
			DurationMs: int(sr.Duration.Milliseconds()),
			Retries:    sr.Retries,
			Fallback:   sr.Fallback,
		}

		if input, ok := result.StepInputs[sr.StepID]; ok {
			stepTrace.Input = input
		}
		if sr.Output != nil {
			stepTrace.Output = sr.Output
		}
		if sr.Error != nil {
			stepTrace.Error = sr.Error.Error()
			stepTrace.Status = "failed"
		} else {
			stepTrace.Status = "completed"
		}

		if sr.Type == string(execution.StepTypeLLM) {
			if turns, ok := result.TurnData[sr.StepID]; ok {
				for _, tr := range turns {
					turnTrace := TurnTraceData{
						TurnNumber:  tr.TurnNumber,
						PlanText:    tr.PlanText,
						StopReason:  string(tr.StopReason),
						DurationMs:  int(tr.Duration.Milliseconds()),
						Observations: tr.Observations,
					}
					turnTrace.Actions = make([]TurnAction, len(tr.Actions))
					copy(turnTrace.Actions, tr.Actions)
					stepTrace.Turns = append(stepTrace.Turns, turnTrace)
				}
			}
		}

		trace.Steps = append(trace.Steps, stepTrace)
	}

	return trace
}

// BuildExecutionTree converts ExecutionTraceData to a reasoning event tree.
func BuildExecutionTree(trace *ExecutionTraceData) *reasoning.ReasoningEvent {
	root := &reasoning.ReasoningEvent{
		Type:       reasoning.EventPlan,
		Summary:    fmt.Sprintf("execution with %d step(s)", len(trace.Steps)),
		DurationMs: trace.DurationMs,
	}

	for _, step := range trace.Steps {
		if step.StepType == string(execution.StepTypeLLM) {
			root.Children = append(root.Children, buildLLMPhaseNode(step))
		} else {
			root.Children = append(root.Children, buildToolSkillNode(step))
		}
	}

	decisionSummary := "execution completed"
	stopReason := trace.StopReason
	if stopReason != "" && stopReason != "goal_achieved" {
		decisionSummary = "execution stopped: " + stopReason
	}
	if trace.StopDetail != "" {
		decisionSummary += " — " + trace.StopDetail
	}
	root.Children = append(root.Children, reasoning.ReasoningEvent{
		Type:       reasoning.EventDecision,
		Summary:    decisionSummary,
		StopReason: stopReason,
	})

	return root
}

func buildLLMPhaseNode(step StepTraceData) reasoning.ReasoningEvent {
	llmNode := reasoning.ReasoningEvent{
		StepID:     step.StepID,
		Type:       reasoning.EventThought,
		StepType:   step.StepType,
		Summary:    fmt.Sprintf("Reasoning Loop (%d turns)", len(step.Turns)),
		DurationMs: step.DurationMs,
		Status:     step.Status,
		Input:      step.Input,
		Output:     step.Output,
	}
	if len(step.Turns) == 0 {
		llmNode.Summary = "Reasoning Loop (no turns completed)"
		if step.Error != "" {
			llmNode.Error = sanitizeError(step.Error)
			llmNode.Status = "failed"
		}
	}

	for _, turn := range step.Turns {
		turnNode := reasoning.ReasoningEvent{
			Type:         reasoning.EventThought,
			Summary:      fmt.Sprintf("Turn %d", turn.TurnNumber),
			TurnNumber:   turn.TurnNumber,
			PlanText:     turn.PlanText,
			DurationMs:   turn.DurationMs,
			StopReason:   turn.StopReason,
			Observations: turn.Observations,
		}

		for _, action := range turn.Actions {
			actionNode := reasoning.ReasoningEvent{
				Type:       reasoning.EventToolCall,
				StepType:   action.StepType,
				Summary:    fmt.Sprintf("Call %s", stepDisplayName(action.StepName)),
				Input:      action.Input,
				DurationMs: action.DurationMs,
			}

			obsSummary := fmt.Sprintf("Result from %s", stepDisplayName(action.StepName))
			obsOutput := action.Output
			if action.Error != "" {
				obsSummary = fmt.Sprintf("Error from %s: %s", stepDisplayName(action.StepName), truncate(sanitizeError(action.Error), 120))
				obsOutput = map[string]any{"error": sanitizeError(action.Error)}
			}
			actionNode.Children = append(actionNode.Children, reasoning.ReasoningEvent{
				Type:       reasoning.EventObservation,
				Summary:    obsSummary,
				Output:     obsOutput,
				DurationMs: action.DurationMs,
			})

			turnNode.Children = append(turnNode.Children, actionNode)
		}

		llmNode.Children = append(llmNode.Children, turnNode)
	}

	return llmNode
}

func buildToolSkillNode(step StepTraceData) reasoning.ReasoningEvent {
	callEvent := reasoning.ReasoningEvent{
		StepID:     step.StepID,
		Type:       reasoning.EventToolCall,
		StepType:   step.StepType,
		Summary:    fmt.Sprintf("Call %s", stepDisplayName(step.StepName)),
		Input:      step.Input,
		Output:     step.Output,
		DurationMs: step.DurationMs,
		Status:     step.Status,
	}
	if step.Error != "" {
		callEvent.Error = sanitizeError(step.Error)
		callEvent.Status = "failed"
	}

	obsSummary := fmt.Sprintf("Result from %s", stepDisplayName(step.StepName))
	obsOutput := step.Output
	if step.Error != "" {
		obsSummary = fmt.Sprintf("Error from %s: %s", stepDisplayName(step.StepName), truncate(sanitizeError(step.Error), 120))
		obsOutput = map[string]any{"error": sanitizeError(step.Error)}
	}
	callEvent.Children = append(callEvent.Children, reasoning.ReasoningEvent{
		StepID:     step.StepID,
		Type:       reasoning.EventObservation,
		Summary:    obsSummary,
		Output:     obsOutput,
		DurationMs: step.DurationMs,
	})

	return callEvent
}

func stepDisplayName(name string) string {
	if name == "" {
		return "unknown"
	}
	s := strings.ReplaceAll(name, ".", " ")
	s = strings.ReplaceAll(s, "_", " ")
	return s
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func sanitizeError(err string) string {
	sanitized := err
	if idx := strings.LastIndex(sanitized, ": "); idx >= 0 {
		sanitized = sanitized[idx+2:]
	}
	if len(sanitized) > 200 {
		sanitized = sanitized[:200] + "..."
	}
	return sanitized
}
