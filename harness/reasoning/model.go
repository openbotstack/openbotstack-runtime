package reasoning

// EventType categorizes a reasoning step for visualization.
type EventType string

const (
	EventPlan        EventType = "plan"
	EventThought     EventType = "thought"
	EventToolCall    EventType = "tool_call"
	EventObservation EventType = "observation"
	EventDecision    EventType = "decision"
)

// ReasoningEvent represents a single step in the execution reasoning tree.
type ReasoningEvent struct {
	StepID      string           `json:"step_id,omitempty"`
	Type        EventType        `json:"type"`
	StepType    string           `json:"step_type,omitempty"`
	Summary     string           `json:"summary"`
	Input       any              `json:"input,omitempty"`
	Output      any              `json:"output,omitempty"`
	DurationMs  int              `json:"duration_ms"`
	Status      string           `json:"status,omitempty"`
	Error       string           `json:"error,omitempty"`
	TurnNumber  int              `json:"turn_number"`
	PlanText    string           `json:"plan_text,omitempty"`
	StopReason  string           `json:"stop_reason,omitempty"`
	Observations []string        `json:"observations,omitempty"`
	Children    []ReasoningEvent `json:"children,omitempty"`
}
