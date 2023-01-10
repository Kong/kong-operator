package manager

import (
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/kong/gateway-operator/controllers"
	"github.com/kong/gateway-operator/internal/utils/index"
)

// -----------------------------------------------------------------------------
// Controller Manager - Controller Definition Interfaces
// -----------------------------------------------------------------------------

// Controller is a Kubernetes controller that can be plugged into Manager.
type Controller interface {
	SetupWithManager(ctrl.Manager) error
}

// AutoHandler decides whether the specific controller shall be enabled (true) or disabled (false).
type AutoHandler func(client.Client) bool

// ControllerDef is a specification of a Controller that can be conditionally registered with Manager.
type ControllerDef struct {
	Enabled     bool
	AutoHandler AutoHandler
	Controller  Controller
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

	if c.AutoHandler != nil {
		if enable := c.AutoHandler(mgr.GetClient()); !enable {
			return nil
		}
	}
	return c.Controller.SetupWithManager(mgr)
}

func setupIndexes(mgr manager.Manager) error {
	return index.IndexDataPlaneNameOnControlPlane(mgr.GetCache())
}

func setupControllers(mgr manager.Manager, c *Config) []ControllerDef {
	controllers := []ControllerDef{
		// GatewayClass controller
		{
			Enabled: c.GatewayControllerEnabled,
			AutoHandler: crdExistsChecker{
				GVR: schema.GroupVersionResource{
					Group:    gatewayv1beta1.SchemeGroupVersion.Group,
					Version:  gatewayv1beta1.SchemeGroupVersion.Version,
					Resource: "gatewayclasses",
				},
			}.CRDExists,
			Controller: &controllers.GatewayClassReconciler{
				Client:          mgr.GetClient(),
				Scheme:          mgr.GetScheme(),
				DevelopmentMode: c.DevelopmentMode,
			},
		},
		// Gateway controller
		{
			Enabled: c.GatewayControllerEnabled,
			AutoHandler: crdExistsChecker{
				GVR: schema.GroupVersionResource{
					Group:    gatewayv1beta1.SchemeGroupVersion.Group,
					Version:  gatewayv1beta1.SchemeGroupVersion.Version,
					Resource: "gateways",
				},
			}.CRDExists,
			Controller: &controllers.GatewayReconciler{
				Client:          mgr.GetClient(),
				Scheme:          mgr.GetScheme(),
				DevelopmentMode: c.DevelopmentMode,
			},
		},
		// ControlPlane controller
		{
			Enabled: c.ControlPlaneControllerEnabled,
			Controller: &controllers.ControlPlaneReconciler{
				Client:                   mgr.GetClient(),
				Scheme:                   mgr.GetScheme(),
				ClusterCASecretName:      c.ClusterCASecretName,
				ClusterCASecretNamespace: c.ClusterCASecretNamespace,
				DevelopmentMode:          c.DevelopmentMode,
			},
		},
		// DataPlane controller
		{
			Enabled: c.DataPlaneControllerEnabled,
			Controller: &controllers.DataPlaneReconciler{
				Client:                   mgr.GetClient(),
				Scheme:                   mgr.GetScheme(),
				ClusterCASecretName:      c.ClusterCASecretName,
				ClusterCASecretNamespace: c.ClusterCASecretNamespace,
				DevelopmentMode:          c.DevelopmentMode,
			},
		},
	}

	return controllers
}

// crdExistsChecker verifies whether the resource type defined by GVR is supported by the k8s apiserver.
type crdExistsChecker struct {
	GVR schema.GroupVersionResource
}

// CRDExists returns true iff the apiserver supports the specified group/version/resource.
func (c crdExistsChecker) CRDExists(r client.Client) bool {
	return CRDExists(r, c.GVR)
}

// CRDExists returns false if CRD does not exist.
func CRDExists(client client.Client, gvr schema.GroupVersionResource) bool {
	_, err := client.RESTMapper().KindFor(gvr)
	return !meta.IsNoMatchError(err)
}
