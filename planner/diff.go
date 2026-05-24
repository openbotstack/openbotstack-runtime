package planner

import (
	"fmt"
	"strings"

	"github.com/openbotstack/openbotstack-core/execution"
)

// DiffType categorizes a plan difference.
type DiffType string

const (
	DiffAdded    DiffType = "added"
	DiffRemoved  DiffType = "removed"
	DiffModified DiffType = "modified"
	DiffReordered DiffType = "reordered"
)

// PlanDiff describes a single difference between two plans.
type PlanDiff struct {
	Type      DiffType `json:"type"`
	StepName  string   `json:"step_name"`
	StepIdx   int      `json:"step_idx"`
	Detail    string   `json:"detail"`
	FieldName string   `json:"field_name,omitempty"` // which field differs (for modified)
	Expected  string   `json:"expected,omitempty"`
	Actual    string   `json:"actual,omitempty"`
}

// PlanDiffResult holds the complete diff between two plans.
type PlanDiffResult struct {
	Diffs     []PlanDiff `json:"diffs"`
	Summary   string     `json:"summary"`
	Similarity float64   `json:"similarity"` // 0.0 - 1.0
}

// DiffPlans computes the structural difference between two plans.
func DiffPlans(golden, actual *execution.ExecutionPlan) *PlanDiffResult {
	result := &PlanDiffResult{Diffs: []PlanDiff{}}

	if golden == nil && actual == nil {
		result.Summary = "both plans are nil"
		result.Similarity = 1.0
		return result
	}
	if golden == nil {
		result.Diffs = append(result.Diffs, PlanDiff{Type: DiffAdded, Detail: "actual plan exists but golden is nil"})
		result.Summary = "golden plan is nil"
		result.Similarity = 0.0
		return result
	}
	if actual == nil {
		result.Diffs = append(result.Diffs, PlanDiff{Type: DiffRemoved, Detail: "golden plan exists but actual is nil"})
		result.Summary = "actual plan is nil"
		result.Similarity = 0.0
		return result
	}

	// Step-level diff using LCS-like approach
	goldenSteps := golden.Steps
	actualSteps := actual.Steps

	maxLen := len(goldenSteps)
	if len(actualSteps) > maxLen {
		maxLen = len(actualSteps)
	}

	matched := 0
	for i := 0; i < maxLen; i++ {
		if i >= len(goldenSteps) {
			result.Diffs = append(result.Diffs, PlanDiff{
				Type:     DiffAdded,
				StepIdx:  i,
				StepName: actualSteps[i].Name,
				Detail:   fmt.Sprintf("extra step %q at index %d not in golden", actualSteps[i].Name, i),
			})
		} else if i >= len(actualSteps) {
			result.Diffs = append(result.Diffs, PlanDiff{
				Type:     DiffRemoved,
				StepIdx:  i,
				StepName: goldenSteps[i].Name,
				Detail:   fmt.Sprintf("missing step %q at index %d from golden", goldenSteps[i].Name, i),
			})
		} else {
			stepDiffs := diffSteps(goldenSteps[i], actualSteps[i], i)
			result.Diffs = append(result.Diffs, stepDiffs...)
			if len(stepDiffs) == 0 {
				matched++
			}
		}
	}

	// Tool-call diff
	toolDiffs := diffToolCalls(goldenSteps, actualSteps)
	result.Diffs = append(result.Diffs, toolDiffs...)

	// Compute similarity
	totalSteps := len(goldenSteps)
	if totalSteps == 0 {
		totalSteps = 1
	}
	result.Similarity = float64(matched) / float64(totalSteps)

	result.Summary = fmt.Sprintf("%d diffs found, similarity %.0f%%", len(result.Diffs), result.Similarity*100)
	return result
}

func diffSteps(golden, actual execution.ExecutionStep, idx int) []PlanDiff {
	var diffs []PlanDiff

	if golden.Name != actual.Name {
		diffs = append(diffs, PlanDiff{
			Type:      DiffModified,
			StepIdx:   idx,
			StepName:  actual.Name,
			FieldName: "name",
			Expected:  golden.Name,
			Actual:    actual.Name,
			Detail:    fmt.Sprintf("step name mismatch: expected %q, got %q", golden.Name, actual.Name),
		})
	}

	if golden.Type != actual.Type {
		diffs = append(diffs, PlanDiff{
			Type:      DiffModified,
			StepIdx:   idx,
			StepName:  actual.Name,
			FieldName: "type",
			Expected:  string(golden.Type),
			Actual:    string(actual.Type),
			Detail:    fmt.Sprintf("step type mismatch: expected %q, got %q", golden.Type, actual.Type),
		})
	}

	// Argument diff
	argDiffs := diffArguments(golden.Arguments, actual.Arguments, idx, actual.Name)
	diffs = append(diffs, argDiffs...)

	return diffs
}

func diffArguments(golden, actual map[string]any, idx int, stepName string) []PlanDiff {
	var diffs []PlanDiff

	// Check for missing arguments
	for k, gv := range golden {
		av, exists := actual[k]
		if !exists {
			diffs = append(diffs, PlanDiff{
				Type:      DiffRemoved,
				StepIdx:   idx,
				StepName:  stepName,
				FieldName: fmt.Sprintf("arguments.%s", k),
				Expected:  fmt.Sprintf("%v", gv),
				Detail:    fmt.Sprintf("missing argument %q in step %q", k, stepName),
			})
			continue
		}
		if fmt.Sprintf("%v", gv) != fmt.Sprintf("%v", av) {
			diffs = append(diffs, PlanDiff{
				Type:      DiffModified,
				StepIdx:   idx,
				StepName:  stepName,
				FieldName: fmt.Sprintf("arguments.%s", k),
				Expected:  fmt.Sprintf("%v", gv),
				Actual:    fmt.Sprintf("%v", av),
				Detail:    fmt.Sprintf("argument %q mismatch: expected %v, got %v", k, gv, av),
			})
		}
	}

	// Check for extra arguments
	for k := range actual {
		if _, exists := golden[k]; !exists {
			diffs = append(diffs, PlanDiff{
				Type:      DiffAdded,
				StepIdx:   idx,
				StepName:  stepName,
				FieldName: fmt.Sprintf("arguments.%s", k),
				Actual:    fmt.Sprintf("%v", actual[k]),
				Detail:    fmt.Sprintf("unexpected argument %q in step %q", k, stepName),
			})
		}
	}

	return diffs
}

func diffToolCalls(golden, actual []execution.ExecutionStep) []PlanDiff {
	var diffs []PlanDiff

	goldenTools := stepNames(golden)
	actualTools := stepNames(actual)

	// Presence diff: count occurrences, not just set membership
	goldenCounts := make(map[string]int)
	for _, n := range goldenTools {
		goldenCounts[n]++
	}
	actualCounts := make(map[string]int)
	for _, n := range actualTools {
		actualCounts[n]++
	}

	// Missing tool calls (appear fewer times in actual)
	for n, gc := range goldenCounts {
		ac := actualCounts[n]
		if ac < gc {
			diffs = append(diffs, PlanDiff{
				Type:     DiffRemoved,
				StepName: n,
				Detail:   fmt.Sprintf("tool %q called %d time(s) in golden but %d in actual", n, gc, ac),
			})
		}
	}

	// Extra tool calls
	for n, ac := range actualCounts {
		gc := goldenCounts[n]
		if ac > gc {
			diffs = append(diffs, PlanDiff{
				Type:     DiffAdded,
				StepName: n,
				Detail:   fmt.Sprintf("tool %q called %d time(s) in actual but %d in golden", n, ac, gc),
			})
		}
	}

	// Ordering diff — compare unique first occurrence position
	goldenFirstPos := make(map[string]int)
	for i, n := range goldenTools {
		if _, exists := goldenFirstPos[n]; !exists {
			goldenFirstPos[n] = i
		}
	}
	actualFirstPos := make(map[string]int)
	for i, n := range actualTools {
		if _, exists := actualFirstPos[n]; !exists {
			actualFirstPos[n] = i
		}
	}

	// Only report reorder for tools that appear exactly once in both
	for n := range goldenCounts {
		if goldenCounts[n] == 1 && actualCounts[n] == 1 {
			gp := goldenFirstPos[n]
			ap := actualFirstPos[n]
			if gp != ap {
				diffs = append(diffs, PlanDiff{
					Type:     DiffReordered,
					StepIdx:  ap,
					StepName: n,
					Expected: fmt.Sprintf("position %d", gp),
					Actual:   fmt.Sprintf("position %d", ap),
					Detail:   fmt.Sprintf("tool %q moved from position %d to %d", n, gp, ap),
				})
			}
		}
	}

	return diffs
}

func stepNames(steps []execution.ExecutionStep) []string {
	names := make([]string, len(steps))
	for i, s := range steps {
		names[i] = s.Name
	}
	return names
}

// FormatDiffResult produces a human-readable diff summary.
func FormatDiffResult(result *PlanDiffResult) string {
	if len(result.Diffs) == 0 {
		return "Plans are identical."
	}

	var sb strings.Builder
	sb.WriteString(result.Summary)
	sb.WriteString("\n")

	for _, d := range result.Diffs {
		icon := "?"
		switch d.Type {
		case DiffAdded:
			icon = "+"
		case DiffRemoved:
			icon = "-"
		case DiffModified:
			icon = "~"
		case DiffReordered:
			icon = ">"
		}
		sb.WriteString(fmt.Sprintf("  %s [%s] %s", icon, d.StepName, d.Detail))
		sb.WriteString("\n")
	}

	return sb.String()
}
