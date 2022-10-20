package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	operatorv1alpha1 "github.com/kong/gateway-operator/apis/v1alpha1"
	operatorerrors "github.com/kong/gateway-operator/internal/errors"
	gwtypes "github.com/kong/gateway-operator/internal/types"
	dataplaneutils "github.com/kong/gateway-operator/internal/utils/dataplane"
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

	return string(gatewayClass.Spec.ControllerName) == vars.ControllerName
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
	gatewayClass, ok := obj.(*gatewayv1beta1.GatewayClass)
	if !ok {
		log.FromContext(context.Background()).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "GatewayClass", "found", reflect.TypeOf(obj),
		)
		return
	}

	gateways := new(gatewayv1beta1.GatewayList)
	if err := r.Client.List(context.Background(), gateways); err != nil {
		log.FromContext(context.Background()).Error(err, "could not list gateways in map func")
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

func (r *GatewayReconciler) listGatewaysForGatewayConfig(obj client.Object) (recs []reconcile.Request) {
	ctx := context.Background()
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

// TODO: this was a quick fix for demos, but a better solution would
// be to use the Owns() functionality to allow the DataPlane to inform
// the Gateway when it's had address changes, rather than this daisy
// chaining to the Service.
//
// See: https://github.com/Kong/gateway-operator/issues/272
func (r *GatewayReconciler) listGatewaysForService(obj client.Object) (recs []reconcile.Request) {
	ctx := context.Background()
	logger := log.FromContext(ctx)

	service, ok := obj.(*corev1.Service)
	if !ok {
		logger.Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "Service", "found", reflect.TypeOf(obj),
		)
		return
	}

	for _, owner := range service.OwnerReferences {
		ourAPIVersion := fmt.Sprintf("%s/%s", operatorv1alpha1.SchemeGroupVersion.Group, operatorv1alpha1.SchemeGroupVersion.Version)
		if owner.APIVersion == ourAPIVersion && owner.Kind == "DataPlane" {
			dataPlane := &operatorv1alpha1.DataPlane{}
			if err := r.Client.Get(ctx, client.ObjectKey{
				Namespace: service.Namespace,
				Name:      owner.Name,
			}, dataPlane); err != nil {
				// if the object doesn't exist, that's fine there's nothing to do.
				// if we get any other error however, log it.
				if !k8serrors.IsNotFound(err) {
					logger.Error(err, "failed to run map funcs")
				}
				return
			}

			for _, owner := range dataPlane.OwnerReferences {
				if strings.Contains(owner.APIVersion, gatewayv1beta1.GroupName) && owner.Kind == "Gateway" {
					recs = append(recs, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Namespace: service.Namespace,
							Name:      owner.Name,
						},
					})
				}
			}
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

func (r *GatewayReconciler) setControlplaneGatewayConfigDefaults(gateway *gwtypes.Gateway, gatewayConfig *operatorv1alpha1.GatewayConfiguration, dataplaneName, dataplaneServiceName string) error {
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

	if _, err := setControlPlaneDefaults(gatewayConfig.Spec.ControlPlaneDeploymentOptions, gateway.Namespace, dataplaneServiceName, dontOverride, r.DevelopmentMode); err != nil {
		return err
	}

	return nil
}
