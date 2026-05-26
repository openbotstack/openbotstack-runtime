package harness

import (
	"context"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

// --- State & Config Tests ---

func TestDefaultHarnessConfig(t *testing.T) {
	cfg := DefaultHarnessConfig()
	if cfg.MaxSteps != 10 {
		t.Errorf("MaxSteps = %d, want 10", cfg.MaxSteps)
	}
	if cfg.MaxSessionRuntime != 600*time.Second {
		t.Errorf("MaxSessionRuntime = %v, want 600s", cfg.MaxSessionRuntime)
	}
	if cfg.MaxParallelSubs != 3 {
		t.Errorf("MaxParallelSubs = %d, want 3", cfg.MaxParallelSubs)
	}
}

func TestDefaultReasoningLoopConfig(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	if cfg.MaxTurns != 5 {
		t.Errorf("MaxTurns = %d, want 5", cfg.MaxTurns)
	}
	if cfg.MaxToolCalls != 10 {
		t.Errorf("MaxToolCalls = %d, want 10", cfg.MaxToolCalls)
	}
	if cfg.MaxTurnRuntime != 180*time.Second {
		t.Errorf("MaxTurnRuntime = %v, want 180s", cfg.MaxTurnRuntime)
	}
}

func TestPermissionConfig_IsAllowed(t *testing.T) {
	tests := []struct {
		name   string
		config *execution.PermissionConfig
		tool   string
		want   bool
	}{
		{"nil config allows all", nil, "anything", true},
		{"deny list blocks", &execution.PermissionConfig{DeniedTools: map[string]bool{"bad": true}}, "bad", false},
		{"deny list allows others", &execution.PermissionConfig{DeniedTools: map[string]bool{"bad": true}}, "good", true},
		{"allow list permits", &execution.PermissionConfig{AllowedTools: map[string]bool{"good": true}}, "good", true},
		{"allow list blocks others", &execution.PermissionConfig{AllowedTools: map[string]bool{"good": true}}, "bad", false},
		{"deny mode blocks all", &execution.PermissionConfig{ApprovalMode: execution.ApprovalModeDeny}, "anything", false},
		{"auto mode allows", &execution.PermissionConfig{ApprovalMode: execution.ApprovalModeAuto}, "anything", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := tt.config.IsAllowed(tt.tool)
			if got != tt.want {
				t.Errorf("IsAllowed(%q) = %v, want %v", tt.tool, got, tt.want)
			}
		})
	}
}

func TestHarness_Run_NilPlan(t *testing.T) {
	h := NewExecutionHarness(DefaultHarnessConfig(), nil, nil, HarnessDeps{})
	ec := execution.NewExecutionContext(context.Background(), "req", "asst", "sess", "tenant", "user")
	_, err := h.Run(context.Background(), nil, ec)
	if err == nil {
		t.Fatal("expected error for nil plan")
	}
}

func TestHarness_Run_NilEC(t *testing.T) {
	h := NewExecutionHarness(DefaultHarnessConfig(), nil, nil, HarnessDeps{})
	_, err := h.Run(context.Background(), &execution.ExecutionPlan{}, nil)
	if err == nil {
		t.Fatal("expected error for nil ExecutionContext")
	}
}

func TestHarness_Run_UnfrozenPlan(t *testing.T) {
	h := NewExecutionHarness(DefaultHarnessConfig(), nil, nil, HarnessDeps{})
	ec := execution.NewExecutionContext(context.Background(), "req", "asst", "sess", "tenant", "user")
	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{{Name: "test", Type: execution.StepTypeTool}},
	}
	_, err := h.Run(context.Background(), plan, ec)
	if err == nil {
		t.Fatal("expected error for unfrozen plan")
	}
}

func TestDefaultRetryPolicy(t *testing.T) {
	p := execution.DefaultRetryPolicy()
	if p.MaxRetries != 2 {
		t.Errorf("MaxRetries = %d, want 2", p.MaxRetries)
	}
	if !p.FailFast {
		t.Error("FailFast should be true")
	}
	if p.InitialBackoff != 500*time.Millisecond {
		t.Errorf("InitialBackoff = %v, want 500ms", p.InitialBackoff)
	}
	if p.MaxBackoff != 5*time.Second {
		t.Errorf("MaxBackoff = %v, want 5s", p.MaxBackoff)
	}
}

// --- Hook Tests ---

func TestHookManager_PreStepExecute_Allow(t *testing.T) {
	hm := NewHookManager()
	hm.RegisterPreStepExecute(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		return &execution.HookResult{}, nil
	})
	result, err := hm.PreStepExecute(context.Background(), &execution.HookContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Deny {
		t.Error("should not be denied")
	}
}

func TestHookManager_PreStepExecute_Deny(t *testing.T) {
	hm := NewHookManager()
	hm.RegisterPreStepExecute(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		return &execution.HookResult{Deny: true, Reason: "forbidden"}, nil
	})
	result, err := hm.PreStepExecute(context.Background(), &execution.HookContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Deny {
		t.Error("should be denied")
	}
}

func TestHookManager_FirstDenyStops(t *testing.T) {
	hm := NewHookManager()
	called := 0
	hm.RegisterPreStepExecute(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		called++
		return &execution.HookResult{Deny: true}, nil
	})
	hm.RegisterPreStepExecute(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		called++
		return &execution.HookResult{}, nil
	})
	result, _ := hm.PreStepExecute(context.Background(), &execution.HookContext{})
	if !result.Deny {
		t.Error("should be denied by first hook")
	}
	if called != 1 {
		t.Errorf("expected 1 hook called, got %d", called)
	}
}

// --- FailureHandler Tests ---

func TestFailureHandler_Backoff(t *testing.T) {
	fh := NewFailureHandler(execution.DefaultRetryPolicy())
	policy := execution.DefaultRetryPolicy()

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 500 * time.Millisecond},
		{2, 1 * time.Second},
		{3, 2 * time.Second},
		{4, 4 * time.Second},
		{5, 5 * time.Second}, // capped at MaxBackoff
	}
	for _, tt := range tests {
		got := fh.backoff(tt.attempt, policy)
		if got != tt.want {
			t.Errorf("backoff(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

// --- PermissionChecker Tests ---

func TestPermissionChecker_NilAllowsAll(t *testing.T) {
	pc := NewPermissionChecker(nil, nil)
	if err := pc.Check(context.Background(), "any-tool", "tenant"); err != nil {
		t.Errorf("nil checker should allow all: %v", err)
	}
}

func TestPermissionChecker_DenyList(t *testing.T) {
	pc := NewPermissionChecker(&execution.PermissionConfig{
		DeniedTools: map[string]bool{"dangerous": true},
	}, nil)
	if err := pc.Check(context.Background(), "safe", "tenant"); err != nil {
		t.Errorf("safe tool should be allowed: %v", err)
	}
	if err := pc.Check(context.Background(), "dangerous", "tenant"); err == nil {
		t.Error("dangerous tool should be denied")
	}
}
