package validation

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/kong/kong-operator/v2/ingress-controller/internal/admission"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/validation/consts"
)

// SetupAdmissionServer sets up the admission webhook server.
// certDir is the directory that holds the server's TLS certificate (tls.crt)
// and key (tls.key).
func SetupAdmissionServer(
	ctx context.Context,
	m ctrl.Manager,
	certDir string,
) (*admission.RequestHandler, error) {
	admissionLogger := ctrl.LoggerFrom(ctx).WithName("admission-server")

	admissionReqHandler := &admission.RequestHandler{
		Logger: admissionLogger,
	}
	srv, err := admission.MakeTLSServer(consts.WebhookPort, admissionReqHandler, certDir)
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
