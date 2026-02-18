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

	"github.com/kong/kong-operator/v2/ingress-controller/internal/manager/consts"
)

var (
	scheme = runtime.NewScheme()
	codecs = serializer.NewCodecFactory(scheme)
)

// NOTE: These paths have to match paths used in Helm Chart.
// E.g. in
const (
	// DefaultAdmissionWebhookBasePath is the default path to validating admission webhook files.
	DefaultAdmissionWebhookBasePath = "/tmp/k8s-webhook-server/serving-certs/validating-admission-webhook/"
	// DefaultAdmissionWebhookCertPath is the default path to the any (validation, conversion) webhook server TLS certificate.
	DefaultAdmissionWebhookCertPath = DefaultAdmissionWebhookBasePath + "tls.crt"
	// DefaultAdmissionWebhookKeyPath is the default path to the any (validation, conversion) webhook server TLS key.
	DefaultAdmissionWebhookKeyPath = DefaultAdmissionWebhookBasePath + "tls.key"
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
