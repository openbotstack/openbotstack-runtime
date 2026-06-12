// Package integration provides full system integration tests.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-runtime/persistence"
)

const (
	binaryPath = "../build/openbotstack"
	repoRoot   = ".."
)

// serverURL is set dynamically by TestFullSystem after allocating a free port.
// Other tests (streaming, error) read this variable to know where the server is.
var serverURL string

func buildBinary(t *testing.T) string {
	t.Helper()
	absBinaryPath, _ := filepath.Abs(binaryPath)
	absRepoRoot, _ := filepath.Abs(repoRoot)

	// Try existing binary first
	if info, err := os.Stat(absBinaryPath); err == nil && !info.IsDir() {
		return absBinaryPath
	}

	// Build on demand
	t.Log("Building binary for integration test...")
	buildDir := filepath.Dir(absBinaryPath)
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	buildCmd := exec.Command("go", "build", "-o", absBinaryPath, "./cmd/openbotstack")
	buildCmd.Dir = absRepoRoot
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}
	return absBinaryPath
}

func TestFullSystem(t *testing.T) {
	// 1. Setup: Ensure binary exists (auto-build if needed)
	absBinaryPath := buildBinary(t)

	// 1b. Allocate a free port dynamically to avoid conflicts
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to allocate a free port: %v", err)
	}
	addr := listener.Addr().(*net.TCPAddr)
	_ = listener.Close() // free the port so the server can bind it

	serverURL = fmt.Sprintf("http://127.0.0.1:%d", addr.Port)

	// 2. Mock LLM Server
	mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}

		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}

		// Extract prompt
		lastMsg := ""
		if len(req.Messages) > 0 {
			lastMsg = req.Messages[len(req.Messages)-1].Content
		}

		// Extract user request part only to avoid matching keywords in skill descriptions.
		// Planner format: "<user_request>\n<msg>\n</user_request>"
		var userRequest string
		if startTag := strings.Index(lastMsg, "<user_request>"); startTag != -1 {
			contentStart := startTag + len("<user_request>")
			remainder := lastMsg[contentStart:]
			if endTag := strings.Index(remainder, "</user_request>"); endTag != -1 {
				userRequest = strings.TrimSpace(remainder[:endTag])
			} else {
				userRequest = remainder
			}
		} else if idx := strings.Index(lastMsg, "User request:"); idx != -1 {
			contentStart := idx + len("User request:")
			remainder := lastMsg[contentStart:]
			if endIdx := strings.Index(remainder, "\n\n"); endIdx != -1 {
				userRequest = remainder[:endIdx]
			} else {
				userRequest = remainder
			}
		} else {
			userRequest = lastMsg
		}

		fmt.Printf("MOCK LLM PARSED REQUEST: %q\n", userRequest)
		msgLower := strings.ToLower(userRequest)

		// Return execution.ExecutionPlan format (steps array)
		var plan execution.ExecutionPlan
		makeStep := func(name string, args map[string]any) execution.ExecutionStep {
			return execution.ExecutionStep{
				Name: name, Type: execution.StepTypeSkill, Arguments: args,
			}
		}

		if strings.Contains(msgLower, "summarize") {
			plan = execution.ExecutionPlan{
				AssistantID: "default",
				Steps:       []execution.ExecutionStep{makeStep("summarize", map[string]any{"text": "some text", "max_length": 100})},
				Reasoning:   "User wants summary",
			}
		} else if strings.Contains(msgLower, "tax") {
			plan = execution.ExecutionPlan{
				AssistantID: "default",
				Steps:       []execution.ExecutionStep{makeStep("classify", map[string]any{"text": "tax related text", "categories": []string{"tax", "finance", "legal"}})},
				Reasoning:   "User wants classification",
			}
		} else {
			plan = execution.ExecutionPlan{
				AssistantID: "default",
				Steps:       []execution.ExecutionStep{makeStep("summarize", map[string]any{"text": "fallback text", "max_length": 50})},
				Reasoning:   "Fallback plan",
			}
		}

		planJSON, _ := json.Marshal(plan)

		resp := map[string]any{
			"id":      "chatcmpl-mock",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "gpt-4o",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": string(planJSON),
					},
					"finish_reason": "stop",
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockLLM.Close() //nolint:errcheck // test cleanup

	// 3. Start Server with Env Vars pointing to Mock LLM
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	absRepoRoot, _ := filepath.Abs(repoRoot)

	// Pre-seed the SQLite DB with the mock provider. Provider config is runtime
	// state loaded from provider_config at startup (InitAI/loadProvidersFromDB),
	// not from config.yaml or env vars. Create+migrate the DB, insert the provider
	// row pointing at the mock LLM, then close it so the server opens it fresh.
	dbPath := filepath.Join(t.TempDir(), "openbotstack.db")
	seedDB, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("Open seed DB: %v", err)
	}
	if err := seedDB.Migrate(); err != nil {
		t.Fatalf("Migrate seed DB: %v", err)
	}
	if _, err := seedDB.Exec(`INSERT INTO provider_config (id, provider, name, base_url, api_key, model, is_default, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, '')`,
		"seed-openai", "openai", "openai", mockLLM.URL, "dummy-key", "gpt-4o", 1); err != nil {
		t.Fatalf("insert provider seed row: %v", err)
	}
	_ = seedDB.Close()

	// Minimal config — provider config lives in the DB now, not config.yaml.
	tmpConfig, err := os.CreateTemp("", "obs-test-config-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp config: %v", err)
	}
	defer os.Remove(tmpConfig.Name())
	configContent := fmt.Sprintf(`
server:
  addr: ":%d"
memory:
  data_dir: "%s/data"
`, addr.Port, absRepoRoot)
	if _, err := tmpConfig.WriteString(configContent); err != nil {
		t.Fatalf("Failed to write temp config: %v", err)
	}
	tmpConfig.Close()

	cmd := exec.CommandContext(ctx, absBinaryPath, "--config", tmpConfig.Name(), "--addr=:"+fmt.Sprintf("%d", addr.Port))
	cmd.Dir = absRepoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	env := os.Environ()
	env = append(env, "OBS_DATABASE_PATH="+dbPath)
	cmd.Env = env

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	defer func() {
		cancel()
		_ = cmd.Wait()
	}()

	// 4. Wait for Healthy
	if !waitForHealth(t, 10*time.Second) {
		t.Fatal("Server did not become healthy")
	}

	// 5. Run Test Cases
	runTestCases(t)
}

func waitForHealth(t *testing.T, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(serverURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

type chatRequest struct {
	TenantID  string `json:"tenant_id"`
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

type chatResponse struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
	SkillUsed string `json:"skill_used"`
}

func runTestCases(t *testing.T) {
	tests := []struct {
		name           string
		req            chatRequest
		wantStatus     int
		wantSkill      string
		wantMsgContent string
	}{
		{
			name: "1. Basic Chat (Fallback)",
			req: chatRequest{
				TenantID: "t1", UserID: "u1", SessionID: "s1",
				Message: "Hello world",
			},
			wantStatus: 200,
			wantSkill:  "summarize",
		},
		{
			name: "2. Summarize Intent",
			req: chatRequest{
				TenantID: "t1", UserID: "u1", SessionID: "s1",
				Message: "Please summarize this",
			},
			wantStatus: 200,
			wantSkill:  "summarize",
		},
		{
			name: "3. Classify Intent",
			req: chatRequest{
				TenantID: "t1", UserID: "u1", SessionID: "s1",
				Message: "Calculate tax",
			},
			wantStatus: 200,
			wantSkill:  "classify",
		},
		{
			name:       "4. UI Redirect",
			req:        chatRequest{},
			wantStatus: 302,
		},
	}

	for _, tt := range tests {
		if tt.name == "4. UI Redirect" {
			client := &http.Client{
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}
			resp, err := client.Get(serverURL + "/")
			if err != nil {
				t.Errorf("UI request failed: %v", err)
				continue
			}
			defer resp.Body.Close() //nolint:errcheck // test cleanup
			if resp.StatusCode != http.StatusFound {
				t.Errorf("Root should redirect, got %d", resp.StatusCode)
			}
			continue
		}

		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.req)
			resp, err := http.Post(serverURL+"/v1/chat", "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer resp.Body.Close() //nolint:errcheck // test cleanup

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("Status code = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			if tt.wantStatus == 200 {
				var chatResp chatResponse
				bodyBytes, _ := io.ReadAll(resp.Body)
				if err := json.Unmarshal(bodyBytes, &chatResp); err != nil {
					t.Logf("Failed to decode response: %s", string(bodyBytes))
					return
				}

				if tt.wantSkill != "" && !strings.Contains(chatResp.SkillUsed, tt.wantSkill) {
					t.Errorf("SkillUsed = %s, want substring %s", chatResp.SkillUsed, tt.wantSkill)
				}
			}
		})
	}
}
