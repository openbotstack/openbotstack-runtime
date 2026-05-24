package planner

import (
	"encoding/json"

	"github.com/openbotstack/openbotstack-core/execution"
)

// GoldenPlanID identifies a standard reference plan.
type GoldenPlanID string

const (
	GoldenMultiStepAnalysis GoldenPlanID = "multi_step_analysis"
	GoldenToolCombination   GoldenPlanID = "tool_combination"
	GoldenErrorRecovery     GoldenPlanID = "error_recovery_retry_fallback"
	GoldenConcurrentTask    GoldenPlanID = "concurrent_task"
)

// GoldenPlanRegistry holds standard reference plans for quality comparison.
type GoldenPlanRegistry struct {
	plans map[GoldenPlanID]*GoldenPlanEntry
}

// GoldenPlanEntry pairs a golden plan with its quality context.
type GoldenPlanEntry struct {
	Plan    *execution.ExecutionPlan
	Context QualityContext
}

// NewGoldenPlanRegistry creates a registry with built-in golden plans.
func NewGoldenPlanRegistry() *GoldenPlanRegistry {
	r := &GoldenPlanRegistry{
		plans: make(map[GoldenPlanID]*GoldenPlanEntry),
	}

	r.registerBuiltins()
	return r
}

// Get retrieves a golden plan by ID.
func (r *GoldenPlanRegistry) Get(id GoldenPlanID) (*GoldenPlanEntry, bool) {
	e, ok := r.plans[id]
	return e, ok
}

// Register adds a golden plan.
func (r *GoldenPlanRegistry) Register(id GoldenPlanID, entry *GoldenPlanEntry) {
	r.plans[id] = entry
}

// IDs returns all registered golden plan IDs.
func (r *GoldenPlanRegistry) IDs() []GoldenPlanID {
	ids := make([]GoldenPlanID, 0, len(r.plans))
	for id := range r.plans {
		ids = append(ids, id)
	}
	return ids
}

// ReplayPlan serializes a plan to JSON and deserializes it back.
// Verifies golden plans are round-trip safe.
func ReplayPlan(plan *execution.ExecutionPlan) (*execution.ExecutionPlan, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}

	var replayed execution.ExecutionPlan
	if err := json.Unmarshal(data, &replayed); err != nil {
		return nil, err
	}
	return &replayed, nil
}

func (r *GoldenPlanRegistry) registerBuiltins() {
	// Multi-step analysis: query → read → test → score → summarize
	r.Register(GoldenMultiStepAnalysis, &GoldenPlanEntry{
		Plan: &execution.ExecutionPlan{
			Steps: []execution.ExecutionStep{
				{Name: "data.query_record", Type: execution.StepTypeTool, Arguments: map[string]any{"record_id": "R001"}},
				{Name: "sensor.read_metrics", Type: execution.StepTypeTool, Arguments: map[string]any{"record_id": "R001"}},
				{Name: "analysis.run_tests", Type: execution.StepTypeTool, Arguments: map[string]any{"record_id": "R001"}},
				{Name: "analysis.compute_score", Type: execution.StepTypeTool, Arguments: map[string]any{
					"record_id":       "R001",
					"value_a":         "{{sensor.read_metrics.value_a}}",
					"value_b":         "{{sensor.read_metrics.value_b}}",
					"anomaly_count":   "{{analysis.run_tests.anomaly_count}}",
				}},
				{Name: "report/generate_summary", Type: execution.StepTypeSkill, Arguments: map[string]any{"record_id": "R001"}},
			},
		},
		Context: QualityContext{
			Intent:            "multi_step_analysis",
			AvailableTools:    map[string]bool{"data.query_record": true, "sensor.read_metrics": true, "analysis.run_tests": true, "analysis.compute_score": true, "report/generate_summary": true},
			RequiredTools:     []string{"data.query_record", "sensor.read_metrics", "analysis.run_tests", "analysis.compute_score"},
			ExpectedStepRange: [2]int{4, 6},
		},
	})

	// Tool combination: data query + analysis
	r.Register(GoldenToolCombination, &GoldenPlanEntry{
		Plan: &execution.ExecutionPlan{
			Steps: []execution.ExecutionStep{
				{Name: "data.query_record", Type: execution.StepTypeTool, Arguments: map[string]any{"region": "us-west"}},
				{Name: "sensor.read_metrics", Type: execution.StepTypeTool, Arguments: map[string]any{"record_id": "R001"}},
				{Name: "analysis.compute_score", Type: execution.StepTypeTool, Arguments: map[string]any{"record_id": "R001"}},
			},
		},
		Context: QualityContext{
			Intent:            "tool_combination",
			AvailableTools:    map[string]bool{"data.query_record": true, "sensor.read_metrics": true, "analysis.run_tests": true, "analysis.compute_score": true},
			RequiredTools:     []string{"data.query_record", "analysis.compute_score"},
			ExpectedStepRange: [2]int{2, 5},
		},
	})

	// Error recovery: retry + fallback
	r.Register(GoldenErrorRecovery, &GoldenPlanEntry{
		Plan: &execution.ExecutionPlan{
			Steps: []execution.ExecutionStep{
				{Name: "sensor.read_metrics", Type: execution.StepTypeTool, Arguments: map[string]any{"record_id": "R001"}},
			},
		},
		Context: QualityContext{
			Intent:            "error_recovery",
			AvailableTools:    map[string]bool{"sensor.read_metrics": true, "data.query_record": true},
			RequiredTools:     []string{"sensor.read_metrics"},
			ExpectedStepRange: [2]int{1, 2},
		},
	})

	// Concurrent task: multiple records
	r.Register(GoldenConcurrentTask, &GoldenPlanEntry{
		Plan: &execution.ExecutionPlan{
			Steps: []execution.ExecutionStep{
				{Name: "data.query_record", Type: execution.StepTypeTool, Arguments: map[string]any{"record_id": "R001"}, Parallelizable: true, ParallelGroup: "records"},
				{Name: "data.query_record", Type: execution.StepTypeTool, Arguments: map[string]any{"record_id": "R002"}, Parallelizable: true, ParallelGroup: "records"},
				{Name: "analysis.compute_score", Type: execution.StepTypeTool, Arguments: map[string]any{"record_id": "R001"}},
				{Name: "analysis.compute_score", Type: execution.StepTypeTool, Arguments: map[string]any{"record_id": "R002"}},
			},
		},
		Context: QualityContext{
			Intent:            "concurrent_task",
			AvailableTools:    map[string]bool{"data.query_record": true, "sensor.read_metrics": true, "analysis.compute_score": true},
			RequiredTools:     []string{"data.query_record", "analysis.compute_score"},
			ExpectedStepRange: [2]int{3, 6},
		},
	})
}
