package api

import (
	"encoding/json"
	"net/http"
)

func (ar *AdminRouter) handleAuditRetention(w http.ResponseWriter, r *http.Request) {
	if ar.retentionPolicy == nil {
		writeAPIError(w, http.StatusServiceUnavailable, ErrUnavailable, "retention not configured")
		return
	}

	switch r.Method {
	case http.MethodGet:
		cfg := ar.retentionPolicy.Config()
		writeJSON(w, http.StatusOK, retentionConfigFromPolicy(cfg))

	case http.MethodPut:
		var req struct {
			TenantID string `json:"tenant_id"`
			Days     int    `json:"days"`
			Remove   bool   `json:"remove"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid JSON")
			return
		}

		if req.TenantID == "" {
			writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "tenant_id is required")
			return
		}

		if req.Remove {
			ar.retentionPolicy.RemoveTenantOverride(req.TenantID)
		} else {
			if req.Days < 1 {
				writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "days must be >= 1")
				return
			}
			ar.retentionPolicy.SetTenantOverride(req.TenantID, req.Days)
		}

		cfg := ar.retentionPolicy.Config()
		writeJSON(w, http.StatusOK, retentionConfigFromPolicy(cfg))

	case http.MethodPost:
		// Trigger manual purge
		total, err := ar.retentionPolicy.PurgeExpired()
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, ErrInternal, "purge failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]int64{"purged": total})

	default:
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
	}
}
