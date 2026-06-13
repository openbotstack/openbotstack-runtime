package skillutil

import (
	"path/filepath"
	"testing"
)

// TestResolveSkillDir pins the mapping from a filesystem event path to the
// skill directory it belongs to. This is the core decision logic that drives
// hot-reload — getting it wrong reloads the wrong skill (or none).
func TestResolveSkillDir(t *testing.T) {
	root := "/data/skills"
	w := &SkillWatcher{skillsDir: root}

	cases := []struct {
		name string
		path string
		want string
	}{
		{"file inside skill dir", filepath.Join(root, "foo", "SKILL.md"), filepath.Join(root, "foo")},
		{"nested file maps to first component", filepath.Join(root, "foo", "sub", "x.yaml"), filepath.Join(root, "foo")},
		{"skill dir itself", filepath.Join(root, "foo"), filepath.Join(root, "foo")},
		{"watched root returns empty", root, ""},
		{"sibling outside root returns empty", "/other/place/foo", ""},
		{"parent escape returns empty", root + "/../other", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := w.resolveSkillDir(c.path); got != c.want {
				t.Errorf("resolveSkillDir(%q) = %q, want %q", c.path, got, c.want)
			}
		})
	}
}
