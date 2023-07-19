package controllers

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
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	"github.com/kong/gateway-operator/internal/consts"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	dataplaneutils "github.com/kong/gateway-operator/internal/utils/dataplane"
	k8sutils "github.com/kong/gateway-operator/internal/utils/kubernetes"
	"github.com/kong/gateway-operator/internal/utils/kubernetes/resources"
	"github.com/kong/gateway-operator/pkg/vars"
)

// -----------------------------------------------------------------------------
// GatewayReconciler - Watch Predicates
// -----------------------------------------------------------------------------

func (r *GatewayReconciler) gatewayHasMatchingGatewayClass(obj client.Object) bool {
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

func (r *GatewayReconciler) gatewayClassMatchesController(obj client.Object) bool {
	gatewayClass, ok := obj.(*gatewayv1beta1.GatewayClass)
	if !ok {
		log.FromContext(context.Background()).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run predicate function",
			"expected", "GatewayClass", "found", reflect.TypeOf(obj),
		)
		return false
	}

	return string(gatewayClass.Spec.ControllerName) == vars.ControllerName()
}

func (r *GatewayReconciler) gatewayConfigurationMatchesController(obj client.Object) bool {
	ctx := context.Background()

	gatewayClassList := new(gatewayv1beta1.GatewayClassList)
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

// -----------------------------------------------------------------------------
// GatewayReconciler - Watch Map Funcs
// -----------------------------------------------------------------------------

func (r *GatewayReconciler) listGatewaysForGatewayClass(ctx context.Context, obj client.Object) (recs []reconcile.Request) {
	gatewayClass, ok := obj.(*gatewayv1beta1.GatewayClass)
	if !ok {
		log.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "GatewayClass", "found", reflect.TypeOf(obj),
		)
		return
	}

	gateways := new(gatewayv1beta1.GatewayList)
	if err := r.Client.List(ctx, gateways); err != nil {
		log.FromContext(ctx).Error(err, "could not list gateways in map func")
		return
	}

	for _, gateway := range gateways.Items {
		if gateway.Spec.GatewayClassName == gatewayv1beta1.ObjectName(gatewayClass.Name) {
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

func (r *GatewayReconciler) listGatewaysForGatewayConfig(ctx context.Context, obj client.Object) (recs []reconcile.Request) {
	logger := log.FromContext(ctx)

	gatewayConfig, ok := obj.(*operatorv1alpha1.GatewayConfiguration)
	if !ok {
		logger.Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "GatewayConfiguration", "found", reflect.TypeOf(obj),
		)
		return
	}

	gatewayClassList := new(gatewayv1beta1.GatewayClassList)
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

	gatewayList := new(gatewayv1beta1.GatewayList)
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
	if gatewayConfig.Spec.DataPlaneOptions == nil {
		gatewayConfig.Spec.DataPlaneOptions = new(operatorv1alpha1.DataPlaneOptions)
	}
	dataplaneutils.SetDataPlaneDefaults(gatewayConfig.Spec.DataPlaneOptions)
}

func (r *GatewayReconciler) setControlplaneGatewayConfigDefaults(gateway *gwtypes.Gateway, gatewayConfig *operatorv1alpha1.GatewayConfiguration, dataplaneName, dataplaneProxyServiceName string) error { //nolint:unparam
	dontOverride := make(map[string]struct{})
	if gatewayConfig.Spec.ControlPlaneOptions == nil {
		gatewayConfig.Spec.ControlPlaneOptions = new(operatorv1alpha1.ControlPlaneOptions)
	}
	if gatewayConfig.Spec.ControlPlaneOptions.DataPlane == nil ||
		*gatewayConfig.Spec.ControlPlaneOptions.DataPlane == "" {
		gatewayConfig.Spec.ControlPlaneOptions.DataPlane = &dataplaneName
	}

	if gatewayConfig.Spec.ControlPlaneOptions.Deployment.PodTemplateSpec == nil {
		gatewayConfig.Spec.ControlPlaneOptions.Deployment.PodTemplateSpec = &corev1.PodTemplateSpec{}
	}
	container := k8sutils.GetPodContainerByName(&gatewayConfig.Spec.ControlPlaneOptions.Deployment.PodTemplateSpec.Spec, consts.ControlPlaneControllerContainerName)
	if container == nil {
		container = lo.ToPtr(resources.GenerateControlPlaneContainer(""))
	}
	for _, env := range container.Env {
		dontOverride[env.Name] = struct{}{}
	}

	_ = setControlPlaneDefaults(gatewayConfig.Spec.ControlPlaneOptions, dontOverride, controlPlaneDefaultsArgs{
		namespace:                 gateway.Namespace,
		dataplaneProxyServiceName: dataplaneProxyServiceName,
	})

	setControlPlaneOptionsDefaults(gatewayConfig.Spec.ControlPlaneOptions)

	return nil
}
