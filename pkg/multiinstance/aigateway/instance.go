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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	ctrlmetricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	aigwonpremconfig "github.com/kong/kong-operator/v2/controller/aigateway/onpremconfig"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	"github.com/kong/kong-operator/v2/internal/utils/index"
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

// Env carries the process-level dependencies an instance needs to build and run its own
// controller-runtime manager. They never drift with the spec, so they stay out of Config,
// which is hashed for drift detection.
type Env struct {
	// RestConfig is the rest config used to reach the Kubernetes API server.
	RestConfig *rest.Config

	// Scheme is the scheme the instance's manager registers its objects in.
	Scheme *runtime.Scheme

	// CacheSyncTimeout is the cache sync timeout for the instance's controllers.
	CacheSyncTimeout time.Duration
}

// Instance is a single on-prem AI Gateway control plane instance. It runs its own
// controller-runtime manager hosting the configuration-entity controllers
// (AIGatewayModel, and its sibling kinds as they are added), so that the controllers
// start and stop together with the OnPremAIGateway resource that owns the instance.
type Instance struct {
	id     manager.ID
	logger logr.Logger
	env    Env
	cfg    Config
	cn     *changenotifier.ChangeNotifier
	// client is the instance's own manager client, set in Run once the manager is built.
	client client.Client
}

var _ instances.Instance = &Instance{}

// NewInstance creates a new on-prem AI Gateway control plane instance. It does not start it.
func NewInstance(
	id manager.ID,
	logger logr.Logger,
	cfg Config,
	env Env,
) *Instance {
	return &Instance{
		id:     id,
		logger: logger.WithValues("instanceID", id.String()),
		env:    env,
		cfg:    cfg,
		cn:     changenotifier.New(),
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

// newCtrlManager builds the instance's own lightweight controller-runtime manager.
func (i *Instance) newCtrlManager() (ctrl.Manager, error) {
	// ponytail: every instance watches all AIGatewayModels with its own informers, unscoped.
	// Scope the cache once the gateway ref carries namespace/kind semantics and N instances
	// ever becomes a real cost.
	return ctrl.NewManager(i.env.RestConfig, ctrl.Options{
		Scheme: i.env.Scheme,
		Logger: i.logger,
		// Every instance registers a controller with the same name; controller-runtime keeps
		// a global list of controller names and panics on duplicates unless this is set.
		Controller:             config.Controller{SkipNameValidation: new(true)},
		Metrics:                ctrlmetricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		// No leader election and no webhooks: instances are ephemeral, one per OnPremAIGateway.
		// No cache namespace scoping: AIGatewayModels may reference the gateway from any namespace.
	})
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
	_ = payload

	for _, w := range warnings {
		// TODO: https://github.com/Kong/kong-operator/issues/5664
		// - emit warnings as events on the OnPremAIGateway resource
		// - emit warnings somewhere in OnPremAIGateway status
		log.Info(i.logger, "AI Gateway configuration warning", "warning", w)
	}
	return nil

}

// syncPending pushes the configuration for every gateway with pending changes and removes
// the synced ones from the set. Failed syncs stay in the set so that the timer retries
// them. lastSyncTS is updated on every attempt so that bursts of changes debounce even
// when syncs fail.
func (i *Instance) syncPending(
	ctx context.Context,
	pending map[types.NamespacedName]struct{},
	lastSyncTS *time.Time,
) {
	if len(pending) == 0 {
		return
	}
	*lastSyncTS = time.Now()
	for nn := range pending {
		if err := i.sendConfig(ctx, &nn); err != nil {
			i.logger.Error(err, "Failed to send configuration",
				"namespace", nn.Namespace, "name", nn.Name)
			continue
		}
		delete(pending, nn)
	}
}

// Run runs the instance. It blocks until the passed context is cancelled.
func (i *Instance) Run(ctx context.Context) error {
	i.logger.Info("Running on-prem AI Gateway control plane instance")
	defer i.cn.Close()

	mgr, err := i.newCtrlManager()
	if err != nil {
		return fmt.Errorf("creating instance controller-runtime manager: %w", err)
	}

	// The field index is used by translator.BuildDocument to list the AIGatewayModels
	// referencing this gateway. It lives on the instance's own cache, alongside its only
	// consumer.
	// TODO: This will require adjustments for indexing excluding the KonnectAIGateway.
	for _, opt := range index.OptionsForAIGatewayModel() {
		if err := mgr.GetFieldIndexer().IndexField(ctx, opt.Object, opt.Field, opt.ExtractValueFn); err != nil {
			return fmt.Errorf("registering AIGatewayModel field index: %w", err)
		}
	}

	// The configuration-entity controllers run on the instance's own manager and feed the
	// instance's own ChangeNotifier.
	cs := &aigwonpremconfig.Controllers{
		Client:           mgr.GetClient(),
		Scheme:           i.env.Scheme,
		Log:              i.logger,
		CacheSyncTimeout: i.env.CacheSyncTimeout,
		ChangeNotifier:   i.cn,
	}
	if err := cs.SetupWithManager(ctx, mgr); err != nil {
		return fmt.Errorf("setting up configuration-entity controllers: %w", err)
	}
	i.client = mgr.GetClient()

	mgrErrCh := make(chan error, 1)
	go func() {
		mgrErrCh <- mgr.Start(ctx)
		close(mgrErrCh)
	}()
	defer func() {
		// Drain the manager result so its goroutine never leaks.
		select {
		case <-mgrErrCh:
		default:
		}
	}()

	var (
		ch         = i.cn.NotifyChannel()
		lastSyncTS time.Time
		// TODO: make this configurable
		minimumInterval = time.Second
		// TODO: make this configurable
		syncInterval = 3 * time.Second
		timer        = time.NewTicker(syncInterval)
		// pending holds the gateways with configuration changes that have not been synced
		// yet. Changes arriving within the debounce window land here and are re-driven by
		// the timer instead of being dropped, as are syncs that failed.
		pending = map[types.NamespacedName]struct{}{}
	)
	defer timer.Stop()

forLoop:
	for {
		select {
		// Handle periodic sync based on the timer: it re-drives changes that were
		// debounced and syncs that failed. It does not touch lastSyncTS when there is
		// nothing to sync, so idle ticks never push the debounce window out.
		case <-timer.C:
			i.syncPending(ctx, pending, &lastSyncTS)

		// Handle sync based on cluster change events.
		case change := <-ch:
			obj := change.Object
			logger := i.logger.WithValues(
				"ID", change.ID,
				"namespace", obj.GetNamespace(),
				"name", obj.GetName(),
			)
			logger.Info("Received change notification")

			if change.ParentNN == nil {
				// TODO: handle nil gw
				logger.Info("Change has no parent gateway reference, skipping")
				continue
			}

			pending[*change.ParentNN] = struct{}{}
			if time.Since(lastSyncTS) >= minimumInterval {
				i.syncPending(ctx, pending, &lastSyncTS)
			}

		// A crashed instance manager (e.g. unreachable API server) tears the instance down:
		// the multi-instance manager removes it and the OnPremAIGateway reconciler reschedules it.
		case err, ok := <-mgrErrCh:
			if !ok {
				break forLoop
			}

			if err != nil {
				i.logger.Error(err, "Instance controller manager failed")
				return fmt.Errorf("instance controller manager failed: %w", err)
			}

		case <-ctx.Done():
			break forLoop
		}
	}
	i.logger.Info("Stopped on-prem AI Gateway control plane instance")
	return nil
}
