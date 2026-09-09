package instances

import (
	"context"
	"net/http"
	"sync"

	"github.com/go-logr/logr"
)

// instance represents a single Instance managed by a Registry.
type instance struct {
	logger  logr.Logger
	in      Instance
	cfgHash string

	stopOnce sync.Once
	stopCh   chan struct{}
}

func newInstance(in Instance, logger logr.Logger) (*instance, error) {
	hash, err := in.ConfigHash()
	if err != nil {
		return nil, err
	}

	return &instance{
		logger:  logger.WithValues("instanceID", in.ID()),
		in:      in,
		cfgHash: hash,
		stopCh:  make(chan struct{}),
	}, nil
}

// Stop stops the instance. Only its first call has an effect.
func (i *instance) Stop() {
	// Close stopCh only once as otherwise it would panic.
	i.stopOnce.Do(func() {
		close(i.stopCh)
	})
}

// StopChannel returns a channel one can use to wait for the instance to stop.
func (i *instance) StopChannel() <-chan struct{} {
	return i.stopCh
}

// ConfigHash returns a hash of the instance's configuration, computed when the instance was scheduled.
func (i *instance) ConfigHash() string {
	return i.cfgHash
}

// Run runs the instance in a goroutine and blocks until the instance is stopped or the context is done.
func (i *instance) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		if err := i.in.Run(ctx); err != nil {
			i.logger.Error(err, "Instance exited with an error")
		}
	}()

	defer cancel() // Cancel the context once the parent context is done or the instance is stopped.
	select {
	case <-ctx.Done():
	case <-i.stopCh:
	}
}

// DiagnosticsHandler returns an HTTP handler that exposes diagnostics information for this instance. It can return
// nil if the instance does not expose diagnostics information.
func (i *instance) DiagnosticsHandler() http.Handler {
	return i.in.DiagnosticsHandler()
}

// IsReady returns an error if the instance is not ready.
func (i *instance) IsReady() error {
	return i.in.IsReady()
}
