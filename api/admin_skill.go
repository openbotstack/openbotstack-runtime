package api

import (
	"log/slog"
	"net/http"
	"strings"
)

func (ar *AdminRouter) handleAdminSkills(w http.ResponseWriter, r *http.Request) {
	if !requireAnyMethod(w, r, http.MethodGet, http.MethodPost) {
		return
	}

	switch r.Method {
	case http.MethodGet:
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

	case http.MethodPost:
		// POST /v1/admin/skills — reload all skills from disk
		if ar.skillAdmin == nil {
			writeAPIError(w, http.StatusInternalServerError, ErrInternal, "skill admin not configured")
			return
		}
		if err := ar.skillAdmin.ReloadSkills(r.Context()); err != nil {
			slog.ErrorContext(r.Context(), "admin handler error",
				"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
			writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to reload skills")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
	}
}

// handleSkillAction handles actions for /v1/admin/skills/{skillID}/{action}
// Actions: enable, disable, reload
// skillID may contain slashes (e.g., "core/summarize").
func (ar *AdminRouter) handleSkillAction(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	// Parse: /v1/admin/skills/{skillID}/{action}
	path := strings.TrimPrefix(r.URL.Path, "/v1/admin/skills/")
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

	switch action {
	case "enable":
		if err := ar.skillAdmin.SetSkillEnabled(skillID, true); err != nil {
			slog.ErrorContext(r.Context(), "admin handler error",
				"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
			writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to update skill")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"id": skillID, "enabled": true})

	case "disable":
		if err := ar.skillAdmin.SetSkillEnabled(skillID, false); err != nil {
			slog.ErrorContext(r.Context(), "admin handler error",
				"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
			writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to update skill")
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"id": skillID, "enabled": false})

	case "reload":
		if err := ar.skillAdmin.ReloadSkill(r.Context(), skillID); err != nil {
			slog.ErrorContext(r.Context(), "admin handler error",
				"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
			writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to reload skill")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": skillID, "status": "reloaded"})

	default:
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "action must be 'enable', 'disable', or 'reload'")
	}
}

// handleAdminSessions returns all sessions across all tenants (admin view).
