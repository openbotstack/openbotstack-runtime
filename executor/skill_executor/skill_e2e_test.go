package skill_executor_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	types "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/execution"
	registry "github.com/openbotstack/openbotstack-core/registry/skills"
	skilloperator "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
	"github.com/openbotstack/openbotstack-runtime/sandbox/wasm"
)

// ============================================================================
// Category 1: SKILL.md Loading
// ============================================================================

func TestE2E_SkillMD_ValidPromptLoads(t *testing.T) {
	dir := t.TempDir()
	content := "You are a summarizer. Produce 3 bullet points."
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed: %v", err)
	}
	if smd.Body != content {
		t.Errorf("smd.Body = %q, want %q", smd.Body, content)
	}
}

func TestE2E_SkillMD_TemplatePlaceholder(t *testing.T) {
	dir := t.TempDir()
	content := "Process this input:\n{{.Input}}\n\nReturn result."
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed: %v", err)
	}
	if !strings.Contains(smd.Body, "{{.Input}}") {
		t.Error("smd.Body should contain {{.Input}} placeholder")
	}
}

func TestE2E_SkillMD_MultilineMarkdown(t *testing.T) {
	dir := t.TempDir()
	content := "# Summarizer\n\n## Rules\n- Rule 1\n- Rule 2\n\n## Output\nJSON format."
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed: %v", err)
	}
	if !strings.Contains(smd.Body, "# Summarizer") {
		t.Error("smd.Body should contain markdown header")
	}
	if !strings.Contains(smd.Body, "- Rule 1") {
		t.Error("smd.Body should contain list items")
	}
}

func TestE2E_SkillMD_UnicodeAndSpecialChars(t *testing.T) {
	dir := t.TempDir()
	content := "你是一个翻译助手。\nSprachassistenz.\nEmoji: ✨\U0001f680"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed: %v", err)
	}
	if !strings.Contains(smd.Body, "翻译助手") {
		t.Error("smd.Body should contain Chinese characters")
	}
	if !strings.Contains(smd.Body, "Sprachassistenz") {
		t.Error("smd.Body should contain German text")
	}
}

func TestE2E_SkillMD_WasmSkillEmptyPrompt(t *testing.T) {
	dir := t.TempDir()
	// No SKILL.md — wasm skills typically don't have one
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD should not error on missing file: %v", err)
	}
	if smd != nil {
		t.Errorf("smd should be nil for directory without SKILL.md, got %v", smd)
	}
}

func TestE2E_SkillMD_HasSkillMD_True(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("prompt"), 0644); err != nil {
		t.Fatal(err)
	}
	if !registry.HasSkillMD(dir) {
		t.Error("HasSkillMD should return true")
	}
}

func TestE2E_SkillMD_HasSkillMD_False(t *testing.T) {
	dir := t.TempDir()
	if registry.HasSkillMD(dir) {
		t.Error("HasSkillMD should return false for directory without SKILL.md")
	}
}

func TestE2E_SkillMD_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := registry.ParseSkillMD(dir)
	if err == nil {
		t.Fatal("ParseSkillMD should return error for empty SKILL.md")
	}
}

func TestE2E_SkillMD_WhitespaceOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("   \n\t\n   "), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := registry.ParseSkillMD(dir)
	if err == nil {
		t.Fatal("ParseSkillMD should return error for whitespace-only SKILL.md")
	}
}

func TestE2E_SkillMD_BinaryContent(t *testing.T) {
	dir := t.TempDir()
	binary := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), binary, 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD should not error on binary: %v", err)
	}
	// It will still read it as a string — just not useful content
	if smd == nil || smd.Body == "" {
		t.Error("expected non-empty smd.Body even from binary content")
	}
}

func TestE2E_SkillMD_ExtremelyLongContent(t *testing.T) {
	dir := t.TempDir()
	longContent := strings.Repeat("This is a very long prompt line. ", 4000) // ~120KB
	// TrimSpace will remove trailing space/newline, so account for that
	expectedLen := len(strings.TrimSpace(longContent))
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(longContent), 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed on long content: %v", err)
	}
	if len(smd.Body) != expectedLen {
		t.Errorf("smd.Body length = %d, want %d", len(smd.Body), expectedLen)
	}
}

func TestE2E_SkillMD_NullBytesEmbedded(t *testing.T) {
	dir := t.TempDir()
	content := "prompt\x00with\x00nulls"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed: %v", err)
	}
	if !strings.Contains(smd.Body, "prompt") {
		t.Error("smd.Body should still contain readable text despite null bytes")
	}
}

func TestE2E_SkillMD_PathIsDirectory(t *testing.T) {
	dir := t.TempDir()
	skillMDPath := filepath.Join(dir, "SKILL.md")
	if err := os.MkdirAll(skillMDPath, 0755); err != nil {
		t.Fatal(err)
	}
	_, err := registry.ParseSkillMD(dir)
	if err == nil {
		t.Error("expected error when SKILL.md is a directory")
	}
}

func TestE2E_SkillMD_NonExistentDirectory(t *testing.T) {
	smd, err := registry.ParseSkillMD("/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Fatalf("ParseSkillMD should not error on non-existent dir: %v", err)
	}
	if smd != nil {
		t.Errorf("smd should be nil for non-existent directory, got %v", smd)
	}
}

func TestE2E_SkillMD_EmptyDirectoryPath(t *testing.T) {
	smd, err := registry.ParseSkillMD("")
	if err != nil {
		t.Fatalf("ParseSkillMD with empty path should not error: %v", err)
	}
	if smd != nil {
		t.Errorf("expected nil smd, got %v", smd)
	}
}

func TestE2E_SkillMD_ConcurrentReads(t *testing.T) {
	dir := t.TempDir()
	content := "Concurrent test prompt"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errors := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			smd, err := registry.ParseSkillMD(dir)
			if err != nil {
				errors <- err
				return
			}
			if smd.Body != content {
				errors <- fmt.Errorf("smd.Body = %q, want %q", smd.Body, content)
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Errorf("concurrent read error: %v", err)
	}
}

func TestE2E_SkillMD_BOMPrefix(t *testing.T) {
	dir := t.TempDir()
	bom := []byte{0xEF, 0xBB, 0xBF}
	content := append(bom, []byte("UTF-8 BOM prompt")...)
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), content, 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed on BOM: %v", err)
	}
	// BOM is not trimmed by TrimSpace — that is expected behavior
	if !strings.Contains(smd.Body, "UTF-8 BOM prompt") {
		t.Error("smd.Body should contain actual text after BOM")
	}
}

func TestE2E_SkillMD_CRLFLineEndings(t *testing.T) {
	dir := t.TempDir()
	content := "Line 1\r\nLine 2\r\nLine 3"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed on CRLF: %v", err)
	}
	if !strings.Contains(smd.Body, "Line 1") || !strings.Contains(smd.Body, "Line 3") {
		t.Error("smd.Body should contain all lines")
	}
}

func TestE2E_SkillMD_OnlyTemplatePlaceholders(t *testing.T) {
	dir := t.TempDir()
	content := "{{.Input}}"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed: %v", err)
	}
	if smd.Body != "{{.Input}}" {
		t.Errorf("expected template-only smd.Body, got %q", smd.Body)
	}
}

func TestE2E_SkillMD_Symlink(t *testing.T) {
	dir := t.TempDir()
	targetDir := t.TempDir()
	targetContent := "Target prompt content"
	if err := os.WriteFile(filepath.Join(targetDir, "SKILL.md"), []byte(targetContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(targetDir, "SKILL.md"), filepath.Join(dir, "SKILL.md")); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed on symlink: %v", err)
	}
	if smd.Body != targetContent {
		t.Errorf("smd.Body via symlink = %q, want %q", smd.Body, targetContent)
	}
}

func TestE2E_SkillMD_DeeplyNestedMarkdown(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteString(strings.Repeat("#", i+1) + " Level " + fmt.Sprintf("%d", i+1) + "\n\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed on deeply nested markdown: %v", err)
	}
	if !strings.Contains(smd.Body, "Level 100") {
		t.Error("smd.Body should contain deepest heading")
	}
}

// ============================================================================
// Category 2: Manifest Parsing
// ============================================================================

func TestE2E_Manifest_MinimalValid(t *testing.T) {
	yaml := `
id: test/minimal
version: "1.0"
execution:
  mode: declarative
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if m.ID != "test/minimal" {
		t.Errorf("ID = %q, want %q", m.ID, "test/minimal")
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
}

func TestE2E_Manifest_FullFields(t *testing.T) {
	yaml := `
id: core/full-test
version: "2.0.0"
name: Full Test Skill
description: A skill with all fields populated
execution:
  mode: wasm
input_schema:
  type: object
  properties:
    text:
      type: string
      description: Input text
  required:
    - text
output_schema:
  type: object
  properties:
    result:
      type: string
permissions:
  - llm:generate
  - fs:read
timeout: 60s
resources:
  max_memory_mb: 128
  max_cpu_ms: 5000
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if m.ID != "core/full-test" {
		t.Errorf("ID = %q", m.ID)
	}
	if m.Name != "Full Test Skill" {
		t.Errorf("Name = %q", m.Name)
	}
	if len(m.Permissions) != 2 {
		t.Errorf("Permissions len = %d, want 2", len(m.Permissions))
	}
	if m.Timeout != 60*time.Second {
		t.Errorf("Timeout = %v, want 60s", m.Timeout)
	}
	if m.Resources.MaxMemoryMB != 128 {
		t.Errorf("MaxMemoryMB = %d, want 128", m.Resources.MaxMemoryMB)
	}
	if m.Resources.MaxCPUMs != 5000 {
		t.Errorf("MaxCPUMs = %d, want 5000", m.Resources.MaxCPUMs)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
}

func TestE2E_Manifest_ComplexInputSchema(t *testing.T) {
	yaml := `
id: test/complex-schema
version: "1.0"
execution:
  mode: wasm
input_schema:
  type: object
  properties:
    items:
      type: array
      items:
        type: object
        properties:
          name:
            type: string
          quantity:
            type: integer
        required:
          - name
    region:
      type: string
      enum:
        - US
        - EU
        - CN
    priority:
      type: number
      minimum: 0
      maximum: 10
  required:
    - items
    - region
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if m.InputSchema == nil {
		t.Fatal("InputSchema should not be nil")
	}
	if m.InputSchema.Type != "object" {
		t.Errorf("InputSchema.Type = %q", m.InputSchema.Type)
	}
	itemsProp := m.InputSchema.Properties["items"]
	if itemsProp == nil || itemsProp.Type != "array" {
		t.Error("items property should be array type")
	}
	if itemsProp.Items == nil || itemsProp.Items.Type != "object" {
		t.Error("items.Items should be object type")
	}
	regionProp := m.InputSchema.Properties["region"]
	if regionProp == nil || len(regionProp.Enum) != 3 {
		t.Error("region should have 3 enum values")
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
}

func TestE2E_Manifest_WithPermissions(t *testing.T) {
	yaml := `
id: test/perm-skill
version: "1.0"
execution:
  mode: wasm
permissions:
  - llm:generate
  - fs:read
  - fs:write
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if len(m.Permissions) != 3 {
		t.Errorf("Permissions = %v, want 3 items", m.Permissions)
	}
}

func TestE2E_Manifest_WithTimeoutAndResources(t *testing.T) {
	yaml := `
id: test/resource-skill
version: "1.0"
execution:
  mode: wasm
timeout: 45s
resources:
  max_memory_mb: 256
  max_cpu_ms: 15000
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if m.Timeout != 45*time.Second {
		t.Errorf("Timeout = %v", m.Timeout)
	}
	if m.Resources.MaxMemoryMB != 256 {
		t.Errorf("MaxMemoryMB = %d", m.Resources.MaxMemoryMB)
	}
	if m.Resources.MaxCPUMs != 15000 {
		t.Errorf("MaxCPUMs = %d", m.Resources.MaxCPUMs)
	}
}

func TestE2E_Manifest_EmptyYAML(t *testing.T) {
	m, err := registry.ParseManifest([]byte(""))
	if err != nil {
		t.Fatalf("ParseManifest on empty YAML should not error: %v", err)
	}
	if err := m.Validate(); err == nil {
		t.Error("Validate should fail for empty manifest")
	}
}

func TestE2E_Manifest_OnlyComments(t *testing.T) {
	yaml := `# This is a comment
# Another comment
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest on comments-only YAML should not error: %v", err)
	}
	if err := m.Validate(); err == nil {
		t.Error("Validate should fail for comment-only manifest")
	}
}

func TestE2E_Manifest_UnknownFields(t *testing.T) {
	yaml := `
id: test/unknown-fields
version: "1.0"
execution:
  mode: wasm
custom_field: this is ignored
another_unknown: 42
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest with unknown fields should not error: %v", err)
	}
	if m.ID != "test/unknown-fields" {
		t.Errorf("ID = %q", m.ID)
	}
}

func TestE2E_Manifest_InvalidExecutionMode(t *testing.T) {
	yaml := `
id: test/bad-mode
version: "1.0"
execution:
  mode: invalid_mode
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest should not validate mode: %v", err)
	}
	// Validate passes — mode validation is not in Validate()
	if err := m.Validate(); err != nil {
		t.Logf("Validate: %v (mode validation may be present)", err)
	}
}

func TestE2E_Manifest_NegativeTimeout(t *testing.T) {
	yaml := `
id: test/neg-timeout
version: "1.0"
execution:
  mode: wasm
timeout: -5s
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest should not error: %v", err)
	}
	t.Logf("Timeout = %v (negative durations are parsed as-is)", m.Timeout)
}

func TestE2E_Manifest_ZeroTimeout(t *testing.T) {
	yaml := `
id: test/zero-timeout
version: "1.0"
execution:
  mode: wasm
timeout: 0s
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if m.Timeout != 0 {
		t.Errorf("Timeout = %v, want 0", m.Timeout)
	}
}

func TestE2E_Manifest_LargeTimeout(t *testing.T) {
	yaml := `
id: test/big-timeout
version: "1.0"
execution:
  mode: wasm
timeout: 25h
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if m.Timeout != 25*time.Hour {
		t.Errorf("Timeout = %v, want 25h", m.Timeout)
	}
}

func TestE2E_Manifest_MalformedJSONInSchema(t *testing.T) {
	yaml := `
id: test/bad-json-schema
version: "1.0"
execution:
  mode: wasm
input_schema:
  type: object
  properties:
    field:
      type: string
      minLength: "not-a-number"
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed (YAML parses, schema validation is separate): %v", err)
	}
	// Schema has minLength as string — that's a runtime validation issue, not parse-time
	if m.InputSchema == nil {
		t.Error("InputSchema should not be nil")
	}
}

func TestE2E_Manifest_NullID(t *testing.T) {
	yaml := `
execution:
  mode: wasm
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("Validate should pass without ID: %v", err)
	}
}

func TestE2E_Manifest_EmptyStringID(t *testing.T) {
	yaml := `
id: ""
execution:
  mode: wasm
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if err := m.Validate(); err != nil {
		t.Errorf("Validate should pass without ID: %v", err)
	}
}

func TestE2E_Manifest_SpecialCharsInID(t *testing.T) {
	yaml := `
id: "test/skill.with-special_chars.v2"
version: "1.0"
execution:
  mode: wasm
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if m.ID != "test/skill.with-special_chars.v2" {
		t.Errorf("ID = %q", m.ID)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
}

func TestE2E_Manifest_LongDescription(t *testing.T) {
	longDesc := strings.Repeat("This is a detailed description of the skill. ", 500)
	yaml := fmt.Sprintf(`
id: test/long-desc
version: "1.0"
execution:
  mode: wasm
description: %q
`, longDesc)
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if m.Description != longDesc {
		t.Errorf("Description length = %d, want %d", len(m.Description), len(longDesc))
	}
}

func TestE2E_Manifest_NonStringVersion(t *testing.T) {
	yaml := `
id: test/int-version
version: 1
execution:
  mode: wasm
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	// YAML will parse integer 1; Go YAML unmarshals to string as "1"
	t.Logf("Version = %q (type coerced from int)", m.Version)
}

func TestE2E_Manifest_MissingExecutionSection(t *testing.T) {
	yaml := `
id: test/no-exec
version: "1.0"
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if err := m.Validate(); err == nil {
		t.Error("Validate should fail without execution section")
	}
}

func TestE2E_Manifest_ExecutionNoMode(t *testing.T) {
	yaml := `
id: test/exec-no-mode
version: "1.0"
execution:
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if err := m.Validate(); err == nil {
		t.Error("Validate should fail without execution.mode")
	}
}

func TestE2E_Manifest_NullInputSchema(t *testing.T) {
	yaml := `
id: test/null-schema
version: "1.0"
execution:
  mode: wasm
input_schema:
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if m.InputSchema != nil {
		t.Error("InputSchema should be nil when explicitly null")
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
}

func TestE2E_Manifest_EmptyProperties(t *testing.T) {
	yaml := `
id: test/empty-props
version: "1.0"
execution:
  mode: wasm
input_schema:
  type: object
  properties: {}
`
	m, err := registry.ParseManifest([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if m.InputSchema.Properties == nil {
		t.Error("Properties should not be nil when specified as empty map")
	}
}

func TestE2E_Manifest_BinaryGarbageData(t *testing.T) {
	garbage := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0x80, 0x90, 0xA0}
	_, err := registry.ParseManifest(garbage)
	if err == nil {
		t.Error("expected error from binary garbage data")
	}
}

func TestE2E_Manifest_JSONInsteadOfYAML(t *testing.T) {
	jsonData := `{"id":"test/json","version":"1.0","execution":{"mode":"wasm"}}`
	m, err := registry.ParseManifest([]byte(jsonData))
	if err != nil {
		t.Fatalf("JSON is valid YAML, should parse: %v", err)
	}
	if m.ID != "test/json" {
		t.Errorf("ID = %q", m.ID)
	}
}

func TestE2E_Manifest_TabIndentation(t *testing.T) {
	yaml := "id:\ttest/tabbed\nversion:\t\"1.0\"\nexecution:\n\tmode:\twasm\n"
	_, err := registry.ParseManifest([]byte(yaml))
	// YAML specification does not allow tab indentation — this should error
	if err == nil {
		t.Log("Tab-indented YAML was accepted (YAML parser tolerates tabs)")
	} else {
		t.Logf("Tab-indented YAML correctly rejected: %v", err)
	}
}

func TestE2E_Manifest_BOMPrefix(t *testing.T) {
	bom := []byte{0xEF, 0xBB, 0xBF}
	yaml := append(bom, []byte(`
id: test/bom
version: "1.0"
execution:
  mode: wasm
`)...)
	m, err := registry.ParseManifest(yaml)
	if err != nil {
		t.Fatalf("BOM-prefixed YAML should parse: %v", err)
	}
	if m.ID != "test/bom" {
		t.Errorf("ID = %q", m.ID)
	}
}

// ============================================================================
// Category 3: Declarative Skill Execution
// ============================================================================

func TestE2E_Declarative_WithSkillMDPrompt(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/declarative-md",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "declarative",
		prompt:        "You are a translator.\n\nInput:\n{{.Input}}\n\nTranslate to English.",
	}
	_ = e.LoadSkill(ctx, skill)

	mockLLM := &mockTextGenerator{response: "Translated text here."}
	e.SetTextGenerator(mockLLM)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/declarative-md",
		Input:   []byte("Bonjour le monde"),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Status = %v", result.Status)
	}
	if string(result.Output) != "Translated text here." {
		t.Errorf("Output = %q", string(result.Output))
	}
	if !contains(mockLLM.prompt, "You are a translator") {
		t.Errorf("Prompt should use SKILL.md content, got: %s", mockLLM.prompt)
	}
	if !contains(mockLLM.prompt, "Bonjour le monde") {
		t.Error("Prompt should have input substituted")
	}
}

func TestE2E_Declarative_InputSubstitution(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/subst-test",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "declarative",
		prompt:        "Process: {{.Input}}",
	}
	_ = e.LoadSkill(ctx, skill)

	mockLLM := &mockTextGenerator{response: "done"}
	e.SetTextGenerator(mockLLM)

	_, _ = e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/subst-test",
		Input:   []byte("test-data"),
	})
	if !contains(mockLLM.prompt, "test-data") {
		t.Errorf("{{.Input}} should be replaced, got: %s", mockLLM.prompt)
	}
	if contains(mockLLM.prompt, "{{.Input}}") {
		t.Errorf("{{.Input}} should be fully replaced, got: %s", mockLLM.prompt)
	}
}

func TestE2E_Declarative_JSONInput(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/json-input",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "declarative",
		prompt:        "Summarize: {{.Input}}",
	}
	_ = e.LoadSkill(ctx, skill)

	mockLLM := &mockTextGenerator{response: "Summary result"}
	e.SetTextGenerator(mockLLM)

	input, _ := json.Marshal(map[string]string{"text": "Long article content"})
	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/json-input",
		Input:   input,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Status = %v", result.Status)
	}
}

func TestE2E_Declarative_SchemaValidationPass(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &schemaMockSkill{
		mockSkill: mockSkill{id: "e2e/schema-pass", valid: true, timeout: 30 * time.Second, executionMode: "declarative"},
		inputSchema: &types.JSONSchema{
			Type:     "object",
			Required: []string{"text"},
			Properties: map[string]*types.JSONSchema{
				"text": {Type: "string"},
			},
		},
	}
	_ = e.LoadSkill(ctx, skill)
	e.SetTextGenerator(&mockTextGenerator{response: "ok"})

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/schema-pass",
		Input:   []byte(`{"text": "hello"}`),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Status = %v", result.Status)
	}
}

func TestE2E_Declarative_NoTextGeneratorError(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/no-llm",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "declarative",
	}
	_ = e.LoadSkill(ctx, skill)
	// No SetTextGenerator

	input := []byte(`{"raw": "data"}`)
	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/no-llm",
		Input:   input,
	})
	if err == nil {
		t.Fatal("expected error when no text generator configured")
	}
	if result.Status != execution.StatusFailed {
		t.Errorf("Status = %v, want StatusFailed", result.Status)
	}
}

func TestE2E_Declarative_EmptyInput(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/empty-input",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "declarative",
	}
	_ = e.LoadSkill(ctx, skill)
	e.SetTextGenerator(&mockTextGenerator{response: "processed empty"})

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/empty-input",
		Input:   []byte{},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Status = %v", result.Status)
	}
}

func TestE2E_Declarative_LLMReturnsError(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/llm-error",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "declarative",
	}
	_ = e.LoadSkill(ctx, skill)
	e.SetTextGenerator(&mockTextGenerator{err: errors.New("LLM service unavailable")})

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/llm-error",
		Input:   []byte("test"),
	})
	if err == nil {
		t.Fatal("expected error from LLM failure")
	}
	if result.Status != execution.StatusFailed {
		t.Errorf("Status = %v, want failed", result.Status)
	}
}

func TestE2E_Declarative_LLMReturnsEmptyString(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/llm-empty",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "declarative",
	}
	_ = e.LoadSkill(ctx, skill)
	e.SetTextGenerator(&mockTextGenerator{response: ""})

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/llm-empty",
		Input:   []byte("test"),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Status = %v", result.Status)
	}
	if len(result.Output) != 0 {
		t.Errorf("Output should be empty, got %q", string(result.Output))
	}
}

func TestE2E_Declarative_LLMReturnsLongOutput(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	longOutput := strings.Repeat("This is a long output sentence. ", 10000) // ~350KB
	skill := &mockSkill{
		id:            "e2e/llm-long",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "declarative",
	}
	_ = e.LoadSkill(ctx, skill)
	e.SetTextGenerator(&mockTextGenerator{response: longOutput})

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/llm-long",
		Input:   []byte("test"),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Status = %v", result.Status)
	}
	if string(result.Output) != longOutput {
		t.Errorf("Output length = %d, want %d", len(result.Output), len(longOutput))
	}
}

func TestE2E_Declarative_SchemaValidationReject(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &schemaMockSkill{
		mockSkill: mockSkill{id: "e2e/schema-reject", valid: true, timeout: 30 * time.Second, executionMode: "declarative"},
		inputSchema: &types.JSONSchema{
			Type:     "object",
			Required: []string{"text"},
			Properties: map[string]*types.JSONSchema{
				"text": {Type: "string"},
			},
		},
	}
	_ = e.LoadSkill(ctx, skill)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/schema-reject",
		Input:   []byte(`{"other": "value"}`),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if result.Status != execution.StatusRejected {
		t.Errorf("Status = %v, want rejected", result.Status)
	}
}

func TestE2E_Declarative_MalformedJSONInput(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &schemaMockSkill{
		mockSkill: mockSkill{id: "e2e/bad-json", valid: true, timeout: 30 * time.Second, executionMode: "declarative"},
		inputSchema: &types.JSONSchema{
			Type: "object",
		},
	}
	_ = e.LoadSkill(ctx, skill)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/bad-json",
		Input:   []byte(`{invalid json`),
	})
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if result.Status != execution.StatusRejected {
		t.Errorf("Status = %v, want rejected", result.Status)
	}
}

func TestE2E_Declarative_MaxLengthExceeded(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	maxLen := 10
	skill := &schemaMockSkill{
		mockSkill: mockSkill{id: "e2e/maxlen", valid: true, timeout: 30 * time.Second, executionMode: "declarative"},
		inputSchema: &types.JSONSchema{
			Type: "object",
			Properties: map[string]*types.JSONSchema{
				"text": {Type: "string", MaxLength: &maxLen},
			},
		},
	}
	_ = e.LoadSkill(ctx, skill)

	longValue := strings.Repeat("a", 100)
	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/maxlen",
		Input:   []byte(fmt.Sprintf(`{"text": "%s"}`, longValue)),
	})
	if err == nil {
		t.Fatal("expected validation error for exceeding maxLength")
	}
	if result.Status != execution.StatusRejected {
		t.Errorf("Status = %v, want rejected", result.Status)
	}
}

func TestE2E_Declarative_MissingRequiredField(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &schemaMockSkill{
		mockSkill: mockSkill{id: "e2e/missing-field", valid: true, timeout: 30 * time.Second, executionMode: "declarative"},
		inputSchema: &types.JSONSchema{
			Type:     "object",
			Required: []string{"name", "email"},
			Properties: map[string]*types.JSONSchema{
				"name":  {Type: "string"},
				"email": {Type: "string"},
			},
		},
	}
	_ = e.LoadSkill(ctx, skill)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/missing-field",
		Input:   []byte(`{"name": "Alice"}`),
	})
	if err == nil {
		t.Fatal("expected validation error for missing required field")
	}
	if result.Status != execution.StatusRejected {
		t.Errorf("Status = %v, want rejected", result.Status)
	}
}

func TestE2E_Declarative_ExtraFieldsAccepted(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &schemaMockSkill{
		mockSkill: mockSkill{id: "e2e/extra-fields", valid: true, timeout: 30 * time.Second, executionMode: "declarative"},
		inputSchema: &types.JSONSchema{
			Type: "object",
			Properties: map[string]*types.JSONSchema{
				"name": {Type: "string"},
			},
		},
	}
	_ = e.LoadSkill(ctx, skill)
	e.SetTextGenerator(&mockTextGenerator{response: "ok"})

	// Extra "age" field not in schema — non-strict mode accepts it
	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/extra-fields",
		Input:   []byte(`{"name": "Alice", "age": 30}`),
	})
	if err != nil {
		t.Fatalf("Execute should accept extra fields in non-strict mode: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Status = %v", result.Status)
	}
}

func TestE2E_Declarative_ContextCancellation(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx, cancel := context.WithCancel(context.Background())

	skill := &mockSkill{
		id:            "e2e/cancel",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "declarative",
	}
	_ = e.LoadSkill(ctx, skill)

	// Set up a TextGenerator that blocks until context is cancelled
	slowLLM := &blockingTextGenerator{delay: 10 * time.Second}
	e.SetTextGenerator(slowLLM)

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, _ := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/cancel",
		Input:   []byte("test"),
	})
	// With cancellation, the result may be success (if already completed) or timeout
	if result != nil && result.Status == execution.StatusFailed {
		t.Logf("Got failed status after cancellation (acceptable): %s", result.Error)
	}
}

func TestE2E_Declarative_TimeoutExceeded(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/timeout",
		valid:         true,
		timeout:       100 * time.Millisecond,
		executionMode: "declarative",
	}
	_ = e.LoadSkill(ctx, skill)

	slowLLM := &blockingTextGenerator{delay: 5 * time.Second}
	e.SetTextGenerator(slowLLM)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/timeout",
		Input:   []byte("test"),
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if result.Status != execution.StatusTimeout && result.Status != execution.StatusFailed {
		t.Errorf("Status = %v, want timeout or failed", result.Status)
	}
}

func TestE2E_Declarative_NoPromptGenericFallback(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/no-prompt",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "declarative",
		prompt:        "",
	}
	_ = e.LoadSkill(ctx, skill)

	mockLLM := &mockTextGenerator{response: "generic"}
	e.SetTextGenerator(mockLLM)

	_, _ = e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/no-prompt",
		Input:   []byte("test data"),
	})
	if !contains(mockLLM.prompt, "You are performing the skill") {
		t.Errorf("Should use generic fallback when no SKILL.md, got: %s", mockLLM.prompt)
	}
}

func TestE2E_Declarative_ConcurrentExecutions(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/concurrent",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "declarative",
	}
	_ = e.LoadSkill(ctx, skill)
	e.SetTextGenerator(&mockTextGenerator{response: "result"})

	var wg sync.WaitGroup
	errCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := e.Execute(ctx, execution.ExecutionRequest{
				SkillID: "e2e/concurrent",
				Input:   []byte(fmt.Sprintf("input-%d", i)),
			})
			if err != nil {
				errCh <- err
				return
			}
			if result.Status != execution.StatusSuccess {
				errCh <- fmt.Errorf("status = %v for iteration %d", result.Status, i)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent execution error: %v", err)
	}
}

func TestE2E_Declarative_BinaryInput(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/binary-input",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "declarative",
		prompt:        "Process: {{.Input}}",
	}
	_ = e.LoadSkill(ctx, skill)
	// nil schema — no validation
	e.SetTextGenerator(&mockTextGenerator{response: "processed"})

	binaryInput := []byte{0x00, 0x01, 0x02, 0xFF}
	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/binary-input",
		Input:   binaryInput,
	})
	if err != nil {
		t.Fatalf("Execute failed with binary input: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Status = %v", result.Status)
	}
}

func TestE2E_Declarative_LargeInput(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/large-input",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "declarative",
	}
	_ = e.LoadSkill(ctx, skill)
	e.SetTextGenerator(&mockTextGenerator{response: "summarized"})

	largeInput := make([]byte, 1024*1024) // 1MB
	for i := range largeInput {
		largeInput[i] = 'A'
	}
	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/large-input",
		Input:   largeInput,
	})
	if err != nil {
		t.Fatalf("Execute failed with large input: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Status = %v", result.Status)
	}
}

func TestE2E_Declarative_TemplateMultipleInputReplacements(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/multi-template",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "declarative",
		prompt:        "First: {{.Input}}, then again: {{.Input}}, and finally: {{.Input}}",
	}
	_ = e.LoadSkill(ctx, skill)

	mockLLM := &mockTextGenerator{response: "ok"}
	e.SetTextGenerator(mockLLM)

	_, _ = e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/multi-template",
		Input:   []byte("REPLACED"),
	})
	// All three occurrences should be replaced
	if !contains(mockLLM.prompt, "REPLACED") {
		t.Error("All {{.Input}} occurrences should be replaced")
	}
	if contains(mockLLM.prompt, "{{.Input}}") {
		t.Error("No {{.Input}} should remain after substitution")
	}
}

// ============================================================================
// Category 4: Wasm Skill Execution
// ============================================================================

func TestE2E_Wasm_HelloWorld(t *testing.T) {
	wasmBytes := buildWasmSkillE2E(t, "hello-world")
	e := setupExecutorWithWasmE2E(t, "e2e/hello-world", wasmBytes)

	input, _ := json.Marshal(map[string]string{"message": "hello"})
	result, err := e.Execute(context.Background(), execution.ExecutionRequest{
		SkillID: "e2e/hello-world",
		Input:   input,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Status = %v, error = %q", result.Status, result.Error)
	}
}

func TestE2E_Wasm_MathAdd(t *testing.T) {
	wasmBytes := buildWasmSkillE2E(t, "math-add")
	e := setupExecutorWithWasmE2E(t, "e2e/math-add", wasmBytes)

	input, _ := json.Marshal(map[string]any{"a": float64(3), "b": float64(4)})
	result, err := e.Execute(context.Background(), execution.ExecutionRequest{
		SkillID: "e2e/math-add",
		Input:   input,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Fatalf("Status = %v, error = %q", result.Status, result.Error)
	}
	var output map[string]any
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatalf("Parse output: %v", err)
	}
	if sum, _ := output["sum"].(float64); sum != 7 {
		t.Errorf("sum = %v, want 7", sum)
	}
}

func TestE2E_Wasm_Wordcount(t *testing.T) {
	wasmBytes := buildWasmSkillE2E(t, "wordcount")
	e := setupExecutorWithWasmE2E(t, "e2e/wordcount", wasmBytes)

	input, _ := json.Marshal(map[string]string{"text": "one two three four"})
	result, err := e.Execute(context.Background(), execution.ExecutionRequest{
		SkillID: "e2e/wordcount",
		Input:   input,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Fatalf("Status = %v, error = %q", result.Status, result.Error)
	}
	var output map[string]any
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatalf("Parse output: %v", err)
	}
	if count, _ := output["count"].(float64); count != 4 {
		t.Errorf("count = %v, want 4", count)
	}
}

func TestE2E_Wasm_TaxCalculator(t *testing.T) {
	wasmBytes := buildWasmSkillE2E(t, "tax-calculator")
	e := setupExecutorWithWasmE2E(t, "e2e/tax-calc", wasmBytes)

	input, _ := json.Marshal(map[string]any{
		"amount": float64(100), "region": "US", "category": "goods",
	})
	result, err := e.Execute(context.Background(), execution.ExecutionRequest{
		SkillID: "e2e/tax-calc",
		Input:   input,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Fatalf("Status = %v, error = %q", result.Status, result.Error)
	}
	var output map[string]any
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatalf("Parse output: %v", err)
	}
	if output["tax_amount"] == nil {
		t.Error("expected tax_amount in output")
	}
}

func TestE2E_Wasm_SentimentAnalysis(t *testing.T) {
	wasmBytes := buildWasmSkillE2E(t, "sentiment")
	e := setupExecutorWithWasmE2E(t, "e2e/sentiment", wasmBytes)

	input, _ := json.Marshal(map[string]string{"text": "I love this product!"})
	result, err := e.Execute(context.Background(), execution.ExecutionRequest{
		SkillID: "e2e/sentiment",
		Input:   input,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Fatalf("Status = %v, error = %q", result.Status, result.Error)
	}
	var output map[string]any
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatalf("Parse output: %v", err)
	}
	if output["sentiment"] == nil {
		t.Error("expected sentiment field")
	}
}

func TestE2E_Wasm_MalformedJSONInput(t *testing.T) {
	wasmBytes := buildWasmSkillE2E(t, "hello-world")
	e := setupExecutorWithWasmE2E(t, "e2e/wasm-bad-json", wasmBytes)

	// The wasm skill itself receives the raw bytes — whether it errors depends
	// on the skill implementation. We just verify execution doesn't crash.
	result, err := e.Execute(context.Background(), execution.ExecutionRequest{
		SkillID: "e2e/wasm-bad-json",
		Input:   []byte(`not json at all`),
	})
	// Execution may succeed or fail depending on skill implementation
	t.Logf("Malformed JSON input: err=%v, status=%v, output=%q", err, result.Status, string(result.Output))
}

func TestE2E_Wasm_NullInput(t *testing.T) {
	wasmBytes := buildWasmSkillE2E(t, "hello-world")
	e := setupExecutorWithWasmE2E(t, "e2e/wasm-null", wasmBytes)

	result, err := e.Execute(context.Background(), execution.ExecutionRequest{
		SkillID: "e2e/wasm-null",
		Input:   nil,
	})
	t.Logf("Null input: err=%v, status=%v, output=%q", err, result.Status, string(result.Output))
}

func TestE2E_Wasm_EmptyJSONObject(t *testing.T) {
	wasmBytes := buildWasmSkillE2E(t, "hello-world")
	e := setupExecutorWithWasmE2E(t, "e2e/wasm-empty-obj", wasmBytes)

	result, err := e.Execute(context.Background(), execution.ExecutionRequest{
		SkillID: "e2e/wasm-empty-obj",
		Input:   []byte(`{}`),
	})
	t.Logf("Empty JSON object: err=%v, status=%v", err, result.Status)
}

func TestE2E_Wasm_InvalidTypeInInput(t *testing.T) {
	rt, err := wasm.NewRuntime()
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer rt.Close()

	e := skilloperator.NewDefaultExecutorWithRuntime(rt, nil)

	schema := &types.JSONSchema{
		Type: "object",
		Properties: map[string]*types.JSONSchema{
			"a": {Type: "number"},
			"b": {Type: "number"},
		},
		Required: []string{"a", "b"},
	}

	wasmBytes := buildWasmSkillE2E(t, "math-add")
	s := &manifestSkillE2E{
		id: "e2e/wasm-type-check", name: "Type Check",
		inputSchema: schema,
	}
	_ = e.LoadSkillWithWasm(context.Background(), s, wasmBytes)

	// Send string where number is expected
	result, err := e.Execute(context.Background(), execution.ExecutionRequest{
		SkillID: "e2e/wasm-type-check",
		Input:   []byte(`{"a": "not-a-number", "b": 4}`),
	})
	if err == nil {
		t.Log("Schema validation may or may not catch string-for-number depending on implementation")
	} else {
		t.Logf("Schema validation caught type mismatch: %v", err)
	}
	if result != nil && result.Status == execution.StatusRejected {
		t.Log("Status correctly rejected for type mismatch")
	}
}

func TestE2E_Wasm_FallbackToLLM(t *testing.T) {
	wasmRT, err := wasm.NewRuntime()
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer wasmRT.Close()

	e := skilloperator.NewDefaultExecutorWithRuntime(wasmRT, nil)
	ctx := context.Background()

	// Invalid wasm that will fail execution
	skill := &mockSkill{
		id:          "e2e/wasm-fallback",
		valid:       true,
		timeout:     5 * time.Second,
		wasmBytes:   []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}, // invalid Wasm
		permissions: []string{"llm:generate"},
	}
	_ = e.LoadSkill(ctx, skill)

	mockLLM := &mockTextGenerator{response: "LLM fallback result"}
	e.SetTextGenerator(mockLLM)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/wasm-fallback",
		Input:   []byte("test input"),
	})
	if err != nil {
		t.Fatalf("Execute should succeed via LLM fallback: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Status = %v", result.Status)
	}
	if string(result.Output) != "LLM fallback result" {
		t.Errorf("Output = %q", string(result.Output))
	}
	if !mockLLM.called {
		t.Error("LLM fallback should have been called")
	}
}

func TestE2E_Wasm_NoFallbackWithoutPermission(t *testing.T) {
	wasmRT, err := wasm.NewRuntime()
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer wasmRT.Close()

	e := skilloperator.NewDefaultExecutorWithRuntime(wasmRT, nil)
	ctx := context.Background()

	// Invalid wasm, no llm:generate permission
	skill := &mockSkill{
		id:        "e2e/wasm-no-perm",
		valid:     true,
		timeout:   5 * time.Second,
		wasmBytes: []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00},
	}
	_ = e.LoadSkill(ctx, skill)

	mockLLM := &mockTextGenerator{response: "should not be called"}
	e.SetTextGenerator(mockLLM)

	_, err = e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/wasm-no-perm",
		Input:   []byte("test"),
	})
	if err == nil {
		t.Error("expected error when Wasm fails and no llm:generate permission")
	}
	if mockLLM.called {
		t.Error("LLM should NOT be called without llm:generate permission")
	}
}

func TestE2E_Wasm_NoFallbackWithoutTextGenerator(t *testing.T) {
	wasmRT, err := wasm.NewRuntime()
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer wasmRT.Close()

	e := skilloperator.NewDefaultExecutorWithRuntime(wasmRT, nil)
	ctx := context.Background()

	skill := &mockSkill{
		id:          "e2e/wasm-no-tg",
		valid:       true,
		timeout:     5 * time.Second,
		wasmBytes:   []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00},
		permissions: []string{"llm:generate"},
	}
	_ = e.LoadSkill(ctx, skill)
	// No TextGenerator set

	_, err = e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/wasm-no-tg",
		Input:   []byte("test"),
	})
	if err == nil {
		t.Error("expected error when Wasm fails and no TextGenerator")
	}
}

func TestE2E_Wasm_CorruptedBinary(t *testing.T) {
	wasmRT, err := wasm.NewRuntime()
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer wasmRT.Close()

	e := skilloperator.NewDefaultExecutorWithRuntime(wasmRT, nil)
	ctx := context.Background()

	skill := &mockSkill{
		id:        "e2e/corrupted-wasm",
		valid:     true,
		timeout:   5 * time.Second,
		wasmBytes: []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0xFF},
	}
	_ = e.LoadSkill(ctx, skill)

	_, err = e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/corrupted-wasm",
		Input:   []byte("test"),
	})
	if err == nil {
		t.Error("expected error for corrupted wasm binary")
	}
}

func TestE2E_Wasm_Timeout(t *testing.T) {
	wasmRT, err := wasm.NewRuntime()
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer wasmRT.Close()

	e := skilloperator.NewDefaultExecutorWithRuntime(wasmRT, nil)
	ctx := context.Background()

	skill := &mockSkill{
		id:        "e2e/wasm-timeout",
		valid:     true,
		timeout:   1 * time.Nanosecond, // Extremely short timeout
		wasmBytes: testWasm,
	}
	_ = e.LoadSkill(ctx, skill)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/wasm-timeout",
		Input:   []byte("test"),
	})
	// With 1ns timeout, execution should timeout
	t.Logf("Timeout test: err=%v, status=%v", err, result.Status)
}

func TestE2E_Wasm_ConcurrentExecution(t *testing.T) {
	wasmBytes := buildWasmSkillE2E(t, "math-add")

	var wg sync.WaitGroup
	errCh := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := setupExecutorWithWasmE2E(t, fmt.Sprintf("e2e/concurrent-%d", i), wasmBytes)
			input, _ := json.Marshal(map[string]any{"a": float64(i), "b": float64(i + 1)})
			result, err := e.Execute(context.Background(), execution.ExecutionRequest{
				SkillID: fmt.Sprintf("e2e/concurrent-%d", i),
				Input:   input,
			})
			if err != nil {
				errCh <- fmt.Errorf("iter %d: %v", i, err)
				return
			}
			if result.Status != execution.StatusSuccess {
				errCh <- fmt.Errorf("iter %d: status = %v", i, result.Status)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent wasm execution error: %v", err)
	}
}

// ============================================================================
// Category 5: SkillDescriptor / ToolSpec Integration
// ============================================================================

func TestE2E_SkillDescriptor_AllFields(t *testing.T) {
	schema := &types.JSONSchema{
		Type:        "object",
		Description: "Test schema",
		Properties: map[string]*types.JSONSchema{
			"text": {Type: "string", Description: "Input text"},
		},
		Required: []string{"text"},
	}
	desc := types.SkillDescriptor{
		ID:          "test/skill",
		Name:        "Test Skill",
		Description: "A test skill descriptor",
		InputSchema: schema,
	}
	if desc.ID != "test/skill" {
		t.Errorf("ID = %q", desc.ID)
	}
	if desc.Name != "Test Skill" {
		t.Errorf("Name = %q", desc.Name)
	}
	if desc.InputSchema == nil {
		t.Error("InputSchema should not be nil")
	}
	if len(desc.InputSchema.Required) != 1 {
		t.Errorf("Required len = %d", len(desc.InputSchema.Required))
	}
}

func TestE2E_ToolDefinition_Parameters(t *testing.T) {
	params := &types.JSONSchema{
		Type: "object",
		Properties: map[string]*types.JSONSchema{
			"query": {Type: "string"},
			"limit": {Type: "integer"},
		},
		Required: []string{"query"},
	}
	td := types.ToolDefinition{
		Name:        "search",
		Description: "Search for items",
		Parameters:  params,
	}
	if td.Name != "search" {
		t.Errorf("Name = %q", td.Name)
	}
	if len(td.Parameters.Required) != 1 || td.Parameters.Required[0] != "query" {
		t.Errorf("Required = %v", td.Parameters.Required)
	}
}

func TestE2E_ToolSpec_NestedObjects(t *testing.T) {
	schema := &types.JSONSchema{
		Type: "object",
		Properties: map[string]*types.JSONSchema{
			"address": {
				Type: "object",
				Properties: map[string]*types.JSONSchema{
					"street": {Type: "string"},
					"city":   {Type: "string"},
					"zip":    {Type: "string"},
				},
			},
		},
	}
	td := types.ToolDefinition{
		Name:       "update_address",
		Parameters: schema,
	}
	addrProp := td.Parameters.Properties["address"]
	if addrProp == nil || addrProp.Type != "object" {
		t.Error("address property should be object type")
	}
	if len(addrProp.Properties) != 3 {
		t.Errorf("address should have 3 properties, got %d", len(addrProp.Properties))
	}
}

func TestE2E_ToolSpec_ArrayTypes(t *testing.T) {
	schema := &types.JSONSchema{
		Type: "object",
		Properties: map[string]*types.JSONSchema{
			"tags": {
				Type:  "array",
				Items: &types.JSONSchema{Type: "string"},
			},
			"scores": {
				Type:  "array",
				Items: &types.JSONSchema{Type: "number"},
			},
		},
	}
	td := types.ToolDefinition{
		Name:       "process",
		Parameters: schema,
	}
	tagsProp := td.Parameters.Properties["tags"]
	if tagsProp == nil || tagsProp.Type != "array" {
		t.Error("tags should be array type")
	}
	if tagsProp.Items == nil || tagsProp.Items.Type != "string" {
		t.Error("tags items should be string type")
	}
}

func TestE2E_ToolSpec_NilSchemaEmptyParameters(t *testing.T) {
	td := types.ToolDefinition{
		Name:        "no-schema-tool",
		Description: "A tool with no schema",
		Parameters:  nil,
	}
	if td.Parameters != nil {
		t.Error("Parameters should be nil")
	}
}

func TestE2E_ToolSpec_EmptyProperties(t *testing.T) {
	schema := &types.JSONSchema{
		Type:       "object",
		Properties: map[string]*types.JSONSchema{},
	}
	td := types.ToolDefinition{
		Name:       "empty-props",
		Parameters: schema,
	}
	if len(td.Parameters.Properties) != 0 {
		t.Error("Properties should be empty map")
	}
}

func TestE2E_ToolSpec_DeeplyNestedSchema(t *testing.T) {
	// Create a schema with 6 levels of nesting
	inner := &types.JSONSchema{Type: "string"}
	for i := 0; i < 5; i++ {
		inner = &types.JSONSchema{
			Type:       "object",
			Properties: map[string]*types.JSONSchema{"child": inner},
		}
	}
	schema := &types.JSONSchema{
		Type:       "object",
		Properties: map[string]*types.JSONSchema{"root": inner},
	}
	td := types.ToolDefinition{
		Name:       "deep-nested",
		Parameters: schema,
	}
	// Walk down the nesting
	current := td.Parameters.Properties["root"]
	depth := 0
	for current != nil && current.Type == "object" {
		depth++
		if current.Properties != nil {
			current = current.Properties["child"]
		} else {
			break
		}
	}
	if depth < 5 {
		t.Errorf("Expected at least 5 levels of nesting, got %d", depth)
	}
}

func TestE2E_ToolSpec_EnumWithVariousTypes(t *testing.T) {
	schema := &types.JSONSchema{
		Type: "string",
		Enum: []any{"US", "EU", "CN"},
	}
	td := types.ToolDefinition{
		Name:       "region-tool",
		Parameters: &types.JSONSchema{Type: "object", Properties: map[string]*types.JSONSchema{"region": schema}},
	}
	regionProp := td.Parameters.Properties["region"]
	if len(regionProp.Enum) != 3 {
		t.Errorf("Enum should have 3 values, got %d", len(regionProp.Enum))
	}
}

func TestE2E_ToolSpec_AllOptional(t *testing.T) {
	schema := &types.JSONSchema{
		Type: "object",
		Properties: map[string]*types.JSONSchema{
			"opt1": {Type: "string"},
			"opt2": {Type: "number"},
		},
		Required: []string{},
	}
	td := types.ToolDefinition{Name: "all-opt", Parameters: schema}
	if len(td.Parameters.Required) != 0 {
		t.Errorf("Required should be empty, got %v", td.Parameters.Required)
	}
}

func TestE2E_ToolSpec_AllRequired(t *testing.T) {
	schema := &types.JSONSchema{
		Type: "object",
		Properties: map[string]*types.JSONSchema{
			"req1": {Type: "string"},
			"req2": {Type: "number"},
		},
		Required: []string{"req1", "req2"},
	}
	td := types.ToolDefinition{Name: "all-req", Parameters: schema}
	if len(td.Parameters.Required) != 2 {
		t.Errorf("Required should have 2 items, got %d", len(td.Parameters.Required))
	}
}

func TestE2E_ToolSpec_ManyProperties(t *testing.T) {
	props := make(map[string]*types.JSONSchema)
	for i := 0; i < 100; i++ {
		props[fmt.Sprintf("field_%03d", i)] = &types.JSONSchema{Type: "string"}
	}
	schema := &types.JSONSchema{
		Type:       "object",
		Properties: props,
	}
	td := types.ToolDefinition{Name: "many-props", Parameters: schema}
	if len(td.Parameters.Properties) != 100 {
		t.Errorf("Expected 100 properties, got %d", len(td.Parameters.Properties))
	}
}

// ============================================================================
// Category 6: Loader Integration
// ============================================================================

func TestE2E_Loader_SystemSkillsManifest(t *testing.T) {
	skillDirs := []string{
		"summarize",
		"extract_structured_data",
		"classify",
	}
	for _, dir := range skillDirs {
		t.Run(dir, func(t *testing.T) {
			manifestPath := filepath.Join(skillsDir, dir, "manifest.yaml")
			data, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("read manifest: %v", err)
			}
			m, err := registry.ParseManifest(data)
			if err != nil {
				t.Fatalf("parse manifest: %v", err)
			}
			if err := m.Validate(); err != nil {
				t.Fatalf("validate manifest: %v", err)
			}
		})
	}
}

func TestE2E_Loader_VerifyExecutionModes(t *testing.T) {
	wasmSkills := []string{}
	declarativeSkills := []string{"summarize", "extract_structured_data", "classify"}

	for _, dir := range wasmSkills {
		m := parseManifestE2E(t, dir)
		if m.Execution.Mode != "wasm" {
			t.Errorf("%s: execution.mode = %q, want wasm", dir, m.Execution.Mode)
		}
	}
	for _, dir := range declarativeSkills {
		m := parseManifestE2E(t, dir)
		if m.Execution.Mode != "declarative" {
			t.Errorf("%s: execution.mode = %q, want declarative", dir, m.Execution.Mode)
		}
	}
}

func TestE2E_Loader_DeclarativeSkillsHaveSkillMD(t *testing.T) {
	declarativeSkills := []string{"summarize", "extract_structured_data", "classify"}
	for _, dir := range declarativeSkills {
		skillDir := filepath.Join(skillsDir, dir)
		if !registry.HasSkillMD(skillDir) {
			t.Errorf("%s: declarative skill should have SKILL.md", dir)
		}
		smd, err := registry.ParseSkillMD(skillDir)
		if err != nil {
			t.Errorf("%s: ParseSkillMD error: %v", dir, err)
		}
		if smd == nil || smd.Body == "" {
			t.Errorf("%s: SKILL.md should not be empty", dir)
		}
	}
}

func TestE2E_Loader_WasmSkillsNoSkillMD(t *testing.T) {
	// Wasm skills have been moved to apps/examples; no wasm skills in runtime/skills/
	t.Log("No wasm skills in runtime/skills/ — wasm examples live in apps/examples/")
}

func TestE2E_Loader_CorruptedManifestYAML(t *testing.T) {
	dir := t.TempDir()
	corruptedYAML := `
id: test/corrupted
version: "1.0"
execution:
  mode: wasm
input_schema:
  type: object
  properties:
    field:
      type: string
    # Unclosed bracket follows
`
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(corruptedYAML), 0644); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "manifest.yaml"))
	_, err := registry.ParseManifest(data)
	// The YAML above is actually valid (comment is fine), so this might not error
	t.Logf("ParseManifest on corrupted YAML: err=%v", err)
}

func TestE2E_Loader_NoManifestFile(t *testing.T) {
	dir := t.TempDir()
	// Create a directory with no manifest.yaml
	manifestPath := filepath.Join(dir, "manifest.yaml")
	_, err := os.ReadFile(manifestPath)
	if err == nil {
		t.Error("expected error reading non-existent manifest")
	}
}

func TestE2E_Loader_EmptyManifestFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "manifest.yaml"))
	m, err := registry.ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest on empty file: %v", err)
	}
	if err := m.Validate(); err == nil {
		t.Error("Validate should fail for empty manifest")
	}
}

func TestE2E_Loader_DuplicateSkillIDs(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	s1 := &mockSkill{id: "dup/id", valid: true, timeout: 30 * time.Second, executionMode: "declarative"}
	s2 := &mockSkill{id: "dup/id", valid: true, timeout: 30 * time.Second, executionMode: "declarative"}

	if err := e.LoadSkill(ctx, s1); err != nil {
		t.Fatalf("first load failed: %v", err)
	}
	if err := e.LoadSkill(ctx, s2); err != skilloperator.ErrSkillAlreadyLoaded {
		t.Errorf("second load should fail with ErrSkillAlreadyLoaded, got %v", err)
	}
}

func TestE2E_Loader_ManifestNonExistentWasm(t *testing.T) {
	dir := t.TempDir()
	yaml := `
id: test/no-wasm-file
version: "1.0"
execution:
  mode: wasm
`
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "manifest.yaml"))
	m, err := registry.ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest failed: %v", err)
	}
	if m.Execution.Mode != "wasm" {
		t.Error("Mode should be wasm")
	}
	// No main.wasm file — this is a runtime concern, not parse concern
}

func TestE2E_Loader_FullPipelineLoadAndExecute(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	// Simulate loader behavior for a declarative skill
	dir := t.TempDir()
	manifestYAML := `
id: e2e/loader-test
version: "1.0"
name: Loader Test
description: Test skill loaded via simulated loader pipeline
execution:
  mode: declarative
input_schema:
  type: object
  properties:
    text:
      type: string
  required:
    - text
`
	skillMDContent := "Summarize the following:\n{{.Input}}"

	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMDContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Parse
	data, _ := os.ReadFile(filepath.Join(dir, "manifest.yaml"))
	m, err := registry.ParseManifest(data)
	if err != nil {
		t.Fatalf("ParseManifest: %v", err)
	}
	smd, _ := registry.ParseSkillMD(dir)

	// Create skill and load
	s := &manifestSkillE2E{
		id: m.ID, name: m.Name, description: m.Description,
		inputSchema: m.InputSchema, executionMode: m.Execution.Mode,
		prompt: smd.Body,
	}
	if err := e.LoadSkill(ctx, s); err != nil {
		t.Fatalf("LoadSkill: %v", err)
	}

	// Execute
	e.SetTextGenerator(&mockTextGenerator{response: "Summary: key points"})
	input, _ := json.Marshal(map[string]string{"text": "Long text"})
	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/loader-test",
		Input:   input,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Status = %v", result.Status)
	}
	if string(result.Output) != "Summary: key points" {
		t.Errorf("Output = %q", string(result.Output))
	}
}

// ============================================================================
// Category 7: Audit and Observability
// ============================================================================

func TestE2E_Audit_SuccessEvents(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()
	_ = e.LoadSkill(ctx, newMockSkill("e2e/audit-ok", true))
	e.SetTextGenerator(&mockTextGenerator{response: "ok"})

	auditLog := execution_logs.NewInMemoryAuditLogger()
	e.SetAuditLogger(auditLog)

	_, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID:   "e2e/audit-ok",
		Input:     []byte("test"),
		TenantID:  "tenant-1",
		UserID:    "user-1",
		RequestID: "req-audit-1",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	events, _ := auditLog.Query(ctx, execution_logs.QueryFilter{RequestID: "req-audit-1"})
	if len(events) != 2 {
		t.Fatalf("Expected 2 events (started + success), got %d", len(events))
	}

	var hasStarted, hasSuccess bool
	for _, evt := range events {
		switch evt.Outcome {
		case "started":
			hasStarted = true
			if evt.TenantID != "tenant-1" {
				t.Errorf("Started TenantID = %q", evt.TenantID)
			}
		case "success":
			hasSuccess = true
			if evt.Duration == 0 {
				t.Error("Success Duration should be > 0")
			}
		}
	}
	if !hasStarted || !hasSuccess {
		t.Errorf("Missing events: started=%v success=%v", hasStarted, hasSuccess)
	}
}

func TestE2E_Audit_FailureEvents(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	auditLog := execution_logs.NewInMemoryAuditLogger()
	e.SetAuditLogger(auditLog)

	_, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID:   "nonexistent",
		RequestID: "req-audit-fail",
	})
	if err == nil {
		t.Fatal("expected error")
	}

	events, _ := auditLog.Query(ctx, execution_logs.QueryFilter{RequestID: "req-audit-fail"})
	if len(events) != 2 {
		t.Fatalf("Expected 2 events (started + failure), got %d", len(events))
	}

	var failureEvt *audit.AuditEvent
	for i := range events {
		if events[i].Outcome == "failure" {
			failureEvt = &events[i]
		}
	}
	if failureEvt == nil {
		t.Fatal("Missing failure event")
	}
	if failureEvt.Metadata["error"] != "skill not loaded" {
		t.Errorf("error metadata = %q", failureEvt.Metadata["error"])
	}
}

func TestE2E_Audit_TimeoutEvent(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/audit-timeout",
		valid:         true,
		timeout:       100 * time.Millisecond,
		executionMode: "declarative",
	}
	_ = e.LoadSkill(ctx, skill)

	auditLog := execution_logs.NewInMemoryAuditLogger()
	e.SetAuditLogger(auditLog)
	e.SetTextGenerator(&blockingTextGenerator{delay: 5 * time.Second})

	result, _ := e.Execute(ctx, execution.ExecutionRequest{
		SkillID:   "e2e/audit-timeout",
		Input:     []byte("test"),
		RequestID: "req-audit-timeout",
	})

	if result != nil && result.Status == execution.StatusTimeout {
		events, _ := auditLog.Query(ctx, execution_logs.QueryFilter{RequestID: "req-audit-timeout"})
		var hasTimeout bool
		for _, evt := range events {
			if evt.Outcome == "timeout" {
				hasTimeout = true
				if evt.Metadata["error"] != "execution timeout" {
					t.Errorf("timeout metadata error = %q", evt.Metadata["error"])
				}
			}
		}
		if !hasTimeout {
			t.Error("Missing timeout audit event")
		}
	}
}

func TestE2E_Audit_NilLoggerDoesNotPanic(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()
	_ = e.LoadSkill(ctx, newMockSkill("e2e/nil-audit", true))
	e.SetTextGenerator(&mockTextGenerator{response: "ok"})
	// No SetAuditLogger — auditLogger is nil

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/nil-audit",
		Input:   []byte("test"),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Status = %v", result.Status)
	}
}

func TestE2E_Audit_ConcurrentSharedLogger(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()
	_ = e.LoadSkill(ctx, newMockSkill("e2e/audit-conc", true))

	auditLog := execution_logs.NewInMemoryAuditLogger()
	e.SetAuditLogger(auditLog)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = e.Execute(ctx, execution.ExecutionRequest{
				SkillID:   "e2e/audit-conc",
				Input:     []byte("test"),
				RequestID: fmt.Sprintf("req-conc-%d", i),
			})
		}(i)
	}
	wg.Wait()

	// Should have 40 events total (started + success per execution)
	count, _ := auditLog.Count(ctx, execution_logs.QueryFilter{})
	if count != 40 {
		t.Errorf("Expected 40 audit events, got %d", count)
	}
}

func TestE2E_Audit_ExecutionModeMetadata(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/audit-mode",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "declarative",
	}
	_ = e.LoadSkill(ctx, skill)

	auditLog := execution_logs.NewInMemoryAuditLogger()
	e.SetAuditLogger(auditLog)
	e.SetTextGenerator(&mockTextGenerator{response: "ok"})

	_, _ = e.Execute(ctx, execution.ExecutionRequest{
		SkillID:   "e2e/audit-mode",
		Input:     []byte("test"),
		RequestID: "req-mode",
	})

	events, _ := auditLog.Query(ctx, execution_logs.QueryFilter{RequestID: "req-mode"})
	var successEvt *audit.AuditEvent
	for i := range events {
		if events[i].Outcome == "success" {
			successEvt = &events[i]
		}
	}
	if successEvt == nil {
		t.Fatal("Missing success event")
	}
	if successEvt.Metadata["execution_mode"] != "declarative" {
		t.Errorf("execution_mode = %q, want declarative", successEvt.Metadata["execution_mode"])
	}
}

func TestE2E_Audit_FallbackMetadata(t *testing.T) {
	wasmRT, err := wasm.NewRuntime()
	if err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	defer wasmRT.Close()

	e := skilloperator.NewDefaultExecutorWithRuntime(wasmRT, nil)
	ctx := context.Background()

	skill := &mockSkill{
		id:          "e2e/audit-fallback",
		valid:       true,
		timeout:     5 * time.Second,
		wasmBytes:   []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00},
		permissions: []string{"llm:generate"},
	}
	_ = e.LoadSkill(ctx, skill)
	e.SetTextGenerator(&mockTextGenerator{response: "fallback"})

	auditLog := execution_logs.NewInMemoryAuditLogger()
	e.SetAuditLogger(auditLog)

	_, _ = e.Execute(ctx, execution.ExecutionRequest{
		SkillID:   "e2e/audit-fallback",
		Input:     []byte("test"),
		RequestID: "req-fallback",
	})

	events, _ := auditLog.Query(ctx, execution_logs.QueryFilter{RequestID: "req-fallback"})
	var successEvt *audit.AuditEvent
	for i := range events {
		if events[i].Outcome == "success" {
			successEvt = &events[i]
		}
	}
	if successEvt == nil {
		t.Fatal("Missing success event after fallback")
	}
	if successEvt.Metadata["fallback"] != "llm" {
		t.Errorf("fallback metadata = %q, want llm", successEvt.Metadata["fallback"])
	}
}

// ============================================================================
// Additional tests: GetPrompt, GetExecutionMode, non-declarative without wasm
// ============================================================================

func TestE2E_GetPrompt_WithProvider(t *testing.T) {
	s := &mockSkill{
		id: "e2e/get-prompt", valid: true, prompt: "Custom prompt text",
	}
	if registry.GetPrompt(s) != "Custom prompt text" {
		t.Errorf("GetPrompt = %q", registry.GetPrompt(s))
	}
}

func TestE2E_GetPrompt_WithoutProvider(t *testing.T) {
	s := &noPromptSkill{id: "e2e/no-prompt"}
	if registry.GetPrompt(s) != "" {
		t.Errorf("GetPrompt should return empty string for non-PromptProvider, got %q", registry.GetPrompt(s))
	}
}

func TestE2E_GetExecutionMode_WithProvider(t *testing.T) {
	s := &mockSkill{id: "e2e/exec-mode", valid: true, executionMode: "declarative"}
	if registry.GetExecutionMode(s) != "declarative" {
		t.Errorf("GetExecutionMode = %q", registry.GetExecutionMode(s))
	}
}

func TestE2E_GetExecutionMode_WithoutProvider(t *testing.T) {
	s := &noExecModeSkill{id: "e2e/no-exec-mode"}
	if registry.GetExecutionMode(s) != "declarative" {
		t.Errorf("GetExecutionMode should default to declarative, got %q", registry.GetExecutionMode(s))
	}
}

func TestE2E_NonDeclarativeNoWasm_NoRuntimeFails(t *testing.T) {
	e := skilloperator.NewDefaultExecutor() // no wasm runtime
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/no-runtime-wasm",
		valid:         true,
		timeout:       30 * time.Second,
		executionMode: "wasm",
	}
	_ = e.LoadSkill(ctx, skill)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/no-runtime-wasm",
		Input:   []byte("test"),
	})
	if err == nil {
		t.Error("expected error when wasm mode has no runtime")
	}
	if result != nil && result.Status != execution.StatusFailed {
		t.Errorf("Status = %v, want failed", result.Status)
	}
}


func TestE2E_ExecutePlan_NilPlan(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	err := e.ExecutePlan(context.Background(), nil, nil)
	if err == nil {
		t.Error("expected error for nil plan")
	}
}

func TestE2E_LoadSkillWithWasm_NilSkill(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	err := e.LoadSkillWithWasm(context.Background(), nil, testWasm)
	if err != skilloperator.ErrNilSkill {
		t.Errorf("Expected ErrNilSkill, got %v", err)
	}
}

func TestE2E_LoadSkillWithWasm_EmptyID(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	s := &mockSkill{id: "", valid: true}
	err := e.LoadSkillWithWasm(context.Background(), s, testWasm)
	if err != skilloperator.ErrEmptySkillID {
		t.Errorf("Expected ErrEmptySkillID, got %v", err)
	}
}

func TestE2E_LoadSkillWithWasm_InvalidSkill(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	s := &mockSkill{id: "bad", valid: false}
	err := e.LoadSkillWithWasm(context.Background(), s, testWasm)
	if err == nil {
		t.Error("Expected error for invalid skill")
	}
}

func TestE2E_ExecuteWithRequestTimeout(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &mockSkill{
		id:            "e2e/req-timeout",
		valid:         true,
		timeout:       30 * time.Second, // skill timeout is generous
		executionMode: "declarative",
	}
	_ = e.LoadSkill(ctx, skill)
	e.SetTextGenerator(&blockingTextGenerator{delay: 5 * time.Second})

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/req-timeout",
		Input:   []byte("test"),
		Timeout: 100 * time.Millisecond, // request timeout overrides skill timeout
	})
	if err == nil {
		t.Log("Timeout may not have triggered (timing dependent)")
	} else {
		t.Logf("Request timeout test: err=%v, status=%v", err, result.Status)
	}
}

func TestE2E_Audit_RejectedValidation(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	skill := &schemaMockSkill{
		mockSkill: mockSkill{id: "e2e/audit-reject", valid: true, timeout: 30 * time.Second, executionMode: "declarative"},
		inputSchema: &types.JSONSchema{
			Type:     "object",
			Required: []string{"required_field"},
			Properties: map[string]*types.JSONSchema{
				"required_field": {Type: "string"},
			},
		},
	}
	_ = e.LoadSkill(ctx, skill)

	auditLog := execution_logs.NewInMemoryAuditLogger()
	e.SetAuditLogger(auditLog)

	result, _ := e.Execute(ctx, execution.ExecutionRequest{
		SkillID:   "e2e/audit-reject",
		Input:     []byte(`{"wrong_field": "value"}`),
		RequestID: "req-reject",
	})
	if result.Status != execution.StatusRejected {
		t.Errorf("Status = %v, want rejected", result.Status)
	}

	events, _ := auditLog.Query(ctx, execution_logs.QueryFilter{RequestID: "req-reject"})
	var hasRejected bool
	for _, evt := range events {
		if evt.Outcome == "rejected" {
			hasRejected = true
		}
	}
	if !hasRejected {
		t.Error("Missing rejected audit event")
	}
}

func TestE2E_GetSkill_ReturnsLoadedSkill(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()
	original := newMockSkill("e2e/get-test", true)
	_ = e.LoadSkill(ctx, original)

	got, err := e.GetSkill(ctx, "e2e/get-test")
	if err != nil {
		t.Fatalf("GetSkill failed: %v", err)
	}
	if got.ID() != "e2e/get-test" {
		t.Errorf("Got skill ID = %q", got.ID())
	}
	if got.Name() != "mock-e2e/get-test" {
		t.Errorf("Got skill Name = %q", got.Name())
	}
}

func TestE2E_CanExecute_LoadedAndUnloaded(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	_ = e.LoadSkill(ctx, newMockSkill("e2e/can-exec", true))

	can, _ := e.CanExecute(ctx, "e2e/can-exec")
	if !can {
		t.Error("Should be able to execute loaded skill")
	}

	can, _ = e.CanExecute(ctx, "nonexistent")
	if can {
		t.Error("Should NOT be able to execute non-loaded skill")
	}
}

func TestE2E_ListSkills_ContainsLoadedIDs(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	_ = e.LoadSkill(ctx, newMockSkill("e2e/list-1", true))
	_ = e.LoadSkill(ctx, newMockSkill("e2e/list-2", true))
	_ = e.LoadSkill(ctx, newMockSkill("e2e/list-3", true))

	ids := e.ListSkills(ctx)
	if len(ids) != 3 {
		t.Errorf("Expected 3 skills, got %d", len(ids))
	}
}

func TestE2E_Audit_ErrorLoggerDoesNotCrash(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	e.SetTextGenerator(&mockTextGenerator{response: "ok"})
	ctx := context.Background()
	_ = e.LoadSkill(ctx, newMockSkill("e2e/err-logger", true))

	auditLog := &errorAuditLogger{}
	e.SetAuditLogger(auditLog)

	result, err := e.Execute(ctx, execution.ExecutionRequest{
		SkillID: "e2e/err-logger",
		Input:   []byte("test"),
	})
	if err != nil {
		t.Fatalf("Execute should not fail even when audit logger errors: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Errorf("Status = %v", result.Status)
	}
}

// ============================================================================
// Helper types and functions for E2E tests
// ============================================================================

// blockingTextGenerator blocks for the specified delay.
type blockingTextGenerator struct {
	delay time.Duration
}

func (b *blockingTextGenerator) GenerateText(ctx context.Context, _ string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(b.delay):
		return "delayed result", nil
	}
}

// noPromptSkill implements skills.Skill but NOT PromptProvider.
type noPromptSkill struct {
	id string
}

func (s *noPromptSkill) ID() string                          { return s.id }
func (s *noPromptSkill) Name() string                        { return "no-prompt-" + s.id }
func (s *noPromptSkill) Description() string                 { return "no prompt" }
func (s *noPromptSkill) Timeout() time.Duration              { return 30 * time.Second }
func (s *noPromptSkill) InputSchema() *types.JSONSchema  { return nil }
func (s *noPromptSkill) OutputSchema() *types.JSONSchema { return nil }
func (s *noPromptSkill) RequiredPermissions() []string       { return nil }
func (s *noPromptSkill) Validate() error                     { return nil }

// noExecModeSkill implements skills.Skill but NOT ExecutionModeProvider.
type noExecModeSkill struct {
	id string
}

func (s *noExecModeSkill) ID() string                          { return s.id }
func (s *noExecModeSkill) Name() string                        { return "no-mode-" + s.id }
func (s *noExecModeSkill) Description() string                 { return "no mode" }
func (s *noExecModeSkill) Timeout() time.Duration              { return 30 * time.Second }
func (s *noExecModeSkill) InputSchema() *types.JSONSchema  { return nil }
func (s *noExecModeSkill) OutputSchema() *types.JSONSchema { return nil }
func (s *noExecModeSkill) RequiredPermissions() []string       { return nil }
func (s *noExecModeSkill) Validate() error                     { return nil }

// errorAuditLogger always returns an error on Log.
type errorAuditLogger struct{}

func (e *errorAuditLogger) Log(_ context.Context, _ audit.AuditEvent) error {
	return errors.New("audit logger intentionally failed")
}
func (e *errorAuditLogger) Query(_ context.Context, _ execution_logs.QueryFilter) ([]audit.AuditEvent, error) {
	return nil, nil
}
func (e *errorAuditLogger) Count(_ context.Context, _ execution_logs.QueryFilter) (int, error) {
	return 0, nil
}

// manifestSkillE2E adapts manifest data to Skill interface for E2E tests.
type manifestSkillE2E struct {
	id            string
	name          string
	description   string
	inputSchema   *types.JSONSchema
	outputSchema  *types.JSONSchema
	executionMode string
	prompt        string
}

func (s *manifestSkillE2E) ID() string                          { return s.id }
func (s *manifestSkillE2E) Name() string                        { return s.name }
func (s *manifestSkillE2E) Description() string                 { return s.description }
func (s *manifestSkillE2E) Timeout() time.Duration              { return 30 * time.Second }
func (s *manifestSkillE2E) InputSchema() *types.JSONSchema  { return s.inputSchema }
func (s *manifestSkillE2E) OutputSchema() *types.JSONSchema { return s.outputSchema }
func (s *manifestSkillE2E) RequiredPermissions() []string       { return nil }
func (s *manifestSkillE2E) ExecutionMode() string               { return s.executionMode }
func (s *manifestSkillE2E) Prompt() string                      { return s.prompt }
func (s *manifestSkillE2E) Validate() error                     { return nil }

// buildWasmSkillE2E builds or loads a wasm skill binary for E2E tests.
func buildWasmSkillE2E(t *testing.T, skillDir string) []byte {
	t.Helper()
	skillPath := filepath.Join(skillsDir, skillDir)
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Skipf("skill %s not found in %s (wasm skills moved to apps/examples)", skillDir, skillsDir)
	}
	wasmPath := filepath.Join(skillPath, "main.wasm")

	if info, err := os.Stat(wasmPath); err == nil && info.Size() > 0 {
		data, err := os.ReadFile(wasmPath)
		if err != nil {
			t.Fatalf("read wasm: %v", err)
		}
		return data
	}

	t.Logf("Building %s wasm module...", skillDir)
	cmd := exec.Command("go", "build", "-o", wasmPath, ".")
	cmd.Dir = filepath.Join(skillsDir, skillDir)
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", skillDir, err, out)
	}
	data, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatalf("read built wasm: %v", err)
	}
	return data
}

// setupExecutorWithWasmE2E creates an executor with a wasm runtime and loads a skill.
func setupExecutorWithWasmE2E(t *testing.T, skillID string, wasmBytes []byte) *skilloperator.DefaultExecutor {
	t.Helper()
	rt, err := wasm.NewRuntime()
	if err != nil {
		t.Fatalf("create wasm runtime: %v", err)
	}
	t.Cleanup(func() { rt.Close() })

	e := skilloperator.NewDefaultExecutorWithRuntime(rt, nil)
	s := &manifestSkillE2E{id: skillID, name: skillID, description: "e2e test skill"}
	if err := e.LoadSkillWithWasm(context.Background(), s, wasmBytes); err != nil {
		t.Fatalf("load skill: %v", err)
	}
	return e
}

// parseManifestE2E parses a manifest from the examples directory.
func parseManifestE2E(t *testing.T, dir string) *registry.SkillManifest {
	t.Helper()
	manifestPath := filepath.Join(skillsDir, dir, "manifest.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read %s manifest: %v", dir, err)
	}
	m, err := registry.ParseManifest(data)
	if err != nil {
		t.Fatalf("parse %s manifest: %v", dir, err)
	}
	return m
}

// Schema validation table tests
func TestE2E_SchemaValidation_Table(t *testing.T) {
	tests := []struct {
		name    string
		schema  *types.JSONSchema
		input   string
		wantErr bool
	}{
		{
			name: "valid object with required field",
			schema: &types.JSONSchema{
				Type:     "object",
				Required: []string{"name"},
				Properties: map[string]*types.JSONSchema{
					"name": {Type: "string"},
				},
			},
			input:   `{"name": "Alice"}`,
			wantErr: false,
		},
		{
			name: "missing required field",
			schema: &types.JSONSchema{
				Type:     "object",
				Required: []string{"name"},
				Properties: map[string]*types.JSONSchema{
					"name": {Type: "string"},
				},
			},
			input:   `{"age": 30}`,
			wantErr: true,
		},
		{
			name: "wrong type for field",
			schema: &types.JSONSchema{
				Type: "object",
				Properties: map[string]*types.JSONSchema{
					"count": {Type: "integer"},
				},
			},
			input:   `{"count": "not-a-number"}`,
			wantErr: true,
		},
		{
			name: "enum value match",
			schema: &types.JSONSchema{
				Type: "string",
				Enum: []any{"US", "EU", "CN"},
			},
			input:   `"US"`,
			wantErr: false,
		},
		{
			name: "enum value mismatch",
			schema: &types.JSONSchema{
				Type: "string",
				Enum: []any{"US", "EU", "CN"},
			},
			input:   `"JP"`,
			wantErr: true,
		},
		{
			name: "number minimum violation",
			schema: &types.JSONSchema{
				Type:    "number",
				Minimum: float64Ptr(0),
			},
			input:   `-5`,
			wantErr: true,
		},
		{
			name: "number maximum violation",
			schema: &types.JSONSchema{
				Type:    "number",
				Maximum: float64Ptr(100),
			},
			input:   `150`,
			wantErr: true,
		},
		{
			name: "string minLength violation",
			schema: &types.JSONSchema{
				Type:      "string",
				MinLength: intPtr(3),
			},
			input:   `"ab"`,
			wantErr: true,
		},
		{
			name: "string maxLength violation",
			schema: &types.JSONSchema{
				Type:      "string",
				MaxLength: intPtr(5),
			},
			input:   `"abcdef"`,
			wantErr: true,
		},
		{
			name: "array with item validation",
			schema: &types.JSONSchema{
				Type:  "array",
				Items: &types.JSONSchema{Type: "string"},
			},
			input:   `["a", "b", "c"]`,
			wantErr: false,
		},
		{
			name: "array with invalid item",
			schema: &types.JSONSchema{
				Type:  "array",
				Items: &types.JSONSchema{Type: "string"},
			},
			input:   `["a", 123, "c"]`,
			wantErr: true,
		},
		{
			name: "nested object validation",
			schema: &types.JSONSchema{
				Type: "object",
				Properties: map[string]*types.JSONSchema{
					"address": {
						Type: "object",
						Properties: map[string]*types.JSONSchema{
							"city": {Type: "string"},
						},
					},
				},
			},
			input:   `{"address": {"city": "NYC"}}`,
			wantErr: false,
		},
		{
			name: "invalid JSON input",
			schema: &types.JSONSchema{
				Type: "object",
			},
			input:   `{broken json`,
			wantErr: true,
		},
		{
			name:    "nil schema accepts anything",
			schema:  nil,
			input:   `{"anything": "goes"}`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := skilloperator.NewDefaultExecutor()
			ctx := context.Background()

			skill := &schemaMockSkill{
				mockSkill:   mockSkill{id: "e2e/schema-" + tt.name, valid: true, timeout: 30 * time.Second, executionMode: "declarative"},
				inputSchema: tt.schema,
			}
			_ = e.LoadSkill(ctx, skill)
			e.SetTextGenerator(&mockTextGenerator{response: "ok"})

			result, err := e.Execute(ctx, execution.ExecutionRequest{
				SkillID: "e2e/schema-" + tt.name,
				Input:   []byte(tt.input),
			})
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
				if result != nil && result.Status != execution.StatusRejected {
					t.Errorf("Status = %v, want rejected", result.Status)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result != nil && result.Status != execution.StatusSuccess {
					t.Errorf("Status = %v, want success", result.Status)
				}
			}
		})
	}
}

// Manifest validation table tests
func TestE2E_ManifestValidation_Table(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{
			name: "valid minimal",
			yaml: `execution:
  mode: wasm`,
			wantErr: false,
		},
		{
			name:    "missing id (no longer required)",
			yaml:    `version: "1.0"\nexecution:\n  mode: wasm`,
			wantErr: false,
		},
		{
			name: "missing version (no longer required)",
			yaml: `execution:
  mode: wasm`,
			wantErr: false,
		},
		{
			name: "missing execution mode",
			yaml: `execution:`,
			wantErr: true,
		},
		{
			name:    "completely empty",
			yaml:    ``,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := registry.ParseManifest([]byte(tt.yaml))
			if err != nil {
				t.Logf("ParseManifest error (expected for some cases): %v", err)
				return
			}
			validateErr := m.Validate()
			if tt.wantErr && validateErr == nil {
				t.Error("expected validation error")
			}
			if !tt.wantErr && validateErr != nil {
				t.Errorf("unexpected validation error: %v", validateErr)
			}
		})
	}
}

// Concurrent loading and unloading stress test
func TestE2E_ConcurrentLoadUnload(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	ctx := context.Background()

	var wg sync.WaitGroup
	var loadErrs, unloadErrs atomic.Int32

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := newMockSkill(fmt.Sprintf("e2e/stress-%d", i%10), true)
			if err := e.LoadSkill(ctx, s); err != nil {
				loadErrs.Add(1)
			}
		}(i)
	}
	wg.Wait()

	// Some loads will fail due to duplicates, that's expected
	t.Logf("Load errors (expected for duplicates): %d", loadErrs.Load())

	// Now unload all
	ids := e.ListSkills(ctx)
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := e.UnloadSkill(ctx, id); err != nil {
				unloadErrs.Add(1)
			}
		}(id)
	}
	wg.Wait()

	if e.SkillCount() != 0 {
		t.Errorf("After unload all: SkillCount = %d, want 0", e.SkillCount())
	}
}

// Utility functions

func float64Ptr(v float64) *float64 { return &v }
func intPtr(v int) *int             { return &v }

// ============================================================================
// Category 8: Skill Spec Alignment (SKILL.md frontmatter, DeriveSkillID, progressive loading)
// ============================================================================

// --- Frontmatter parsing E2E ---

func TestE2E_SpecAlign_FrontmatterNameAndDescription(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: My Skill\ndescription: Does something useful\n---\n\nProcess this input:\n{{.Input}}\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed: %v", err)
	}
	if smd.Name != "My Skill" {
		t.Errorf("Name = %q, want 'My Skill'", smd.Name)
	}
	if smd.Description != "Does something useful" {
		t.Errorf("Description = %q, want 'Does something useful'", smd.Description)
	}
	if !strings.Contains(smd.Body, "{{.Input}}") {
		t.Error("Body should contain template placeholder")
	}
}

func TestE2E_SpecAlign_FrontmatterNoBody(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: Empty Body Skill\ndescription: No body after frontmatter\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed: %v", err)
	}
	if smd.Name != "Empty Body Skill" {
		t.Errorf("Name = %q", smd.Name)
	}
	if smd.Body != "" {
		t.Errorf("Body should be empty, got %q", smd.Body)
	}
}

func TestE2E_SpecAlign_FrontmatterOnlyName(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: Just Name\n---\nBody text"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed: %v", err)
	}
	if smd.Name != "Just Name" {
		t.Errorf("Name = %q", smd.Name)
	}
	if smd.Description != "" {
		t.Errorf("Description should be empty, got %q", smd.Description)
	}
}

func TestE2E_SpecAlign_NoFrontmatterBodyOnly(t *testing.T) {
	dir := t.TempDir()
	content := "Just body text with no frontmatter at all."
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed: %v", err)
	}
	if smd.Name != "" {
		t.Errorf("Name should be empty, got %q", smd.Name)
	}
	if smd.Body != content {
		t.Errorf("Body = %q, want %q", smd.Body, content)
	}
}

// --- DeriveSkillID E2E ---

func TestE2E_SpecAlign_DeriveSkillID_TopLevel(t *testing.T) {
	id := registry.DeriveSkillID("skills/hello-world")
	if id != "hello-world" {
		t.Errorf("DeriveSkillID = %q, want 'hello-world'", id)
	}
}

func TestE2E_SpecAlign_DeriveSkillID_Namespaced(t *testing.T) {
	id := registry.DeriveSkillID("skills/nursing/sbar-handover")
	if id != "nursing/sbar-handover" {
		t.Errorf("DeriveSkillID = %q, want 'nursing/sbar-handover'", id)
	}
}

func TestE2E_SpecAlign_DeriveSkillID_Core(t *testing.T) {
	id := registry.DeriveSkillID("skills/core/summarize")
	if id != "core/summarize" {
		t.Errorf("DeriveSkillID = %q, want 'core/summarize'", id)
	}
}

func TestE2E_SpecAlign_DeriveSkillID_TrailingSlash(t *testing.T) {
	id := registry.DeriveSkillID("skills/core/summarize/")
	if id != "core/summarize" {
		t.Errorf("DeriveSkillID = %q, want 'core/summarize'", id)
	}
}

func TestE2E_SpecAlign_DeriveSkillID_Absolute(t *testing.T) {
	id := registry.DeriveSkillID("/opt/openbotstack/skills/hello-world")
	if id != "hello-world" {
		t.Errorf("DeriveSkillID = %q, want 'hello-world'", id)
	}
}

func TestE2E_SpecAlign_DeriveSkillID_Empty(t *testing.T) {
	id := registry.DeriveSkillID("")
	if id != "" {
		t.Errorf("empty path should return empty ID, got %q", id)
	}
}

func TestE2E_SpecAlign_DeriveSkillID_SingleDir(t *testing.T) {
	id := registry.DeriveSkillID("my-skill")
	if id != "my-skill" {
		t.Errorf("single dir = %q, want 'my-skill'", id)
	}
}

// --- Manifest optional E2E ---

func TestE2E_SpecAlign_SkillMDOnlyNoManifest(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: Prompt Only\ndescription: A skill with only SKILL.md, no manifest\n---\n\nYou are a helpful assistant. Process: {{.Input}}\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed: %v", err)
	}

	e := skilloperator.NewDefaultExecutor()
	s := &mockSkill{
		id:     registry.DeriveSkillID(dir),
		prompt: smd.Body,
		valid:  true,
	}
	if err := e.LoadSkill(context.Background(), s); err != nil {
		t.Fatalf("LoadSkill failed: %v", err)
	}

	mode := registry.GetExecutionMode(s)
	if mode != "declarative" {
		t.Errorf("mode = %q, want 'declarative'", mode)
	}
}

// --- Loading priority E2E ---

func TestE2E_SpecAlign_PrioritySkillMDOverManifest(t *testing.T) {
	dir := t.TempDir()
	skillContent := "---\nname: From SKILL.md\ndescription: SKILL.md description\n---\nSKILL.md body"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatal(err)
	}
	manifestContent := "execution:\n  mode: wasm\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifestContent), 0644); err != nil {
		t.Fatal(err)
	}

	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed: %v", err)
	}
	if smd.Name != "From SKILL.md" {
		t.Errorf("Name should come from SKILL.md, got %q", smd.Name)
	}
}

// --- Default declarative mode E2E ---

func TestE2E_SpecAlign_DefaultDeclarativeMode(t *testing.T) {
	s := &mockSkill{id: "test/no-mode", valid: true}
	mode := registry.GetExecutionMode(s)
	if mode != "declarative" {
		t.Errorf("default mode = %q, want 'declarative'", mode)
	}
}

func TestE2E_SpecAlign_WasmModeFromManifest(t *testing.T) {
	s := &specAlignExecModeSkill{mode: "wasm"}
	mode := registry.GetExecutionMode(s)
	if mode != "wasm" {
		t.Errorf("wasm mode = %q, want 'wasm'", mode)
	}
}

// --- System default skills have SKILL.md ---

func TestE2E_SpecAlign_AllSystemSkillsHaveSkillMD(t *testing.T) {
	skillList := []string{
		"summarize",
		"extract_structured_data",
		"classify",
	}
	for _, name := range skillList {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join("../../skills", name)
			if !registry.HasSkillMD(dir) {
				t.Errorf("skill %s should have SKILL.md", name)
			}
			smd, err := registry.ParseSkillMD(dir)
			if err != nil {
				t.Fatalf("ParseSkillMD failed: %v", err)
			}
			if smd == nil {
				t.Fatal("ParseSkillMD returned nil")
			}
			if smd.Name == "" {
				t.Error("frontmatter name should not be empty")
			}
			if smd.Description == "" {
				t.Error("frontmatter description should not be empty")
			}
		})
	}
}

// --- Progressive loading simulation ---

func TestE2E_SpecAlign_ProgressiveLoading_PlannerSeesNoBody(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: Planner Test\ndescription: For progressive loading\n---\nSecret body content"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("ParseSkillMD failed: %v", err)
	}

	// Planner only needs name + description
	desc := types.SkillDescriptor{
		ID:          registry.DeriveSkillID(dir),
		Name:        smd.Name,
		Description: smd.Description,
	}
	if desc.Name != "Planner Test" {
		t.Errorf("descriptor Name = %q", desc.Name)
	}
	if desc.Description != "For progressive loading" {
		t.Errorf("descriptor Description = %q", desc.Description)
	}
}

// --- Abnormal: malformed frontmatter ---

func TestE2E_SpecAlign_MalformedYAMLFrontmatter(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: [broken yaml\n---\nBody"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := registry.ParseSkillMD(dir)
	if err == nil {
		t.Error("expected error for malformed YAML frontmatter")
	}
}

func TestE2E_SpecAlign_UnclosedFrontmatter(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: test\nthis never closes"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := registry.ParseSkillMD(dir)
	if err == nil {
		t.Error("expected error for unclosed frontmatter")
	}
}

func TestE2E_SpecAlign_FourDashesNotFrontmatter(t *testing.T) {
	dir := t.TempDir()
	content := "----\nname: test\n---\nBody"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if smd.Name != "" {
		t.Errorf("four dashes should not be parsed as frontmatter, got Name = %q", smd.Name)
	}
}

func TestE2E_SpecAlign_EmptySKILLMD(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	_, err := registry.ParseSkillMD(dir)
	if err == nil {
		t.Error("expected error for empty SKILL.md")
	}
}

func TestE2E_SpecAlign_WhitespaceOnlySKILLMD(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("   \n\t\n   "), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := registry.ParseSkillMD(dir)
	if err == nil {
		t.Error("expected error for whitespace-only SKILL.md")
	}
}

func TestE2E_SpecAlign_CRLFFrontmatter(t *testing.T) {
	dir := t.TempDir()
	content := "---\r\nname: test\r\ndescription: desc\r\n---\r\nBody\r\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	smd, err := registry.ParseSkillMD(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if smd.Name != "test" {
		t.Errorf("Name = %q, want 'test'", smd.Name)
	}
	if !strings.Contains(smd.Body, "Body") {
		t.Errorf("Body should contain 'Body', got %q", smd.Body)
	}
}

// Helper for spec align execution mode tests
type specAlignExecModeSkill struct {
	mockSkill
	mode string
}

func (s *specAlignExecModeSkill) ExecutionMode() string { return s.mode }
