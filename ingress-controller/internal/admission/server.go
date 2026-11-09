package admission

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	mgrconsts "github.com/kong/kong-operator/v2/ingress-controller/internal/manager/consts"
)

var (
	scheme = runtime.NewScheme()
	codecs = serializer.NewCodecFactory(scheme)
)

const (
	certFileName = "tls.crt"
	keyFileName  = "tls.key"
)

type Server struct {
	s           *http.Server
	certWatcher *certwatcher.CertWatcher
	// certDir is the directory that holds the server's TLS certificate and key.
	certDir string
}

// MakeTLSServer creates a TLS server that reads its certificate (tls.crt) and
// key (tls.key) from certDir, reloading them when the files change.
func MakeTLSServer(port int32, handler http.Handler, certDir string) (*Server, error) {
	const defaultHTTPReadHeaderTimeout = 10 * time.Second

	if certDir == "" {
		return nil, errors.New("the directory holding the webhook TLS certificate and key must not be empty")
	}
	certPath := filepath.Join(certDir, certFileName)
	keyPath := filepath.Join(certDir, keyFileName)

	watcher, err := certwatcher.New(certPath, keyPath)
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
		certDir:     certDir,
	}, nil
}

// Start starts the admission server and blocks until the context is done.
func (s *Server) Start(ctx context.Context) error {
	logger := ctrllog.FromContext(ctx)
	go func() {
		if err := s.s.ListenAndServeTLS(filepath.Join(s.certDir, certFileName), filepath.Join(s.certDir, keyFileName)); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error(err, "Failed to start admission server")
		}
	}()

	go func() {
		if err := s.certWatcher.Start(ctx); err != nil {
			logger.Error(err, "Failed to start CertWatcher")
		}
	}()

	<-ctx.Done()

	ctx, cancel := context.WithTimeout(context.Background(), mgrconsts.DefaultGracefulShutdownTimeout) //nolint:contextcheck
	defer cancel()
	return s.s.Shutdown(ctx)
}
