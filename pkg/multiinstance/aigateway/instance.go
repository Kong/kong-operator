package aigateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/Kong/ai-deck-converter/convert"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway/changenotifier"
	"github.com/kong/kong-operator/v2/pkg/multiinstance/aigateway/translator"
	"github.com/kong/kong-operator/v2/pkg/multiinstance/instances"
	"github.com/kong/kong-operator/v2/pkg/multiinstance/manager"
)

// Config is the resolved configuration of a single on-prem AI Gateway control plane instance.
type Config struct {
	// DBLessConfig is the rendered dbless declarative payload for this gateway, ready to be
	// pushed to its data planes' Admin API.
	DBLessConfig []byte
}

// Hash computes a hash of the given config. It's used to detect configuration drift of running instances.
func Hash(cfg Config) (string, error) {
	sum := sha256.Sum256(cfg.DBLessConfig)
	return hex.EncodeToString(sum[:]), nil
}

// Instance is a single on-prem AI Gateway control plane instance.
type Instance struct {
	id     manager.ID
	logger logr.Logger
	client client.Client
	cfg    Config
	cn     *changenotifier.ChangeNotifier
}

var _ instances.Instance = &Instance{}

// NewInstance creates a new on-prem AI Gateway control plane instance. It does not start it.
func NewInstance(
	id manager.ID,
	logger logr.Logger,
	cl client.Client,
	cfg Config,
	cn *changenotifier.ChangeNotifier,
) *Instance {
	return &Instance{
		id:     id,
		logger: logger.WithValues("instanceID", id.String()),
		client: cl,
		cfg:    cfg,
		cn:     cn,
	}
}

// ID returns the unique identifier of the instance.
func (i *Instance) ID() manager.ID {
	return i.id
}

// Config returns the configuration of the instance.
func (i *Instance) Config() Config {
	return i.cfg
}

// ConfigHash returns a hash of the instance's configuration.
func (i *Instance) ConfigHash() (string, error) {
	return Hash(i.cfg)
}

// IsReady returns an error if the instance is not ready yet.
//
// There is nothing to wait for yet: the instance does not talk to anything on startup. Once configuration
// assembly and push land, this has to report the actual readiness of the pushing machinery.
// TODO: https://github.com/Kong/kong-operator/issues/5569
func (i *Instance) IsReady() error {
	return nil
}

// DiagnosticsHandler returns nil: the instance does not expose diagnostics data yet.
// TODO: https://github.com/Kong/kong-operator/issues/5630
func (i *Instance) DiagnosticsHandler() http.Handler {
	return nil
}

func (i *Instance) sendConfig(
	ctx context.Context,
	gw *types.NamespacedName,
) error {
	// TODO: handle this
	if gw == nil {
		return fmt.Errorf("nil gateway reference")
	}

	doc, err := translator.BuildDocument(ctx, i.client, *gw)
	if err != nil {
		return fmt.Errorf("building configuration document: %w", err)
	}
	payload, warnings, err := convert.ConvertDocumentToDBLessYAML(doc, convert.Options{Strict: false})
	if err != nil {
		return fmt.Errorf("rendering dbless configuration: %w", err)
	}
	_ = warnings
	_ = payload

	for _, w := range warnings {
		// TODO: https://github.com/Kong/kong-operator/issues/5664
		// - emit warnings as events on the OnPremAIGateway resource
		// - emit warnings somewhere in OnPremAIGateway status
		log.Info(i.logger, "AI Gateway configuration warning", "warning", w)
	}
	return nil

}

// Run runs the instance. It blocks until the passed context is cancelled.
//
// It's a no-op for now: it holds nothing but the instance's identity and configuration. Configuration
// assembly and pushing it to the AI Gateway data planes go here.
// TODO: https://github.com/Kong/kong-operator/issues/5569
func (i *Instance) Run(ctx context.Context) error {
	i.logger.Info("Running on-prem AI Gateway control plane instance")
	var (
		ch              = i.cn.NotifyChannel()
		minimumInterval = time.Second
		lastSyncTs      = time.Time{}
		syncInterval    = 3 * time.Second
		timer           = time.NewTicker(syncInterval)
	)
forLoop:
	for {
		select {
		// Handle periodic sync based on the timer.
		case <-timer.C:
			// TODO: sync
			lastSyncTs = time.Now()
			i.logger.Info("Syncing...")

		// Handle sync based on cluster change events.
		case change := <-ch:
			obj := change.Object
			i.logger.Info("Received change notification",
				"ID", change.ID,
				"namespace", obj.GetNamespace(),
				"name", obj.GetName(),
			)

			if time.Since(lastSyncTs) < minimumInterval {
				// Debounce
				continue
			}

			// TODO: handle nil gw
			gw := change.ParentNN

			// TODO: sync
			lastSyncTs = time.Now()
			if err := i.sendConfig(ctx, gw); err != nil {
				i.logger.Error(err, "Failed to send configuration")
			}

		case <-ctx.Done():
			break forLoop
		}
	}
	i.logger.Info("Stopped on-prem AI Gateway control plane instance")
	return nil
}
