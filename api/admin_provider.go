package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/openbotstack/openbotstack-runtime/internal/crypto"
)

func (ar *AdminRouter) handleProviders(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
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

// validProviders lists provider driver names accepted by the admin API.
var validProviders = map[string]bool{
	"openai":      true,
	"modelscope":  true,
	"siliconflow": true,
	"claude":      true,
}

var defaultBaseURLs = map[string]string{
	"openai":      "https://api.openai.com/v1",
	"modelscope":  "https://api-inference.modelscope.cn/v1",
	"siliconflow": "https://api.siliconflow.cn/v1",
	"claude":      "https://api.anthropic.com/v1",
}

// handleProviderConfig handles GET (list), PUT (create/update), and DELETE.
func (ar *AdminRouter) handleProviderConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ar.getProviderConfig(w, r)
	case http.MethodPut:
		ar.updateProviderConfig(w, r)
	case http.MethodDelete:
		ar.deleteProviderConfig(w, r)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
	}
}

func (ar *AdminRouter) getProviderConfig(w http.ResponseWriter, r *http.Request) {
	encKey := crypto.EncryptionKey()
	rows, err := ar.db.Query("SELECT id, provider, name, base_url, api_key, model, is_default FROM provider_config ORDER BY provider, name")
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to read provider config")
		return
	}
	defer func() { _ = rows.Close() }()

	configs := make([]ProviderConfigEntry, 0)

	for rows.Next() {
		var id, provider, name, baseURL, apiKey, model string
		var isDefault int
		if err := rows.Scan(&id, &provider, &name, &baseURL, &apiKey, &model, &isDefault); err != nil {
			continue
		}
		apiKeySet := apiKey != ""
		if encKey != nil && crypto.IsEncrypted(apiKey) {
			dec, err := crypto.Decrypt(encKey, apiKey)
			if err != nil {
				slog.WarnContext(r.Context(), "failed to decrypt stored provider key",
					"id", id, "error", err)
			} else {
				apiKeySet = dec != ""
			}
		}
		configs = append(configs, ProviderConfigEntry{
			ID:        id,
			Provider:  provider,
			Name:      name,
			BaseURL:   baseURL,
			APIKeySet: apiKeySet,
			Model:     model,
			IsDefault: isDefault == 1,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"providers": configs,
	})
}

func (ar *AdminRouter) updateProviderConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID        string `json:"id"`
		Provider  string `json:"provider"`
		Name      string `json:"name"`
		BaseURL   string `json:"base_url"`
		APIKey    string `json:"api_key"`
		Model     string `json:"model"`
		IsDefault string `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request")
		return
	}

	if req.Provider == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "provider driver is required")
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


	isDefault := req.IsDefault == "true"
	now := time.Now().UTC().Format(time.RFC3339Nano)

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

	encKey := crypto.EncryptionKey()

	if req.ID != "" {
		// Update existing config by ID — preserve values if empty
		if req.BaseURL == "" || req.APIKey == "" {
			var existingURL, existingKey string
			_ = tx.QueryRow("SELECT base_url, api_key FROM provider_config WHERE id = ?", req.ID).Scan(&existingURL, &existingKey)
			if req.BaseURL == "" && existingURL != "" {
				req.BaseURL = existingURL
			}
			if req.APIKey == "" && existingKey != "" {
				if encKey != nil && crypto.IsEncrypted(existingKey) {
					dec, err := crypto.Decrypt(encKey, existingKey)
					if err == nil {
						req.APIKey = dec
					}
				} else {
					req.APIKey = existingKey
				}
			}
		}
		storedKey := req.APIKey
		if encKey != nil && req.APIKey != "" {
			enc, err := crypto.Encrypt(encKey, req.APIKey)
			if err != nil {
				writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to encrypt api key")
				return
			}
			storedKey = enc
		}

		isDefaultInt := 0
		if isDefault {
			isDefaultInt = 1
		}

		_, err = tx.Exec(`UPDATE provider_config SET provider=?, name=?, base_url=?, api_key=?, model=?, is_default=?, updated_at=? WHERE id=?`,
			req.Provider, req.Name, req.BaseURL, storedKey, req.Model, isDefaultInt, now, req.ID)
		if err != nil {
			slog.ErrorContext(r.Context(), "admin handler error",
				"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
			writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to update provider config")
			return
		}
	} else {
		// Create new config
		newID := generateProviderID()

		storedKey := req.APIKey
		if encKey != nil && req.APIKey != "" {
			enc, err := crypto.Encrypt(encKey, req.APIKey)
			if err != nil {
				writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to encrypt api key")
				return
			}
			storedKey = enc
		}

		isDefaultInt := 0
		if isDefault {
			isDefaultInt = 1
		}

		if req.Name == "" {
			req.Name = req.Provider
		}

		_, err = tx.Exec(`INSERT INTO provider_config (id, provider, name, base_url, api_key, model, is_default, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			newID, req.Provider, req.Name, req.BaseURL, storedKey, req.Model, isDefaultInt, now)
		if err != nil {
			slog.ErrorContext(r.Context(), "admin handler error",
				"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
			writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to create provider config")
			return
		}
		req.ID = newID
	}

	if err := tx.Commit(); err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to commit transaction")
		return
	}

	// Hot-reload provider if reloader is available
	if ar.providerReloader != nil {
		if err := ar.providerReloader.ReloadProvider(req.Provider, req.BaseURL, req.APIKey, req.Model); err != nil {
			slog.ErrorContext(r.Context(), "provider hot-reload failed",
				"id", req.ID, "provider", req.Provider, "error", err)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":       req.ID,
		"provider": req.Provider,
		"saved":    true,
	})
}

func (ar *AdminRouter) deleteProviderConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request")
		return
	}
	if req.ID == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "id is required")
		return
	}

	result, err := ar.db.Exec("DELETE FROM provider_config WHERE id = ?", req.ID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to delete provider config")
		return
	}

	n, _ := result.RowsAffected()
	if n == 0 {
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "provider config not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":      req.ID,
		"deleted": true,
	})
}

func generateProviderID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "prov-" + hex.EncodeToString(b)
}

// handleProviderTest tests connectivity to a provider endpoint.
func (ar *AdminRouter) handleProviderTest(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
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

	if req.BaseURL == "" {
		req.BaseURL = defaultBaseURLs[req.Provider]
	}

	endpoint := strings.TrimRight(req.BaseURL, "/") + "/models"
	start := time.Now()

	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		slog.ErrorContext(r.Context(), "provider test: request creation failed", "error", err)
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false, "message": "failed to create connection test request",
		})
		return
	}
	httpReq.Header.Set("Authorization", "Bearer "+req.APIKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	latency := time.Since(start)

	if err != nil {
		slog.ErrorContext(r.Context(), "provider test: connection failed", "error", err, "latency_ms", latency.Milliseconds())
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":    false,
			"message":    "connection test failed",
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
