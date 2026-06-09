package memory_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-runtime/memory"
)

// mockProvider implements providers.ModelProvider for testing.
type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) ID() string                                             { return "mock" }
func (m *mockProvider) Capabilities() []aitypes.CapabilityType                 { return []aitypes.CapabilityType{aitypes.CapTextGeneration} }
func (m *mockProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) { return nil, nil }
func (m *mockProvider) Generate(ctx context.Context, req aitypes.GenerateRequest) (*aitypes.GenerateResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &aitypes.GenerateResponse{Content: m.response}, nil
}

// mockRouter implements providers.ModelRouter for testing.
type mockRouter struct {
	provider providers.ModelProvider
	err      error
}

func (m *mockRouter) Route(requirements []aitypes.CapabilityType, constraints aitypes.ModelConstraints) (providers.ModelProvider, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.provider, nil
}
func (m *mockRouter) Register(p providers.ModelProvider) error { return nil }
func (m *mockRouter) List() []string                           { return []string{"mock"} }

// --- Test: CompactMessages compresses messages into TurnSummaries ---

func TestSessionCompactor_CompactMessages(t *testing.T) {
	turnJSON := `[{"topic":"Auth discussion","summary":"Decided on JWT.","decisions":["Use JWT"],"facts":["15-min expiry"]}]`
	router := &mockRouter{provider: &mockProvider{response: turnJSON}}
	compactor := memory.NewSessionCompactor(router)

	msgs := []aitypes.Message{
		aitypes.NewTextMessage("user", "What auth should we use?"),
		aitypes.NewTextMessage("assistant", "I recommend JWT with refresh tokens."),
	}
	plan := memory.CompactionPlan{MessagesToCompress: msgs}
	result, err := compactor.Compact(context.Background(), plan)
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}
	if len(result.NewTurnSummaries) != 1 {
		t.Fatalf("expected 1 TurnSummary, got %d", len(result.NewTurnSummaries))
	}
	ts := result.NewTurnSummaries[0]
	if ts.Topic != "Auth discussion" {
		t.Errorf("Topic = %q, want Auth discussion", ts.Topic)
	}
	if ts.Summary != "Decided on JWT." {
		t.Errorf("Summary = %q", ts.Summary)
	}
	if len(ts.Decisions) != 1 || ts.Decisions[0] != "Use JWT" {
		t.Errorf("Decisions = %v", ts.Decisions)
	}
	if len(ts.Facts) != 1 || ts.Facts[0] != "15-min expiry" {
		t.Errorf("Facts = %v", ts.Facts)
	}
}

// --- Test: CompactMessages with routing failure ---

func TestSessionCompactor_CompactMessages_RoutingFailure(t *testing.T) {
	router := &mockRouter{err: errors.New("no provider available")}
	compactor := memory.NewSessionCompactor(router)

	msgs := []aitypes.Message{aitypes.NewTextMessage("user", "hello")}
	plan := memory.CompactionPlan{MessagesToCompress: msgs}
	_, err := compactor.Compact(context.Background(), plan)
	if err == nil {
		t.Error("expected error from routing failure")
	}
}

// --- Test: CompactMessages with empty input ---

func TestSessionCompactor_CompactMessages_EmptyInput(t *testing.T) {
	router := &mockRouter{provider: &mockProvider{}}
	compactor := memory.NewSessionCompactor(router)

	plan := memory.CompactionPlan{}
	result, err := compactor.Compact(context.Background(), plan)
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}
	if len(result.NewTurnSummaries) != 0 {
		t.Errorf("expected 0 TurnSummaries, got %d", len(result.NewTurnSummaries))
	}
	if result.TokensSaved != 0 {
		t.Errorf("TokensSaved = %d, want 0", result.TokensSaved)
	}
}

// --- Test: ArchiveTurns merges turn summaries into archive ---

func TestSessionCompactor_ArchiveTurns(t *testing.T) {
	archiveResponse := "User discussed API design, chose REST over GraphQL. Decided on JWT auth and PostgreSQL database."
	router := &mockRouter{provider: &mockProvider{response: archiveResponse}}
	compactor := memory.NewSessionCompactor(router)

	turns := []memory.TurnSummary{
		{Topic: "Auth", Summary: "Decided on JWT.", Decisions: []string{"Use JWT"}},
		{Topic: "DB", Summary: "Chose PostgreSQL.", Decisions: []string{"Use JSONB"}},
	}
	plan := memory.CompactionPlan{
		TurnsToArchive: turns,
		ArchiveSummary: "Previous session about API basics.",
	}
	result, err := compactor.Compact(context.Background(), plan)
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}
	if result.UpdatedArchive == "" {
		t.Error("expected non-empty UpdatedArchive")
	}
	if !strings.Contains(result.UpdatedArchive, "JWT") {
		t.Errorf("UpdatedArchive should mention JWT, got: %q", result.UpdatedArchive)
	}
}

// --- Test: TokensSaved is positive after compression ---

func TestSessionCompactor_TokensSaved(t *testing.T) {
	turnJSON := `[{"topic":"Auth","summary":"Decided on JWT.","decisions":null,"facts":null}]`
	router := &mockRouter{provider: &mockProvider{response: turnJSON}}
	compactor := memory.NewSessionCompactor(router)

	msgs := []aitypes.Message{
		aitypes.NewTextMessage("user", strings.Repeat("What auth should we use for our application? ", 10)),
		aitypes.NewTextMessage("assistant", strings.Repeat("I recommend using JWT with refresh tokens. ", 10)),
	}
	plan := memory.CompactionPlan{MessagesToCompress: msgs}
	result, err := compactor.Compact(context.Background(), plan)
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}
	if result.TokensSaved <= 0 {
		t.Errorf("TokensSaved = %d, want positive", result.TokensSaved)
	}
}

// --- Test: Compact with context cancellation ---

func TestSessionCompactor_ContextCancelled(t *testing.T) {
	router := &mockRouter{provider: &mockProvider{response: `[]`}}
	compactor := memory.NewSessionCompactor(router)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	msgs := []aitypes.Message{aitypes.NewTextMessage("user", "hello")}
	plan := memory.CompactionPlan{MessagesToCompress: msgs}
	_, err := compactor.Compact(ctx, plan)
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

// --- Test: invalid JSON from LLM ---

func TestSessionCompactor_InvalidLLMResponse(t *testing.T) {
	router := &mockRouter{provider: &mockProvider{response: "not valid json at all"}}
	compactor := memory.NewSessionCompactor(router)

	msgs := []aitypes.Message{aitypes.NewTextMessage("user", "hello")}
	plan := memory.CompactionPlan{MessagesToCompress: msgs}
	_, err := compactor.Compact(context.Background(), plan)
	if err == nil {
		t.Error("expected error from invalid JSON response")
	}
}

// --- Test: JSON response parsing ---

func TestSessionCompactor_JSONResponseParsing(t *testing.T) {
	turnJSON := `[{"topic":"API Design","summary":"Chose REST over GraphQL.","decisions":["Use REST","Version with /v1/"],"facts":["Team has REST experience"]}]`
	router := &mockRouter{provider: &mockProvider{response: turnJSON}}
	compactor := memory.NewSessionCompactor(router)

	msgs := []aitypes.Message{
		aitypes.NewTextMessage("user", "Should we use REST or GraphQL?"),
		aitypes.NewTextMessage("assistant", "REST is better for our use case."),
	}
	plan := memory.CompactionPlan{MessagesToCompress: msgs}
	result, err := compactor.Compact(context.Background(), plan)
	if err != nil {
		t.Fatalf("Compact() error: %v", err)
	}

	if len(result.NewTurnSummaries) != 1 {
		t.Fatalf("expected 1, got %d", len(result.NewTurnSummaries))
	}
	ts := result.NewTurnSummaries[0]
	if ts.Topic != "API Design" {
		t.Errorf("Topic = %q", ts.Topic)
	}
	if len(ts.Decisions) != 2 {
		t.Errorf("Decisions = %v", ts.Decisions)
	}
	if len(ts.Facts) != 1 {
		t.Errorf("Facts = %v", ts.Facts)
	}

	b, _ := json.Marshal(ts)
	if !strings.Contains(string(b), "API Design") {
		t.Errorf("JSON round-trip failed: %s", string(b))
	}
}
