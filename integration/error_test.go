package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func isServerHealthy() bool {
	resp, err := http.Get(serverURL + "/health")
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}

func TestErrorResponses_JSONFormat(t *testing.T) {
	if !isServerHealthy() {
		t.Skip("Server not running. Run TestFullSystem first.")
	}

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "chat method not allowed",
			method:     http.MethodGet,
			path:       "/v1/chat",
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   "METHOD_NOT_ALLOWED",
		},
		{
			name:       "chat invalid body",
			method:     http.MethodPost,
			path:       "/v1/chat",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_REQUEST",
		},
		{
			name:       "stream method not allowed",
			method:     http.MethodGet,
			path:       "/v1/chat/stream",
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   "METHOD_NOT_ALLOWED",
		},
		{
			name:       "stream invalid body",
			method:     http.MethodPost,
			path:       "/v1/chat/stream",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
			wantCode:   "INVALID_REQUEST",
		},
		{
			name:       "session not found",
			method:     http.MethodGet,
			path:       "/v1/sessions/bad/not-history",
			wantStatus: http.StatusNotFound,
			wantCode:   "NOT_FOUND",
		},
		{
			name:       "skills method not allowed",
			method:     http.MethodPost,
			path:       "/v1/skills",
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   "METHOD_NOT_ALLOWED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp *http.Response
			var err error
			if tt.body != "" {
				resp, err = http.Post(serverURL+tt.path, "application/json", strings.NewReader(tt.body))
			} else {
				req, reqErr := http.NewRequest(tt.method, serverURL+tt.path, nil)
				if reqErr != nil {
					t.Fatalf("Failed to create request: %v", reqErr)
				}
				resp, err = http.DefaultClient.Do(req)
			}
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			ct := resp.Header.Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}

			var apiErr apiError
			if err := json.NewDecoder(resp.Body).Decode(&apiErr); err != nil {
				t.Fatalf("Failed to decode JSON error: %v", err)
			}

			if apiErr.Error.Code != tt.wantCode {
				t.Errorf("error code = %q, want %q", apiErr.Error.Code, tt.wantCode)
			}
			if apiErr.Error.Message == "" {
				t.Error("error message should not be empty")
			}
		})
	}
}
