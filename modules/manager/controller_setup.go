package manager

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/gateway-operator/controller/controlplane"
	"github.com/kong/gateway-operator/controller/dataplane"
	"github.com/kong/gateway-operator/controller/gateway"
	"github.com/kong/gateway-operator/controller/gatewayclass"
	"github.com/kong/gateway-operator/controller/kongplugininstallation"
	"github.com/kong/gateway-operator/controller/konnect"
	"github.com/kong/gateway-operator/controller/konnect/constraints"
	sdkops "github.com/kong/gateway-operator/controller/konnect/ops/sdk"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/controller/pkg/secrets"
	"github.com/kong/gateway-operator/controller/specialized"
	"github.com/kong/gateway-operator/internal/metrics"
	"github.com/kong/gateway-operator/internal/utils/index"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	operatorv1alpha1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1alpha1"
	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

const (
	// GatewayClassControllerName is the name of the GatewayClass controller.
	GatewayClassControllerName = "GatewayClass"
	// GatewayControllerName is the name of the Gateway controller.
	GatewayControllerName = "Gateway"
	// ControlPlaneControllerName is the name of ControlPlane controller.
	ControlPlaneControllerName = "ControlPlane"
	// DataPlaneControllerName is the name of the DataPlane controller.
	DataPlaneControllerName = "DataPlane"
	// DataPlaneBlueGreenControllerName is the name of the DataPlaneBlueGreen controller.
	DataPlaneBlueGreenControllerName = "DataPlaneBlueGreen"
	// DataPlaneOwnedServiceFinalizerControllerName is the name of the DataPlaneOwnedServiceFinalizer controller.
	DataPlaneOwnedServiceFinalizerControllerName = "DataPlaneOwnedServiceFinalizer"
	// DataPlaneOwnedSecretFinalizerControllerName is the name of the DataPlaneOwnedSecretFinalizer controller.
	DataPlaneOwnedSecretFinalizerControllerName = "DataPlaneOwnedSecretFinalizer"
	// DataPlaneOwnedDeploymentFinalizerControllerName is the name of the DataPlaneOwnedDeploymentFinalizer controller.
	DataPlaneOwnedDeploymentFinalizerControllerName = "DataPlaneOwnedDeploymentFinalizer"
	// KonnectExtensionControllerName is the name of the KonnectExtension controller.
	KonnectExtensionControllerName = "KonnectExtension"
	// AIGatewayControllerName is the name of the AIGateway controller.
	AIGatewayControllerName = "AIGateway"
	// KongPluginInstallationControllerName is the name of the KongPluginInstallation controller.
	KongPluginInstallationControllerName = "KongPluginInstallation"
	// KonnectAPIAuthConfigurationControllerName is the name of the KonnectAPIAuthConfiguration controller.
	KonnectAPIAuthConfigurationControllerName = "KonnectAPIAuthConfiguration"
	// KonnectGatewayControlPlaneControllerName is the name of the KonnectGatewayControlPlane controller.
	KonnectGatewayControlPlaneControllerName = "KonnectGatewayControlPlane"
	// KonnectCloudGatewayNetworkControllerName is the name of the KonnectCloudGatewayNetwork controller.
	KonnectCloudGatewayNetworkControllerName = "KonnectCloudGatewayNetwork"
	// KongServiceControllerName is the name of the KongService controller.
	KongServiceControllerName = "KongService"
	// KongRouteControllerName is the name of the KongRoute controller.
	KongRouteControllerName = "KongRoute"
	// KongConsumerControllerName is the name of the KongConsumer controller.
	KongConsumerControllerName = "KongConsumer"
	// KongConsumerGroupControllerName is the name of the KongConsumerGroup controller.
	KongConsumerGroupControllerName = "KongConsumerGroup"
	// KongPluginBindingControllerName is the name of the KongPluginBinding controller.
	KongPluginBindingControllerName = "KongPluginBinding"
	// KongPluginControllerName is the name of the KongPlugin controller.
	KongPluginControllerName = "KongPlugin"
	// KongUpstreamControllerName is the name of the KongUpstream controller.
	KongUpstreamControllerName = "KongUpstream"
	// KongTargetControllerName is the name of the KongTarget controller.
	KongTargetControllerName = "KongTarget"
	// KongServicePluginBindingFinalizerControllerName is the name of the KongService PluginBinding finalizer controller.
	KongServicePluginBindingFinalizerControllerName = "KongServicePluginBindingFinalizer"
	// KongRoutePluginBindingFinalizerControllerName is the name of the KongRoute PluginBinding finalizer controller.
	KongRoutePluginBindingFinalizerControllerName = "KongRoutePluginBindingFinalizer"
	// KongConsumerPluginBindingFinalizerControllerName is the name of the KongConsumer PluginBinding finalizer controller.
	KongConsumerPluginBindingFinalizerControllerName = "KongConsumerPluginBindingFinalizer"
	// KongConsumerGroupPluginBindingFinalizerControllerName is the name of the KongConsumerGroup PluginBinding finalizer controller.
	KongConsumerGroupPluginBindingFinalizerControllerName = "KongConsumerGroupPluginBindingFinalizer"
	// KongCredentialsSecretControllerName is the name of the Credentials Secret controller.
	KongCredentialsSecretControllerName = "KongCredentialSecret"
	// KongCredentialBasicAuthControllerName is the name of the KongCredentialBasicAuth controller.
	KongCredentialBasicAuthControllerName = "KongCredentialBasicAuth" //nolint:gosec
	// KongCredentialAPIKeyControllerName is the name of the KongCredentialAPIKey controller.
	KongCredentialAPIKeyControllerName = "KongCredentialAPIKey" //nolint:gosec
	// KongCredentialACLControllerName is the name of the KongCredentialACL controller.
	KongCredentialACLControllerName = "KongCredentialACL" //nolint:gosec
	// KongCredentialHMACControllerName is the name of the KongCredentialHMAC controller.
	KongCredentialHMACControllerName = "KongCredentialHMAC" //nolint:gosec
	// KongCredentialJWTControllerName is the name of the KongCredentialJWT controller.
	KongCredentialJWTControllerName = "KongCredentialJWT" //nolint:gosec
	// KongCACertificateControllerName is the name of the KongCACertificate controller.
	KongCACertificateControllerName = "KongCACertificate"
	// KongCertificateControllerName is the name of the KongCertificate controller.
	KongCertificateControllerName = "KongCertificate"
	// KongVaultControllerName is the name of KongVault controller.
	KongVaultControllerName = "KongVault"
	// KongKeyControllerName is the name of KongKey controller.
	KongKeyControllerName = "KongKey"
	// KongKeySetControllerName is the name of KongKeySet controller.
	KongKeySetControllerName = "KongKeySet"
	// KongSNIControllerName is the name of KongSNI controller.
	KongSNIControllerName = "KongSNI"
	// KongDataPlaneClientCertificateControllerName is the name of KongDataPlaneClientCertificate controller.
	KongDataPlaneClientCertificateControllerName = "KongDataPlaneClientCertificate"
)

// SetupControllersShim runs SetupControllers and returns its result as a slice of the map values.
func SetupControllersShim(mgr manager.Manager, c *Config) ([]ControllerDef, error) {
	controllers, err := SetupControllers(mgr, c)
	if err != nil {
		return []ControllerDef{}, err
	}

	return lo.Values(controllers), nil
}

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

func setupIndexes(ctx context.Context, mgr manager.Manager, cfg Config) error {
	if cfg.ControlPlaneControllerEnabled || cfg.GatewayControllerEnabled {
		log.GetLogger(ctx, "ControlPlane", cfg.DevelopmentMode).Info(
			"creating index",
			"indexField", index.DataPlaneNameIndex,
		)
		if err := index.DataPlaneNameOnControlPlane(ctx, mgr.GetCache()); err != nil {
			return fmt.Errorf("failed to setup index for DataPlane names on ControlPlane: %w", err)
		}
		if cfg.KongPluginInstallationControllerEnabled {
			log.GetLogger(ctx, "DataPlane", cfg.DevelopmentMode).Info(
				"creating index",
				"indexField", index.KongPluginInstallationsIndex,
			)
			if err := index.KongPluginInstallationsOnDataPlane(ctx, mgr.GetCache()); err != nil {
				return fmt.Errorf("failed to setup index for KongPluginInstallations on DataPlane: %w", err)
			}
		}
		if cfg.KonnectControllersEnabled {
			if err := index.DataPlaneOnDataPlaneKonnecExtension(ctx, mgr.GetCache()); err != nil {
				return fmt.Errorf("failed to setup index for DataPlanes on KonnectExtensions: %w", err)
			}
		}
	}
	return nil
}

// SetupControllers returns a list of ControllerDefs based on config.
func SetupControllers(mgr manager.Manager, c *Config) (map[string]ControllerDef, error) {
	ctx := context.Background()
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
				operatorv1beta1.ControlPlaneGVR(),
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
			Condition: c.KongPluginInstallationControllerEnabled,
			GVRs: []schema.GroupVersionResource{
				operatorv1alpha1.KongPluginInstallationGVR(),
			},
		},
		{
			Condition: c.KonnectControllersEnabled,
			GVRs: []schema.GroupVersionResource{
				{
					Group:    konnectv1alpha1.SchemeGroupVersion.Group,
					Version:  konnectv1alpha1.SchemeGroupVersion.Version,
					Resource: "konnectextensions",
				},
				{
					Group:    konnectv1alpha1.SchemeGroupVersion.Group,
					Version:  konnectv1alpha1.SchemeGroupVersion.Version,
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

	keyType, err := keyTypeToX509PublicKeyAlgorithm(c.ClusterCAKeyType)
	if err != nil {
		return nil, fmt.Errorf("unsupported cluster CA key type: %w", err)
	}

	clusterCAKeyConfig := secrets.KeyConfig{
		Type: keyType,
		Size: c.ClusterCAKeySize,
	}

	controllers := map[string]ControllerDef{
		// GatewayClass controller
		GatewayClassControllerName: {
			Enabled: c.GatewayControllerEnabled,
			Controller: &gatewayclass.Reconciler{
				Client:                        mgr.GetClient(),
				Scheme:                        mgr.GetScheme(),
				DevelopmentMode:               c.DevelopmentMode,
				GatewayAPIExperimentalEnabled: c.GatewayAPIExperimentalEnabled,
			},
		},
		// Gateway controller
		GatewayControllerName: {
			Enabled: c.GatewayControllerEnabled,
			Controller: &gateway.Reconciler{
				Client:                mgr.GetClient(),
				Scheme:                mgr.GetScheme(),
				DevelopmentMode:       c.DevelopmentMode,
				DefaultDataPlaneImage: consts.DefaultDataPlaneImage,
			},
		},
		// ControlPlane controller
		ControlPlaneControllerName: {
			Enabled: c.GatewayControllerEnabled || c.ControlPlaneControllerEnabled,
			Controller: &controlplane.Reconciler{
				Client:                   mgr.GetClient(),
				Scheme:                   mgr.GetScheme(),
				ClusterCASecretName:      c.ClusterCASecretName,
				ClusterCASecretNamespace: c.ClusterCASecretNamespace,
				ClusterCAKeyConfig:       clusterCAKeyConfig,
				DevelopmentMode:          c.DevelopmentMode,
			},
		},
		// DataPlane controller
		DataPlaneControllerName: {
			Enabled: (c.DataPlaneControllerEnabled || c.GatewayControllerEnabled) && !c.DataPlaneBlueGreenControllerEnabled,
			Controller: &dataplane.Reconciler{
				Client:                   mgr.GetClient(),
				Scheme:                   mgr.GetScheme(),
				ClusterCASecretName:      c.ClusterCASecretName,
				ClusterCASecretNamespace: c.ClusterCASecretNamespace,
				ClusterCAKeyConfig:       clusterCAKeyConfig,
				DevelopmentMode:          c.DevelopmentMode,
				Callbacks: dataplane.DataPlaneCallbacks{
					BeforeDeployment: dataplane.CreateCallbackManager(),
					AfterDeployment:  dataplane.CreateCallbackManager(),
				},
				DefaultImage:   consts.DefaultDataPlaneImage,
				KonnectEnabled: c.KonnectControllersEnabled,
			},
		},
		// DataPlaneBlueGreen controller
		DataPlaneBlueGreenControllerName: {
			Enabled: c.DataPlaneBlueGreenControllerEnabled,
			Controller: &dataplane.BlueGreenReconciler{
				Client:                   mgr.GetClient(),
				DevelopmentMode:          c.DevelopmentMode,
				ClusterCASecretName:      c.ClusterCASecretName,
				ClusterCASecretNamespace: c.ClusterCASecretNamespace,
				ClusterCAKeyConfig:       clusterCAKeyConfig,
				DataPlaneController: &dataplane.Reconciler{
					Client:                   mgr.GetClient(),
					Scheme:                   mgr.GetScheme(),
					ClusterCASecretName:      c.ClusterCASecretName,
					ClusterCASecretNamespace: c.ClusterCASecretNamespace,
					ClusterCAKeyConfig:       clusterCAKeyConfig,
					DevelopmentMode:          c.DevelopmentMode,
					DefaultImage:             consts.DefaultDataPlaneImage,
					Callbacks: dataplane.DataPlaneCallbacks{
						BeforeDeployment: dataplane.CreateCallbackManager(),
						AfterDeployment:  dataplane.CreateCallbackManager(),
					},
					KonnectEnabled: c.KonnectControllersEnabled,
				},
				Callbacks: dataplane.DataPlaneCallbacks{
					BeforeDeployment: dataplane.CreateCallbackManager(),
					AfterDeployment:  dataplane.CreateCallbackManager(),
				},
				DefaultImage:   consts.DefaultDataPlaneImage,
				KonnectEnabled: c.KonnectControllersEnabled,
			},
		},
		DataPlaneOwnedServiceFinalizerControllerName: {
			Enabled: c.DataPlaneControllerEnabled || c.DataPlaneBlueGreenControllerEnabled,
			Controller: dataplane.NewDataPlaneOwnedResourceFinalizerReconciler[corev1.Service](
				mgr.GetClient(),
				c.DevelopmentMode,
			),
		},
		DataPlaneOwnedSecretFinalizerControllerName: {
			Enabled: c.DataPlaneControllerEnabled || c.DataPlaneBlueGreenControllerEnabled,
			Controller: dataplane.NewDataPlaneOwnedResourceFinalizerReconciler[corev1.Secret](
				mgr.GetClient(),
				c.DevelopmentMode,
			),
		},
		DataPlaneOwnedDeploymentFinalizerControllerName: {
			Enabled: c.DataPlaneControllerEnabled || c.DataPlaneBlueGreenControllerEnabled,
			Controller: dataplane.NewDataPlaneOwnedResourceFinalizerReconciler[appsv1.Deployment](
				mgr.GetClient(),
				c.DevelopmentMode,
			),
		},
		// AIGateway Controller
		AIGatewayControllerName: {
			Enabled: c.AIGatewayControllerEnabled,
			Controller: &specialized.AIGatewayReconciler{
				Client:          mgr.GetClient(),
				Scheme:          mgr.GetScheme(),
				DevelopmentMode: c.DevelopmentMode,
			},
		},
		// KongPluginInstallation controller
		KongPluginInstallationControllerName: {
			Enabled: c.KongPluginInstallationControllerEnabled,
			Controller: &kongplugininstallation.Reconciler{
				Client:          mgr.GetClient(),
				Scheme:          mgr.GetScheme(),
				DevelopmentMode: c.DevelopmentMode,
			},
		},
	}

	// Konnect controllers
	if c.KonnectControllersEnabled {
		if err := SetupCacheIndicesForKonnectTypes(ctx, mgr, c.DevelopmentMode); err != nil {
			return nil, err
		}

		// REVIEW: Should we define the recorder here, or define it out of the section to allow setting custom metrics in other controllers

		sdkFactory := sdkops.NewSDKFactory()
		controllerFactory := konnectControllerFactory{
			sdkFactory:              sdkFactory,
			devMode:                 c.DevelopmentMode,
			client:                  mgr.GetClient(),
			syncPeriod:              c.KonnectSyncPeriod,
			maxConcurrentReconciles: c.KonnectMaxConcurrentReconciles,
			metricRecorder:          metricRecorder,
		}

		konnectControllers := map[string]ControllerDef{
			KonnectAPIAuthConfigurationControllerName: {
				Enabled: c.KonnectControllersEnabled,
				Controller: konnect.NewKonnectAPIAuthConfigurationReconciler(
					sdkFactory,
					c.DevelopmentMode,
					mgr.GetClient(),
				),
			},

			KongPluginControllerName: {
				Enabled: c.KonnectControllersEnabled,
				Controller: konnect.NewKongPluginReconciler(
					c.DevelopmentMode,
					mgr.GetClient(),
				),
			},

			KongCredentialsSecretControllerName: {
				Enabled: c.KonnectControllersEnabled,
				Controller: konnect.NewKongCredentialSecretReconciler(
					c.DevelopmentMode,
					mgr.GetClient(),
					mgr.GetScheme(),
				),
			},

			KonnectExtensionControllerName: {
				Enabled: (c.DataPlaneControllerEnabled || c.DataPlaneBlueGreenControllerEnabled) && c.KonnectControllersEnabled,
				Controller: konnect.NewKonnectExtensionReconciler(
					sdkFactory,
					c.DevelopmentMode,
					mgr.GetClient(),
				),
			},

			// Controllers responsible for cleaning up KongPluginBinding cleanup finalizers.
			KongServicePluginBindingFinalizerControllerName:       newKonnectPluginController[configurationv1alpha1.KongService](controllerFactory),
			KongRoutePluginBindingFinalizerControllerName:         newKonnectPluginController[configurationv1alpha1.KongRoute](controllerFactory),
			KongConsumerPluginBindingFinalizerControllerName:      newKonnectPluginController[configurationv1.KongConsumer](controllerFactory),
			KongConsumerGroupPluginBindingFinalizerControllerName: newKonnectPluginController[configurationv1beta1.KongConsumerGroup](controllerFactory),

			// Controllers responsible for creating, updating and deleting Konnect entities.
			KonnectGatewayControlPlaneControllerName:     newKonnectEntityController[konnectv1alpha1.KonnectGatewayControlPlane](controllerFactory),
			KonnectCloudGatewayNetworkControllerName:     newKonnectEntityController[konnectv1alpha1.KonnectCloudGatewayNetwork](controllerFactory),
			KongServiceControllerName:                    newKonnectEntityController[configurationv1alpha1.KongService](controllerFactory),
			KongRouteControllerName:                      newKonnectEntityController[configurationv1alpha1.KongRoute](controllerFactory),
			KongConsumerControllerName:                   newKonnectEntityController[configurationv1.KongConsumer](controllerFactory),
			KongConsumerGroupControllerName:              newKonnectEntityController[configurationv1beta1.KongConsumerGroup](controllerFactory),
			KongUpstreamControllerName:                   newKonnectEntityController[configurationv1alpha1.KongUpstream](controllerFactory),
			KongCACertificateControllerName:              newKonnectEntityController[configurationv1alpha1.KongCACertificate](controllerFactory),
			KongCertificateControllerName:                newKonnectEntityController[configurationv1alpha1.KongCertificate](controllerFactory),
			KongTargetControllerName:                     newKonnectEntityController[configurationv1alpha1.KongTarget](controllerFactory),
			KongPluginBindingControllerName:              newKonnectEntityController[configurationv1alpha1.KongPluginBinding](controllerFactory),
			KongCredentialBasicAuthControllerName:        newKonnectEntityController[configurationv1alpha1.KongCredentialBasicAuth](controllerFactory),
			KongCredentialAPIKeyControllerName:           newKonnectEntityController[configurationv1alpha1.KongCredentialAPIKey](controllerFactory),
			KongCredentialACLControllerName:              newKonnectEntityController[configurationv1alpha1.KongCredentialACL](controllerFactory),
			KongCredentialHMACControllerName:             newKonnectEntityController[configurationv1alpha1.KongCredentialHMAC](controllerFactory),
			KongCredentialJWTControllerName:              newKonnectEntityController[configurationv1alpha1.KongCredentialJWT](controllerFactory),
			KongKeyControllerName:                        newKonnectEntityController[configurationv1alpha1.KongKey](controllerFactory),
			KongKeySetControllerName:                     newKonnectEntityController[configurationv1alpha1.KongKeySet](controllerFactory),
			KongDataPlaneClientCertificateControllerName: newKonnectEntityController[configurationv1alpha1.KongDataPlaneClientCertificate](controllerFactory),
			KongVaultControllerName:                      newKonnectEntityController[configurationv1alpha1.KongVault](controllerFactory),
			KongSNIControllerName:                        newKonnectEntityController[configurationv1alpha1.KongSNI](controllerFactory),
			// NOTE: Reconcilers for new supported entities should be added here.
		}

		// Merge Konnect controllers into the controllers map. This is done this way instead of directly assigning
		// to the controllers map to avoid duplicate keys.
		for name, controller := range konnectControllers {
			if _, duplicate := controllers[name]; duplicate {
				return nil, fmt.Errorf("duplicate controller key: %s", name)
			}
			controllers[name] = controller
		}
	}

	return controllers, nil
}

// SetupCacheIndicesForKonnectTypes sets up the cache indices for the controllers.
// This is done only once because 1 manager's cache can only have one index with
// a predefined key and so that different controllers can share the same indices.
func SetupCacheIndicesForKonnectTypes(ctx context.Context, mgr manager.Manager, developmentMode bool) error {
	cl := mgr.GetClient()
	types := []struct {
		Object interface {
			client.Object
			GetTypeName() string
		}
		IndexOptions []konnect.ReconciliationIndexOption
	}{
		{
			Object:       &configurationv1alpha1.KongPluginBinding{},
			IndexOptions: konnect.IndexOptionsForKongPluginBinding(),
		},
		{
			Object:       &configurationv1alpha1.KongCredentialBasicAuth{},
			IndexOptions: konnect.IndexOptionsForCredentialsBasicAuth(),
		},
		{
			Object:       &configurationv1alpha1.KongCredentialACL{},
			IndexOptions: konnect.IndexOptionsForCredentialsACL(),
		},
		{
			Object:       &configurationv1alpha1.KongCredentialJWT{},
			IndexOptions: konnect.IndexOptionsForCredentialsJWT(),
		},
		{
			Object:       &configurationv1alpha1.KongCredentialAPIKey{},
			IndexOptions: konnect.IndexOptionsForCredentialsAPIKey(),
		},
		{
			Object:       &configurationv1alpha1.KongCredentialHMAC{},
			IndexOptions: konnect.IndexOptionsForCredentialsHMAC(),
		},
		{
			Object:       &configurationv1.KongConsumer{},
			IndexOptions: konnect.IndexOptionsForKongConsumer(cl),
		},
		{
			Object:       &configurationv1beta1.KongConsumerGroup{},
			IndexOptions: konnect.IndexOptionsForKongConsumerGroup(cl),
		},
		{
			Object:       &configurationv1alpha1.KongService{},
			IndexOptions: konnect.IndexOptionsForKongService(cl),
		},
		{
			Object:       &configurationv1alpha1.KongRoute{},
			IndexOptions: konnect.IndexOptionsForKongRoute(),
		},
		{
			Object:       &configurationv1alpha1.KongUpstream{},
			IndexOptions: konnect.IndexOptionsForKongUpstream(cl),
		},
		{
			Object:       &configurationv1alpha1.KongTarget{},
			IndexOptions: konnect.IndexOptionsForKongTarget(),
		},
		{
			Object:       &configurationv1alpha1.KongSNI{},
			IndexOptions: konnect.IndexOptionsForKongSNI(),
		},
		{
			Object:       &configurationv1alpha1.KongKey{},
			IndexOptions: konnect.IndexOptionsForKongKey(cl),
		},
		{
			Object:       &configurationv1alpha1.KongKeySet{},
			IndexOptions: konnect.IndexOptionsForKongKeySet(cl),
		},
		{
			Object:       &configurationv1alpha1.KongDataPlaneClientCertificate{},
			IndexOptions: konnect.IndexOptionsForKongDataPlaneCertificate(cl),
		},
		{
			Object:       &configurationv1alpha1.KongVault{},
			IndexOptions: konnect.IndexOptionsForKongVault(cl),
		},
		{
			Object:       &configurationv1alpha1.KongCertificate{},
			IndexOptions: konnect.IndexOptionsForKongCertificate(cl),
		},
		{
			Object:       &configurationv1alpha1.KongCACertificate{},
			IndexOptions: konnect.IndexOptionsForKongCACertificate(cl),
		},
		{
			Object:       &konnectv1alpha1.KonnectGatewayControlPlane{},
			IndexOptions: konnect.IndexOptionsForKonnectGatewayControlPlane(),
		},
		{
			Object:       &konnectv1alpha1.KonnectCloudGatewayNetwork{},
			IndexOptions: konnect.IndexOptionsForKonnectCloudGatewayNetwork(),
		},
		{
			Object:       &konnectv1alpha1.KonnectExtension{},
			IndexOptions: konnect.IndexOptionsForKonnectExtension(),
		},
	}

	for _, t := range types {
		var (
			entityTypeName = constraints.EntityTypeNameForObj(t.Object)
			logger         = log.GetLogger(ctx, entityTypeName, developmentMode)
		)
		for _, ind := range t.IndexOptions {
			logger.Info("creating index", "indexField", ind.IndexField)
			err := mgr.
				GetCache().
				IndexField(ctx, ind.IndexObject, ind.IndexField, ind.ExtractValue)
			if err != nil {
				return fmt.Errorf("failed to setup cache indices for %s: %w", entityTypeName, err)
			}
		}
	}

	return nil
}

type konnectControllerFactory struct {
	sdkFactory              sdkops.SDKFactory
	devMode                 bool
	client                  client.Client
	syncPeriod              time.Duration
	maxConcurrentReconciles uint
	metricRecorder          metrics.Recorder
}

func newKonnectEntityController[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
](f konnectControllerFactory) ControllerDef {
	return ControllerDef{
		Enabled: true,
		Controller: konnect.NewKonnectEntityReconciler(
			f.sdkFactory,
			f.devMode,
			f.client,
			konnect.WithKonnectEntitySyncPeriod[T, TEnt](f.syncPeriod),
			konnect.WithKonnectMaxConcurrentReconciles[T, TEnt](f.maxConcurrentReconciles),
			konnect.WithMetricRecorder[T, TEnt](f.metricRecorder),
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
			f.devMode,
			f.client,
		),
	}
}
