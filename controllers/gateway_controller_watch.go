package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	dataplaneutils "github.com/kong/gateway-operator/internal/utils/dataplane"
	"github.com/kong/gateway-operator/pkg/vars"
)

// -----------------------------------------------------------------------------
// GatewayReconciler - Watch Predicates
// -----------------------------------------------------------------------------

func (r *GatewayReconciler) gatewayHasMatchingGatewayClass(obj client.Object) bool {
	gateway, ok := obj.(*gatewayv1alpha2.Gateway)
	if !ok {
		log.FromContext(context.Background()).Error(
			fmt.Errorf("unexpected object type provided"),
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

func (r *GatewayReconciler) gatewayClassMatchesController(obj client.Object) bool {
	gatewayClass, ok := obj.(*gatewayv1alpha2.GatewayClass)
	if !ok {
		log.FromContext(context.Background()).Error(
			fmt.Errorf("unexpected object type provided"),
			"failed to run predicate function",
			"expected", "GatewayClass", "found", reflect.TypeOf(obj),
		)
		return false
	}

	return string(gatewayClass.Spec.ControllerName) == vars.ControllerName
}

func (r *GatewayReconciler) gatewayConfigurationMatchesController(obj client.Object) bool {
	ctx := context.Background()

	gatewayClassList := new(gatewayv1alpha2.GatewayClassList)
	if err := r.Client.List(ctx, gatewayClassList); err != nil {
		log.FromContext(ctx).Error(
			fmt.Errorf("unexpected object type provided"),
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
		if string(gatewayClass.Spec.ControllerName) == vars.ControllerName {
			return true
		}
	}

	return false
}

// -----------------------------------------------------------------------------
// GatewayReconciler - Watch Map Funcs
// -----------------------------------------------------------------------------

func (r *GatewayReconciler) listGatewaysForGatewayClass(obj client.Object) (recs []reconcile.Request) {
	gatewayClass, ok := obj.(*gatewayv1alpha2.GatewayClass)
	if !ok {
		log.FromContext(context.Background()).Error(
			fmt.Errorf("unexpected object type provided"),
			"failed to run map funcs",
			"expected", "GatewayClass", "found", reflect.TypeOf(obj),
		)
		return
	}

	gateways := new(gatewayv1alpha2.GatewayList)
	if err := r.Client.List(context.Background(), gateways); err != nil {
		log.FromContext(context.Background()).Error(err, "could not list gateways in map func")
		return
	}

	for _, gateway := range gateways.Items {
		if gateway.Spec.GatewayClassName == gatewayv1alpha2.ObjectName(gatewayClass.Name) {
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

func (r *GatewayReconciler) listGatewaysForGatewayConfig(obj client.Object) (recs []reconcile.Request) {
	ctx := context.Background()
	logger := log.FromContext(ctx)

	gatewayConfig, ok := obj.(*operatorv1alpha1.GatewayConfiguration)
	if !ok {
		logger.Error(
			fmt.Errorf("unexpected object type provided"),
			"failed to run map funcs",
			"expected", "GatewayConfiguration", "found", reflect.TypeOf(obj),
		)
		return
	}

	gatewayClassList := new(gatewayv1alpha2.GatewayClassList)
	if err := r.Client.List(ctx, gatewayClassList); err != nil {
		log.FromContext(ctx).Error(
			fmt.Errorf("unexpected error occurred while listing GatewayClass resources"),
			"failed to run map funcs",
			"error", err.Error(),
		)
		return
	}

	matchingGatewayClasses := make(map[string]struct{})
	for _, gatewayClass := range gatewayClassList.Items {
		if gatewayClass.Spec.ParametersRef != nil &&
			string(gatewayClass.Spec.ParametersRef.Group) == operatorv1alpha1.SchemeGroupVersion.Group &&
			string(gatewayClass.Spec.ParametersRef.Kind) == "GatewayConfiguration" &&
			gatewayClass.Spec.ParametersRef.Name == gatewayConfig.Name {
			matchingGatewayClasses[gatewayClass.Name] = struct{}{}
		}
	}

	gatewayList := new(gatewayv1alpha2.GatewayList)
	if err := r.Client.List(ctx, gatewayList); err != nil {
		log.FromContext(ctx).Error(
			fmt.Errorf("unexpected error occurred while listing Gateway resources"),
			"failed to run map funcs",
			"error", err.Error(),
		)
		return
	}

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

	return
}

func (r *GatewayReconciler) setDataplaneGatewayConfigDefaults(gatewayConfig *operatorv1alpha1.GatewayConfiguration) {
	if gatewayConfig.Spec.DataPlaneDeploymentOptions == nil {
		gatewayConfig.Spec.DataPlaneDeploymentOptions = new(operatorv1alpha1.DataPlaneDeploymentOptions)
	}
	dataplaneutils.SetDataPlaneDefaults(gatewayConfig.Spec.DataPlaneDeploymentOptions)
}

func (r *GatewayReconciler) setControlplaneGatewayConfigDefaults(gateway *gatewayv1alpha2.Gateway, gatewayConfig *operatorv1alpha1.GatewayConfiguration, dataplaneName, dataplaneServiceName string) {
	dontOverride := make(map[string]struct{})
	if gatewayConfig.Spec.ControlPlaneDeploymentOptions == nil {
		gatewayConfig.Spec.ControlPlaneDeploymentOptions = new(operatorv1alpha1.ControlPlaneDeploymentOptions)
	}
	if gatewayConfig.Spec.ControlPlaneDeploymentOptions.DataPlane == nil ||
		*gatewayConfig.Spec.ControlPlaneDeploymentOptions.DataPlane == "" {
		gatewayConfig.Spec.ControlPlaneDeploymentOptions.DataPlane = &dataplaneName
	}
	for _, env := range gatewayConfig.Spec.ControlPlaneDeploymentOptions.Env {
		dontOverride[env.Name] = struct{}{}
	}

	setControlPlaneDefaults(gatewayConfig.Spec.ControlPlaneDeploymentOptions, gateway.Namespace, dataplaneServiceName, dontOverride)
}
