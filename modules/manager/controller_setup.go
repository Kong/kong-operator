package manager

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	configurationv1 "github.com/kong/kong-operator/v2/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kong-operator/v2/api/configuration/v1beta1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/v2/controller/controlplane"
	"github.com/kong/kong-operator/v2/controller/cpextensions"
	"github.com/kong/kong-operator/v2/controller/cpextensions/metricsscraper"
	"github.com/kong/kong-operator/v2/controller/dataplane"
	"github.com/kong/kong-operator/v2/controller/gateway"
	"github.com/kong/kong-operator/v2/controller/gatewayclass"
	hybridgateway "github.com/kong/kong-operator/v2/controller/hybridgateway"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/converter"
	"github.com/kong/kong-operator/v2/controller/kongplugininstallation"
	"github.com/kong/kong-operator/v2/controller/konnect"
	"github.com/kong/kong-operator/v2/controller/konnect/constraints"
	sdkops "github.com/kong/kong-operator/v2/controller/konnect/ops/sdk"
	"github.com/kong/kong-operator/v2/controller/mcpserver"
	"github.com/kong/kong-operator/v2/controller/specialized"
	"github.com/kong/kong-operator/v2/ingress-controller/pkg/manager/multiinstance"
	"github.com/kong/kong-operator/v2/internal/metrics"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/internal/utils/index"
	"github.com/kong/kong-operator/v2/modules/manager/logging"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// -----------------------------------------------------------------------------
// Controller Manager - Controller Definition Interfaces
// -----------------------------------------------------------------------------

// Controller is a Kubernetes controller that can be plugged into Manager.
type Controller interface {
	SetupWithManager(context.Context, ctrl.Manager) error
}

// AutoHandler decides whether the specific controller shall be enabled (true) or disabled (false).
type AutoHandler func(client.Client) (bool, error)

// ControllerDef is a specification of a Controller that can be conditionally registered with Manager.
type ControllerDef struct {
	Enabled    bool
	Controller Controller
}

// Name returns a human-readable name of the controller.
func (c *ControllerDef) Name() string {
	return reflect.TypeOf(c.Controller).String()
}

// MaybeSetupWithManager runs SetupWithManager on the controller if it is enabled
// and its AutoHandler (if any) indicates that it can load.
func (c *ControllerDef) MaybeSetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	if !c.Enabled {
		return nil
	}

	return c.Controller.SetupWithManager(ctx, mgr)
}

// SetupCacheIndexes sets up all the cache indexes required by the controllers.
func SetupCacheIndexes(ctx context.Context, mgr manager.Manager, cfg Config) error {
	var indexOptions []index.Option

	if cfg.ControlPlaneControllerEnabled || cfg.GatewayControllerEnabled {
		indexOptions = slices.Concat(indexOptions,
			index.OptionsForControlPlane(cfg.KonnectControllersEnabled),
			index.OptionsForDataPlane(index.DataPlaneFlags{
				KongPluginInstallationControllerEnabled: cfg.KongPluginInstallationControllerEnabled,
				KonnectControllersEnabled:               cfg.KonnectControllersEnabled,
				GatewayAPIGatewayControllerEnabled:      cfg.GatewayControllerEnabled,
			}),
		)
	}

	if cfg.GatewayControllerEnabled {
		indexOptions = slices.Concat(indexOptions,
			index.OptionsForGatewayClass(),
			index.OptionsForGateway(),
			index.OptionsForHTTPRoute(),
		)
	}

	if cfg.KonnectControllersEnabled {
		cl := mgr.GetClient()
		indexOptions = slices.Concat(indexOptions,
			index.OptionsForGatewayConfiguration(),
			index.OptionsForKongPluginBinding(),
			index.OptionsForCredentialsBasicAuth(),
			index.OptionsForCredentialsACL(),
			index.OptionsForCredentialsJWT(),
			index.OptionsForCredentialsAPIKey(),
			index.OptionsForCredentialsHMAC(),
			index.OptionsForKongConsumer(cl),
			index.OptionsForKongConsumerGroup(cl),
			index.OptionsForKongService(cl),
			index.OptionsForKongRoute(cl),
			index.OptionsForKongUpstream(cl),
			index.OptionsForKongTarget(),
			index.OptionsForKongSNI(),
			index.OptionsForKongKey(cl),
			index.OptionsForKongKeySet(cl),
			index.OptionsForKongDataPlaneCertificate(cl),
			index.OptionsForKongVault(cl),
			index.OptionsForKongCertificate(cl),
			index.OptionsForKongCACertificate(cl),
			index.OptionsForKonnectGatewayControlPlane(),
			index.OptionsForKonnectAPIAuthConfiguration(),
			index.OptionsForKonnectCloudGatewayNetwork(),
			index.OptionsForKonnectExtension(),
			index.OptionsForKonnectCloudGatewayDataPlaneGroupConfiguration(cl),
		)
	}

	for _, e := range indexOptions {
		ctrllog.FromContext(ctx).Info("Setting up index", "index", e.String())
		if err := mgr.GetCache().IndexField(ctx, e.Object, e.Field, e.ExtractValueFn); err != nil {
			return fmt.Errorf("failed to set up index %q: %w", e, err)
		}
	}
	return nil
}

// SetupControllers returns a list of ControllerDefs based on config.
func SetupControllers(mgr manager.Manager, c *Config, cpsMgr *multiinstance.Manager) ([]ControllerDef, error) {
	// metricRecorder is the recorder used to record custom metrics in the controller manager's metrics server.
	metricRecorder := metrics.NewGlobalCtrlRuntimeMetricsRecorder()

	// These checks prevent controller-runtime spamming in logs about failing
	// to get informer from cache.
	// This way we only ever check the CRD once and issue clear log entry about
	// particular CRD missing.
	// Also this makes it possible to specify more complex boolean conditions
	// whether to check for particular CRD or not, and also makes it possible to
	// specify several CRDs to be checked for existence, which are required
	// for specific controller to work.
	crdChecks := []struct {
		Condition bool
		GVRs      []schema.GroupVersionResource
	}{
		{
			Condition: c.GatewayControllerEnabled || c.DataPlaneBlueGreenControllerEnabled || c.DataPlaneControllerEnabled,
			GVRs: []schema.GroupVersionResource{
				operatorv1beta1.DataPlaneGVR(),
				operatorv1alpha1.KongPluginInstallationGVR(),
			},
		},
		{
			Condition: c.GatewayControllerEnabled || c.ControlPlaneControllerEnabled,
			GVRs: []schema.GroupVersionResource{
				gwtypes.ControlPlaneGVR(),
			},
		},
		{
			Condition: c.GatewayControllerEnabled,
			GVRs: []schema.GroupVersionResource{
				{
					Group:    gatewayv1.SchemeGroupVersion.Group,
					Version:  gatewayv1.SchemeGroupVersion.Version,
					Resource: "gatewayclasses",
				},
				{
					Group:    gatewayv1.SchemeGroupVersion.Group,
					Version:  gatewayv1.SchemeGroupVersion.Version,
					Resource: "gateways",
				},
				{
					Group:    gatewayv1beta1.SchemeGroupVersion.Group,
					Version:  gatewayv1beta1.SchemeGroupVersion.Version,
					Resource: "referencegrants",
				},
				operatorv1alpha1.KongPluginInstallationGVR(),
			},
		},
		{
			Condition: c.AIGatewayControllerEnabled,
			GVRs: []schema.GroupVersionResource{
				operatorv1alpha1.AIGatewayGVR(),
			},
		},
		{
			Condition: c.FeatureGates.Enabled(FeatureGateMCPServer),
			GVRs: []schema.GroupVersionResource{
				{
					Group:    konnectv1alpha1.SchemeGroupVersion.Group,
					Version:  konnectv1alpha1.SchemeGroupVersion.Version,
					Resource: "mcpservers",
				},
			},
		},
		{
			Condition: c.KongPluginInstallationControllerEnabled,
			GVRs: []schema.GroupVersionResource{
				operatorv1alpha1.KongPluginInstallationGVR(),
			},
		},
		{
			Condition: c.KonnectControllersEnabled,
			GVRs: []schema.GroupVersionResource{
				{
					Group:    konnectv1alpha2.SchemeGroupVersion.Group,
					Version:  konnectv1alpha2.SchemeGroupVersion.Version,
					Resource: "konnectextensions",
				},
				{
					Group:    konnectv1alpha2.SchemeGroupVersion.Group,
					Version:  konnectv1alpha2.SchemeGroupVersion.Version,
					Resource: "konnectgatewaycontrolplanes",
				},
				{
					Group:    konnectv1alpha1.SchemeGroupVersion.Group,
					Version:  konnectv1alpha1.SchemeGroupVersion.Version,
					Resource: "konnectapiauthconfigurations",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongcacertificates",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongcertificates",
				},
				{
					Group:    configurationv1beta1.SchemeGroupVersion.Group,
					Version:  configurationv1beta1.SchemeGroupVersion.Version,
					Resource: "kongconsumergroups",
				},
				{
					Group:    configurationv1.SchemeGroupVersion.Group,
					Version:  configurationv1.SchemeGroupVersion.Version,
					Resource: "kongconsumers",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongcredentialacls",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongcredentialapikeys",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongcredentialbasicauths",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongcredentialhmacs",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongcredentialjwts",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongdataplaneclientcertificates",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongkeys",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongkeysets",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongpluginbindings",
				},
				{
					Group:    configurationv1.SchemeGroupVersion.Group,
					Version:  configurationv1.SchemeGroupVersion.Version,
					Resource: "kongplugins",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongroutes",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongservices",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongsnis",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongtargets",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongupstreams",
				},
				{
					Group:    configurationv1alpha1.SchemeGroupVersion.Group,
					Version:  configurationv1alpha1.SchemeGroupVersion.Version,
					Resource: "kongvaults",
				},
			},
		},
	}

	checker := k8sutils.CRDChecker{Client: mgr.GetClient()}
	for _, check := range crdChecks {
		if !check.Condition {
			continue
		}

		for _, gvr := range check.GVRs {
			if ok, err := checker.CRDExists(gvr); err != nil {
				return nil, err
			} else if !ok {
				return nil, fmt.Errorf("missing a required CRD: %v", gvr)
			}
		}
	}

	const (
		// NOTE: This will be parametrized.
		metricsScrapeInterval = 10 * time.Second
	)
	scrapersMgr := metricsscraper.NewManager(
		mgr.GetLogger(),
		metricsScrapeInterval,
		mgr.GetClient(),
		k8stypes.NamespacedName{
			Name:      c.ClusterCASecretName,
			Namespace: c.ClusterCASecretNamespace,
		},
	)
	if err := mgr.Add(scrapersMgr); err != nil {
		return nil, fmt.Errorf("failed to add scrapers manager to controller-runtime manager: %w", err)
	}
	podLabels, err := k8sutils.GetSelfPodLabels()
	if err != nil {
		if k8sutils.RunningOnKubernetes() {
			return nil, fmt.Errorf("failed to get pod labels: %w", err)
		}
		podLabels = map[string]string{}
	}

	ctrlOpts := controller.Options{
		CacheSyncTimeout: c.CacheSyncTimeout,
	}

	controllers := []ControllerDef{
		// GatewayClass controller
		{
			Enabled: c.GatewayControllerEnabled,
			Controller: &gatewayclass.Reconciler{
				ControllerOptions:             ctrlOpts,
				Client:                        mgr.GetClient(),
				LoggingMode:                   c.LoggingMode,
				GatewayAPIExperimentalEnabled: c.GatewayAPIExperimentalEnabled,
			},
		},
		// Gateway controller
		{
			Enabled: c.GatewayControllerEnabled,
			Controller: &gateway.Reconciler{
				ControllerOptions:       controllerOptions(ctrlOpts, withMaxConcurrentReconciles(int(c.MaxConcurrentReconcilesGateway))),
				Client:                  mgr.GetClient(),
				Scheme:                  mgr.GetScheme(),
				Namespace:               c.ControllerNamespace,
				PodLabels:               podLabels,
				DefaultDataPlaneImage:   consts.DefaultDataPlaneImage,
				KonnectEnabled:          c.KonnectControllersEnabled,
				AnonymousReportsEnabled: c.AnonymousReports,
				LoggingMode:             c.LoggingMode,
			},
		},
		// ControlPlane controller
		{
			Enabled: c.GatewayControllerEnabled || c.ControlPlaneControllerEnabled,
			Controller: &controlplane.Reconciler{
				ControllerOptions:        controllerOptions(ctrlOpts, withMaxConcurrentReconciles(int(c.MaxConcurrentReconcilesControlPlane))),
				AnonymousReportsEnabled:  c.AnonymousReports,
				LoggingMode:              c.LoggingMode,
				Client:                   mgr.GetClient(),
				ClusterCASecretName:      c.ClusterCASecretName,
				ClusterCASecretNamespace: c.ClusterCASecretNamespace,
				SecretLabelSelector:      c.SecretLabelSelector,
				ConfigMapLabelSelector:   c.ConfigMapLabelSelector,
				KonnectEnabled:           c.KonnectControllersEnabled,
				EnforceConfig:            c.EnforceConfig,
				KubeConfigPath:           c.KubeconfigPath,
				RestConfig:               mgr.GetConfig(),
				InstancesManager:         cpsMgr,
				ClusterDomain:            c.ClusterDomain,
				EmitKubernetesEvents:     c.EmitKubernetesEvents,
				WatchNamespaces:          c.WatchNamespaces,
			},
		},
		// DataPlane controller
		{
			Enabled: (c.DataPlaneControllerEnabled || c.GatewayControllerEnabled) && !c.DataPlaneBlueGreenControllerEnabled,
			Controller: &dataplane.Reconciler{
				ControllerOptions:        controllerOptions(ctrlOpts, withMaxConcurrentReconciles(int(c.MaxConcurrentReconcilesDataPlane))),
				Client:                   mgr.GetClient(),
				ClusterCASecretName:      c.ClusterCASecretName,
				ClusterCASecretNamespace: c.ClusterCASecretNamespace,
				SecretLabelSelector:      c.SecretLabelSelector,
				ConfigMapLabelSelector:   c.ConfigMapLabelSelector,
				DefaultImage:             consts.DefaultDataPlaneImage,
				KonnectEnabled:           c.KonnectControllersEnabled,
				EnforceConfig:            c.EnforceConfig,
				LoggingMode:              c.LoggingMode,
				ValidateDataPlaneImage:   c.ValidateImages,
			},
		},
		// DataPlaneBlueGreen controller
		{
			Enabled: c.DataPlaneBlueGreenControllerEnabled,
			Controller: &dataplane.BlueGreenReconciler{
				ControllerOptions:        controllerOptions(ctrlOpts, withMaxConcurrentReconciles(int(c.MaxConcurrentReconcilesDataPlane))),
				CacheSyncTimeout:         c.CacheSyncTimeout,
				Client:                   mgr.GetClient(),
				ClusterCASecretName:      c.ClusterCASecretName,
				ClusterCASecretNamespace: c.ClusterCASecretNamespace,
				SecretLabelSelector:      c.SecretLabelSelector,
				DataPlaneController: &dataplane.Reconciler{
					ControllerOptions:        controllerOptions(ctrlOpts, withMaxConcurrentReconciles(int(c.MaxConcurrentReconcilesDataPlane))),
					Client:                   mgr.GetClient(),
					ClusterCASecretName:      c.ClusterCASecretName,
					ClusterCASecretNamespace: c.ClusterCASecretNamespace,
					SecretLabelSelector:      c.SecretLabelSelector,
					ConfigMapLabelSelector:   c.ConfigMapLabelSelector,
					DefaultImage:             consts.DefaultDataPlaneImage,
					KonnectEnabled:           c.KonnectControllersEnabled,
					EnforceConfig:            c.EnforceConfig,
					ValidateDataPlaneImage:   c.ValidateImages,
					LoggingMode:              c.LoggingMode,
				},
				DefaultImage:           consts.DefaultDataPlaneImage,
				KonnectEnabled:         c.KonnectControllersEnabled,
				EnforceConfig:          c.EnforceConfig,
				ValidateDataPlaneImage: c.ValidateImages,
				LoggingMode:            c.LoggingMode,
			},
		},
		// DataPlaneOwnedServiceFinalizer controller
		{
			Enabled: c.DataPlaneControllerEnabled || c.DataPlaneBlueGreenControllerEnabled,
			Controller: dataplane.NewDataPlaneOwnedResourceFinalizerReconciler[corev1.Service](
				mgr.GetClient(),
				c.LoggingMode,
				ctrlOpts,
			),
		},
		// DataPlaneOwnedSecretFinalizer controller
		{
			Enabled: c.DataPlaneControllerEnabled || c.DataPlaneBlueGreenControllerEnabled,
			Controller: dataplane.NewDataPlaneOwnedResourceFinalizerReconciler[corev1.Secret](
				mgr.GetClient(),
				c.LoggingMode,
				ctrlOpts,
			),
		},
		// DataPlaneOwnedDeploymentFinalizer controller
		{
			Enabled: c.DataPlaneControllerEnabled || c.DataPlaneBlueGreenControllerEnabled,
			Controller: dataplane.NewDataPlaneOwnedResourceFinalizerReconciler[appsv1.Deployment](
				mgr.GetClient(),
				c.LoggingMode,
				ctrlOpts,
			),
		},
		// AIGateway Controller
		{
			Enabled: c.AIGatewayControllerEnabled,
			Controller: &specialized.AIGatewayReconciler{
				ControllerOptions: ctrlOpts,
				Client:            mgr.GetClient(),
				LoggingMode:       c.LoggingMode,
			},
		},
		// KongPluginInstallation controller
		{
			Enabled: c.KongPluginInstallationControllerEnabled,
			Controller: &kongplugininstallation.Reconciler{
				ControllerOptions:      ctrlOpts,
				Client:                 mgr.GetClient(),
				Scheme:                 mgr.GetScheme(),
				LoggingMode:            c.LoggingMode,
				ConfigMapLabelSelector: c.ConfigMapLabelSelector,
			},
		},
		// ControlPlaneExtensions controller
		{
			Enabled: c.ControlPlaneExtensionsControllerEnabled,
			Controller: &cpextensions.Reconciler{
				ControllerOptions:               ctrlOpts,
				Client:                          mgr.GetClient(),
				LoggingMode:                     c.LoggingMode,
				DataPlaneScraperManagerNotifier: scrapersMgr,
			},
		},
	}

	// MCPServer controllers
	if c.FeatureGates.Enabled(FeatureGateMCPServer) {
		controllers = append(controllers, newMCPServerControllers(mgr, c, ctrlOpts)...)
	}

	// Konnect controllers
	if c.KonnectControllersEnabled {
		sdkFactory := sdkops.NewSDKFactory()
		ctrlOpts := controllerOptions(ctrlOpts, withMaxConcurrentReconciles(int(c.MaxConcurrentReconcilesKonnect)))

		controllerFactory := konnectControllerFactory{
			sdkFactory:        sdkFactory,
			loggingMode:       c.LoggingMode,
			client:            mgr.GetClient(),
			syncPeriod:        c.KonnectSyncPeriod,
			controllerOptions: ctrlOpts,
			metricRecorder:    metricRecorder,
		}

		// Add additional Konnect controllers
		controllers = append(
			controllers,
			// KonnectAPIAuthConfiguration controller
			ControllerDef{
				Enabled: c.KonnectControllersEnabled,
				Controller: konnect.NewKonnectAPIAuthConfigurationReconciler(
					ctrlOpts,
					sdkFactory,
					c.LoggingMode,
					mgr.GetClient(),
				),
			},
			// KongPlugin controller
			ControllerDef{
				Enabled: c.KonnectControllersEnabled,
				Controller: konnect.NewKongPluginReconciler(
					ctrlOpts,
					c.LoggingMode,
					mgr.GetClient(),
				),
			},
			// KongCredentialSecret controller
			ControllerDef{
				Enabled: c.KonnectControllersEnabled,
				Controller: konnect.NewKongCredentialSecretReconciler(
					ctrlOpts,
					c.LoggingMode,
					mgr.GetClient(),
					mgr.GetScheme(),
				),
			},
			// KonnectSecretReference controller
			ControllerDef{
				Enabled: c.KonnectControllersEnabled,
				Controller: konnect.NewKonnectSecretReferenceController(
					mgr.GetClient(),
					ctrlOpts,
					c.LoggingMode,
				),
			},
			// KonnectExtension controller
			ControllerDef{
				Enabled: (c.DataPlaneControllerEnabled || c.DataPlaneBlueGreenControllerEnabled) && c.KonnectControllersEnabled,
				Controller: &konnect.KonnectExtensionReconciler{
					ControllerOptions:        ctrlOpts,
					SdkFactory:               sdkFactory,
					LoggingMode:              c.LoggingMode,
					Client:                   mgr.GetClient(),
					SyncPeriod:               c.KonnectSyncPeriod,
					ClusterCASecretName:      c.ClusterCASecretName,
					ClusterCASecretNamespace: c.ClusterCASecretNamespace,
					SecretLabelSelector:      c.SecretLabelSelector,
				},
			},
		)

		// Add controllers responsible for cleaning up KongPluginBinding cleanup finalizers
		controllers = append(
			controllers,
			newKonnectPluginController[configurationv1alpha1.KongService](controllerFactory),
			newKonnectPluginController[configurationv1alpha1.KongRoute](controllerFactory),
			newKonnectPluginController[configurationv1.KongConsumer](controllerFactory),
			newKonnectPluginController[configurationv1beta1.KongConsumerGroup](controllerFactory),
		)

		// Add controllers responsible for creating, updating and deleting Konnect entities
		controllers = append(
			controllers,
			newKonnectEntityController[konnectv1alpha2.KonnectGatewayControlPlane](controllerFactory),
			newKonnectEntityController[konnectv1alpha1.KonnectCloudGatewayNetwork](controllerFactory),
			newKonnectEntityController[konnectv1alpha1.KonnectCloudGatewayDataPlaneGroupConfiguration](controllerFactory),
			newKonnectEntityController[konnectv1alpha1.KonnectCloudGatewayTransitGateway](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongService](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongRoute](controllerFactory),
			newKonnectEntityController[configurationv1.KongConsumer](controllerFactory),
			newKonnectEntityController[configurationv1beta1.KongConsumerGroup](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongUpstream](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongCACertificate](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongCertificate](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongTarget](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongPluginBinding](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongCredentialBasicAuth](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongCredentialAPIKey](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongCredentialACL](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongCredentialHMAC](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongCredentialJWT](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongKey](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongKeySet](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongDataPlaneClientCertificate](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongVault](controllerFactory),
			newKonnectEntityController[configurationv1alpha1.KongSNI](controllerFactory),
		)

		if c.KonnectControllersEnabled {
			controllers = append(controllers,
				newGatewayAPIHybridController[gwtypes.Gateway](mgr, c.FQDNModeEnabled, c.ClusterDomain),
				newGatewayAPIHybridController[gwtypes.HTTPRoute](mgr, c.FQDNModeEnabled, c.ClusterDomain),
			)
		}
	}

	return controllers, nil
}

type konnectControllerFactory struct {
	sdkFactory        sdkops.SDKFactory
	loggingMode       logging.Mode
	client            client.Client
	syncPeriod        time.Duration
	metricRecorder    metrics.Recorder
	controllerOptions controller.Options
}

func newKonnectEntityController[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](f konnectControllerFactory) ControllerDef {
	return ControllerDef{
		Enabled: true,
		Controller: konnect.NewKonnectEntityReconciler(
			f.sdkFactory,
			f.loggingMode,
			f.client,
			konnect.WithKonnectEntitySyncPeriod[T, TEnt](f.syncPeriod),
			konnect.WithMetricRecorder[T, TEnt](f.metricRecorder),
			konnect.WithControllerOptions[T, TEnt](f.controllerOptions),
		),
	}
}

func newKonnectPluginController[
	T constraints.SupportedKonnectEntityPluginReferenceableType,
	TEnt constraints.EntityType[T],
](f konnectControllerFactory) ControllerDef {
	return ControllerDef{
		Enabled: true,
		Controller: konnect.NewKonnectEntityPluginReconciler[T, TEnt](
			f.controllerOptions,
			f.loggingMode,
			f.client,
		),
	}
}

func newGatewayAPIHybridController[t converter.RootObject, tPtr converter.RootObjectPtr[t]](mgr ctrl.Manager, fqdnMode bool, clusterDomain string) ControllerDef {
	return ControllerDef{
		Enabled:    true,
		Controller: hybridgateway.NewHybridGatewayReconciler[t, tPtr](mgr, fqdnMode, clusterDomain),
	}
}

func newMCPServerControllers(mgr manager.Manager, c *Config, ctrlOpts controller.Options) []ControllerDef {
	sm := mcpserver.NewSignalManager(mgr.GetLogger().WithName("mcp-signal-manager"), mgr.GetClient(), mgr.GetScheme())
	sdkFactory := sdkops.NewSDKFactory()
	return []ControllerDef{
		{
			Enabled: true,
			Controller: &mcpserver.MCPServerReconciler{
				ControllerOptions: ctrlOpts,
				Client:            mgr.GetClient(),
				Scheme:            mgr.GetScheme(),
				LoggingMode:       c.LoggingMode,
				SignalManager:     sm,
			},
		},
		{
			Enabled: true,
			Controller: &mcpserver.MCPServerCPReconciler{
				ControllerOptions: ctrlOpts,
				Client:            mgr.GetClient(),
				LoggingMode:       c.LoggingMode,
				SignalManager:     sm,
				SdkFactory:        sdkFactory,
			},
		},
	}
}

func controllerOptions(base controller.Options, opts ...func(*controller.Options)) controller.Options {
	ret := base
	for _, o := range opts {
		o(&ret)
	}
	return ret
}

func withMaxConcurrentReconciles(n int) func(*controller.Options) {
	return func(o *controller.Options) {
		o.MaxConcurrentReconciles = n
	}
}
