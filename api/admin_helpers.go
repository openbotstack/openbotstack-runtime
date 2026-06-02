package api

import "net/http"

// requireMethod returns true if r.Method matches the required method.
// Otherwise writes a 405 JSON error and returns false.
func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
	return false
}

// requireAnyMethod returns true if r.Method matches any of the allowed methods.
// Otherwise writes a 405 JSON error and returns false.
func requireAnyMethod(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, m := range methods {
		if r.Method == m {
			return true
		}
	}
	writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
	return false
}
