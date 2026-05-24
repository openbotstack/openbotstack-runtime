package reasoning

// EventType categorizes a reasoning step for visualization.
type EventType string

const (
	EventPlan      EventType = "plan"
	EventThought   EventType = "thought"
	EventToolCall  EventType = "tool_call"
	EventObservation EventType = "observation"
	EventDecision  EventType = "decision"
)

// ReasoningEvent represents a single step in the execution reasoning tree.
// Events form a tree: top-level nodes are plan/thought/decision,
// and tool_call nodes have observation children.
type ReasoningEvent struct {
	StepID     string            `json:"step_id,omitempty"`
	Type       EventType         `json:"type"`
	Summary    string            `json:"summary"`
	Input      any               `json:"input,omitempty"`
	Output     any               `json:"output,omitempty"`
	DurationMs int               `json:"duration_ms,omitempty"`
	Children   []ReasoningEvent  `json:"children,omitempty"`
}
