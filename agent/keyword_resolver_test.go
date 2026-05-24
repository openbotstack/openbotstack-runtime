package agent

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

// mockWorkflow implements agent.Workflow for testing.
type mockWorkflow struct {
	id        string
	name      string
	stepsFn   func(input map[string]any) ([]execution.ExecutionStep, error)
	timeoutFn func() time.Duration
}

func (m *mockWorkflow) ID() string                                         { return m.id }
func (m *mockWorkflow) Name() string                                       { return m.name }
func (m *mockWorkflow) Steps(input map[string]any) ([]execution.ExecutionStep, error) {
	if m.stepsFn != nil {
		return m.stepsFn(input)
	}
	return nil, nil
}
func (m *mockWorkflow) Timeout() time.Duration {
	if m.timeoutFn != nil {
		return m.timeoutFn()
	}
	return 30 * time.Second
}

func TestKeywordResolver_NoPatterns_ReturnsNil(t *testing.T) {
	r := NewKeywordWorkflowResolver()
	wf, input, err := r.Resolve("anything")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wf != nil {
		t.Error("expected nil workflow with no patterns")
	}
	if input != nil {
		t.Error("expected nil input with no patterns")
	}
}

func TestKeywordResolver_SingleKeyword_Match(t *testing.T) {
	r := NewKeywordWorkflowResolver()
	wf := &mockWorkflow{id: "test/summary", name: "Summary"}
	r.Register(wf, []string{"summarize"}, nil)

	result, input, err := r.Resolve("Please summarize this document")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected workflow match")
	}
	if result.ID() != "test/summary" {
		t.Errorf("expected test/summary, got %s", result.ID())
	}
	if input != nil {
		t.Errorf("expected nil input when registered with nil, got %v", input)
	}
}

func TestKeywordResolver_MultiKeyword_AND_Logic(t *testing.T) {
	r := NewKeywordWorkflowResolver()
	wf := &mockWorkflow{id: "test/patient_summary", name: "Patient Summary"}
	r.Register(wf, []string{"patient", "summary"}, nil)

	// Both keywords present → match
	result, _, err := r.Resolve("Generate a patient summary report")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected match when both keywords present")
	}

	// Only one keyword → no match
	result, _, err = r.Resolve("Get patient information")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected no match when only one keyword present")
	}
}

func TestKeywordResolver_CaseInsensitive(t *testing.T) {
	r := NewKeywordWorkflowResolver()
	wf := &mockWorkflow{id: "test/tax", name: "Tax"}
	r.Register(wf, []string{"CALCULATE", "Tax"}, nil)

	result, _, err := r.Resolve("calculate my tax please")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected case-insensitive match")
	}
}

func TestKeywordResolver_FirstMatchWins(t *testing.T) {
	r := NewKeywordWorkflowResolver()
	wf1 := &mockWorkflow{id: "first", name: "First"}
	wf2 := &mockWorkflow{id: "second", name: "Second"}
	r.Register(wf1, []string{"report"}, nil)
	r.Register(wf2, []string{"report"}, nil)

	result, _, err := r.Resolve("Generate a report")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID() != "first" {
		t.Errorf("expected first registered pattern to win, got %s", result.ID())
	}
}

func TestKeywordResolver_PartialKeyword_NoMatch(t *testing.T) {
	r := NewKeywordWorkflowResolver()
	wf := &mockWorkflow{id: "test/summary", name: "Summary"}
	r.Register(wf, []string{"summarize"}, nil)

	// "summary" contains "summar" but is not "summarize"
	result, _, err := r.Resolve("Give me a summary")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected no match for partial keyword")
	}
}

func TestKeywordResolver_EmptyMessage_ReturnsNil(t *testing.T) {
	r := NewKeywordWorkflowResolver()
	wf := &mockWorkflow{id: "test/summary", name: "Summary"}
	r.Register(wf, []string{"summarize"}, nil)

	result, _, err := r.Resolve("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil for empty message")
	}
}

func TestKeywordResolver_NilInput_ReturnsEmptyMap(t *testing.T) {
	r := NewKeywordWorkflowResolver()
	wf := &mockWorkflow{id: "test/summary", name: "Summary"}
	r.Register(wf, []string{"summarize"}, nil)

	_, input, err := r.Resolve("Please summarize this")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input != nil {
		t.Errorf("expected nil input when registered with nil, got %v", input)
	}
}

func TestKeywordResolver_DefaultInput_Returned(t *testing.T) {
	r := NewKeywordWorkflowResolver()
	wf := &mockWorkflow{id: "test/patient", name: "Patient"}
	r.Register(wf, []string{"patient"}, map[string]any{"patient_id": "auto"})

	_, input, err := r.Resolve("Get patient info")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if input == nil {
		t.Fatal("expected input to be returned")
	}
	if input["patient_id"] != "auto" {
		t.Errorf("expected patient_id=auto, got %v", input["patient_id"])
	}
}

func TestKeywordResolver_DefaultInput_NoMutation(t *testing.T) {
	r := NewKeywordWorkflowResolver()
	wf := &mockWorkflow{id: "test/patient", name: "Patient"}
	original := map[string]any{"patient_id": "auto"}
	r.Register(wf, []string{"patient"}, original)

	_, input, _ := r.Resolve("Get patient info")
	input["extra"] = "modified" // mutate returned map

	// Resolve again — original should not be affected
	_, input2, _ := r.Resolve("Get patient info")
	if _, ok := input2["extra"]; ok {
		t.Error("default input was mutated across resolves — should return a copy")
	}
}

func TestKeywordResolver_ConcurrentSafe(t *testing.T) {
	r := NewKeywordWorkflowResolver()
	wf := &mockWorkflow{id: "test/tax", name: "Tax"}
	r.Register(wf, []string{"tax"}, map[string]any{"amount": 1000})

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, input, err := r.Resolve("calculate my tax")
			if err != nil {
				errors <- err
				return
			}
			if result == nil {
				errors <- errNotFound("expected match")
				return
			}
			if input == nil {
				errors <- errNotFound("expected input")
				return
			}
		}()
	}
	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent resolve error: %v", err)
	}
}

func TestKeywordResolver_ConcurrentReadWrite(t *testing.T) {
	r := NewKeywordWorkflowResolver()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			wf := &mockWorkflow{id: fmt.Sprintf("wf/%d", id), name: fmt.Sprintf("WF %d", id)}
			r.Register(wf, []string{fmt.Sprintf("keyword%d", id)}, nil)
		}(i)
		go func() {
			defer wg.Done()
			r.Resolve("some test message")
		}()
	}
	wg.Wait()
}

func TestKeywordResolver_RegisterEmptyKeywords_Panics(t *testing.T) {
	r := NewKeywordWorkflowResolver()
	wf := &mockWorkflow{id: "test/bad", name: "Bad"}

	defer func() {
		if rec := recover(); rec == nil {
			t.Error("expected panic for empty keywords")
		}
	}()
	r.Register(wf, []string{}, nil)
}

func TestKeywordResolver_RegisterWhitespaceKeyword_Panics(t *testing.T) {
	r := NewKeywordWorkflowResolver()
	wf := &mockWorkflow{id: "test/bad", name: "Bad"}

	defer func() {
		if rec := recover(); rec == nil {
			t.Error("expected panic for whitespace-only keyword")
		}
	}()
	r.Register(wf, []string{"  "}, nil)
}

type errNotFound string

func (e errNotFound) Error() string { return string(e) }
