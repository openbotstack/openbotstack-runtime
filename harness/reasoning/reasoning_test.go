package reasoning_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-runtime/api"
	"github.com/openbotstack/openbotstack-runtime/harness/reasoning"
)

// ========================================================================
// Audit → Reasoning Conversion Tests
// ========================================================================

// C1: Empty trail produces plan root with empty summary
func TestConvert_EmptyTrail(t *testing.T) {
	tree := reasoning.BuildReasoningTree(nil)
	if tree.Type != reasoning.EventPlan {
		t.Errorf("type = %q, want plan", tree.Type)
	}
	if tree.Summary != "empty execution" {
		t.Errorf("summary = %q, want empty execution", tree.Summary)
	}
}

// C2: Single step produces plan → tool_call → observation → decision
func TestConvert_SingleStep(t *testing.T) {
	trail := []audit.AuditEvent{
		{StepID: "s1", StepName: "ehr.query_patient", StepType: string(execution.StepTypeTool), Status: "started", ToolInput: map[string]any{"patient_id": "P001"}},
		{StepID: "s1", StepName: "ehr.query_patient", StepType: string(execution.StepTypeTool), Status: "completed", ToolOutput: map[string]any{"name": "John"}, Duration: 50 * time.Millisecond},
	}

	tree := reasoning.BuildReasoningTree(trail)
	if tree.Type != reasoning.EventPlan {
		t.Fatalf("root type = %q, want plan", tree.Type)
	}

	// plan node → 2 children: tool_call + decision
	if len(tree.Children) != 2 {
		t.Fatalf("plan children = %d, want 2", len(tree.Children))
	}

	call := tree.Children[0]
	if call.Type != reasoning.EventToolCall {
		t.Errorf("child[0] type = %q, want tool_call", call.Type)
	}
	if call.StepID != "s1" {
		t.Errorf("child[0] stepID = %q, want s1", call.StepID)
	}
	if len(call.Children) != 1 {
		t.Fatalf("tool_call children = %d, want 1 (observation)", len(call.Children))
	}

	obs := call.Children[0]
	if obs.Type != reasoning.EventObservation {
		t.Errorf("observation type = %q, want observation", obs.Type)
	}

	decision := tree.Children[1]
	if decision.Type != reasoning.EventDecision {
		t.Errorf("child[1] type = %q, want decision", decision.Type)
	}
}

// C3: Multiple steps produce ordered children
func TestConvert_MultipleSteps(t *testing.T) {
	trail := []audit.AuditEvent{
		{StepID: "s1", StepName: "ehr.query_patient", StepType: string(execution.StepTypeTool), Status: "started"},
		{StepID: "s1", StepName: "ehr.query_patient", StepType: string(execution.StepTypeTool), Status: "completed", Duration: 30 * time.Millisecond},
		{StepID: "s2", StepName: "analytics.risk_score", StepType: string(execution.StepTypeTool), Status: "started"},
		{StepID: "s2", StepName: "analytics.risk_score", StepType: string(execution.StepTypeTool), Status: "completed", Duration: 20 * time.Millisecond},
	}

	tree := reasoning.BuildReasoningTree(trail)
	// 2 tool_calls + 1 decision = 3
	if len(tree.Children) != 3 {
		t.Fatalf("children = %d, want 3", len(tree.Children))
	}
	if tree.Children[0].Summary != "Call ehr query patient" {
		t.Errorf("child[0] summary = %q, want 'Call ehr query patient'", tree.Children[0].Summary)
	}
	if tree.Children[1].Summary != "Call analytics risk score" {
		t.Errorf("child[1] summary = %q, want 'Call analytics risk score'", tree.Children[1].Summary)
	}
}

// C4: Error step produces error observation
func TestConvert_ErrorStep(t *testing.T) {
	trail := []audit.AuditEvent{
		{StepID: "s1", StepName: "tool-a", StepType: string(execution.StepTypeTool), Status: "started"},
		{StepID: "s1", StepName: "tool-a", StepType: string(execution.StepTypeTool), Status: "failed", Error: "connection refused", Duration: 10 * time.Millisecond},
	}

	tree := reasoning.BuildReasoningTree(trail)
	call := tree.Children[0]
	obs := call.Children[0]
	if obs.Type != reasoning.EventObservation {
		t.Fatalf("type = %q, want observation", obs.Type)
	}
	obsOutput, ok := obs.Output.(map[string]any)
	if !ok {
		t.Fatalf("output type = %T, want map[string]any", obs.Output)
	}
	if obsOutput["error"] != "connection refused" {
		t.Errorf("error = %v, want connection refused", obsOutput["error"])
	}
}

// C5: Conversion is deterministic — same input produces same output
func TestConvert_Deterministic(t *testing.T) {
	trail := []audit.AuditEvent{
		{StepID: "s1", StepName: "a", StepType: string(execution.StepTypeTool), Status: "started"},
		{StepID: "s1", StepName: "a", StepType: string(execution.StepTypeTool), Status: "completed", ToolOutput: "ok"},
	}

	tree1 := reasoning.BuildReasoningTree(trail)
	tree2 := reasoning.BuildReasoningTree(trail)

	b1, _ := json.Marshal(tree1)
	b2, _ := json.Marshal(tree2)
	if string(b1) != string(b2) {
		t.Errorf("determinism violation:\n%s\nvs\n%s", b1, b2)
	}
}

// C6: Step with only output entry (no start)
func TestConvert_OutputOnlyEntry(t *testing.T) {
	trail := []audit.AuditEvent{
		{StepID: "s1", StepName: "standalone", StepType: string(execution.StepTypeSkill), Status: "completed", ToolOutput: "done", Duration: 100 * time.Millisecond},
	}

	tree := reasoning.BuildReasoningTree(trail)
	if len(tree.Children) != 2 { // tool_call + decision
		t.Fatalf("children = %d, want 2", len(tree.Children))
	}
	call := tree.Children[0]
	if call.StepID != "s1" {
		t.Errorf("stepID = %q, want s1", call.StepID)
	}
	if call.DurationMs != 100 {
		t.Errorf("duration = %d, want 100", call.DurationMs)
	}
}

// ========================================================================
// Reasoning Text Snapshot Tests
// ========================================================================

// T1: Render empty tree
func TestRender_EmptyTree(t *testing.T) {
	text := reasoning.RenderReasoningText(nil)
	if text != "" {
		t.Errorf("expected empty string, got %q", text)
	}
}

// T2: Render single step
func TestRender_SingleStep(t *testing.T) {
	tree := &reasoning.ReasoningEvent{
		Type: reasoning.EventPlan,
		Children: []reasoning.ReasoningEvent{
			{
				Type:    reasoning.EventToolCall,
				Summary: "Call ehr query patient",
				Children: []reasoning.ReasoningEvent{
					{Type: reasoning.EventObservation, Summary: "Result from ehr query patient"},
				},
			},
			{Type: reasoning.EventDecision, Summary: "execution completed"},
		},
	}

	text := reasoning.RenderReasoningText(tree)
	expected := "Step 1: Call ehr query patient\n  → Result from ehr query patient\nDecision: execution completed"
	if text != expected {
		t.Errorf("text = %q\nwant: %q", text, expected)
	}
}

// T3: Render multiple steps
func TestRender_MultipleSteps(t *testing.T) {
	tree := &reasoning.ReasoningEvent{
		Type: reasoning.EventPlan,
		Children: []reasoning.ReasoningEvent{
			{
				Type:    reasoning.EventToolCall,
				Summary: "Call step a",
				Children: []reasoning.ReasoningEvent{
					{Type: reasoning.EventObservation, Summary: "Result from step a"},
				},
			},
			{
				Type:    reasoning.EventToolCall,
				Summary: "Call step b",
				Children: []reasoning.ReasoningEvent{
					{Type: reasoning.EventObservation, Summary: "Result from step b"},
				},
			},
			{Type: reasoning.EventDecision, Summary: "done"},
		},
	}

	text := reasoning.RenderReasoningText(tree)
	if text == "" {
		t.Fatal("text is empty")
	}

	// Must contain numbered steps
	if !contains(text, "Step 1:") {
		t.Error("missing Step 1")
	}
	if !contains(text, "Step 2:") {
		t.Error("missing Step 2")
	}
	if !contains(text, "Decision: done") {
		t.Error("missing Decision")
	}
}

// T4: Render from converted trail matches snapshot
func TestRender_ConvertedSnapshot(t *testing.T) {
	trail := []audit.AuditEvent{
		{StepID: "s1", StepName: "ehr.query_vitals", StepType: string(execution.StepTypeTool), Status: "started", ToolInput: map[string]any{"patient_id": "P001"}},
		{StepID: "s1", StepName: "ehr.query_vitals", StepType: string(execution.StepTypeTool), Status: "completed", ToolOutput: map[string]any{"heart_rate": 110}, Duration: 45 * time.Millisecond},
		{StepID: "s2", StepName: "analytics.risk_score", StepType: string(execution.StepTypeTool), Status: "started", ToolInput: map[string]any{"patient_id": "P001"}},
		{StepID: "s2", StepName: "analytics.risk_score", StepType: string(execution.StepTypeTool), Status: "completed", ToolOutput: map[string]any{"score": 0.7}, Duration: 30 * time.Millisecond},
	}

	tree := reasoning.BuildReasoningTree(trail)
	text := reasoning.RenderReasoningText(tree)

	expected := "Step 1: Call ehr query vitals\n  → Result from ehr query vitals\nStep 2: Call analytics risk score\n  → Result from analytics risk score\nDecision: execution completed"
	if text != expected {
		t.Errorf("snapshot mismatch:\ngot:\n%s\nwant:\n%s", text, expected)
	}
}

// ========================================================================
// Nested Steps Test
// ========================================================================

// N1: Deeply nested children are preserved
func TestConvert_NestedPreservesChildren(t *testing.T) {
	trail := []audit.AuditEvent{
		{StepID: "s1", StepName: "parent-step", StepType: string(execution.StepTypeLLM), Status: "started"},
		{StepID: "s1", StepName: "parent-step", StepType: string(execution.StepTypeLLM), Status: "completed", ToolOutput: "thought result"},
	}

	tree := reasoning.BuildReasoningTree(trail)
	call := tree.Children[0]
	if len(call.Children) != 1 {
		t.Fatalf("tool_call children = %d, want 1", len(call.Children))
	}
	obs := call.Children[0]
	if obs.Type != reasoning.EventObservation {
		t.Errorf("child type = %q, want observation", obs.Type)
	}
}

// ========================================================================
// InMemoryStore Tests
// ========================================================================

// S1: Store and retrieve trail
func TestStore_GetAuditTrail(t *testing.T) {
	store := reasoning.NewInMemoryStore()
	trail := []audit.AuditEvent{
		{StepID: "s1", StepName: "test", Status: "completed"},
	}
	store.StoreTrail("exec-1", trail)

	got, err := store.GetAuditTrail(context.Background(), "exec-1")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("trail length = %d, want 1", len(got))
	}
	if got[0].StepID != "s1" {
		t.Errorf("stepID = %q, want s1", got[0].StepID)
	}
}

// S2: Missing execution returns nil (not error)
func TestStore_MissingExecution(t *testing.T) {
	store := reasoning.NewInMemoryStore()
	got, err := store.GetAuditTrail(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// S3: Store returns a copy (mutations don't affect stored data)
func TestStore_ReturnsCopy(t *testing.T) {
	store := reasoning.NewInMemoryStore()
	trail := []audit.AuditEvent{
		{StepID: "s1", StepName: "original"},
	}
	store.StoreTrail("exec-1", trail)

	got, _ := store.GetAuditTrail(context.Background(), "exec-1")
	got[0].StepName = "modified"

	again, _ := store.GetAuditTrail(context.Background(), "exec-1")
	if again[0].StepName != "original" {
		t.Error("store did not return a copy — mutation leaked")
	}
}

// ========================================================================
// API Handler Tests
// ========================================================================

// A1: GET /v1/execution/{id}/reasoning returns reasoning tree
func TestAPI_ReasoningEndpoint(t *testing.T) {
	store := reasoning.NewInMemoryStore()
	trail := []audit.AuditEvent{
		{StepID: "s1", StepName: "ehr.query_patient", StepType: string(execution.StepTypeTool), Status: "started", ToolInput: map[string]any{"patient_id": "P001"}},
		{StepID: "s1", StepName: "ehr.query_patient", StepType: string(execution.StepTypeTool), Status: "completed", ToolOutput: map[string]any{"name": "Jane"}, Duration: 50 * time.Millisecond},
	}
	store.StoreTrail("exec-123", trail)

	router := api.NewRouter(api.RouterConfig{ReasoningStore: store})

	req := httptest.NewRequest(http.MethodGet, "/v1/execution/exec-123/reasoning", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}

	var resp api.ReasoningResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json error: %v", err)
	}
	if resp.ExecutionID != "exec-123" {
		t.Errorf("execution_id = %q, want exec-123", resp.ExecutionID)
	}
	if resp.Tree == nil {
		t.Fatal("tree is nil")
	}
	if resp.Tree.Type != reasoning.EventPlan {
		t.Errorf("tree type = %q, want plan", resp.Tree.Type)
	}
	if resp.Text == "" {
		t.Error("text is empty")
	}
	if resp.Debug != nil {
		t.Error("debug should be nil without ?debug=true")
	}
}

// A2: GET with ?debug=true includes audit trail
func TestAPI_ReasoningEndpoint_DebugMode(t *testing.T) {
	store := reasoning.NewInMemoryStore()
	trail := []audit.AuditEvent{
		{StepID: "s1", StepName: "test-step", StepType: string(execution.StepTypeTool), Status: "started"},
		{StepID: "s1", StepName: "test-step", StepType: string(execution.StepTypeTool), Status: "completed", ToolOutput: "ok", Duration: 10 * time.Millisecond},
	}
	store.StoreTrail("exec-456", trail)

	router := api.NewRouter(api.RouterConfig{ReasoningStore: store})

	req := httptest.NewRequest(http.MethodGet, "/v1/execution/exec-456/reasoning?debug=true", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp api.ReasoningResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json error: %v", err)
	}
	if resp.Debug == nil {
		t.Fatal("debug is nil, should be populated with ?debug=true")
	}
	if len(resp.Debug.AuditTrail) != 2 {
		t.Errorf("audit_trail length = %d, want 2", len(resp.Debug.AuditTrail))
	}
}

// A3: Missing execution returns 404
func TestAPI_ReasoningEndpoint_NotFound(t *testing.T) {
	store := reasoning.NewInMemoryStore()
	router := api.NewRouter(api.RouterConfig{ReasoningStore: store})

	req := httptest.NewRequest(http.MethodGet, "/v1/execution/nonexistent/reasoning", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

// A4: No reasoning store returns 404
func TestAPI_ReasoningEndpoint_NoStore(t *testing.T) {
	router := api.NewRouter(api.RouterConfig{})
	// Don't set reasoning store

	req := httptest.NewRequest(http.MethodGet, "/v1/execution/x/reasoning", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

// A5: POST method returns 405
func TestAPI_ReasoningEndpoint_MethodNotAllowed(t *testing.T) {
	router := api.NewRouter(api.RouterConfig{ReasoningStore: reasoning.NewInMemoryStore()})

	req := httptest.NewRequest(http.MethodPost, "/v1/execution/x/reasoning", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

// ========================================================================
// JSON Serialization Test
// ========================================================================

// J1: ReasoningEvent serializes correctly
func TestJSON_Serialization(t *testing.T) {
	tree := &reasoning.ReasoningEvent{
		Type:    reasoning.EventPlan,
		Summary: "test plan",
		Children: []reasoning.ReasoningEvent{
			{
				StepID:     "s1",
				Type:       reasoning.EventToolCall,
				Summary:    "Call test",
				Input:      map[string]any{"key": "value"},
				DurationMs: 50,
				Children: []reasoning.ReasoningEvent{
					{
						Type:    reasoning.EventObservation,
						Summary: "Result",
						Output:  "ok",
					},
				},
			},
		},
	}

	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var parsed reasoning.ReasoningEvent
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if parsed.Type != reasoning.EventPlan {
		t.Errorf("type = %q, want plan", parsed.Type)
	}
	if len(parsed.Children) != 1 {
		t.Fatalf("children = %d, want 1", len(parsed.Children))
	}
	if parsed.Children[0].Children[0].Type != reasoning.EventObservation {
		t.Errorf("nested child type = %q, want observation", parsed.Children[0].Children[0].Type)
	}
}

// ========================================================================
// Full Pipeline Test
// ========================================================================

// F1: Full pipeline: audit trail → convert → render → API response
func TestFullPipeline(t *testing.T) {
	trail := []audit.AuditEvent{
		{TraceID: "tr1", StepID: "s1", StepName: "ehr.query_patient", StepType: string(execution.StepTypeTool), Status: "started", ToolInput: map[string]any{"patient_id": "P001"}, Timestamp: time.Now()},
		{TraceID: "tr1", StepID: "s1", StepName: "ehr.query_patient", StepType: string(execution.StepTypeTool), Status: "completed", ToolOutput: map[string]any{"name": "Alice", "age": 45}, Duration: 80 * time.Millisecond, Timestamp: time.Now()},
		{TraceID: "tr1", StepID: "s2", StepName: "ehr.query_vitals", StepType: string(execution.StepTypeTool), Status: "started", ToolInput: map[string]any{"patient_id": "P001"}, Timestamp: time.Now()},
		{TraceID: "tr1", StepID: "s2", StepName: "ehr.query_vitals", StepType: string(execution.StepTypeTool), Status: "completed", ToolOutput: map[string]any{"heart_rate": 110, "systolic_bp": 140}, Duration: 45 * time.Millisecond, Timestamp: time.Now()},
		{TraceID: "tr1", StepID: "s3", StepName: "analytics.risk_score", StepType: string(execution.StepTypeTool), Status: "started", ToolInput: map[string]any{"patient_id": "P001", "heart_rate": "110"}, Timestamp: time.Now()},
		{TraceID: "tr1", StepID: "s3", StepName: "analytics.risk_score", StepType: string(execution.StepTypeTool), Status: "completed", ToolOutput: map[string]any{"score": 0.72, "level": "high"}, Duration: 30 * time.Millisecond, Timestamp: time.Now()},
	}

	tree := reasoning.BuildReasoningTree(trail)

	// Verify tree structure
	if tree.Type != reasoning.EventPlan {
		t.Fatalf("root type = %q, want plan", tree.Type)
	}
	// 3 tool_calls + 1 decision = 4
	if len(tree.Children) != 4 {
		t.Fatalf("children = %d, want 4", len(tree.Children))
	}

	// Verify text rendering
	text := reasoning.RenderReasoningText(tree)
	if !contains(text, "Step 1:") || !contains(text, "Step 3:") {
		t.Errorf("text missing step numbers: %q", text)
	}
	if !contains(text, "Decision:") {
		t.Errorf("text missing decision: %q", text)
	}

	// Verify JSON round-trip
	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var roundTripped reasoning.ReasoningEvent
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if roundTripped.Type != reasoning.EventPlan {
		t.Errorf("round-trip type = %q", roundTripped.Type)
	}

	// Verify API response
	store := reasoning.NewInMemoryStore()
	store.StoreTrail("full-1", trail)
	router := api.NewRouter(api.RouterConfig{ReasoningStore: store})

	req := httptest.NewRequest(http.MethodGet, "/v1/execution/full-1/reasoning?debug=true", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("API status = %d; body: %s", rr.Code, rr.Body.String())
	}

	var resp api.ReasoningResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("API json: %v", err)
	}
	if resp.ExecutionID != "full-1" {
		t.Errorf("execution_id = %q", resp.ExecutionID)
	}
	if len(resp.Debug.AuditTrail) != 6 {
		t.Errorf("debug audit entries = %d, want 6", len(resp.Debug.AuditTrail))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
