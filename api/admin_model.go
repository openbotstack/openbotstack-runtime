package api

import (
	"net/http"
)

func (ar *AdminRouter) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	if ar.modelRegistry == nil {
		writeJSON(w, http.StatusOK, []ModelInfo{})
		return
	}

	models := ar.modelRegistry.ListModels()
	if models == nil {
		models = []ModelInfo{}
	}
	writeJSON(w, http.StatusOK, models)
}
