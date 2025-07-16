package diagnostics

import "net/http"

// HTTPHandler is a handler for the diagnostics HTTP endpoints.
type HTTPHandler struct {
	mux *http.ServeMux
}

// NewHTTPHandler
func NewHTTPHandler() *HTTPHandler {
	h := &HTTPHandler{}

	mux := http.NewServeMux()
	mux.HandleFunc("/controlplanes", h.handleControlPlanes)
	h.mux = mux

	return h
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *HTTPHandler) handleControlPlanes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
	}

	w.Write([]byte("Listing control planes"))
}
