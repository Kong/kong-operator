package hybridgateway

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kong/kong-operator/controller/hybridgateway/converter"
	"github.com/kong/kong-operator/controller/hybridgateway/route"
	"github.com/kong/kong-operator/controller/hybridgateway/watch"
	"github.com/kong/kong-operator/controller/pkg/log"
	"github.com/kong/kong-operator/internal/utils/index"
)

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongroutes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongroutes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongservices,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongupstreams,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongupstreams/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongtargets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongtargets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongplugins/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongpluginbindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongpluginbindings/status,verbs=get;update;patch

// HybridGatewayReconciler is a generic reconciler for handling Gateway API resources
// in a hybrid environment. It operates on objects implementing the RootObject and
// RootObjectPtr interfaces, allowing flexible reconciliation logic for different resource types.
type HybridGatewayReconciler[t converter.RootObject, tPtr converter.RootObjectPtr[t]] struct {
	client.Client

	sharedStatusMap *route.SharedRouteStatusMap
}

// NewHybridGatewayReconciler creates a new instance of GatewayAPIHybridReconciler for the specified
// generic types t and tPtr. It initializes the reconciler with the client from the provided manager.
func NewHybridGatewayReconciler[t converter.RootObject, tPtr converter.RootObjectPtr[t]](mgr ctrl.Manager, sharedStatusMap *route.SharedRouteStatusMap) *HybridGatewayReconciler[t, tPtr] {
	return &HybridGatewayReconciler[t, tPtr]{
		Client:          mgr.GetClient(),
		sharedStatusMap: sharedStatusMap,
	}
}

// SetupWithManager sets up the controller with the provided manager.
// It registers the reconciler to watch and manage resources of type 'u'.
func (r *HybridGatewayReconciler[t, tPtr]) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	obj := any(new(t)).(tPtr)
	filter, err := watch.FilterBy(r.Client, obj)
	if err != nil {
		return err
	}
	builder := ctrl.NewControllerManagedBy(mgr).
		For(obj).
		WithEventFilter(predicate.NewPredicateFuncs(filter))

	// Add watches for owned resources.
	for _, owned := range watch.Owns(obj) {
		builder = builder.Owns(owned)
	}

	// Watch for services to trigger reconciliation of HTTPRoutes that reference them.
	// Watch for services to trigger reconciliation of HTTPRoutes that reference them.
	builder.Watches(
		&corev1.Service{},
		handler.EnqueueRequestsFromMapFunc(r.findHTTPRoutesForService),
	)

	// Watch for endpoint slices to trigger reconciliation of HTTPRoutes that reference them.
	builder.Watches(
		&discoveryv1.EndpointSlice{},
		handler.EnqueueRequestsFromMapFunc(r.findHTTPRoutesForEndpointSlice),
	)

	return builder.Complete(r)
}

// Reconcile reconciles the state of a custom resource by fetching the object,
// converting it to the expected type, translating it, and enforcing its desired state.
func (r *HybridGatewayReconciler[t, tPtr]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var obj tPtr = new(t)

	logger := ctrllog.FromContext(ctx).WithName("HybridGateway")

	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	rootObj, ok := any(*obj).(t)
	if !ok {
		return ctrl.Result{}, fmt.Errorf("failed to convert object of type %T to route object type %T", obj, rootObj)
	}

	gvk := obj.GetObjectKind().GroupVersionKind()
	log.Debug(logger, "Reconciling object", "Group", gvk.Group, "Kind", gvk.Kind)

	conv, err := converter.NewConverter(rootObj, r.Client, r.sharedStatusMap)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := Translate(conv, ctx); err != nil {
		return ctrl.Result{}, err
	}

	requeue, _, err := EnforceState(ctx, r.Client, logger, conv)
	if err != nil || requeue {
		return ctrl.Result{Requeue: true}, err
	}

	if err := CleanOrphanedResources[t, tPtr](ctx, r.Client, logger, conv); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *HybridGatewayReconciler[t, tPtr]) findHTTPRoutesForService(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := ctrllog.FromContext(ctx).WithName("HybridGatewayServiceWatcher")
	service, ok := obj.(*corev1.Service)
	if !ok {
		logger.Error(fmt.Errorf("unexpected type %T, expected %T", obj, &corev1.Service{}), "failed to cast object to service")
		return nil
	}
	return r.httpRoutesForService(ctx, service)
}

func (r *HybridGatewayReconciler[t, tPtr]) findHTTPRoutesForEndpointSlice(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := ctrllog.FromContext(ctx).WithName("HybridGatewayEndpointSliceWatcher")
	endpointSlice, ok := obj.(*discoveryv1.EndpointSlice)
	if !ok {
		logger.Error(fmt.Errorf("unexpected type %T, expected %T", obj, &discoveryv1.EndpointSlice{}), "failed to cast object to endpointslice")
		return nil
	}

	serviceName, ok := endpointSlice.Labels[discoveryv1.LabelServiceName]
	if !ok {
		logger.Info("endpointslice has no service name label", "namespace", endpointSlice.Namespace, "name", endpointSlice.Name)
		return nil
	}

	service := &corev1.Service{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: endpointSlice.Namespace, Name: serviceName}, service); err != nil {
		logger.Error(err, "failed to get service for endpointslice", "servicename", serviceName, "endpointslicenamespace", endpointSlice.Namespace)
		return nil
	}

	return r.httpRoutesForService(ctx, service)
}

func (r *HybridGatewayReconciler[t, tPtr]) httpRoutesForService(ctx context.Context, service *corev1.Service) []reconcile.Request {
	logger := ctrllog.FromContext(ctx).WithName("HybridGatewayWatcher")
	var httpRoutes gatewayv1.HTTPRouteList
	if err := r.List(ctx, &httpRoutes,
		client.MatchingFields{
			index.BackendServicesOnHTTPRouteIndex: service.Namespace + "/" + service.Name,
		},
	); err != nil {
		logger.Error(err, "failed to list httproutes")
		return nil
	}

	requests := make(map[reconcile.Request]struct{})
	for _, httpRoute := range httpRoutes.Items {
		for _, rule := range httpRoute.Spec.Rules {
			for _, backendRef := range rule.BackendRefs {
				if backendRef.Kind != nil && *backendRef.Kind == "Service" && backendRef.Name == gatewayv1.ObjectName(service.Name) {
					namespace := httpRoute.Namespace
					if backendRef.Namespace != nil {
						namespace = string(*backendRef.Namespace)
					}
					if namespace == service.Namespace {
						requests[reconcile.Request{
							NamespacedName: types.NamespacedName{
								Namespace: httpRoute.Namespace,
								Name:      httpRoute.Name,
							},
						}] = struct{}{}
					}
				}
			}
		}
	}

	reqs := make([]reconcile.Request, 0, len(requests))
	for req := range requests {
		reqs = append(reqs, req)
	}

	return reqs
}
