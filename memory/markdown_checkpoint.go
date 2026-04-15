package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/openbotstack/openbotstack-runtime/loop"
)

// MarkdownCheckpoint implements loop.Checkpoint using markdown files.
// Each task gets its own file: {dataDir}/checkpoints/task_{index}.md
// The latest state is also mirrored to latest.md for quick access.
type MarkdownCheckpoint struct {
	dataDir string
}

// NewMarkdownCheckpoint creates a checkpoint store under the given data directory.
func NewMarkdownCheckpoint(dataDir string) *MarkdownCheckpoint {
	return &MarkdownCheckpoint{dataDir: dataDir}
}

// Save persists the task execution state as a markdown file.
func (c *MarkdownCheckpoint) Save(ctx context.Context, taskIndex int, taskResult *loop.TaskResult, metrics *loop.LoopMetrics) error {
	dir := filepath.Join(c.dataDir, "checkpoints")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("checkpoint: failed to create directory: %w", err)
	}

	content := c.formatCheckpoint(taskIndex, taskResult, metrics)

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

func (c *MarkdownCheckpoint) formatCheckpoint(taskIndex int, taskResult *loop.TaskResult, metrics *loop.LoopMetrics) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "task_index: %d\n", taskIndex)
	if metrics != nil {
		fmt.Fprintf(&sb, "workflow_steps: %d\n", metrics.WorkflowSteps)
		fmt.Fprintf(&sb, "total_turns: %d\n", metrics.TotalTurns)
		fmt.Fprintf(&sb, "total_tool_calls: %d\n", metrics.TotalToolCalls)
		fmt.Fprintf(&sb, "total_runtime_ms: %d\n", metrics.TotalRuntime.Milliseconds())
	}
	sb.WriteString("status: \"in_progress\"\n")
	sb.WriteString("---\n\n")

	if taskResult != nil {
		fmt.Fprintf(&sb, "## Task %d Result\n\n", taskIndex)
		fmt.Fprintf(&sb, "- Stop reason: %s\n", taskResult.StopReason)
		fmt.Fprintf(&sb, "- Turn count: %d\n", taskResult.TurnCount)
		fmt.Fprintf(&sb, "- Tool calls: %d\n", taskResult.ToolCallsUsed)
		fmt.Fprintf(&sb, "- Duration: %s\n", taskResult.Duration)
		if taskResult.Error != nil {
			fmt.Fprintf(&sb, "- Error: %q\n", taskResult.Error.Error())
		}
		if taskResult.FinalOutput != nil {
			output := fmt.Sprintf("%v", taskResult.FinalOutput)
			if len(output) > 1000 {
				output = output[:1000] + "..."
			}
			fmt.Fprintf(&sb, "\n### Output\n\n```\n%s\n```\n", output)
		}
	}

	return sb.String()
}
