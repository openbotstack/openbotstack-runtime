package loop

// ProgressEvent represents an intermediate execution event emitted during
// inner/outer loop execution. These events are forwarded to SSE clients so the
// UI can display real-time progress instead of waiting for the final result.
type ProgressEvent struct {
	Type    string `json:"type"`              // "thought", "tool_call", "tool_result", "checkpoint", "turn_complete"
	Content string `json:"content"`           // Human-readable description or payload
	Turn    int    `json:"turn,omitempty"`    // Inner loop turn number (1-based)
	Tool    string `json:"tool,omitempty"`    // Tool/skill name for tool_call/tool_result events
}

// ProgressCallback is invoked at key execution points inside the loops.
// Implementations must be safe for concurrent use if the caller requires it,
// though the current dual-loop kernel runs sequentially.
// A nil callback is treated as a no-op.
type ProgressCallback func(event ProgressEvent)
