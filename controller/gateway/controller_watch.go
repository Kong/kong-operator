package gateway

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/controller/pkg/controlplane"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
	"github.com/kong/gateway-operator/pkg/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/pkg/vars"
)

// -----------------------------------------------------------------------------
// GatewayReconciler - Watch Predicates
// -----------------------------------------------------------------------------

func (r *Reconciler) gatewayHasMatchingGatewayClass(obj client.Object) bool {
	gateway, ok := obj.(*gwtypes.Gateway)
	if !ok {
		log.FromContext(context.Background()).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run predicate function",
			"expected", "Gateway", "found", reflect.TypeOf(obj),
		)
		return false
	}

	_, err := r.verifyGatewayClassSupport(context.Background(), gateway)
	if err != nil {
		// filtering here is just an optimization, the reconciler will check the
		// class as well. If we fail here it's most likely because of some failure
		// of the Kubernetes API and it's technically better to enqueue the object
		// than to drop it for eventual consistency during cluster outages.
		return !errors.Is(err, operatorerrors.ErrUnsupportedGateway)
	}

	return true
}

func (r *Reconciler) gatewayConfigurationMatchesController(obj client.Object) bool {
	ctx := context.Background()

	gatewayClassList := new(gatewayv1.GatewayClassList)
	if err := r.Client.List(ctx, gatewayClassList); err != nil {
		log.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run predicate function",
			"expected", "GatewayClass", "found", reflect.TypeOf(obj),
		)
		// filtering here is just an optimization, the reconciler will check the
		// class as well. If we fail here it's most likely because of some failure
		// of the Kubernetes API and it's technically better to enqueue the object
		// than to drop it for eventual consistency during cluster outages.
		return true
	}

	for _, gatewayClass := range gatewayClassList.Items {
		if string(gatewayClass.Spec.ControllerName) == vars.ControllerName() {
			return true
		}
	}

	return false
}

// Predicates to filter only the ReferenceGrants that allow a Gateway
// cross-namespace reference.
func referenceGrantHasGatewayFrom(obj client.Object) bool {
	grant, ok := obj.(*gatewayv1beta1.ReferenceGrant)
	if !ok {
		return false
	}
	for _, from := range grant.Spec.From {
		if from.Kind == "Gateway" && from.Group == gatewayv1.GroupName {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// GatewayReconciler - Watch Map Funcs
// -----------------------------------------------------------------------------

func (r *Reconciler) listGatewaysForGatewayClass(ctx context.Context, obj client.Object) (recs []reconcile.Request) {
	gatewayClass, ok := obj.(*gatewayv1.GatewayClass)
	if !ok {
		log.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "GatewayClass", "found", reflect.TypeOf(obj),
		)
		return
	}

	gateways := new(gatewayv1.GatewayList)
	if err := r.Client.List(ctx, gateways); err != nil {
		log.FromContext(ctx).Error(err, "could not list gateways in map func")
		return
	}

	for _, gateway := range gateways.Items {
		if gateway.Spec.GatewayClassName == gatewayv1.ObjectName(gatewayClass.Name) {
			recs = append(recs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: gateway.Namespace,
					Name:      gateway.Name,
				},
			})
		}
	}

	return
}

func (r *Reconciler) listGatewaysForGatewayConfig(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := log.FromContext(ctx)

	gatewayConfig, ok := obj.(*operatorv1beta1.GatewayConfiguration)
	if !ok {
		logger.Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "GatewayConfiguration", "found", reflect.TypeOf(obj),
		)
		return nil
	}

	gatewayClassList := new(gatewayv1.GatewayClassList)
	if err := r.Client.List(ctx, gatewayClassList); err != nil {
		log.FromContext(ctx).Error(
			fmt.Errorf("unexpected error occurred while listing GatewayClass resources"),
			"failed to run map funcs",
			"error", err.Error(),
		)
		return nil
	}

	matchingGatewayClasses := make(map[string]struct{})
	for _, gatewayClass := range gatewayClassList.Items {
		if gatewayClass.Spec.ParametersRef != nil &&
			string(gatewayClass.Spec.ParametersRef.Group) == operatorv1beta1.SchemeGroupVersion.Group &&
			string(gatewayClass.Spec.ParametersRef.Kind) == "GatewayConfiguration" &&
			gatewayClass.Spec.ParametersRef.Name == gatewayConfig.Name {
			matchingGatewayClasses[gatewayClass.Name] = struct{}{}
		}
	}

	gatewayList := new(gatewayv1.GatewayList)
	if err := r.Client.List(ctx, gatewayList); err != nil {
		log.FromContext(ctx).Error(
			fmt.Errorf("unexpected error occurred while listing Gateway resources"),
			"failed to run map funcs",
			"error", err.Error(),
		)
		return nil
	}

	var recs []reconcile.Request
	for _, gateway := range gatewayList.Items {
		if _, ok := matchingGatewayClasses[string(gateway.Spec.GatewayClassName)]; ok {
			recs = append(recs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: gateway.Namespace,
					Name:      gateway.Name,
				},
			})
		}
	}
	return recs
}

// listReferenceGrantsForGateway is a watch predicate which finds all Gateways mentioned in a From clause for a
// ReferenceGrant.
func (r *Reconciler) listReferenceGrantsForGateway(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := log.FromContext(ctx)

	grant, ok := obj.(*gatewayv1beta1.ReferenceGrant)
	if !ok {
		logger.Error(
			fmt.Errorf("unexpected object type"),
			"Referencegrant watch predicate received unexpected object type",
			"expected", "*gatewayapi.ReferenceGrant", "found", reflect.TypeOf(obj),
		)
		return nil
	}
	gateways := &gatewayv1.GatewayList{}
	if err := r.Client.List(ctx, gateways); err != nil {
		logger.Error(err, "Failed to list gateways in watch", "referencegrant", grant.Name)
		return nil
	}
	recs := []reconcile.Request{}
	for _, gateway := range gateways.Items {
		for _, from := range grant.Spec.From {
			if string(from.Namespace) == gateway.Namespace &&
				from.Kind == gatewayv1beta1.Kind("Gateway") &&
				from.Group == gatewayv1beta1.Group("gateway.networking.k8s.io") {
				recs = append(recs, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: gateway.Namespace,
						Name:      gateway.Name,
					},
				})
			}
		}
	}
	return recs
}

// listManagedGatewaysInNamespace is a watch predicate which finds all Gateways
// in provided namespace.
func (r *Reconciler) listManagedGatewaysInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := log.FromContext(ctx)

	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		logger.Error(
			fmt.Errorf("unexpected object type"),
			"Namespace watch predicate received unexpected object type",
			"expected", "*corev1.Namespace", "found", reflect.TypeOf(obj),
		)
		return nil
	}
	gateways := &gatewayv1.GatewayList{}
	if err := r.Client.List(ctx, gateways, &client.ListOptions{
		Namespace: ns.Name,
	}); err != nil {
		logger.Error(err, "Failed to list gateways in watch", "namespace", ns.Name)
		return nil
	}
	recs := make([]reconcile.Request, 0, len(gateways.Items))
	for _, gateway := range gateways.Items {
		objKey := client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}

		var gatewaClass gatewayv1.GatewayClass
		if err := r.Client.Get(ctx, objKey, &gatewaClass); err != nil {
			logger.Error(
				fmt.Errorf("failed to get GatewayClass"),
				"failed to Get Gateway's GatewayClass",
				"gatewayClass", objKey.Name, "gateway", gateway.Name, "namespace", gateway.Namespace,
			)
			continue
		}
		if string(gatewaClass.Spec.ControllerName) == vars.ControllerName() {
			recs = append(recs, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: gateway.Namespace,
					Name:      gateway.Name,
				},
			})
		}
	}
	return recs
}

// listGatewaysAttachedByHTTPRoute is a watch predicate which finds all Gateways mentioned
// in HTTPRoutes' Parents field.
func (r *Reconciler) listGatewaysAttachedByHTTPRoute(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := log.FromContext(ctx)

	httpRoute, ok := obj.(*gatewayv1beta1.HTTPRoute)
	if !ok {
		logger.Error(
			fmt.Errorf("unexpected object type"),
			"HTTPRoute watch predicate received unexpected object type",
			"expected", "*gatewayapi.HTTPRoute", "found", reflect.TypeOf(obj),
		)
		return nil
	}
	gateways := &gatewayv1.GatewayList{}
	if err := r.Client.List(ctx, gateways); err != nil {
		logger.Error(err, "Failed to list gateways in watch", "HTTPRoute", httpRoute.Name)
		return nil
	}
	var recs []reconcile.Request
	for _, gateway := range gateways.Items {
		for _, parentRef := range httpRoute.Spec.ParentRefs {
			if parentRef.Group != nil && string(*parentRef.Group) == gatewayv1.GroupName &&
				parentRef.Kind != nil && string(*parentRef.Kind) == "Gateway" &&
				string(parentRef.Name) == gateway.Name {
				recs = append(recs, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: gateway.Namespace,
						Name:      gateway.Name,
					},
				})
			}
		}
	}
	return recs
}

// -----------------------------------------------------------------------------
// GatewayReconciler - Config Defaults
// -----------------------------------------------------------------------------

func (r *Reconciler) setDataPlaneGatewayConfigDefaults(gatewayConfig *operatorv1beta1.GatewayConfiguration) {
	if gatewayConfig.Spec.DataPlaneOptions == nil {
		gatewayConfig.Spec.DataPlaneOptions = new(operatorv1beta1.GatewayConfigDataPlaneOptions)
	}
}

func (r *Reconciler) setControlPlaneGatewayConfigDefaults(gateway *gwtypes.Gateway,
	gatewayConfig *operatorv1beta1.GatewayConfiguration,
	dataplaneName,
	dataplaneIngressServiceName,
	dataplaneAdminServiceName,
	controlPlaneName string,
) {
	if gatewayConfig.Spec.ControlPlaneOptions == nil {
		gatewayConfig.Spec.ControlPlaneOptions = new(operatorv1beta1.ControlPlaneOptions)
	}
	if gatewayConfig.Spec.ControlPlaneOptions.DataPlane == nil ||
		*gatewayConfig.Spec.ControlPlaneOptions.DataPlane == "" {
		gatewayConfig.Spec.ControlPlaneOptions.DataPlane = &dataplaneName
	}

	if gatewayConfig.Spec.ControlPlaneOptions.Deployment.PodTemplateSpec == nil {
		gatewayConfig.Spec.ControlPlaneOptions.Deployment.PodTemplateSpec = &corev1.PodTemplateSpec{}
	}

	controlPlanePodTemplateSpec := gatewayConfig.Spec.ControlPlaneOptions.Deployment.PodTemplateSpec
	container := k8sutils.GetPodContainerByName(&controlPlanePodTemplateSpec.Spec, consts.ControlPlaneControllerContainerName)
	if container == nil {
		// We currently do not require an image to be specified for ControlPlanes
		// hence we need to check if it has been provided.
		// If it wasn't then add it by appending the generated ControlPlane to
		// GatewayConfiguration spec.
		// This change will not be saved in the API server (i.e. user applied resource
		// will not be changed) - which is the desired behavior - since the caller
		// only uses the changed GatewayConfiguration to generate ControlPlane resource.
		container = lo.ToPtr[corev1.Container](resources.GenerateControlPlaneContainer(
			resources.GenerateContainerForControlPlaneParams{
				Image: consts.DefaultControlPlaneImage,
			},
		))
		controlPlanePodTemplateSpec.Spec.Containers = append(controlPlanePodTemplateSpec.Spec.Containers, *container)
	}

	// an actual ControlPlane will have ObjectMeta populated with ownership information. this includes a stand-in to
	// satisfy the signature
	_ = controlplane.SetDefaults(gatewayConfig.Spec.ControlPlaneOptions,
		controlplane.DefaultsArgs{
			Namespace:                   gateway.Namespace,
			DataPlaneIngressServiceName: dataplaneIngressServiceName,
			DataPlaneAdminServiceName:   dataplaneAdminServiceName,
			OwnedByGateway:              gateway.Name,
			ControlPlaneName:            controlPlaneName,
			AnonymousReportsEnabled:     controlplane.DeduceAnonymousReportsEnabled(r.DevelopmentMode, gatewayConfig.Spec.ControlPlaneOptions),
		})

	setControlPlaneOptionsDefaults(gatewayConfig.Spec.ControlPlaneOptions)
}
