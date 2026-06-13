package skillutil

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	registry "github.com/openbotstack/openbotstack-core/registry/skills"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
)

// TestLoadSkillFromDir_Declarative pins the disk-loading path: a declarative
// skill is parsed from SKILL.md, registered, and given the default 30s timeout
// when the manifest omits one.
func TestLoadSkillFromDir_Declarative(t *testing.T) {
	skillDir := writeSkill(t, "my-skill", "---\nname: My Skill\ndescription: a test skill\n---\n\nYou do X.\n", "")

	exec := executor.NewDefaultExecutor()
	id, err := LoadSkillFromDir(context.Background(), exec, skillDir)
	if err != nil {
		t.Fatalf("LoadSkillFromDir: %v", err)
	}

	wantID := registry.DeriveSkillID(skillDir)
	if id != wantID {
		t.Errorf("skill ID = %q, want %q", id, wantID)
	}
	if got := exec.List(); len(got) != 1 || got[0] != wantID {
		t.Errorf("executor.List() = %v, want [%s]", got, wantID)
	}
	loaded, err := exec.GetSkill(context.Background(), wantID)
	if err != nil || loaded == nil {
		t.Fatalf("GetSkill: %v", err)
	}
	if loaded.Timeout() != 30*time.Second {
		t.Errorf("default Timeout = %v, want 30s", loaded.Timeout())
	}
}

// TestLoadSkillFromDir_ManifestTimeout pins that a manifest-declared timeout
// overrides the default.
func TestLoadSkillFromDir_ManifestTimeout(t *testing.T) {
	skillDir := writeSkill(t, "timed",
		"---\nname: T\ndescription: t\n---\n\nbody\n",
		"id: timed\nname: T\nexecution:\n  mode: declarative\ntimeout: 10s\n")

	exec := executor.NewDefaultExecutor()
	id, err := LoadSkillFromDir(context.Background(), exec, skillDir)
	if err != nil {
		t.Fatalf("LoadSkillFromDir: %v", err)
	}
	loaded, _ := exec.GetSkill(context.Background(), id)
	if loaded.Timeout() != 10*time.Second {
		t.Errorf("Timeout = %v, want 10s", loaded.Timeout())
	}
}

// writeSkill creates a temp skill directory with SKILL.md and an optional manifest.
func writeSkill(t *testing.T, name, skillMD, manifest string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0644); err != nil {
		t.Fatal(err)
	}
	if manifest != "" {
		if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
