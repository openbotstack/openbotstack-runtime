package reasoning

import (
	"fmt"
	"strings"

	"github.com/openbotstack/openbotstack-core/audit"
)

// BuildReasoningTree converts an audit trail into a structured reasoning tree.
// The tree has:
//   - A synthetic "plan" root summarizing the execution
//   - One "tool_call" node per step (input = arguments)
//   - One "observation" child per step (output = result)
//   - A synthetic "decision" node for the final result
//
// The conversion is deterministic: same input → same tree.
func BuildReasoningTree(trail []audit.AuditEvent) *ReasoningEvent {
	if len(trail) == 0 {
		return &ReasoningEvent{
			Type:    EventPlan,
			Summary: "empty execution",
		}
	}

	// Group entries by StepID to pair input/output
	type stepPair struct {
		input  *audit.AuditEvent
		output *audit.AuditEvent
	}
	pairs := make(map[string]*stepPair)
	var stepOrder []string

	for i := range trail {
		e := &trail[i]
		if _, exists := pairs[e.StepID]; !exists {
			pairs[e.StepID] = &stepPair{}
			stepOrder = append(stepOrder, e.StepID)
		}
		if e.Status == "started" || e.Status == "executing" {
			pairs[e.StepID].input = e
		} else {
			// completed, failed, error — treat as output
			pairs[e.StepID].output = e
		}
	}

	root := &ReasoningEvent{
		Type:    EventPlan,
		Summary: fmt.Sprintf("execution with %d step(s)", len(stepOrder)),
	}

	for _, id := range stepOrder {
		pair := pairs[id]

		// Determine step name and type from whichever entry is available
		name := id
		var input map[string]any
		var output any
		var durationMs int
		var errorMsg string

		if pair.input != nil {
			name = stepDisplayName(pair.input.StepName)
			input = pair.input.ToolInput
		}
		if pair.output != nil {
			name = stepDisplayName(pair.output.StepName)
			output = pair.output.ToolOutput
			durationMs = int(pair.output.Duration.Milliseconds())
			if pair.output.Error != "" {
				errorMsg = pair.output.Error
			}
		}

		callEvent := ReasoningEvent{
			StepID:     id,
			Type:       EventToolCall,
			Summary:    fmt.Sprintf("Call %s", name),
			Input:      sanitizeForJSON(input),
			DurationMs: durationMs,
		}

		// Observation child
		obsSummary := fmt.Sprintf("Result from %s", name)
		obsOutput := sanitizeForJSON(output)
		if errorMsg != "" {
			obsSummary = fmt.Sprintf("Error from %s: %s", name, truncate(errorMsg, 120))
			obsOutput = map[string]any{"error": errorMsg}
		}
		callEvent.Children = append(callEvent.Children, ReasoningEvent{
			StepID:     id,
			Type:       EventObservation,
			Summary:    obsSummary,
			Output:     obsOutput,
			DurationMs: durationMs,
		})

		root.Children = append(root.Children, callEvent)
	}

	// Synthetic decision node from the last step's output
	lastPair := pairs[stepOrder[len(stepOrder)-1]]
	decisionSummary := "execution completed"
	var decisionOutput any
	if lastPair != nil && lastPair.output != nil {
		decisionOutput = sanitizeForJSON(lastPair.output.ToolOutput)
		if lastPair.output.Error != "" {
			decisionSummary = "execution ended with error"
		}
	}
	root.Children = append(root.Children, ReasoningEvent{
		Type:    EventDecision,
		Summary: decisionSummary,
		Output:  decisionOutput,
	})

	return root
}

// stepDisplayName converts a step name to a human-friendly display name.
func stepDisplayName(name string) string {
	if name == "" {
		return "unknown"
	}
	s := strings.ReplaceAll(name, ".", " ")
	s = strings.ReplaceAll(s, "_", " ")
	return s
}

// sanitizeForJSON ensures the value is JSON-serializable.
// map[string]any and []any are already safe; other types pass through.
func sanitizeForJSON(v any) any {
	if v == nil {
		return nil
	}
	return v
}

// truncate clips a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
