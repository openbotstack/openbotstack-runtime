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
				fmt.Fprintf(&sb, "Turn %d", child.TurnNumber)
				if child.StopReason != "" {
					fmt.Fprintf(&sb, " [%s]", child.StopReason)
				}
				sb.WriteString("\n")
				for _, action := range child.Children {
					if action.Type == EventToolCall {
						fmt.Fprintf(&sb, "  Step: %s (%dms)\n", action.Summary, action.DurationMs)
					}
				}
			} else {
				// LLM phase wrapper
				sb.WriteString(child.Summary)
				sb.WriteString("\n")
				for _, turn := range child.Children {
					fmt.Fprintf(&sb, "  %s\n", turn.Summary)
				}
			}
		case EventToolCall:
			stepNum++
			fmt.Fprintf(&sb, "Step %d: %s", stepNum, child.Summary)
			sb.WriteString("\n")
			for _, obs := range child.Children {
				if obs.Type == EventObservation {
					fmt.Fprintf(&sb, "  → %s", obs.Summary)
					sb.WriteString("\n")
				}
			}
		case EventDecision:
			fmt.Fprintf(&sb, "Decision: %s", child.Summary)
			sb.WriteString("\n")
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}
