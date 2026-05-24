package jsonrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// StdioTransport communicates with an MCP server via subprocess stdin/stdout.
type StdioTransport struct {
	cmd      *exec.Cmd
	stdin    *bufio.Writer
	stdinRaw io.WriteCloser
	stdout   *bufio.Reader
	mu       sync.Mutex
}

// NewStdioTransport launches a subprocess and establishes stdin/stdout pipes
// for JSON-RPC communication.
func NewStdioTransport(command string, args []string, env map[string]string) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	stdinRaw, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutRaw, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start process: %w", err)
	}

	return &StdioTransport{
		cmd:      cmd,
		stdinRaw: stdinRaw,
		stdin:    bufio.NewWriter(stdinRaw),
		stdout:   bufio.NewReader(stdoutRaw),
	}, nil
}

// Send writes a JSON-RPC request to stdin and reads the response from stdout.
// Respects context cancellation for timeout control.
func (t *StdioTransport) Send(ctx context.Context, request json.RawMessage) (json.RawMessage, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, err := t.stdin.Write(append(request, '\n')); err != nil {
		return nil, fmt.Errorf("write to stdin: %w", err)
	}
	if err := t.stdin.Flush(); err != nil {
		return nil, fmt.Errorf("flush stdin: %w", err)
	}

	// Read response with context awareness
	type readResult struct {
		data json.RawMessage
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		line, err := t.stdout.ReadBytes('\n')
		if err != nil {
			ch <- readResult{nil, fmt.Errorf("read from stdout: %w", err)}
			return
		}
		ch <- readResult{json.RawMessage(line), nil}
	}()

	select {
	case <-ctx.Done():
		// Process may be stuck; kill it to unblock the goroutine
		_ = t.cmd.Process.Kill()
		return nil, fmt.Errorf("read timeout: %w", ctx.Err())
	case result := <-ch:
		return result.data, result.err
	}
}

// SendNotification writes a notification to stdin without reading a response.
// Notifications in JSON-RPC are one-way messages that do not produce a reply.
func (t *StdioTransport) SendNotification(request json.RawMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, err := t.stdin.Write(append(request, '\n')); err != nil {
		return fmt.Errorf("write notification to stdin: %w", err)
	}
	return t.stdin.Flush()
}

// Close shuts down the subprocess.
func (t *StdioTransport) Close() error {
	_ = t.stdin.Flush()
	_ = t.stdinRaw.Close()
	return t.cmd.Wait()
}
