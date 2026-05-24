package harness

import (
	"context"
	"fmt"
	"sync"

	"github.com/openbotstack/openbotstack-core/execution"
)

// HookManager runs hooks deterministically outside the LLM context.
type HookManager struct {
	mu             sync.RWMutex
	preStepHooks  []execution.PreStepExecuteHook
	postStepHooks []execution.PostStepExecuteHook
	preToolHooks  []execution.PreToolUseHook
	postToolHooks []execution.PostToolUseHook
	onStopHooks   []execution.OnStopHook
}

// NewHookManager creates an empty hook manager.
func NewHookManager() *HookManager {
	return &HookManager{}
}

// RegisterPreStepExecute adds a pre-step execution hook.
func (hm *HookManager) RegisterPreStepExecute(h execution.PreStepExecuteHook) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.preStepHooks = append(hm.preStepHooks, h)
}

// RegisterPostStepExecute adds a post-step execution hook.
func (hm *HookManager) RegisterPostStepExecute(h execution.PostStepExecuteHook) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.postStepHooks = append(hm.postStepHooks, h)
}

// RegisterPreToolUse adds a pre-tool-use hook.
func (hm *HookManager) RegisterPreToolUse(h execution.PreToolUseHook) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.preToolHooks = append(hm.preToolHooks, h)
}

// RegisterPostToolUse adds a post-tool-use hook.
func (hm *HookManager) RegisterPostToolUse(h execution.PostToolUseHook) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.postToolHooks = append(hm.postToolHooks, h)
}

// RegisterOnStop adds an on-stop hook.
func (hm *HookManager) RegisterOnStop(h execution.OnStopHook) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.onStopHooks = append(hm.onStopHooks, h)
}

// PreStepExecute runs all pre-step hooks. Returns denied if any hook denies.
func (hm *HookManager) PreStepExecute(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
	hm.mu.RLock()
	hooks := hm.preStepHooks
	hm.mu.RUnlock()

	for _, h := range hooks {
		result, err := h(ctx, hctx)
		if err != nil {
			return nil, fmt.Errorf("pre-step hook error: %w", err)
		}
		if result != nil && result.Deny {
			return result, nil
		}
	}
	return &execution.HookResult{}, nil
}

// PostStepExecute runs all post-step hooks.
func (hm *HookManager) PostStepExecute(ctx context.Context, hctx *execution.HookContext) error {
	hm.mu.RLock()
	hooks := hm.postStepHooks
	hm.mu.RUnlock()

	for _, h := range hooks {
		if err := h(ctx, hctx); err != nil {
			return fmt.Errorf("post-step hook error: %w", err)
		}
	}
	return nil
}

// PreToolUse runs all pre-tool hooks. Returns denied if any hook denies.
func (hm *HookManager) PreToolUse(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
	hm.mu.RLock()
	hooks := hm.preToolHooks
	hm.mu.RUnlock()

	for _, h := range hooks {
		result, err := h(ctx, hctx)
		if err != nil {
			return nil, fmt.Errorf("pre-tool hook error: %w", err)
		}
		if result != nil && result.Deny {
			return result, nil
		}
	}
	return &execution.HookResult{}, nil
}

// PostToolUse runs all post-tool hooks.
func (hm *HookManager) PostToolUse(ctx context.Context, hctx *execution.HookContext) error {
	hm.mu.RLock()
	hooks := hm.postToolHooks
	hm.mu.RUnlock()

	for _, h := range hooks {
		if err := h(ctx, hctx); err != nil {
			return fmt.Errorf("post-tool hook error: %w", err)
		}
	}
	return nil
}

// OnStop runs all on-stop hooks. Hooks cannot deny stop.
func (hm *HookManager) OnStop(ctx context.Context, hctx *execution.HookContext) {
	hm.mu.RLock()
	hooks := hm.onStopHooks
	hm.mu.RUnlock()

	for _, h := range hooks {
		h(ctx, hctx)
	}
}
