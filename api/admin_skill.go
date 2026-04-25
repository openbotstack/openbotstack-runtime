package api

import (
	"log/slog"
	"net/http"
	"strings"
)

func (ar *AdminRouter) handleAdminSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	if ar.skillAdmin == nil {
		writeJSON(w, http.StatusOK, []SkillAdminInfo{})
		return
	}

	skills, err := ar.skillAdmin.ListSkills()
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to list skills")
		return
	}
	if skills == nil {
		skills = []SkillAdminInfo{}
	}
	writeJSON(w, http.StatusOK, skills)
}

// handleSkillAction handles enable/disable actions for /v1/admin/skills/{skillID}/{action}
// Path format: /v1/admin/skills/{skillID}/enable or /v1/admin/skills/{skillID}/disable
// skillID may contain slashes (e.g., "core/summarize").
func (ar *AdminRouter) handleSkillAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	// Parse: /v1/admin/skills/{skillID}/{action}
	path := strings.TrimPrefix(r.URL.Path, "/v1/admin/skills/")
	// Split into parts — the last part is the action, everything before is the skillID
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash < 0 {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid path format")
		return
	}
	skillID := path[:lastSlash]
	action := path[lastSlash+1:]

	if ar.skillAdmin == nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "skill admin not configured")
		return
	}

	var enabled bool
	switch action {
	case "enable":
		enabled = true
	case "disable":
		enabled = false
	default:
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "action must be 'enable' or 'disable'")
		return
	}

	if err := ar.skillAdmin.SetSkillEnabled(skillID, enabled); err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to update skill")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"id": skillID, "enabled": enabled})
}

// handleAdminSessions returns all sessions across all tenants (admin view).
