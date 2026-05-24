package reasoning

import (
	"fmt"
	"strings"
)

// RenderReasoningText produces a human-readable, step-by-step text
// representation of the reasoning tree.
//
// Format:
//
//	Step 1: Call data query record
//	  → Result from data query record
//	Step 2: Call analytics risk score
//	  → Result from analytics risk score
//	Decision: execution completed
func RenderReasoningText(tree *ReasoningEvent) string {
	if tree == nil {
		return ""
	}

	var sb strings.Builder
	stepNum := 0

	for _, child := range tree.Children {
		switch child.Type {
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
