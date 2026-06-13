package server

import (
	"context"
	"testing"

	"github.com/openbotstack/openbotstack-core/capability"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
)

// TestCapabilityRegistrar_RegisterBuiltins pins the builtin-registration path
// as it is hoisted out of package main: every builtin tool is wrapped as a
// capability and the runner is returned for further wiring.
func TestCapabilityRegistrar_RegisterBuiltins(t *testing.T) {
	reg := capability.NewMemoryCapabilityRegistry()
	exec := executor.NewDefaultExecutor()
	r := NewCapabilityRegistrar(reg, exec)

	runner := r.RegisterBuiltins()
	if runner == nil {
		t.Fatal("RegisterBuiltins returned nil runner")
	}
	if tools := runner.Tools(); len(tools) == 0 {
		t.Error("builtin runner has no tools")
	}
	if caps := reg.List(); len(caps) == 0 {
		t.Error("RegisterBuiltins registered no capabilities")
	}
}

// TestCapabilityRegistrar_RegisterSkills pins that skills already loaded into
// the executor are surfaced as capabilities.
func TestCapabilityRegistrar_RegisterSkills(t *testing.T) {
	reg := capability.NewMemoryCapabilityRegistry()
	exec := executor.NewDefaultExecutor()
	// No skills loaded → RegisterSkills registers nothing but must not error.
	r := NewCapabilityRegistrar(reg, exec)
	r.RegisterSkills(context.Background())
	if got := len(reg.List()); got != 0 {
		t.Errorf("expected 0 capabilities for empty executor, got %d", got)
	}
}
