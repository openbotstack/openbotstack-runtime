package reasoning

import (
	"fmt"
	"strings"
)

// RenderReasoningText produces a human-readable, step-by-step text
// representation of the reasoning tree.
func RenderReasoningText(tree *ReasoningEvent) string {
	if tree == nil {
		return ""
	}

	var sb strings.Builder
	stepNum := 0

	for _, child := range tree.Children {
		switch child.Type {
		case EventThought:
			if child.TurnNumber > 0 {
				// Individual turn
				sb.WriteString(fmt.Sprintf("Turn %d", child.TurnNumber))
				if child.StopReason != "" {
					sb.WriteString(fmt.Sprintf(" [%s]", child.StopReason))
				}
				sb.WriteString("\n")
				for _, action := range child.Children {
					if action.Type == EventToolCall {
						sb.WriteString(fmt.Sprintf("  Step: %s (%dms)\n", action.Summary, action.DurationMs))
					}
				}
			} else {
				// LLM phase wrapper
				sb.WriteString(child.Summary)
				sb.WriteString("\n")
				for _, turn := range child.Children {
					sb.WriteString(fmt.Sprintf("  %s\n", turn.Summary))
				}
			}
		case EventToolCall:
			stepNum++
			sb.WriteString(fmt.Sprintf("Step %d: %s", stepNum, child.Summary))
			sb.WriteString("\n")
			for _, obs := range child.Children {
				if obs.Type == EventObservation {
					sb.WriteString(fmt.Sprintf("  → %s", obs.Summary))
					sb.WriteString("\n")
				}
			}
		case EventDecision:
			sb.WriteString(fmt.Sprintf("Decision: %s", child.Summary))
			sb.WriteString("\n")
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}
