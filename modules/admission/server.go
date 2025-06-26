package admission

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-logr/logr"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gwtypes "github.com/kong/kong-operator/internal/types"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
)

var (
	scheme = runtime.NewScheme()
	codecs = serializer.NewCodecFactory(scheme)
)

// RequestHandler handles the requests of validating objects.
type RequestHandler struct {
	Logger logr.Logger
}

// NewRequestHandler create a RequestHandler to handle validation requests.
func NewRequestHandler(c client.Client, l logr.Logger) *RequestHandler {
	return &RequestHandler{
		Logger: l.WithValues("component", "validation-server"),
	}
}

// ServeHTTP serves for HTTP requests.
func (h *RequestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		h.Logger.Error(err, "failed to read request from client")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	review := &admissionv1.AdmissionReview{}
	if err := json.Unmarshal(data, review); err != nil {
		h.Logger.Error(err, "failed to parse AdmissionReview object")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response, err := h.handleValidation(r.Context(), review.Request)
	if err != nil {
		h.Logger.Error(err, "failed to run validation")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	review.Response = response
	data, err = json.Marshal(review)
	if err != nil {
		h.Logger.Error(err, "failed to marshal response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = w.Write(data)
	if err != nil {
		h.Logger.Error(err, "failed to write response")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

var (
	controlPlaneGVResource = metav1.GroupVersionResource{
		Group:    operatorv1beta1.SchemeGroupVersion.Group,
		Version:  operatorv1beta1.SchemeGroupVersion.Version,
		Resource: "controlplanes",
	}
	dataPlaneGVResource = metav1.GroupVersionResource{
		Group:    operatorv1beta1.SchemeGroupVersion.Group,
		Version:  operatorv1beta1.SchemeGroupVersion.Version,
		Resource: "dataplanes",
	}
)

func (h *RequestHandler) handleValidation(_ context.Context, req *admissionv1.AdmissionRequest) (
	*admissionv1.AdmissionResponse, error,
) {
	if req == nil {
		return &admissionv1.AdmissionResponse{
			Allowed: false,
			Result: &metav1.Status{
				Code:    http.StatusBadRequest,
				Reason:  metav1.StatusReasonBadRequest,
				Message: "empty request",
				Status:  metav1.StatusFailure,
			},
		}, nil
	}

	var (
		response     admissionv1.AdmissionResponse
		ok           = true
		msg          string
		deserializer = codecs.UniversalDeserializer()
	)

	switch req.Resource {
	case controlPlaneGVResource:
		controlPlane := gwtypes.ControlPlane{}
		if req.Operation == admissionv1.Create || req.Operation == admissionv1.Update {
			_, _, err := deserializer.Decode(req.Object.Raw, nil, &controlPlane)
			if err != nil {
				return nil, err
			}
		}
	case dataPlaneGVResource:
		if req.Operation == admissionv1.Create || req.Operation == admissionv1.Update {
			dataPlane, old := operatorv1beta1.DataPlane{}, operatorv1beta1.DataPlane{}
			_, _, err := deserializer.Decode(req.Object.Raw, nil, &dataPlane)
			if err != nil {
				return nil, err
			}
			_, _, err = deserializer.Decode(req.OldObject.Raw, nil, &old)
			if err != nil {
				return nil, err
			}
		}
	}

	response.UID = req.UID
	response.Allowed = ok

	response.Result = &metav1.Status{
		Message: msg,
	}
	if ok {
		response.Result.Code = http.StatusOK
		response.Result.Status = metav1.StatusSuccess

	} else {
		response.Result.Code = http.StatusBadRequest
		response.Result.Reason = metav1.StatusReasonBadRequest
		response.Result.Status = metav1.StatusFailure
	}
	return &response, nil
}
