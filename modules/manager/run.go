/*
Copyright 2022 Kong Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package manager

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/kong/kong-operator/controller/pkg/secrets"
	"github.com/kong/kong-operator/ingress-controller/pkg/manager/multiinstance"
	"github.com/kong/kong-operator/internal/telemetry"
	mgrconfig "github.com/kong/kong-operator/modules/manager/config"
	"github.com/kong/kong-operator/modules/manager/logging"
	"github.com/kong/kong-operator/modules/manager/metadata"
	"github.com/kong/kong-operator/pkg/vars"
)

// Config represents the configuration for the manager.
type Config struct {
	MetricsAddr             string
	MetricsAccessFilter     MetricsAccessFilter
	ProbeAddr               string
	LeaderElection          bool
	LeaderElectionNamespace string

	AnonymousReports bool
	LoggingMode      logging.Mode
	ValidateImages   bool

	Out                      *os.File
	ControllerName           string
	ControllerNamespace      string
	APIServerHost            string
	APIServerQPS             int
	APIServerBurst           int
	KubeconfigPath           string
	CacheSyncPeriod          time.Duration
	CacheSyncTimeout         time.Duration
	ClusterCASecretName      string
	ClusterCASecretNamespace string
	ClusterCAKeyType         mgrconfig.KeyType
	ClusterCAKeySize         int
	LoggerOpts               *zap.Options
	EnforceConfig            bool
	ClusterDomain            string
	EmitKubernetesEvents     bool

	// ServiceAccountToImpersonate is the name of the service account to impersonate,
	// by the controller manager, when making requests to the API server.
	// Use for testing purposes only.
	ServiceAccountToImpersonate string

	// controllers for standard APIs and features
	GatewayControllerEnabled            bool
	ControlPlaneControllerEnabled       bool
	DataPlaneControllerEnabled          bool
	DataPlaneBlueGreenControllerEnabled bool

	// Controllers for specialty APIs and experimental features.
	AIGatewayControllerEnabled              bool
	KongPluginInstallationControllerEnabled bool
	KonnectSyncPeriod                       time.Duration
	KonnectMaxConcurrentReconciles          uint
	GatewayAPIExperimentalEnabled           bool
	ControlPlaneExtensionsControllerEnabled bool

	// Controllers for Konnect APIs.
	KonnectControllersEnabled bool
}

// DefaultConfig returns a default configuration for the manager.
func DefaultConfig() Config {
	const (
		defaultNamespace               = "kong-system"
		defaultLeaderElectionNamespace = defaultNamespace
	)

	return Config{
		MetricsAddr:                   ":8080",
		MetricsAccessFilter:           MetricsAccessFilterOff,
		ProbeAddr:                     ":8081",
		LeaderElection:                true,
		LeaderElectionNamespace:       defaultLeaderElectionNamespace,
		ClusterCASecretName:           "kong-operator-ca",
		ClusterCASecretNamespace:      defaultNamespace,
		ControllerNamespace:           defaultNamespace,
		LoggerOpts:                    &zap.Options{},
		GatewayControllerEnabled:      true,
		ControlPlaneControllerEnabled: true,
		DataPlaneControllerEnabled:    true,
	}
}

// SetupControllersFunc represents function to setup controllers, which is called
// in Run function.
type SetupControllersFunc func(manager.Manager, *Config, *multiinstance.Manager) ([]ControllerDef, error)

//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

// Run runs the manager. Parameter cfg represents the configuration for the manager
// that for normal operation is derived from command-line flags. The function
// setupControllers is expected to return a list of configured ControllerDef
// that will be added to the manager. The function admissionRequestHandler is
// used to construct the admission webhook handler for the validating webhook
// that is added to the manager too. Argument startedChan can be used as a signal
// to notify the caller when the manager has been started. Specifically, this channel
// gets closed when manager.Start() is called. Pass nil if you don't need this signal.
func Run(
	cfg Config,
	scheme *runtime.Scheme,
	setupControllers SetupControllersFunc,
	startedChan chan<- struct{},
	metadata metadata.Info,
) error {
	setupLog := ctrl.Log.WithName("setup")

	setupLog.Info("starting controller manager",
		"release", metadata.Release,
		"repo", metadata.Repo,
		"commit", metadata.Commit,
	)

	warnIfLegacyDevelopmentModeEnabled(setupLog)

	if cfg.ControllerName != "" {
		setupLog.Info(fmt.Sprintf("custom controller name provided: %s", cfg.ControllerName))
		vars.SetControllerName(cfg.ControllerName)
	}

	if cfg.LeaderElection {
		setupLog.Info("leader election enabled", "namespace", cfg.LeaderElectionNamespace)
	} else {
		setupLog.Info("leader election disabled")
	}

	restCfg, err := clientcmd.BuildConfigFromFlags(cfg.APIServerHost, cfg.KubeconfigPath)
	if err != nil {
		return err
	}
	// Configure K8s client rate-limiting.
	restCfg.QPS = float32(cfg.APIServerQPS)
	restCfg.Burst = cfg.APIServerBurst
	restCfg.UserAgent = metadata.UserAgent()
	restCfg.Impersonate = rest.ImpersonationConfig{
		UserName: cfg.ServiceAccountToImpersonate,
	}

	cacheOptions := cache.Options{}
	if cfg.CacheSyncPeriod > 0 {
		setupLog.Info("cache sync period set", "period", cfg.CacheSyncPeriod)
		cacheOptions.SyncPeriod = &cfg.CacheSyncPeriod
	}

	mgr, err := ctrl.NewManager(
		restCfg,
		ctrl.Options{
			Controller: config.Controller{
				// This is needed because controller-runtime since v0.19.0 keeps a global list of controller
				// names and panics if there are duplicates. This is a workaround for that in tests.
				// Ref: https://github.com/kubernetes-sigs/controller-runtime/pull/2902#issuecomment-2284194683
				SkipNameValidation: lo.ToPtr(true),
			},
			Scheme: scheme,
			Metrics: server.Options{
				BindAddress: cfg.MetricsAddr,
				FilterProvider: func() func(c *rest.Config, httpClient *http.Client) (server.Filter, error) {
					switch cfg.MetricsAccessFilter {
					case MetricsAccessFilterRBAC:
						return filters.WithAuthenticationAndAuthorization
					case MetricsAccessFilterOff:
						return nil
					default:
						// This is checked in flags validation so this should never happen.
						panic("unsupported metrics filter")
					}
				}(),
			},
			HealthProbeBindAddress:  cfg.ProbeAddr,
			LeaderElection:          cfg.LeaderElection,
			LeaderElectionNamespace: cfg.LeaderElectionNamespace,
			LeaderElectionID:        "a7feedc84.konghq.com",
			Cache:                   cacheOptions,
		},
	)
	if err != nil {
		return err
	}

	keyType, err := KeyTypeToX509PublicKeyAlgorithm(cfg.ClusterCAKeyType)
	if err != nil {
		return fmt.Errorf("unsupported cluster CA key type: %w", err)
	}

	caMgr := &caManager{
		Logger:          ctrl.Log.WithName("ca_manager"),
		Client:          mgr.GetClient(),
		SecretName:      cfg.ClusterCASecretName,
		SecretNamespace: cfg.ClusterCASecretNamespace,
		KeyConfig: secrets.KeyConfig{
			Type: keyType,
			Size: cfg.ClusterCAKeySize,
		},
	}
	if err = mgr.Add(caMgr); err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	ctx := context.Background()

	if err := SetupCacheIndexes(ctx, mgr, cfg); err != nil {
		return err
	}

	cpInstancesMgr := multiinstance.NewManager(mgr.GetLogger())
	if err := mgr.Add(cpInstancesMgr); err != nil {
		return fmt.Errorf("unable to add CP instances manager: %w", err)
	}

	controllers, err := setupControllers(mgr, &cfg, cpInstancesMgr)
	if err != nil {
		setupLog.Error(err, "failed setting up controllers")
		return err
	}
	for _, c := range controllers {
		if err := c.MaybeSetupWithManager(ctx, mgr); err != nil {
			return fmt.Errorf("unable to create controller %q: %w", c.Name(), err)
		}
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}

	// Enable anonymous reporting when configured.
	if cfg.AnonymousReports {
		stopAnonymousReports, err := setupAnonymousReports(ctx, restCfg, setupLog, metadata, cfg)
		if err != nil {
			setupLog.Error(err, "failed setting up anonymous reports")
		} else {
			defer stopAnonymousReports()
		}
	}

	setupLog.Info("starting manager")
	// If started channel is set, close it to notify the caller that manager has started.
	if startedChan != nil {
		close(startedChan)
	}
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}

	return nil
}

// warnIfLegacyDevelopmentModeEnabled logs a warning if any of the legacy development mode environment variables are set
// and suggests the new environment variables to use instead.
// This can be removed after a few releases.
func warnIfLegacyDevelopmentModeEnabled(log logr.Logger) {
	legacyEnvVars := []string{
		"GATEWAY_OPERATOR_DEVELOPMENT_MODE",
		"CONTROLLER_DEVELOPMENT_MODE",
	}

	replacingEnvVars := []string{
		"GATEWAY_OPERATOR_ANONYMOUS_REPORTS",
		"GATEWAY_OPERATOR_LOGGING_MODE",
		"GATEWAY_OPERATOR_VALIDATE_IMAGES",
	}

	for _, envVar := range legacyEnvVars {
		if os.Getenv(envVar) != "" {
			log.Info(fmt.Sprintf(
				"WARNING: %s is ineffective. Depending on your needs, use one of: %s",
				envVar,
				strings.Join(replacingEnvVars, ", "),
			))
		}
	}
}

// caManager is a manager responsible for creating a cluster CA certificate.
type caManager struct {
	Logger          logr.Logger
	Client          client.Client
	SecretName      string
	SecretNamespace string
	KeyConfig       secrets.KeyConfig
}

// Start starts the CA manager.
func (m *caManager) Start(ctx context.Context) error {
	if m.SecretName == "" {
		return fmt.Errorf("cannot use an empty secret name when creating a CA secret")
	}
	if m.SecretNamespace == "" {
		return fmt.Errorf("cannot use an empty secret namespace when creating a CA secret")
	}
	return m.maybeCreateCACertificate(ctx)
}

func (m *caManager) maybeCreateCACertificate(ctx context.Context) error {
	// TODO https://github.com/kong/kong-operator/issues/199 this also needs to check if the CA is expired and
	// managed, and needs to reissue it (and all issued certificates) if so
	ctx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()

	var (
		ca        corev1.Secret
		objectKey = client.ObjectKey{Namespace: m.SecretNamespace, Name: m.SecretName}
	)

	if err := m.Client.Get(ctx, objectKey, &ca); err != nil {
		if k8serrors.IsNotFound(err) {
			m.Logger.Info(fmt.Sprintf("no CA certificate Secret %s found, generating CA certificate", objectKey))
			return secrets.CreateClusterCACertificate(ctx, m.Client, objectKey, m.KeyConfig)
		}

		return err
	}
	return nil
}

// setupAnonymousReports sets up and starts the anonymous reporting and returns
// a cleanup function and an error.
// The caller is responsible to call the returned function - when the returned
// error is not nil - to stop the reports sending.
func setupAnonymousReports(
	ctx context.Context,
	restCfg *rest.Config,
	logger logr.Logger,
	metadata metadata.Info,
	cfg Config,
) (func(), error) {
	logger.Info("starting anonymous reports")

	// NOTE: this is needed to break the import cycle between telemetry and manager packages.
	tCfg := telemetry.Config{
		DataPlaneControllerEnabled:          cfg.DataPlaneControllerEnabled,
		DataPlaneBlueGreenControllerEnabled: cfg.DataPlaneBlueGreenControllerEnabled,
		ControlPlaneControllerEnabled:       cfg.ControlPlaneControllerEnabled,
		GatewayControllerEnabled:            cfg.GatewayControllerEnabled,
		KonnectControllerEnabled:            cfg.KonnectControllersEnabled,
		AIGatewayControllerEnabled:          cfg.AIGatewayControllerEnabled,
	}

	tMgr, err := telemetry.CreateManager(telemetry.SignalPing, restCfg, logger, metadata, tCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create anonymous reports manager: %w", err)
	}

	if err := tMgr.Start(); err != nil {
		return nil, fmt.Errorf("anonymous reports failed to start: %w", err)
	}

	if err := tMgr.TriggerExecute(ctx, telemetry.SignalStart); err != nil {
		// We failed to send initial start signal with telemetry data.
		// Don't abort and return an error, just log an error and continue.
		logger.WithValues("error", err).
			Info("failed to send an initial telemetry start signal")
	}

	return tMgr.Stop, nil
}
