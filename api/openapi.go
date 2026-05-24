package api

import (
	"encoding/json"
	"net/http"
	"sync"

	"gopkg.in/yaml.v3"
)

// OpenAPISpec serves the embedded OpenAPI specification as JSON.
type OpenAPISpec struct {
	once sync.Once
	data []byte
	err  error
}

// NewOpenAPISpec creates an OpenAPI spec server from raw YAML or JSON.
func NewOpenAPISpec(yamlOrJSON []byte) *OpenAPISpec {
	return &OpenAPISpec{data: yamlOrJSON}
}

// ServeHTTP returns the spec as JSON.
func (o *OpenAPISpec) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}
	o.once.Do(func() {
		var v any
		// Try JSON first
		if err := json.Unmarshal(o.data, &v); err == nil {
			o.data, o.err = json.Marshal(v)
			return
		}
		// Fallback: parse as YAML and re-encode to JSON
		if err := yaml.Unmarshal(o.data, &v); err != nil {
			o.err = err
			return
		}
		o.data, o.err = json.Marshal(v)
	})

	if o.err != nil {
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to parse openapi spec")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write(o.data)
}
