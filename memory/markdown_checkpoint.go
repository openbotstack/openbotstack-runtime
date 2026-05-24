package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-runtime/harness"
)

// MarkdownCheckpoint saves execution state as markdown files.
// Each task gets its own file: {dataDir}/checkpoints/task_{index}.md
// The latest state is also mirrored to latest.md for quick access.
type MarkdownCheckpoint struct {
	dataDir string
}

// NewMarkdownCheckpoint creates a checkpoint store under the given data directory.
func NewMarkdownCheckpoint(dataDir string) *MarkdownCheckpoint {
	return &MarkdownCheckpoint{dataDir: dataDir}
}

// Save persists the harness execution state as a markdown file.
func (c *MarkdownCheckpoint) Save(ctx context.Context, taskIndex int, stepResults []execution.StepResult, metrics *harness.HarnessMetrics) error {
	dir := filepath.Join(c.dataDir, "checkpoints")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("checkpoint: failed to create directory: %w", err)
	}

	content := c.formatCheckpoint(taskIndex, stepResults, metrics)

	// Write per-task file and mirror to latest
	taskPath := filepath.Join(dir, fmt.Sprintf("task_%d.md", taskIndex))
	if err := os.WriteFile(taskPath, []byte(content), 0600); err != nil {
		return fmt.Errorf("checkpoint: failed to write task file: %w", err)
	}

	latestPath := filepath.Join(dir, "latest.md")
	if err := os.WriteFile(latestPath, []byte(content), 0600); err != nil {
		return fmt.Errorf("checkpoint: failed to write latest file: %w", err)
	}

	return nil
}

func (c *MarkdownCheckpoint) formatCheckpoint(taskIndex int, stepResults []execution.StepResult, metrics *harness.HarnessMetrics) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "task_index: %d\n", taskIndex)
	if metrics != nil {
		fmt.Fprintf(&sb, "total_steps: %d\n", metrics.TotalSteps)
		fmt.Fprintf(&sb, "total_llm_turns: %d\n", metrics.TotalLLMTurns)
		fmt.Fprintf(&sb, "total_tool_calls: %d\n", metrics.TotalToolCalls)
		fmt.Fprintf(&sb, "total_runtime_ms: %d\n", metrics.TotalRuntime.Milliseconds())
	}
	sb.WriteString("status: \"in_progress\"\n")
	sb.WriteString("---\n\n")

	if len(stepResults) > 0 {
		fmt.Fprintf(&sb, "## Task %d Result\n\n", taskIndex)
		for _, sr := range stepResults {
			fmt.Fprintf(&sb, "### Step: %s (%s)\n", sr.StepName, sr.Type)
			fmt.Fprintf(&sb, "- Duration: %s\n", sr.Duration)
			if sr.Error != nil {
				fmt.Fprintf(&sb, "- Error: %q\n", sr.Error.Error())
			}
			if sr.Retries > 0 {
				fmt.Fprintf(&sb, "- Retries: %d\n", sr.Retries)
			}
			if sr.Output != nil {
				output := fmt.Sprintf("%v", sr.Output)
				if len(output) > 500 {
					output = output[:500] + "..."
				}
				fmt.Fprintf(&sb, "\n```\n%s\n```\n", output)
			}
		}
	}

	return sb.String()
}
