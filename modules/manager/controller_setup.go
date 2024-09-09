package manager

import (
	"context"
	"fmt"
	"reflect"

	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1alpha1 "github.com/kong/gateway-operator/api/v1alpha1"
	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/controller/controlplane"
	"github.com/kong/gateway-operator/controller/dataplane"
	"github.com/kong/gateway-operator/controller/gateway"
	"github.com/kong/gateway-operator/controller/gatewayclass"
	"github.com/kong/gateway-operator/controller/kongplugininstallation"
	"github.com/kong/gateway-operator/controller/konnect"
	konnectops "github.com/kong/gateway-operator/controller/konnect/ops"
	"github.com/kong/gateway-operator/controller/specialized"
	"github.com/kong/gateway-operator/internal/utils/index"
	dataplanevalidator "github.com/kong/gateway-operator/internal/validation/dataplane"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"

	configurationv1 "github.com/kong/kubernetes-configuration/api/configuration/v1"
	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
	configurationv1beta1 "github.com/kong/kubernetes-configuration/api/configuration/v1beta1"
	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

const (
	// GatewayClassControllerName is the name of the GatewayClass controller.
	GatewayClassControllerName = "GatewayClass"
	// GatewayControllerName is the name of the GatewayClass controller.
	GatewayControllerName = "Gateway"
	// ControlPlaneControllerName is the name of the GatewayClass controller.
	ControlPlaneControllerName = "ControlPlane"
	// DataPlaneControllerName is the name of the GatewayClass controller.
	DataPlaneControllerName = "DataPlane"
	// DataPlaneBlueGreenControllerName is the name of the GatewayClass controller.
	DataPlaneBlueGreenControllerName = "DataPlaneBlueGreen"
	// DataPlaneOwnedServiceFinalizerControllerName is the name of the GatewayClass controller.
	DataPlaneOwnedServiceFinalizerControllerName = "DataPlaneOwnedServiceFinalizer"
	// DataPlaneOwnedSecretFinalizerControllerName is the name of the GatewayClass controller.
	DataPlaneOwnedSecretFinalizerControllerName = "DataPlaneOwnedSecretFinalizer"
	// DataPlaneOwnedDeploymentFinalizerControllerName is the name of the GatewayClass controller.
	DataPlaneOwnedDeploymentFinalizerControllerName = "DataPlaneOwnedDeploymentFinalizer"
	// AIGatewayControllerName is the name of the GatewayClass controller.
	AIGatewayControllerName = "AIGateway"

	// KongPluginInstallationControllerName is the name of the KongPluginInstallation controller.
	KongPluginInstallationControllerName = "KongPluginInstallation"

	// KonnectAPIAuthConfigurationControllerName is the name of the KonnectAPIAuthConfiguration controller.
	KonnectAPIAuthConfigurationControllerName = "KonnectAPIAuthConfiguration"
	// KonnectGatewayControlPlaneControllerName is the name of the KonnectGatewayControlPlane controller.
	KonnectGatewayControlPlaneControllerName = "KonnectGatewayControlPlane"
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
		if err := index.DataPlaneNameOnControlPlane(ctx, mgr.GetCache()); err != nil {
			return fmt.Errorf("failed to setup index for DataPlane names on ControlPlane: %w", err)
		}
	}
	return nil
}

// SetupControllers returns a list of ControllerDefs based on config.
func SetupControllers(mgr manager.Manager, c *Config) (map[string]ControllerDef, error) {
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
			},
		},
		{
			Condition: c.AIGatewayControllerEnabled,
			GVRs: []schema.GroupVersionResource{
				operatorv1alpha1.AIGatewayGVR(),
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

	controllers := map[string]ControllerDef{
		// GatewayClass controller
		GatewayClassControllerName: {
			Enabled: c.GatewayControllerEnabled,
			Controller: &gatewayclass.Reconciler{
				Client:          mgr.GetClient(),
				Scheme:          mgr.GetScheme(),
				DevelopmentMode: c.DevelopmentMode,
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
				DevelopmentMode:          c.DevelopmentMode,
				Validator:                dataplanevalidator.NewValidator(mgr.GetClient()),
				Callbacks: dataplane.DataPlaneCallbacks{
					BeforeDeployment: dataplane.CreateCallbackManager(),
					AfterDeployment:  dataplane.CreateCallbackManager(),
				},
				DefaultImage: consts.DefaultDataPlaneImage,
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
				DataPlaneController: &dataplane.Reconciler{
					Client:                   mgr.GetClient(),
					Scheme:                   mgr.GetScheme(),
					ClusterCASecretName:      c.ClusterCASecretName,
					ClusterCASecretNamespace: c.ClusterCASecretNamespace,
					DevelopmentMode:          c.DevelopmentMode,
					Validator:                dataplanevalidator.NewValidator(mgr.GetClient()),
					DefaultImage:             consts.DefaultDataPlaneImage,
					Callbacks: dataplane.DataPlaneCallbacks{
						BeforeDeployment: dataplane.CreateCallbackManager(),
						AfterDeployment:  dataplane.CreateCallbackManager(),
					},
				},
				Callbacks: dataplane.DataPlaneCallbacks{
					BeforeDeployment: dataplane.CreateCallbackManager(),
					AfterDeployment:  dataplane.CreateCallbackManager(),
				},
				DefaultImage: consts.DefaultDataPlaneImage,
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
		sdkFactory := konnectops.NewSDKFactory()
		konnectControllers := map[string]ControllerDef{
			KonnectAPIAuthConfigurationControllerName: {
				Enabled: c.KonnectControllersEnabled,
				Controller: konnect.NewKonnectAPIAuthConfigurationReconciler(
					sdkFactory,
					c.DevelopmentMode,
					mgr.GetClient(),
				),
			},
			KonnectGatewayControlPlaneControllerName: {
				Enabled: c.KonnectControllersEnabled,
				Controller: konnect.NewKonnectEntityReconciler(
					sdkFactory,
					c.DevelopmentMode,
					mgr.GetClient(),
					konnect.WithKonnectEntitySyncPeriod[konnectv1alpha1.KonnectGatewayControlPlane](c.KonnectSyncPeriod),
				),
			},
			KongServiceControllerName: {
				Enabled: c.KonnectControllersEnabled,
				Controller: konnect.NewKonnectEntityReconciler(
					sdkFactory,
					c.DevelopmentMode,
					mgr.GetClient(),
					konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongService](c.KonnectSyncPeriod),
				),
			},
			KongRouteControllerName: {
				Enabled: c.KonnectControllersEnabled,
				Controller: konnect.NewKonnectEntityReconciler(
					sdkFactory,
					c.DevelopmentMode,
					mgr.GetClient(),
					konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongRoute](c.KonnectSyncPeriod),
				),
			},
			KongConsumerControllerName: {
				Enabled: c.KonnectControllersEnabled,
				Controller: konnect.NewKonnectEntityReconciler(
					sdkFactory,
					c.DevelopmentMode,
					mgr.GetClient(),
					konnect.WithKonnectEntitySyncPeriod[configurationv1.KongConsumer](c.KonnectSyncPeriod),
				),
			},
			KongConsumerGroupControllerName: {
				Enabled: c.KonnectControllersEnabled,
				Controller: konnect.NewKonnectEntityReconciler(
					sdkFactory,
					c.DevelopmentMode,
					mgr.GetClient(),
					konnect.WithKonnectEntitySyncPeriod[configurationv1beta1.KongConsumerGroup](c.KonnectSyncPeriod),
				),
			},
			KongPluginBindingControllerName: {
				Enabled: c.KonnectControllersEnabled,
				Controller: konnect.NewKonnectEntityReconciler[configurationv1alpha1.KongPluginBinding](
					sdkFactory,
					c.DevelopmentMode,
					mgr.GetClient(),
					konnect.WithKonnectEntitySyncPeriod[configurationv1alpha1.KongPluginBinding](c.KonnectSyncPeriod),
				),
			},
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
