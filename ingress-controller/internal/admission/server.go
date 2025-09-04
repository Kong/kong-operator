package admission

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/kong-operator/ingress-controller/internal/manager/consts"
)

var (
	scheme = runtime.NewScheme()
	codecs = serializer.NewCodecFactory(scheme)
)

// To match paths used in Helm Chart and expected by conversion webhook from internal/webhook/conversion/webhook.go.
// Source for those paths is:
// https://github.com/kubernetes-sigs/controller-runtime/blob/3554729cfb3179c1a13f554b828d658d062dceb9/pkg/webhook/server.go#L81
const (
	// DefaultAdmissionWebhookCertPath is the default path to the any (validation, conversion) webhook server TLS certificate.
	DefaultAdmissionWebhookCertPath = "/tmp/k8s-webhook-server/serving-certs/tls.crt"
	// DefaultAdmissionWebhookKeyPath is the default path to the any (validation, conversion) webhook server TLS key.
	DefaultAdmissionWebhookKeyPath = "/tmp/k8s-webhook-server/serving-certs/tls.key"
)

type Server struct {
	s           *http.Server
	certWatcher *certwatcher.CertWatcher
}

func MakeTLSServer(port int32, handler http.Handler) (*Server, error) {
	const defaultHTTPReadHeaderTimeout = 10 * time.Second

	watcher, err := certwatcher.New(DefaultAdmissionWebhookCertPath, DefaultAdmissionWebhookKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create CertWatcher: %w", err)
	}

	return &Server{
		s: &http.Server{
			Addr: fmt.Sprintf(":%d", port),
			TLSConfig: &tls.Config{
				MinVersion:     tls.VersionTLS12,
				MaxVersion:     tls.VersionTLS13,
				GetCertificate: watcher.GetCertificate,
			},
			Handler:           handler,
			ReadHeaderTimeout: defaultHTTPReadHeaderTimeout,
		},
		certWatcher: watcher,
	}, nil
}

// Start starts the admission server and blocks until the context is done.
func (s *Server) Start(ctx context.Context) error {
	logger := ctrllog.FromContext(ctx)
	go func() {
		if err := s.s.ListenAndServeTLS(DefaultAdmissionWebhookCertPath, DefaultAdmissionWebhookKeyPath); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(err, "Failed to start admission server")
		}
	}()

	go func() {
		if err := s.certWatcher.Start(ctx); err != nil {
			logger.Error(err, "Failed to start CertWatcher")
		}
	}()

	<-ctx.Done()

	ctx, cancel := context.WithTimeout(context.Background(), consts.DefaultGracefulShutdownTimeout) //nolint:contextcheck
	defer cancel()
	return s.s.Shutdown(ctx)
}
