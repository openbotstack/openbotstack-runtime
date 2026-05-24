package planner

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/openbotstack/openbotstack-core/execution"
)

// Pre-compiled regex for template reference detection.
var qualityTemplateRe = regexp.MustCompile(`\{\{([\w][\w.-]*)\}\}`)

// QualitySeverity indicates the severity of a plan quality issue.
type QualitySeverity string

const (
	SeverityCritical QualitySeverity = "critical"
	SeverityWarning  QualitySeverity = "warning"
	SeverityInfo     QualitySeverity = "info"
)

// QualityIssue describes a single plan quality problem.
type QualityIssue struct {
	Code     string          `json:"code"`
	Severity QualitySeverity `json:"severity"`
	Message  string          `json:"message"`
	StepIdx  int             `json:"step_idx,omitempty"` // -1 = global
	StepName string          `json:"step_name,omitempty"`
}

// QualitySuggestion is an actionable recommendation.
type QualitySuggestion struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	StepIdx     int    `json:"step_idx,omitempty"`
}

// QualityReport is the output of a plan quality audit.
type QualityReport struct {
	Score       float64             `json:"score"` // 0.0 - 1.0
	Issues      []QualityIssue      `json:"issues"`
	Suggestions []QualitySuggestion  `json:"suggestions"`
}

// QualityContext provides the context needed for quality auditing.
type QualityContext struct {
	AvailableTools    map[string]bool
	RequiredTools     []string
	ExpectedStepRange [2]int
	Intent            string
}

// QualityValidator audits a plan's decision quality.
type QualityValidator struct {
	ctx QualityContext
}

// NewQualityValidator creates a validator with the given quality context.
func NewQualityValidator(qctx QualityContext) *QualityValidator {
	return &QualityValidator{ctx: qctx}
}

// Audit evaluates a plan and returns a quality report.
func (qv *QualityValidator) Audit(plan *execution.ExecutionPlan) *QualityReport {
	report := &QualityReport{
		Score:       1.0,
		Issues:      []QualityIssue{},
		Suggestions: []QualitySuggestion{},
	}

	if plan == nil || len(plan.Steps) == 0 {
		report.Issues = append(report.Issues, QualityIssue{
			Code:     "EMPTY_PLAN",
			Severity: SeverityCritical,
			Message:  "plan has no steps",
			StepIdx:  -1,
		})
		report.Score = 0.0
		return report
	}

	qv.detectRedundantSteps(plan, report)
	qv.detectMissingCriticalSteps(plan, report)
	qv.detectWrongToolSelection(plan, report)
	qv.detectOverPlanning(plan, report)
	qv.detectUnderPlanning(plan, report)
	qv.detectUnreachableSteps(plan, report)
	qv.detectCircularDependencies(plan, report)

	report.Score = computeScore(report)
	return report
}

func (qv *QualityValidator) detectRedundantSteps(plan *execution.ExecutionPlan, report *QualityReport) {
	seen := make(map[string]int)

	for i, step := range plan.Steps {
		key := stableStepKey(step)
		if firstIdx, exists := seen[key]; exists {
			report.Issues = append(report.Issues, QualityIssue{
				Code:     "REDUNDANT_STEP",
				Severity: SeverityWarning,
				Message:  fmt.Sprintf("step %q at index %d is identical to step at index %d", step.Name, i, firstIdx),
				StepIdx:  i,
				StepName: step.Name,
			})
			report.Suggestions = append(report.Suggestions, QualitySuggestion{
				Code:        "REMOVE_DUPLICATE",
				Description: fmt.Sprintf("remove duplicate step %q at index %d or merge with index %d", step.Name, i, firstIdx),
				StepIdx:     i,
			})
		}
		seen[key] = i
	}
}

func (qv *QualityValidator) detectMissingCriticalSteps(plan *execution.ExecutionPlan, report *QualityReport) {
	if len(qv.ctx.RequiredTools) == 0 {
		return
	}

	plannedTools := make(map[string]bool)
	for _, step := range plan.Steps {
		plannedTools[step.Name] = true
	}

	for _, required := range qv.ctx.RequiredTools {
		if !plannedTools[required] {
			report.Issues = append(report.Issues, QualityIssue{
				Code:     "MISSING_CRITICAL_STEP",
				Severity: SeverityCritical,
				Message:  fmt.Sprintf("required tool %q is missing from plan", required),
				StepIdx:  -1,
			})
			report.Suggestions = append(report.Suggestions, QualitySuggestion{
				Code:        "ADD_MISSING_TOOL",
				Description: fmt.Sprintf("add step using tool %q to satisfy intent %q", required, qv.ctx.Intent),
			})
		}
	}
}

func (qv *QualityValidator) detectWrongToolSelection(plan *execution.ExecutionPlan, report *QualityReport) {
	if len(qv.ctx.AvailableTools) == 0 {
		return
	}

	for i, step := range plan.Steps {
		if !qv.ctx.AvailableTools[step.Name] {
			report.Issues = append(report.Issues, QualityIssue{
				Code:     "UNKNOWN_TOOL",
				Severity: SeverityCritical,
				Message:  fmt.Sprintf("tool %q at index %d is not in the available tool set", step.Name, i),
				StepIdx:  i,
				StepName: step.Name,
			})
			report.Suggestions = append(report.Suggestions, QualitySuggestion{
				Code:        "REPLACE_UNKNOWN_TOOL",
				Description: fmt.Sprintf("replace %q with an available tool, or register it", step.Name),
				StepIdx:     i,
			})
		}
	}
}

func (qv *QualityValidator) detectOverPlanning(plan *execution.ExecutionPlan, report *QualityReport) {
	maxSteps := qv.ctx.ExpectedStepRange[1]
	if maxSteps <= 0 {
		return
	}

	if len(plan.Steps) > maxSteps {
		report.Issues = append(report.Issues, QualityIssue{
			Code:     "OVER_PLANNING",
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("plan has %d steps, expected at most %d", len(plan.Steps), maxSteps),
			StepIdx:  -1,
		})
		report.Suggestions = append(report.Suggestions, QualitySuggestion{
			Code:        "SIMPLIFY_PLAN",
			Description: fmt.Sprintf("consider combining or eliminating steps to reduce from %d to <= %d", len(plan.Steps), maxSteps),
		})
	}
}

func (qv *QualityValidator) detectUnderPlanning(plan *execution.ExecutionPlan, report *QualityReport) {
	minSteps := qv.ctx.ExpectedStepRange[0]
	if minSteps <= 0 {
		return
	}

	if len(plan.Steps) < minSteps {
		report.Issues = append(report.Issues, QualityIssue{
			Code:     "UNDER_PLANNING",
			Severity: SeverityWarning,
			Message:  fmt.Sprintf("plan has %d steps, expected at least %d", len(plan.Steps), minSteps),
			StepIdx:  -1,
		})
		report.Suggestions = append(report.Suggestions, QualitySuggestion{
			Code:        "EXPAND_PLAN",
			Description: fmt.Sprintf("the intent %q likely requires more than %d step(s)", qv.ctx.Intent, len(plan.Steps)),
		})
	}
}

// detectUnreachableSteps finds {{step_name.field}} references to steps not present
// in the plan, or steps that appear later in the sequence (forward references).
func (qv *QualityValidator) detectUnreachableSteps(plan *execution.ExecutionPlan, report *QualityReport) {
	stepNames := make(map[string]int)
	for i, step := range plan.Steps {
		stepNames[step.Name] = i
	}

	templateRe := qualityTemplateRe

	for i, step := range plan.Steps {
		for _, val := range step.Arguments {
			strVal, ok := val.(string)
			if !ok {
				continue
			}
			matches := templateRe.FindAllStringSubmatch(strVal, -1)
			for _, m := range matches {
				refName := resolveStepRef(m[1], stepNames)
				if refName == "" {
					report.Issues = append(report.Issues, QualityIssue{
						Code:     "UNREACHABLE_STEP_REF",
						Severity: SeverityCritical,
						Message:  fmt.Sprintf("step %q at index %d references %q which does not exist in plan", step.Name, i, m[1]),
						StepIdx:  i,
						StepName: step.Name,
					})
					report.Suggestions = append(report.Suggestions, QualitySuggestion{
						Code:        "FIX_REFERENCE",
						Description: fmt.Sprintf("add step referenced by %q before step %q, or remove the reference", m[1], step.Name),
						StepIdx:     i,
					})
				} else if refIdx := stepNames[refName]; refIdx >= i {
					report.Issues = append(report.Issues, QualityIssue{
						Code:     "FORWARD_REFERENCE",
						Severity: SeverityWarning,
						Message:  fmt.Sprintf("step %q at index %d references %q at index %d (must be earlier)", step.Name, i, refName, refIdx),
						StepIdx:  i,
						StepName: step.Name,
					})
					report.Suggestions = append(report.Suggestions, QualitySuggestion{
						Code:        "REORDER_STEPS",
						Description: fmt.Sprintf("move step %q before step %q to resolve forward reference", refName, step.Name),
						StepIdx:     i,
					})
				}
			}
		}
	}
}

// detectCircularDependencies checks for cycles in step result references.
func (qv *QualityValidator) detectCircularDependencies(plan *execution.ExecutionPlan, report *QualityReport) {
	// Build adjacency: step[i] depends on step[j] if step[i] has {{step[j].Name|...}}
	stepNames := make(map[string]int)
	for i, step := range plan.Steps {
		stepNames[step.Name] = i
	}

	templateRe := qualityTemplateRe

	// For each step, check if any referenced step also references this step
	for i, step := range plan.Steps {
		for _, val := range step.Arguments {
			strVal, ok := val.(string)
			if !ok {
				continue
			}
			matches := templateRe.FindAllStringSubmatch(strVal, -1)
			for _, m := range matches {
				refName := resolveStepRef(m[1], stepNames)
				if refName == "" {
					continue
				}
				refIdx := stepNames[refName]
				// Check if the referenced step also references this step
				refStep := plan.Steps[refIdx]
				for _, refVal := range refStep.Arguments {
					refStr, ok := refVal.(string)
					if !ok {
						continue
					}
					refRefs := templateRe.FindAllStringSubmatch(refStr, -1)
					for _, rr := range refRefs {
						rrName := resolveStepRef(rr[1], stepNames)
						if rrName == step.Name {
							report.Issues = append(report.Issues, QualityIssue{
								Code:     "CIRCULAR_DEPENDENCY",
								Severity: SeverityCritical,
								Message:  fmt.Sprintf("circular reference: step %q ↔ step %q", step.Name, refName),
								StepIdx:  i,
								StepName: step.Name,
							})
						}
					}
				}
			}
		}
	}
}

// resolveStepRef maps a template reference to a known step name.
// For {{sensor.read_metrics.temperature}}, tries full string first,
// then strips the last .segment until a match is found.
func resolveStepRef(ref string, stepNames map[string]int) string {
	if _, exists := stepNames[ref]; exists {
		return ref
	}
	for {
		idx := strings.LastIndex(ref, ".")
		if idx <= 0 {
			break
		}
		ref = ref[:idx]
		if _, exists := stepNames[ref]; exists {
			return ref
		}
	}
	return ""
}

// stableStepKey produces a deterministic key for a step by sorting argument keys.
func stableStepKey(step execution.ExecutionStep) string {
	parts := []string{step.Name, string(step.Type)}
	argKeys := make([]string, 0, len(step.Arguments))
	for k := range step.Arguments {
		argKeys = append(argKeys, k)
	}
	sort.Strings(argKeys)
	for _, k := range argKeys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, step.Arguments[k]))
	}
	return strings.Join(parts, "|")
}

func computeScore(report *QualityReport) float64 {
	score := 1.0
	for _, issue := range report.Issues {
		switch issue.Severity {
		case SeverityCritical:
			score -= 0.25
		case SeverityWarning:
			score -= 0.1
		case SeverityInfo:
			score -= 0.02
		}
	}
	if score < 0 {
		score = 0
	}
	return score
}
