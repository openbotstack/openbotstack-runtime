package skillutil

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	registry "github.com/openbotstack/openbotstack-core/registry/skills"
	executor "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
)

// SkillWatcher monitors the skills directory for changes and hot-reloads skills.
type SkillWatcher struct {
	executor  *executor.DefaultExecutor
	skillsDir string
	watcher   *fsnotify.Watcher
	loaded    map[string]string // skillDir → skillID
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	debounce  map[string]*time.Timer
	debMu     sync.Mutex
}

// NewSkillWatcher creates a new filesystem watcher for the skills directory.
func NewSkillWatcher(exec *executor.DefaultExecutor, skillsDir string) *SkillWatcher {
	return &SkillWatcher{
		executor:  exec,
		skillsDir: skillsDir,
		loaded:    make(map[string]string),
		debounce:  make(map[string]*time.Timer),
	}
}

// Start begins watching the skills directory for changes.
func (w *SkillWatcher) Start(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	w.watcher = watcher
	w.ctx, w.cancel = context.WithCancel(ctx)

	if err := watcher.Add(w.skillsDir); err != nil {
		_ = watcher.Close()
		return err
	}

	// Watch existing subdirectories
	entries, err := os.ReadDir(w.skillsDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				subDir := filepath.Join(w.skillsDir, entry.Name())
				if err := watcher.Add(subDir); err != nil {
					slog.Warn("skill watcher: cannot watch subdirectory", "dir", subDir, "error", err)
				}
			}
		}
	}

	w.rebuildLoadedMap(ctx)

	go w.eventLoop()

	slog.Info("skill watcher started", "dir", w.skillsDir)
	return nil
}

// Stop terminates the watcher.
func (w *SkillWatcher) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	if w.watcher != nil {
		_ = w.watcher.Close()
	}
	w.debMu.Lock()
	for _, t := range w.debounce {
		t.Stop()
	}
	w.debounce = make(map[string]*time.Timer)
	w.debMu.Unlock()
	slog.Info("skill watcher stopped")
}

// Rescan performs a full rescan of the skills directory.
func (w *SkillWatcher) Rescan(ctx context.Context) error {
	return LoadSkills(ctx, w.executor, w.skillsDir)
}

// RescanDir reloads a single skill directory.
func (w *SkillWatcher) RescanDir(ctx context.Context, skillDir string) error {
	skillID, err := LoadSkillFromDir(ctx, w.executor, skillDir)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.loaded[skillDir] = skillID
	w.mu.Unlock()
	return nil
}

// ReloadSkillByID reloads a single skill by its ID.
func (w *SkillWatcher) ReloadSkillByID(ctx context.Context, skillID string) error {
	w.mu.RLock()
	var skillDir string
	for dir, id := range w.loaded {
		if id == skillID {
			skillDir = dir
			break
		}
	}
	w.mu.RUnlock()

	if skillDir == "" {
		slog.Warn("skill reload: skill ID not found in watcher", "id", skillID)
		return nil
	}
	newID, err := LoadSkillFromDir(ctx, w.executor, skillDir)
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.loaded[skillDir] = newID
	w.mu.Unlock()
	return nil
}

func (w *SkillWatcher) eventLoop() {
	for {
		select {
		case <-w.ctx.Done():
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			slog.Error("skill watcher error", "error", err)
		}
	}
}

func (w *SkillWatcher) handleEvent(event fsnotify.Event) {
	skillDir := w.resolveSkillDir(event.Name)
	if skillDir == "" {
		return
	}

	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		info, err := os.Stat(event.Name)
		if err != nil {
			return
		}
		if info.IsDir() {
			if err := w.watcher.Add(event.Name); err != nil {
				slog.Warn("skill watcher: cannot watch new subdirectory", "dir", event.Name, "error", err)
			}
			w.scheduleReload(skillDir)
		} else {
			w.scheduleReload(skillDir)
		}

	case event.Op&fsnotify.Write == fsnotify.Write:
		w.scheduleReload(skillDir)

	case event.Op&fsnotify.Rename == fsnotify.Rename:
		// Rename on the source name means the old path no longer exists.
		// Try removal first; if the dir still exists (renamed INTO this path), schedule reload.
		if !dirExists(skillDir) {
			w.handleRemoval(skillDir)
		} else {
			w.scheduleReload(skillDir)
		}

	case event.Op&fsnotify.Remove == fsnotify.Remove:
		if event.Name == skillDir || !dirExists(skillDir) {
			w.handleRemoval(skillDir)
		} else {
			w.scheduleReload(skillDir)
		}
	}
}

// resolveSkillDir returns the skill directory path for a given file event.
// It extracts the first path component under the skills root as the skill directory.
func (w *SkillWatcher) resolveSkillDir(eventPath string) string {
	if eventPath == w.skillsDir {
		return ""
	}

	rel, err := filepath.Rel(w.skillsDir, eventPath)
	if err != nil {
		return ""
	}

	// First component of relative path is the skill directory name
	firstComponent := strings.Split(filepath.ToSlash(rel), "/")[0]
	if firstComponent == "" || firstComponent == "." || firstComponent == ".." {
		return ""
	}

	return filepath.Join(w.skillsDir, firstComponent)
}

func (w *SkillWatcher) scheduleReload(skillDir string) {
	w.debMu.Lock()
	defer w.debMu.Unlock()

	if t, ok := w.debounce[skillDir]; ok {
		t.Stop()
	}

	t := time.AfterFunc(500*time.Millisecond, func() {
		w.debMu.Lock()
		delete(w.debounce, skillDir)
		w.debMu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		skillID, err := LoadSkillFromDir(ctx, w.executor, skillDir)
		if err != nil {
			slog.Warn("skill hot-reload failed", "dir", skillDir, "error", err)
		} else {
			slog.Info("skill hot-reloaded", "dir", skillDir)
			w.mu.Lock()
			w.loaded[skillDir] = skillID
			w.mu.Unlock()
		}
	})
	w.debounce[skillDir] = t
}

func (w *SkillWatcher) handleRemoval(skillDir string) {
	w.mu.Lock()
	skillID := w.loaded[skillDir]
	delete(w.loaded, skillDir)
	w.mu.Unlock()

	// Always try to unload, even if not tracked — the executor may have it
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := UnloadSkillByDir(ctx, w.executor, skillDir); err != nil {
		slog.Debug("skill hot-unload: skill not in executor (expected for untracked dirs)", "dir", skillDir)
	} else {
		slog.Info("skill hot-unloaded", "id", skillID, "dir", skillDir)
	}

	// Remove the fsnotify watch to prevent fd leak
	_ = w.watcher.Remove(skillDir)
}

func (w *SkillWatcher) rebuildLoadedMap(ctx context.Context) {
	entries, err := os.ReadDir(w.skillsDir)
	if err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(w.skillsDir, entry.Name())
		skillID := deriveSkillIDSimple(skillDir)
		if skillID != "" {
			w.loaded[skillDir] = skillID
		}
	}
}

// deriveSkillIDSimple derives a skill ID from a directory path without loading.
func deriveSkillIDSimple(skillDir string) string {
	smd, err := registry.ParseSkillMD(skillDir)
	if err != nil || smd == nil {
		return ""
	}
	return registry.DeriveSkillID(skillDir)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
