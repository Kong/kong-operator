package diagnostics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/ingress-controller/pkg/manager"
	gwtypes "github.com/kong/kong-operator/internal/types"
)

// HTTPHandler is a handler for the diagnostics HTTP endpoints.
type HTTPHandler struct {
	cl      client.Client
	logger  logr.Logger
	mux     *http.ServeMux
	exposer *ControlPlaneDiagnosticsExposer
}

// NewHTTPHandler returns a new HTTP Handler to handle HTTP requests to the diagnostics server.
func NewHTTPHandler(
	cl client.Client,
	logger logr.Logger,
	exposer *ControlPlaneDiagnosticsExposer,
) *HTTPHandler {
	h := &HTTPHandler{
		cl:      cl,
		logger:  logger,
		exposer: exposer,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/debug/controlplanes", h.handleAllControlPlanes)
	mux.HandleFunc("/debug/controlplanes/namespace/{namespace}", h.handleControlPlanesByNamespace)
	mux.HandleFunc("/debug/controlplanes/namespace/{namespace}/name/{name}/config/", h.handleControlPlaneConfigDump)
	h.mux = mux

	return h
}

// ServeHTTP serves HTTP requests to the diagnostics server.
func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// ListControlPlanesResponse is the response for listing managed ControlPlanes.
type ListControlPlanesResponse struct {
	ControlPlanes []ListControlPlaneItem `json:"controlPlanes"`
}

// ListControlPlaneItem represents a ControlPlane in the response of listing managed ControlPlanes.
type ListControlPlaneItem struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	ID        string `json:"id"`
}

func (h *HTTPHandler) handleListControlPlanes(rw http.ResponseWriter, r *http.Request, cl client.Client) {
	// List all ControlPlane CRDs.
	cpList := gwtypes.ControlPlaneList{}
	if err := cl.List(r.Context(), &cpList); err != nil {
		h.logger.Error(err, "failed to list managed ControlPlanes")
		rw.WriteHeader(http.StatusInternalServerError)
		_, _ = rw.Write([]byte("failed to list ControlPlanes"))
		return
	}

	// List all CP instances registered to the exposer and filter CPs by their UIDs.
	cpIDs := h.exposer.listInstances()
	cpIDMap := lo.SliceToMap(cpIDs, func(id manager.ID) (string, struct{}) {
		return id.String(), struct{}{}
	})

	managedCPs := lo.Filter(cpList.Items, func(cp gwtypes.ControlPlane, _ int) bool {
		_, ok := cpIDMap[string(cp.UID)]
		return ok
	})

	// Make up the response from the filtered ControlPlanes.
	resp := &ListControlPlanesResponse{
		ControlPlanes: lo.Map(managedCPs, func(cp gwtypes.ControlPlane, _ int) ListControlPlaneItem {
			return ListControlPlaneItem{
				Namespace: cp.Namespace,
				Name:      cp.Name,
				ID:        string(cp.UID),
			}
		}),
	}
	rw.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(rw).Encode(resp)
}

func (h *HTTPHandler) handleAllControlPlanes(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		rw.WriteHeader(http.StatusMethodNotAllowed)
	}

	h.handleListControlPlanes(rw, r, h.cl)
}

func (h *HTTPHandler) handleControlPlanesByNamespace(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		rw.WriteHeader(http.StatusMethodNotAllowed)
	}

	namespace := r.PathValue("namespace")
	if namespace == "" {
		rw.WriteHeader(http.StatusBadRequest)
		_, _ = rw.Write([]byte("empty namespace"))
	}

	clientNamespaced := client.NewNamespacedClient(h.cl, namespace)
	h.handleListControlPlanes(rw, r, clientNamespaced)
}

func (h *HTTPHandler) handleControlPlaneConfigDump(rw http.ResponseWriter, r *http.Request) {
	namespace := r.PathValue("namespace")
	name := r.PathValue("name")

	if namespace == "" {
		rw.WriteHeader(http.StatusBadRequest)
		_, _ = rw.Write([]byte("empty namespace"))
		return
	}

	if name == "" {
		rw.WriteHeader(http.StatusBadRequest)
		_, _ = rw.Write([]byte("empty name"))
		return
	}

	cp := &gwtypes.ControlPlane{}
	if err := h.cl.Get(r.Context(), client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, cp); err != nil {
		if k8serrors.IsNotFound(err) {
			rw.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(rw, "ControlPlane %s/%s not found", namespace, name)
			return
		}
		h.logger.Error(err, "failed to get control plane", "namespace", namespace, "name", name)
		rw.WriteHeader(http.StatusInternalServerError)
		_, _ = rw.Write([]byte("failed to list ControlPlanes"))
		return
	}
	cpID, _ := manager.NewID(string(cp.UID))
	handler, ok := h.exposer.getHandlerByID(cpID)
	if !ok {
		rw.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(rw, "ControlPlane %s/%s is not ready or not managed by the controller", namespace, name)
		return
	}
	// If ControlPlane does not enable config dump, a `nil` handler is registered.
	if handler == nil {
		rw.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(rw, "ControlPlane %s/%s does not enable config dump", namespace, name)
		return
	}

	// Return something here if the handler is nil which indiacates that the ControlPlane did not enable dumping config.
	path := r.URL.Path
	prefix := fmt.Sprintf("/debug/controlplanes/namespace/%s/name/%s/config", namespace, name)
	newReq := r.Clone(r.Context())
	newReq.URL.Path = strings.TrimPrefix(path, prefix)

	handler.ServeHTTP(rw, newReq)
}
