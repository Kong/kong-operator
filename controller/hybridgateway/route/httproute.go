package route

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/controller/pkg/op"
	"github.com/kong/kong-operator/controller/pkg/patch"
	gwtypes "github.com/kong/kong-operator/internal/types"
	"github.com/kong/kong-operator/pkg/vars"
)

// TODO(mlavacca): re-evaluate if this structure still makes sense or we can
// extract the status logic in a simpler way, even considering the addition
// of more route types.

// HTTPRouteKey identifies HTTPRoute resources.
const HTTPRouteKey = "HTTPRoute"

// httpRouteStatusUpdater updates status for HTTPRoute resources.
type httpRouteStatusUpdater struct {
	client.Client

	logger                     logr.Logger
	route                      gwtypes.HTTPRoute
	parentProgrammedConditions map[string][]metav1.Condition
	sharedStatusMap            *SharedRouteStatusMap
}

// newHTTPRouteStatusUpdater returns a new httpRouteStatusUpdater for the given HTTPRoute.
func newHTTPRouteStatusUpdater(route gwtypes.HTTPRoute, cl client.Client, logger logr.Logger, sharedStatusMap *SharedRouteStatusMap) *httpRouteStatusUpdater {
	return &httpRouteStatusUpdater{
		Client:                     cl,
		logger:                     logger,
		route:                      route,
		parentProgrammedConditions: map[string][]metav1.Condition{},
		sharedStatusMap:            sharedStatusMap,
	}
}

// ComputeStatus checks backend service programming and updates parent conditions.
func (r *httpRouteStatusUpdater) ComputeStatus() {
	backendRefs := make(map[string]any)
	for _, rule := range r.route.Spec.Rules {
		for _, backendRef := range rule.BackendRefs {
			namespace := r.route.Namespace
			if backendRef.Namespace != nil {
				namespace = string(*backendRef.Namespace)
			}
			backendNN := namespace + "/" + string(backendRef.Name)
			backendRefs[backendNN] = nil
		}
	}

	for _, parentRef := range r.route.Spec.ParentRefs {
		serviceProgrammedCondition := metav1.Condition{
			Type:    ConditionTypeBackendsProgrammed,
			Status:  metav1.ConditionTrue,
			Reason:  ConditionReasonBackendsProgrammed,
			Message: "All backend services are programmed",
		}

		namespace := r.route.Namespace
		if parentRef.Namespace != nil {
			namespace = string(*parentRef.Namespace)
		}
		gatewayKey := namespace + "/" + string(parentRef.Name)
		routeKey := r.route.Namespace + "/" + r.route.Name
		key := StatusMapKey(HTTPRouteKey, routeKey, gatewayKey)
		var allProgrammed = true
		for k := range backendRefs {
			n, initiated := r.sharedStatusMap.GetProgrammedServices(key, k)
			if !initiated {
				// Don't touch the condition if the service controller hasn't processed this service yet.
				break
			}
			if n != 1 {
				allProgrammed = false
				break
			}
		}
		if !allProgrammed {
			serviceProgrammedCondition.Status = metav1.ConditionFalse
			serviceProgrammedCondition.Reason = ConditionReasonBackendsNotProgrammed
			serviceProgrammedCondition.Message = "Not all backend services are programmed"
		}

		r.parentProgrammedConditions[gatewayKey] = append(r.parentProgrammedConditions[gatewayKey], serviceProgrammedCondition)
	}
}

// EnforceStatus applies computed status to the HTTPRoute resource.
func (r *httpRouteStatusUpdater) EnforceStatus(ctx context.Context) (op.Result, error) {
	newRoute := r.route.DeepCopy()
	for _, parentRef := range r.route.Spec.ParentRefs {
		namespace := r.route.Namespace
		if parentRef.Namespace != nil {
			namespace = string(*parentRef.Namespace)
		}
		gatewayKey := namespace + "/" + string(parentRef.Name)

		conditions, ok := r.parentProgrammedConditions[gatewayKey]
		if !ok {
			continue
		}
		var parentRefStatus gwtypes.RouteParentStatus
		var index = -1
		for i, parentStatus := range newRoute.Status.Parents {
			if parentStatus.ParentRef.Name == parentRef.Name && ((parentStatus.ParentRef.Namespace == nil && parentRef.Namespace == nil) || *parentStatus.ParentRef.Namespace == *parentRef.Namespace) {
				parentRefStatus = parentStatus
				index = i
				break
			}
		}

		for _, cond := range conditions {
			meta.SetStatusCondition(&parentRefStatus.Conditions, cond)
		}
		if newRoute.Status.Parents == nil {
			newRoute.Status.Parents = []gwtypes.RouteParentStatus{}
		}
		initParentStatus(&parentRefStatus, parentRef)
		if index == -1 {
			newRoute.Status.Parents = append(newRoute.Status.Parents, parentRefStatus)
		} else {
			newRoute.Status.Parents[index] = parentRefStatus
		}
	}
	return patch.ApplyStatusPatchIfNotEmpty(ctx, r.Client, r.logger, newRoute, &r.route)
}

func initParentStatus(parentStatus *gwtypes.RouteParentStatus, parentRef gwtypes.ParentReference) {
	parentStatus.ParentRef = parentRef
	parentStatus.ControllerName = gwtypes.GatewayController(vars.ControllerName())
	if parentStatus.Conditions == nil {
		parentStatus.Conditions = []metav1.Condition{}
	}
}
