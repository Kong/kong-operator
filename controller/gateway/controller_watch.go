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

	configurationv1alpha1 "github.com/kong/kong-operator/api/configuration/v1alpha1"
	operatorv1beta1 "github.com/kong/kong-operator/api/gateway-operator/v1beta1"
	operatorv2beta1 "github.com/kong/kong-operator/api/gateway-operator/v2beta1"
	konnectv1alpha1 "github.com/kong/kong-operator/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/api/konnect/v1alpha2"
	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/controller/pkg/secrets/ref"
	operatorerrors "github.com/kong/kong-operator/internal/errors"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/internal/utils/gatewayclass"
	"github.com/kong/kong-operator/internal/utils/index"
	"github.com/kong/kong-operator/pkg/vars"
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

// listGatewayReconcileRequestsForSecret returns reconcile requests for Gateways
// that reference the given Secret via listeners.tls.certificateRefs.
// It uses a field index to efficiently find matching Gateways.
func (r *Reconciler) listGatewayReconcileRequestsForSecret(ctx context.Context, s *corev1.Secret) []reconcile.Request {
	var gwList gwtypes.GatewayList
	nn := client.ObjectKeyFromObject(s)
	if err := r.List(ctx, &gwList, client.MatchingFields{index.TLSCertificateSecretsOnGatewayIndex: nn.String()}); err != nil {
		ctrllog.FromContext(ctx).Error(err, "failed to list indexed gateways for Secret watch", "secret", nn)
		return nil
	}
	recs := make([]reconcile.Request, 0, len(gwList.Items))
	for i := range gwList.Items {
		gw := gwList.Items[i]
		// Only enqueue Gateways managed by this controller.
		if !r.gatewayHasMatchingGatewayClass(&gw) {
			continue
		}
		recs = append(recs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&gw)})
	}
	return recs
}

// listGatewaysForKonnectExtension is a watch predicate which finds all Gateways
// that use a GatewayConfiguration that references a specific KonnectExtension.
func (r *Reconciler) listGatewaysForKonnectExtension(ctx context.Context, ext *konnectv1alpha2.KonnectExtension) []reconcile.Request {
	gatewayConfigurationsRequests := index.ListObjectsReferencingKonnectExtension(r.Client, &operatorv2beta1.GatewayConfigurationList{})(ctx, ext)
	gatewayConfigurations := lo.Map(gatewayConfigurationsRequests, func(req reconcile.Request, _ int) operatorv2beta1.GatewayConfiguration {
		return operatorv2beta1.GatewayConfiguration{
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

	gatewayConfig, ok := obj.(*operatorv2beta1.GatewayConfiguration)
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
			gatewayClass.Spec.ParametersRef.Name == gatewayConfig.Name &&
			gatewayClass.Spec.ParametersRef.Namespace != nil &&
			string(*gatewayClass.Spec.ParametersRef.Namespace) == gatewayConfig.Namespace {
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

// listGatewaysForKongReferenceGrant returns reconcile requests for Gateways that might be affected by
// a KongReferenceGrant that allows GatewayConfiguration -> KonnectAPIAuthConfiguration references.
func (r *Reconciler) listGatewaysForKongReferenceGrant(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := ctrllog.FromContext(ctx)

	grant, ok := obj.(*configurationv1alpha1.KongReferenceGrant)
	if !ok {
		logger.Error(
			fmt.Errorf("unexpected object type"),
			"KongReferenceGrant watch predicate received unexpected object type",
			"expected", "*configurationv1alpha1.KongReferenceGrant", "found", reflect.TypeOf(obj),
		)
		return nil
	}

	var fromNamespaces []string
	for _, from := range grant.Spec.From {
		if string(from.Group) == operatorv2beta1.SchemeGroupVersion.Group &&
			string(from.Kind) == "GatewayConfiguration" {
			fromNamespaces = append(fromNamespaces, string(from.Namespace))
		}
	}
	if len(fromNamespaces) == 0 {
		return nil
	}

	var (
		allowAnyAuth bool
		authNames    = map[string]struct{}{}
	)
	for _, to := range grant.Spec.To {
		if string(to.Group) != konnectv1alpha1.GroupVersion.Group ||
			string(to.Kind) != "KonnectAPIAuthConfiguration" {
			continue
		}
		if to.Name == nil {
			allowAnyAuth = true
			break
		}
		authNames[string(*to.Name)] = struct{}{}
	}
	if !allowAnyAuth && len(authNames) == 0 {
		return nil
	}

	recs := make([]reconcile.Request, 0)
	seen := map[types.NamespacedName]struct{}{}
	for _, ns := range fromNamespaces {
		var gatewayConfigList operatorv2beta1.GatewayConfigurationList
		if err := r.List(ctx, &gatewayConfigList, client.InNamespace(ns)); err != nil {
			logger.Error(err, "failed to list GatewayConfigurations for KongReferenceGrant", "namespace", ns)
			continue
		}
		for i := range gatewayConfigList.Items {
			gatewayConfig := &gatewayConfigList.Items[i]
			if gatewayConfig.Spec.Konnect == nil || gatewayConfig.Spec.Konnect.APIAuthConfigurationRef == nil {
				continue
			}
			authRef := gatewayConfig.Spec.Konnect.APIAuthConfigurationRef
			authNamespace := gatewayConfig.Namespace
			if authRef.Namespace != nil && *authRef.Namespace != "" {
				authNamespace = *authRef.Namespace
			}
			if authNamespace != grant.Namespace {
				continue
			}
			if !allowAnyAuth {
				if _, ok := authNames[authRef.Name]; !ok {
					continue
				}
			}

			for _, req := range r.listGatewaysForGatewayConfig(ctx, gatewayConfig) {
				if _, ok := seen[req.NamespacedName]; ok {
					continue
				}
				seen[req.NamespacedName] = struct{}{}
				recs = append(recs, req)
			}
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
