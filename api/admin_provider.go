package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

func (ar *AdminRouter) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	if ar.providerLister == nil {
		writeJSON(w, http.StatusOK, []ProviderInfo{})
		return
	}

	providers := ar.providerLister.ListProviders()
	if providers == nil {
		providers = []ProviderInfo{}
	}
	writeJSON(w, http.StatusOK, providers)
}

// validProviders lists provider names accepted by the admin API.
var validProviders = map[string]bool{
	"openai":      true,
	"modelscope":  true,
	"siliconflow": true,
	"claude":      true,
}

// handleProviderConfig handles GET (read) and PUT (update) for provider configuration.
func (ar *AdminRouter) handleProviderConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ar.getProviderConfig(w, r)
	case http.MethodPut:
		ar.updateProviderConfig(w, r)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
	}
}

func (ar *AdminRouter) getProviderConfig(w http.ResponseWriter, r *http.Request) {
	// Read all provider configs from SQLite
	rows, err := ar.db.Query("SELECT provider_name, base_url, api_key, model, is_default FROM provider_config")
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to read provider config")
		return
	}
	defer func() { _ = rows.Close() }()

	providers := make(map[string]interface{})
	defaultProvider := ""

	for rows.Next() {
		var name, baseURL, apiKey, model string
		var isDefault int
		if err := rows.Scan(&name, &baseURL, &apiKey, &model, &isDefault); err != nil {
			continue
		}
		providers[name] = ProviderConfigEntry{
			Name:      name,
			BaseURL:   baseURL,
			APIKeySet: apiKey != "",
			Model:     model,
			IsDefault: isDefault == 1,
		}
		if isDefault == 1 {
			defaultProvider = name
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"default":   defaultProvider,
		"providers": providers,
	})
}

func (ar *AdminRouter) updateProviderConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider  string `json:"provider"`
		BaseURL   string `json:"base_url"`
		APIKey    string `json:"api_key"`
		Model     string `json:"model"`
		IsDefault string `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request")
		return
	}

	// Validation
	if req.Provider == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "provider is required")
		return
	}
	if !validProviders[req.Provider] {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "unsupported provider (use: openai, modelscope, siliconflow, claude)")
		return
	}
	if req.Model == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "model is required")
		return
	}

	// Preserve existing base_url if not provided in the update request
	if req.BaseURL == "" {
		var existingURL string
		err := ar.db.QueryRow("SELECT base_url FROM provider_config WHERE provider_name = ?", req.Provider).Scan(&existingURL)
		if err == nil && existingURL != "" {
			req.BaseURL = existingURL
		} else {
			switch req.Provider {
			case "openai":
				req.BaseURL = "https://api.openai.com/v1"
			case "modelscope":
				req.BaseURL = "https://api-inference.modelscope.cn/v1"
			case "siliconflow":
				req.BaseURL = "https://api.siliconflow.cn/v1"
			case "claude":
				req.BaseURL = "https://api.anthropic.com/v1"
			}
		}
	}

	// Parse is_default
	isDefault := req.IsDefault == "true"

	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Use transaction to atomically clear defaults + upsert
	tx, err := ar.db.Begin()
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to begin transaction")
		return
	}
	defer tx.Rollback()

	// If setting as default, clear other defaults first
	if isDefault {
		if _, err := tx.Exec("UPDATE provider_config SET is_default = 0 WHERE is_default = 1"); err != nil {
			slog.ErrorContext(r.Context(), "admin handler error",
				"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
			writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to update provider config")
			return
		}
	}

	// Upsert provider config
	// If api_key is empty, preserve the existing one
	var existingKey string
	_ = tx.QueryRow("SELECT api_key FROM provider_config WHERE provider_name = ?", req.Provider).Scan(&existingKey)

	apiKey := req.APIKey
	if apiKey == "" {
		apiKey = existingKey
	}

	isDefaultInt := 0
	if isDefault {
		isDefaultInt = 1
	}

	_, err = tx.Exec(`INSERT INTO provider_config (provider_name, base_url, api_key, model, is_default, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider_name) DO UPDATE SET base_url = ?, api_key = ?, model = ?, is_default = ?, updated_at = ?`,
		req.Provider, req.BaseURL, apiKey, req.Model, isDefaultInt, now,
		req.BaseURL, apiKey, req.Model, isDefaultInt, now)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to save provider config")
		return
	}

	if err := tx.Commit(); err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to commit transaction")
		return
	}

	// Hot-reload provider if reloader is available and we have an API key
	if ar.providerReloader != nil && apiKey != "" {
		if err := ar.providerReloader.ReloadProvider(req.Provider, req.BaseURL, apiKey, req.Model); err != nil {
			slog.ErrorContext(r.Context(), "provider hot-reload failed",
				"provider", req.Provider, "error", err)
			// Still return success for the config save, but log the reload failure
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"provider": req.Provider,
		"saved":    true,
	})
}

// handleProviderTest tests connectivity to a provider endpoint.
func (ar *AdminRouter) handleProviderTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Provider string `json:"provider"`
		BaseURL  string `json:"base_url"`
		APIKey   string `json:"api_key"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request")
		return
	}

	if req.Provider == "" || req.APIKey == "" || req.Model == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "provider, api_key, and model are required")
		return
	}

	// Use stored base_url if not provided
	if req.BaseURL == "" {
		_ = ar.db.QueryRow("SELECT base_url FROM provider_config WHERE provider_name = ?", req.Provider).Scan(&req.BaseURL)
		if req.BaseURL == "" {
			switch req.Provider {
			case "openai":
				req.BaseURL = "https://api.openai.com/v1"
			case "modelscope":
				req.BaseURL = "https://api-inference.modelscope.cn/v1"
			case "siliconflow":
				req.BaseURL = "https://api.siliconflow.cn/v1"
			case "claude":
				req.BaseURL = "https://api.anthropic.com/v1"
			default:
				writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "unknown provider")
				return
			}
		}
	}

	// Test by making a minimal models list request
	endpoint := strings.TrimRight(req.BaseURL, "/") + "/models"
	start := time.Now()

	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false, "message": fmt.Sprintf("failed to create request: %v", err),
		})
		return
	}
	httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	latency := time.Since(start)

	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":    false,
			"message":    fmt.Sprintf("connection failed: %v", err),
			"latency_ms": latency.Milliseconds(),
		})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":    true,
			"message":    "connection successful",
			"latency_ms": latency.Milliseconds(),
		})
	} else {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":    false,
			"message":    fmt.Sprintf("server returned status %d", resp.StatusCode),
			"latency_ms": latency.Milliseconds(),
		})
	}
}

// handleAdminSkills returns all loaded skills with their enabled status.
