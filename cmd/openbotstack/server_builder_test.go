package main

import (
	"testing"

	"github.com/openbotstack/openbotstack-runtime/config"
	"github.com/openbotstack/openbotstack-runtime/persistence"
)

func TestServerBuilder_RequireInit_Infrastructure(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when calling InitAI without InitInfrastructure")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
		if msg != "ServerBuilder: InitAI requires InitInfrastructure() to run first" {
			t.Errorf("unexpected panic message: %s", msg)
		}
	}()
	b := NewServerBuilder()
	b.InitAI() // should panic: cfg is nil
}

func TestServerBuilder_RequireInit_Execution(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when calling InitExecution without InitAI")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
		// Should complain about modelRouter (set by InitAI)
		if msg != "ServerBuilder: InitExecution requires InitAI() to run first" {
			t.Errorf("unexpected panic message: %s", msg)
		}
	}()
	// Can't easily test partial init without real infrastructure,
	// so test the requireInit function directly
	b := NewServerBuilder()
	b.requireInit("modelRouter", "InitExecution") // should panic
}

func TestServerBuilder_RequireInit_Capabilities(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic when calling InitCapabilities without InitExecution")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("expected string panic, got %T: %v", r, r)
		}
		if msg != "ServerBuilder: InitCapabilities requires InitExecution() to run first" {
			t.Errorf("unexpected panic message: %s", msg)
		}
	}()
	b := NewServerBuilder()
	b.requireInit("exec", "InitCapabilities")
}

func TestServerBuilder_RequireInit_NoPanicWhenSet(t *testing.T) {
	b := NewServerBuilder()
	// Simulate infrastructure init
	b.cfg = &config.Config{}
	b.pdb = &persistence.DB{}

	// These should NOT panic
	b.requireInit("cfg", "test")
	b.requireInit("pdb", "test")
}
