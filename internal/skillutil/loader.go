// Package skillutil holds runtime-side skill plumbing: disk loading,
// the in-process Skill implementation, and skill-type classification.
package skillutil

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/openbotstack/openbotstack-core/ai/types"
	registry "github.com/openbotstack/openbotstack-core/registry/skills"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
)

// LoadSkills scans the directory and loads skills into the executor.
// SKILL.md is the primary file; manifest.yaml is optional.
func LoadSkills(ctx context.Context, exec *executor.DefaultExecutor, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, entry.Name())
		if _, err := LoadSkillFromDir(ctx, exec, skillDir); err != nil {
			slog.Warn("failed to load skill", "dir", skillDir, "error", err)
		}
	}
	return nil
}

// LoadSkillFromDir loads a single skill from its directory into the executor.
// Returns the skill ID on success.
func LoadSkillFromDir(ctx context.Context, exec *executor.DefaultExecutor, skillDir string) (string, error) {
	entryName := filepath.Base(skillDir)

	smd, err := registry.ParseSkillMD(skillDir)
	if err != nil {
		return "", fmt.Errorf("failed to read SKILL.md: %w", err)
	}
	if smd == nil {
		return "", fmt.Errorf("no SKILL.md found")
	}

	skillID := registry.DeriveSkillID(skillDir)

	var m *registry.SkillManifest
	manifestPath := filepath.Join(skillDir, "manifest.yaml")
	if data, err := os.ReadFile(manifestPath); err == nil {
		m, err = registry.ParseManifest(data)
		if err != nil {
			return "", fmt.Errorf("invalid manifest: %w", err)
		}
	}

	name := smd.Name
	if name == "" && m != nil {
		name = m.Name
	}
	if name == "" {
		name = entryName
	}

	description := smd.Description
	if description == "" && m != nil {
		description = m.Description
	}

	var version string
	var inputSchema, outputSchema *types.JSONSchema
	var timeout time.Duration
	var permissions []string
	var executionMode string

	if m != nil {
		version = m.Version
		inputSchema = m.InputSchema
		outputSchema = m.OutputSchema
		if m.Timeout > 0 {
			timeout = m.Timeout
		}
		permissions = m.Permissions
		executionMode = m.Execution.Mode
	}

	wasmPath := filepath.Join(skillDir, "main.wasm")
	var wasmBytes []byte
	if info, err := os.Stat(wasmPath); err == nil && !info.IsDir() && info.Size() > 0 {
		wasmBytes, err = os.ReadFile(wasmPath)
		if err != nil {
			return "", fmt.Errorf("failed to read wasm: %w", err)
		}
	}

	resolvedMode := executionMode
	if resolvedMode == "" {
		resolvedMode = "declarative"
	}
	if resolvedMode == "declarative" && smd.Body == "" {
		slog.Warn("declarative skill has empty prompt body", "skill_id", skillID)
	}

	s := &simpleSkill{
		id:            skillID,
		name:          name,
		description:   description,
		version:       version,
		inputSchema:   inputSchema,
		outputSchema:  outputSchema,
		timeout:       timeout,
		permissions:   permissions,
		executionMode: executionMode,
		prompt:        smd.Body,
	}

	// Unload first to allow idempotent reload
	_ = exec.UnloadSkill(ctx, skillID)

	if len(wasmBytes) > 0 {
		err = exec.LoadSkillWithWasm(ctx, s, wasmBytes)
	} else {
		err = exec.LoadSkill(ctx, s)
	}

	if err != nil {
		// ErrSkillAlreadyLoaded means a concurrent reload won — not an error
		if err.Error() == "executor: skill already loaded" {
			slog.Debug("skill reload: concurrent load won, skipping", "id", skillID)
		} else {
			return "", fmt.Errorf("executor rejected skill: %w", err)
		}
	}

	hasManifest := "no-manifest"
	if m != nil {
		hasManifest = "has-manifest"
	}
	slog.Info("loaded skill", "id", skillID, "name", name, "mode", executionMode, "source", hasManifest)
	return skillID, nil
}

// UnloadSkillByDir unloads a skill identified by its directory path.
func UnloadSkillByDir(ctx context.Context, exec *executor.DefaultExecutor, skillDir string) error {
	skillID := registry.DeriveSkillID(skillDir)
	if err := exec.UnloadSkill(ctx, skillID); err != nil {
		return fmt.Errorf("failed to unload skill %s: %w", skillID, err)
	}
	slog.Info("unloaded skill", "id", skillID)
	return nil
}

// simpleSkill adapts parsed data to the Skill interface.
type simpleSkill struct {
	id            string
	name          string
	description   string
	version       string
	inputSchema   *types.JSONSchema
	outputSchema  *types.JSONSchema
	timeout       time.Duration
	permissions   []string
	executionMode string
	prompt        string
}

// Compile-time interface checks
var _ registry.PromptProvider = (*simpleSkill)(nil)
var _ registry.ExecutionModeProvider = (*simpleSkill)(nil)

func (s *simpleSkill) ID() string                      { return s.id }
func (s *simpleSkill) Name() string                    { return s.name }
func (s *simpleSkill) Description() string             { return s.description }
func (s *simpleSkill) Version() string                 { return s.version }
func (s *simpleSkill) Prompt() string                  { return s.prompt }
func (s *simpleSkill) Timeout() time.Duration {
	if s.timeout > 0 {
		return s.timeout
	}
	return 30 * time.Second
}
func (s *simpleSkill) InputSchema() *types.JSONSchema  { return s.inputSchema }
func (s *simpleSkill) OutputSchema() *types.JSONSchema { return s.outputSchema }
func (s *simpleSkill) RequiredPermissions() []string    { return s.permissions }
func (s *simpleSkill) Validate() error                  { return nil }
func (s *simpleSkill) ExecutionMode() string            { return s.executionMode }
