package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/openbotstack/openbotstack-core/control/skills"
	registry "github.com/openbotstack/openbotstack-core/registry/skills"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
)

// loadSkills scans the directory and loads skills into the executor.
func loadSkills(ctx context.Context, exec *executor.DefaultExecutor, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No skills directory, ignore
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillDir := filepath.Join(dir, entry.Name())
		manifestPath := filepath.Join(skillDir, "manifest.yaml")
		wasmPath := filepath.Join(skillDir, "main.wasm")

		// Read manifest
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			fmt.Printf("Skipping %s: failed to read manifest: %v\n", entry.Name(), err)
			continue
		}

		m, err := registry.ParseManifest(data)
		if err != nil {
			fmt.Printf("Skipping %s: invalid manifest: %v\n", entry.Name(), err)
			continue
		}

		// Read Wasm if exists
		var wasmBytes []byte
		if _, err := os.Stat(wasmPath); err == nil {
			wasmBytes, err = os.ReadFile(wasmPath)
			if err != nil {
				fmt.Printf("Skipping %s: failed to read wasm: %v\n", entry.Name(), err)
				continue
			}
		}

		// Create skill adapter
		s := &simpleSkill{
			id:          m.ID,
			name:        m.Name,
			description: m.Description,
		}

		// Load into executor
		if len(wasmBytes) > 0 {
			err = exec.LoadSkillWithWasm(ctx, s, wasmBytes)
		} else {
			err = exec.LoadSkill(ctx, s)
		}

		if err != nil {
			fmt.Printf("Failed to load skill %s: %v\n", m.ID, err)
		} else {
			fmt.Printf("Loaded skill: %s (%s)\n", m.ID, m.Name)
		}
	}
	return nil
}

// simpleSkill adapts manifest data to Skill interface
type simpleSkill struct {
	id          string
	name        string
	description string
}

func (s *simpleSkill) ID() string                      { return s.id }
func (s *simpleSkill) Name() string                    { return s.name }
func (s *simpleSkill) Description() string             { return s.description }
func (s *simpleSkill) Timeout() time.Duration          { return 30 * time.Second }
func (s *simpleSkill) InputSchema() *skills.JSONSchema  { return nil }
func (s *simpleSkill) OutputSchema() *skills.JSONSchema { return nil }
func (s *simpleSkill) RequiredPermissions() []string   { return nil }
func (s *simpleSkill) Validate() error                 { return nil }
