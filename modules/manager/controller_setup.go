package manager

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"

	"golang.org/x/exp/maps"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/controller/controlplane"
	"github.com/kong/gateway-operator/controller/dataplane"
	"github.com/kong/gateway-operator/controller/gateway"
	"github.com/kong/gateway-operator/controller/gatewayclass"
	"github.com/kong/gateway-operator/controller/specialized"
	"github.com/kong/gateway-operator/internal/utils/index"
	dataplanevalidator "github.com/kong/gateway-operator/internal/validation/dataplane"
	"github.com/kong/gateway-operator/pkg/consts"
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
)

// SetupControllersShim runs SetupControllers and returns its result as a slice of the map values.
func SetupControllersShim(mgr manager.Manager, c *Config) ([]ControllerDef, error) {
	controllers, err := SetupControllers(mgr, c)
	if err != nil {
		return []ControllerDef{}, err
	}

	return maps.Values(controllers), nil
}

// -----------------------------------------------------------------------------
// Controller Manager - Controller Definition Interfaces
// -----------------------------------------------------------------------------

// Controller is a Kubernetes controller that can be plugged into Manager.
type Controller interface {
	SetupWithManager(ctrl.Manager) error
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
func (c *ControllerDef) MaybeSetupWithManager(mgr ctrl.Manager) error {
	if !c.Enabled {
		return nil
	}

	return c.Controller.SetupWithManager(mgr)
}

func setupIndexes(mgr manager.Manager) error {
	return index.IndexDataPlaneNameOnControlPlane(mgr.GetCache())
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
	}
	checker := crdChecker{client: mgr.GetClient()}
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
	}

	return controllers, nil
}

// crdChecker verifies whether the resource type defined by GVR is supported by the k8s apiserver.
type crdChecker struct {
	client client.Client
}

// CRDExists returns true if the apiserver supports the specified group/version/resource.
func (c crdChecker) CRDExists(gvr schema.GroupVersionResource) (bool, error) {
	_, err := c.client.RESTMapper().KindFor(gvr)

	if meta.IsNoMatchError(err) {
		return false, nil
	}

	if errD := (&discovery.ErrGroupDiscoveryFailed{}); errors.As(err, &errD) {
		for _, e := range errD.Groups {

			// If this is an API StatusError:
			if errS := (&k8serrors.StatusError{}); errors.As(e, &errS) {
				switch errS.ErrStatus.Code {
				case 404:
					// If it's a 404 status code then we're sure that it's just
					// a missing CRD. Don't report an error, just false.
					return false, nil
				default:
					return false, fmt.Errorf("unexpected API error status code when looking up CRD (%v): %w", gvr, err)
				}
			}

			// It is a network error.
			if errU := (&url.Error{}); errors.As(e, &errU) {
				return false, fmt.Errorf("unexpected network error when looking up CRD (%v): %w", gvr, err)
			}
		}

		// Otherwise it's a different error, report a missing CRD.
		return false, err
	}

	return true, nil
}
