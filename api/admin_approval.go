package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/openbotstack/openbotstack-core/execution"
)

// handleApprovalList handles GET /v1/admin/approval — lists pending approvals.
func (ar *AdminRouter) handleApprovalList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}
	if ar.approvalGateway == nil {
		writeAPIError(w, http.StatusServiceUnavailable, ErrUnavailable, "approval not configured")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	approvals := ar.approvalGateway.ListPending(tenantID)
	if approvals == nil {
		approvals = []execution.ApprovalRequest{}
	}
	writeJSON(w, http.StatusOK, approvals)
}

// handleApprovalAction routes /v1/admin/approval/{id} and /v1/admin/approval/{id}/approve|deny.
func (ar *AdminRouter) handleApprovalAction(w http.ResponseWriter, r *http.Request) {
	// Path: /v1/admin/approval/{id} or /v1/admin/approval/{id}/approve or /v1/admin/approval/{id}/deny
	path := strings.TrimPrefix(r.URL.Path, "/v1/admin/approval/")
	parts := strings.SplitN(path, "/", 2)

	if len(parts) == 0 || parts[0] == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "missing approval ID")
		return
	}

	id := parts[0]

	if ar.approvalGateway == nil {
		writeAPIError(w, http.StatusServiceUnavailable, ErrUnavailable, "approval not configured")
		return
	}

	// Route based on sub-path
	if len(parts) == 1 || parts[1] == "" {
		// GET /v1/admin/approval/{id}
		ar.handleApprovalGet(w, r, id)
		return
	}

	switch parts[1] {
	case "approve":
		ar.handleApprovalApprove(w, r, id)
	case "deny":
		ar.handleApprovalDeny(w, r, id)
	default:
		writeAPIError(w, http.StatusNotFound, ErrNotFound, "not found")
	}
}

// handleApprovalGet handles GET /v1/admin/approval/{id}.
func (ar *AdminRouter) handleApprovalGet(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	approval, err := ar.approvalGateway.GetApproval(id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, ErrNotFound, fmt.Sprintf("approval %q not found", id))
		return
	}
	writeJSON(w, http.StatusOK, approval)
}

// approvalActionRequest is the request body for approve/deny actions.
type approvalActionRequest struct {
	ApproverID string `json:"approver_id"`
	Reason     string `json:"reason,omitempty"`
}

// handleApprovalApprove handles POST /v1/admin/approval/{id}/approve.
func (ar *AdminRouter) handleApprovalApprove(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	var body approvalActionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request body")
		return
	}
	if body.ApproverID == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "approver_id is required")
		return
	}

	if err := ar.approvalGateway.Approve(id, body.ApproverID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeAPIError(w, http.StatusNotFound, ErrNotFound, err.Error())
		} else {
			writeAPIError(w, http.StatusConflict, "CONFLICT", err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"id": id, "status": "approved", "approver_id": body.ApproverID,
	})
}

// handleApprovalDeny handles POST /v1/admin/approval/{id}/deny.
func (ar *AdminRouter) handleApprovalDeny(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	var body approvalActionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request body")
		return
	}
	if body.ApproverID == "" {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "approver_id is required")
		return
	}

	if err := ar.approvalGateway.Deny(id, body.ApproverID, body.Reason); err != nil {
		if strings.Contains(err.Error(), "not found") {
			writeAPIError(w, http.StatusNotFound, ErrNotFound, err.Error())
		} else {
			writeAPIError(w, http.StatusConflict, "CONFLICT", err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"id": id, "status": "denied", "approver_id": body.ApproverID,
	})
}
