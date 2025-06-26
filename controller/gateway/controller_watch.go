package gateway

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/controller/pkg/secrets/ref"
	operatorerrors "github.com/kong/kong-operator/internal/errors"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/internal/utils/gatewayclass"
	"github.com/kong/kong-operator/internal/utils/index"
	"github.com/kong/kong-operator/pkg/vars"

	operatorv1beta1 "github.com/kong/kubernetes-configuration/api/gateway-operator/v1beta1"
	konnectv1alpha2 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha2"
)

// -----------------------------------------------------------------------------
// GatewayReconciler - Watch Predicates
// -----------------------------------------------------------------------------

func (r *Reconciler) gatewayHasMatchingGatewayClass(obj client.Object) bool {
	gateway, ok := obj.(*gwtypes.Gateway)
	if !ok {
		ctrllog.FromContext(context.Background()).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run predicate function",
			"expected", "Gateway", "found", reflect.TypeOf(obj),
		)
		return false
	}

	_, err := gatewayclass.Get(context.Background(), r.Client, string(gateway.Spec.GatewayClassName))
	if err != nil {
		// filtering here is just an optimization, the reconciler will check the
		// class as well. If we fail here it's most likely because of some failure
		// of the Kubernetes API and it's technically better to enqueue the object
		// than to drop it for eventual consistency during cluster outages.
		return !errors.As(err, &operatorerrors.ErrUnsupportedGatewayClass{}) &&
			!errors.As(err, &operatorerrors.ErrNotAcceptedGatewayClass{})
	}

	return true
}

func (r *Reconciler) gatewayConfigurationMatchesController(obj client.Object) bool {
	ctx := context.Background()

	gatewayClassList := new(gatewayv1.GatewayClassList)
	if err := r.List(ctx, gatewayClassList); err != nil {
		ctrllog.FromContext(ctx).Error(
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

func (r *Reconciler) listGatewaysForGatewayClass(ctx context.Context, obj client.Object) (recs []reconcile.Request) {
	gatewayClass, ok := obj.(*gatewayv1.GatewayClass)
	if !ok {
		ctrllog.FromContext(ctx).Error(
			operatorerrors.ErrUnexpectedObject,
			"failed to run map funcs",
			"expected", "GatewayClass", "found", reflect.TypeOf(obj),
		)
		return
	}

	gateways := new(gatewayv1.GatewayList)
	if err := r.List(ctx, gateways); err != nil {
		ctrllog.FromContext(ctx).Error(err, "could not list gateways in map func")
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

// listGatewaysForKonnectExtension is a watch predicate which finds all Gateways
// that use a GatewayConfiguration that references a specific KonnectExtension.
func (r *Reconciler) listGatewaysForKonnectExtension(ctx context.Context, ext *konnectv1alpha2.KonnectExtension) []reconcile.Request {
	gatewayConfigurationsRequests := index.ListObjectsReferencingKonnectExtension(r.Client, &operatorv1beta1.GatewayConfigurationList{})(ctx, ext)
	gatewayConfigurations := lo.Map(gatewayConfigurationsRequests, func(req reconcile.Request, _ int) operatorv1beta1.GatewayConfiguration {
		return operatorv1beta1.GatewayConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: req.Namespace,
				Name:      req.Name,
			},
		}
	})
	affectedGateways := make([]reconcile.Request, 0)
	for _, gwConf := range gatewayConfigurations {
		affectedGateways = append(affectedGateways, r.listGatewaysForGatewayConfig(ctx, &gwConf)...)
	}
	return affectedGateways
}

// listGatewaysForGatewayConfig is a watch predicate which finds all Gateways
// that use a specific GatewayConfiguration.
func (r *Reconciler) listGatewaysForGatewayConfig(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := ctrllog.FromContext(ctx)

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
	if err := r.List(ctx, gatewayClassList); err != nil {
		ctrllog.FromContext(ctx).Error(
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
	if err := r.List(ctx, gatewayList); err != nil {
		ctrllog.FromContext(ctx).Error(
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
	logger := ctrllog.FromContext(ctx)

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
	if err := r.List(ctx, gateways); err != nil {
		logger.Error(err, "Failed to list gateways in watch", "referencegrant", grant.Name)
		return nil
	}
	var recs []reconcile.Request
	for _, gateway := range gateways.Items {
		if ref.IsReferenceGrantForObj(grant, &gateway) {
			recs = append(recs, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&gateway),
			})
		}
	}
	return recs
}

// listManagedGatewaysInNamespace is a watch predicate which finds all Gateways
// in provided namespace.
func (r *Reconciler) listManagedGatewaysInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := ctrllog.FromContext(ctx)

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
	if err := r.List(ctx, gateways, &client.ListOptions{
		Namespace: ns.Name,
	}); err != nil {
		logger.Error(err, "Failed to list gateways in watch", "namespace", ns.Name)
		return nil
	}
	recs := make([]reconcile.Request, 0, len(gateways.Items))
	for _, gateway := range gateways.Items {
		objKey := client.ObjectKey{Name: string(gateway.Spec.GatewayClassName)}

		if _, err := gatewayclass.Get(ctx, r.Client, string(gateway.Spec.GatewayClassName)); err != nil {
			switch {
			case errors.As(err, &operatorerrors.ErrUnsupportedGatewayClass{}):
				log.Debug(logger, "gateway class not supported, ignoring")
			case errors.As(err, &operatorerrors.ErrNotAcceptedGatewayClass{}):
				log.Debug(logger, "gateway class not accepted, ignoring")
			default:
				log.Error(logger, err, "failed to get Gateway's GatewayClass",
					"gatewayClass", objKey.Name,
					"gateway", gateway.Name,
					"namespace", gateway.Namespace,
				)
			}
			continue
		}
		recs = append(recs, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: gateway.Namespace,
				Name:      gateway.Name,
			},
		})
	}
	return recs
}

// listGatewaysAttachedByHTTPRoute is a watch predicate which finds all Gateways mentioned
// in HTTPRoutes' Parents field.
func (r *Reconciler) listGatewaysAttachedByHTTPRoute(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := ctrllog.FromContext(ctx)

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
	if err := r.List(ctx, gateways); err != nil {
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

func (r *Reconciler) setDataPlaneGatewayConfigDefaults(gatewayConfig *GatewayConfiguration) {
	if gatewayConfig.Spec.DataPlaneOptions == nil {
		gatewayConfig.Spec.DataPlaneOptions = new(GatewayConfigDataPlaneOptions)
	}
}

func (r *Reconciler) setControlPlaneGatewayConfigDefaults(
	gateway *gwtypes.Gateway,
	gatewayConfig *GatewayConfiguration,
	dataplaneName,
	dataplaneIngressServiceName,
	dataplaneAdminServiceName,
	controlPlaneName string,
) {
	// TODO(pmalek): add support for GatewayConfiguration v2 https://github.com/kong/kong-operator/issues/1728
	// if gatewayConfig.Spec.ControlPlaneOptions == nil {
	// Set defaults
	// }
}
