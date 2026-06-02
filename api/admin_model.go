package api

import (
	"net/http"
)

func (ar *AdminRouter) handleModels(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
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
