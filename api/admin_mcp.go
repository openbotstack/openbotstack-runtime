package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	mcpcore "github.com/openbotstack/openbotstack-core/mcp"
	"github.com/openbotstack/openbotstack-runtime/mcp"
)

// handleMCPServers handles GET (list servers) and POST (add server) for /v1/admin/mcp/servers.
func (ar *AdminRouter) handleMCPServers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ar.listMCPServers(w, r)
	case http.MethodPost:
		ar.addMCPServer(w, r)
	default:
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
	}
}

func (ar *AdminRouter) listMCPServers(w http.ResponseWriter, r *http.Request) {
	if ar.mcpAdmin == nil {
		writeJSON(w, http.StatusOK, []mcpcore.ServerStatus{})
		return
	}
	servers := ar.mcpAdmin.ListServers()
	if servers == nil {
		servers = []mcpcore.ServerStatus{}
	}
	writeJSON(w, http.StatusOK, servers)
}

func (ar *AdminRouter) addMCPServer(w http.ResponseWriter, r *http.Request) {
	if ar.mcpAdmin == nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "mcp admin not configured")
		return
	}

	var cfg mcpcore.ServerConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request body")
		return
	}

	if cfg.ID == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "id is required")
		return
	}
	if cfg.Transport == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "transport is required")
		return
	}
	if cfg.Transport != "stdio" && cfg.Transport != "sse" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "transport must be 'stdio' or 'sse'")
		return
	}

	if err := ar.mcpAdmin.AddServer(r.Context(), cfg); err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to add mcp server")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":      cfg.ID,
		"name":    cfg.Name,
		"status":  "added",
	})
}

// handleMCPServerAction routes by path suffix:
//   /v1/admin/mcp/servers/{id}           GET|PUT|DELETE
//   /v1/admin/mcp/servers/{id}/tools     GET
//   /v1/admin/mcp/servers/{id}/reconnect POST
func (ar *AdminRouter) handleMCPServerAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/admin/mcp/servers/")
	if path == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "server id is required")
		return
	}

	parts := strings.SplitN(path, "/", 2)
	serverID := parts[0]

	if len(parts) == 1 {
		// /v1/admin/mcp/servers/{id}
		switch r.Method {
		case http.MethodGet:
			ar.getMCPServer(w, r, serverID)
		case http.MethodPut:
			ar.updateMCPServer(w, r, serverID)
		case http.MethodDelete:
			ar.removeMCPServer(w, r, serverID)
		default:
			writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		}
		return
	}

	action := parts[1]
	switch action {
	case "tools":
		if !requireMethod(w, r, http.MethodGet) {
		return
	}
		ar.getMCPServerTools(w, r, serverID)
	case "reconnect":
		if !requireMethod(w, r, http.MethodPost) {
		return
	}
		ar.reconnectMCPServer(w, r, serverID)
	default:
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "unknown action")
	}
}

func (ar *AdminRouter) getMCPServer(w http.ResponseWriter, r *http.Request, serverID string) {
	if ar.mcpAdmin == nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "mcp admin not configured")
		return
	}
	servers := ar.mcpAdmin.ListServers()
	for _, s := range servers {
		if s.ID == serverID {
			writeJSON(w, http.StatusOK, s)
			return
		}
	}
	writeAPIError(w, http.StatusNotFound, ErrNotFound, "server not found")
}

func (ar *AdminRouter) updateMCPServer(w http.ResponseWriter, r *http.Request, serverID string) {
	if ar.mcpAdmin == nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "mcp admin not configured")
		return
	}

	var cfg mcpcore.ServerConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request body")
		return
	}

	if err := ar.mcpAdmin.UpdateServer(r.Context(), serverID, cfg); err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to update mcp server")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":     serverID,
		"status": "updated",
	})
}

func (ar *AdminRouter) removeMCPServer(w http.ResponseWriter, r *http.Request, serverID string) {
	if ar.mcpAdmin == nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "mcp admin not configured")
		return
	}

	if err := ar.mcpAdmin.RemoveServer(r.Context(), serverID); err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to remove mcp server")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":     serverID,
		"status": "removed",
	})
}

func (ar *AdminRouter) getMCPServerTools(w http.ResponseWriter, r *http.Request, serverID string) {
	if ar.mcpAdmin == nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "mcp admin not configured")
		return
	}

	tools, err := ar.mcpAdmin.GetServerTools(r.Context(), serverID)
	if err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to get server tools")
		return
	}
	if tools == nil {
		tools = []mcpcore.ClientTool{}
	}
	writeJSON(w, http.StatusOK, tools)
}

func (ar *AdminRouter) reconnectMCPServer(w http.ResponseWriter, r *http.Request, serverID string) {
	if ar.mcpAdmin == nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "mcp admin not configured")
		return
	}

	if err := ar.mcpAdmin.ReconnectServer(r.Context(), serverID); err != nil {
		slog.ErrorContext(r.Context(), "admin handler error",
			"method", r.Method, "path", r.URL.Path, "status", http.StatusInternalServerError, "error", err)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to reconnect mcp server")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":     serverID,
		"status": "reconnecting",
	})
}

// handleCapabilities returns all registered capabilities (skills + MCP tools).
func (ar *AdminRouter) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	if ar.capabilityLister == nil {
		writeJSON(w, http.StatusOK, []CapabilityDescriptor{})
		return
	}

	writeJSON(w, http.StatusOK, ar.capabilityLister.List())
}

func (ar *AdminRouter) handleMCPHealth(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	if ar.mcpAdmin == nil {
		writeJSON(w, http.StatusOK, []mcp.ServerHealth{})
		return
	}
	health := ar.mcpAdmin.HealthCheck(r.Context())
	if health == nil {
		health = []mcp.ServerHealth{}
	}
	writeJSON(w, http.StatusOK, health)
}
