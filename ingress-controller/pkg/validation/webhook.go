package validation

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/admission"
)

// SetupAdmissionServer sets up the admission webhook server.
func SetupAdmissionServer(
	ctx context.Context,
	m ctrl.Manager,
) (*admission.RequestHandler, error) {
	admissionLogger := ctrl.LoggerFrom(ctx).WithName("admission-server")

	admissionReqHandler := &admission.RequestHandler{
		Logger: admissionLogger,
	}
	srv, err := admission.MakeTLSServer(5443, admissionReqHandler)
	if err != nil {
		return nil, err
	}

	go func() {
		if err := srv.Start(ctx); err != nil {
			admissionLogger.Error(err, "Admission server exited")
		}
	}()

	return admissionReqHandler, nil
}
