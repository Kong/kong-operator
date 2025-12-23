package hybridgateway

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	eventconst "github.com/kong/kong-operator/controller/hybridgateway/const/events"
	finalizerconst "github.com/kong/kong-operator/controller/hybridgateway/const/finalizers"
	"github.com/kong/kong-operator/controller/hybridgateway/converter"
	"github.com/kong/kong-operator/controller/hybridgateway/events"
	"github.com/kong/kong-operator/controller/hybridgateway/watch"
	"github.com/kong/kong-operator/controller/pkg/finalizer"
	"github.com/kong/kong-operator/controller/pkg/log"
)

const (
	// ControllerName is the name used for logging and event recording in the hybrid gateway controller.
	ControllerName = "hybridgateway"
)

//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongroutes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongroutes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongservices/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongupstreams,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongupstreams/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongtargets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongtargets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongplugins,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongplugins/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongpluginbindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongpluginbindings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcertificates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongcertificates/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongsnis,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=configuration.konghq.com,resources=kongsnis/status,verbs=get;update;patch

// HybridGatewayReconciler is a generic reconciler for handling Gateway API resources
// in a hybrid environment. It operates on objects implementing the RootObject and
// RootObjectPtr interfaces, allowing flexible reconciliation logic for different resource types.
type HybridGatewayReconciler[t converter.RootObject, tPtr converter.RootObjectPtr[t]] struct {
	client.Client
	// EventRecorder is used to record Kubernetes events with type-specific reasons.
	eventRecorder *events.TypedEventRecorder
	// FQDNMode indicates whether to use FQDN endpoints for service discovery.
	fqdnMode bool
	// ClusterDomain is the cluster domain to use for FQDN (empty uses service.namespace.svc format).
	clusterDomain string
}

// NewHybridGatewayReconciler creates a new instance of GatewayAPIHybridReconciler for the specified
// generic types t and tPtr. It initializes the reconciler with the client from the provided manager.
func NewHybridGatewayReconciler[t converter.RootObject, tPtr converter.RootObjectPtr[t]](mgr ctrl.Manager, fqdnMode bool, clusterDomain string) *HybridGatewayReconciler[t, tPtr] {
	return &HybridGatewayReconciler[t, tPtr]{
		Client:        mgr.GetClient(),
		eventRecorder: events.NewTypedEventRecorder(mgr.GetEventRecorderFor(ControllerName)),
		fqdnMode:      fqdnMode,
		clusterDomain: clusterDomain,
	}
}

// SetupWithManager sets up the controller with the provided manager.
// It registers the reconciler to watch and manage resources of type 'u'.
func (r *HybridGatewayReconciler[t, tPtr]) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	obj := any(new(t)).(tPtr)

	builder := ctrl.NewControllerManagedBy(mgr).For(obj)

	// Add filtering predicates if applicable.
	if predicateFuncs, err := watch.FilterBy(ctx, r.Client, obj); err != nil {
		return err
	} else if predicateFuncs != nil {
		builder = builder.WithEventFilter(*predicateFuncs)
	}

	// Add watches for owned resources.
	for _, owned := range watch.Owns(obj) {
		builder = builder.Owns(owned)
	}

	// Add watches for other resources.
	for _, w := range watch.Watches(obj, r.Client) {
		builder = builder.Watches(w.Object, handler.EnqueueRequestsFromMapFunc(w.MapFunc))
	}

	return builder.Complete(r)
}

// Reconcile reconciles the state of a custom resource by fetching the object,
// converting it to the expected type, translating it, and enforcing its desired state.
func (r *HybridGatewayReconciler[t, tPtr]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var obj tPtr = new(t)

	logger := ctrllog.FromContext(ctx).WithName(ControllerName)

	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	rootObj, ok := any(*obj).(t)
	if !ok {
		return ctrl.Result{}, fmt.Errorf("failed to convert object of type %T to route object type %T", obj, rootObj)
	}

	gvk := obj.GetObjectKind().GroupVersionKind()
	log.Debug(logger, "Reconciling object", "Group", gvk.Group, "Kind", gvk.Kind)

	// Determine if we should process this object.
	if !shouldProcessObject[t](ctx, r.Client, obj, logger) {
		return ctrl.Result{}, nil
	}

	// Handle deletion and finalizer cleanup
	if obj.GetDeletionTimestamp() != nil {
		return r.handleDeletion(ctx, logger, obj, rootObj)
	}

	// Ensure finalizer is present
	finalizerName := finalizerconst.GetFinalizerForType(rootObj)
	if !controllerutil.ContainsFinalizer(obj, finalizerName) {
		log.Debug(logger, "Adding finalizer", "finalizer", finalizerName)
		old := obj.DeepCopyObject().(tPtr)
		controllerutil.AddFinalizer(obj, finalizerName)
		if err := r.Patch(ctx, obj, client.MergeFrom(old)); err != nil {
			log.Error(logger, err, "Failed to add finalizer", "finalizer", finalizerName)
			return finalizer.HandlePatchOrUpdateError(err, logger)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	conv, err := converter.NewConverter(rootObj, r.Client, r.fqdnMode, r.clusterDomain)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Phase 1: Status Update.
	statusChanged, stop, err := enforceStatus(ctx, logger, conv)
	if err != nil && !k8serrors.IsConflict(err) {
		// Record status update failure event.
		r.eventRecorder.Event(
			obj,
			corev1.EventTypeWarning,
			eventconst.EventReasonStatusUpdateFailed,
			fmt.Sprintf("Status update failed: %v", err),
		)
		return ctrl.Result{}, err
	} else if k8serrors.IsConflict(err) {
		return ctrl.Result{Requeue: true}, nil
	}
	if stop {
		log.Debug(logger, "Stopping further reconciliation as the resource is not ready for processing")
		return ctrl.Result{}, nil
	}

	// Only emit success event if status was actually changed.
	if statusChanged {
		r.eventRecorder.Event(
			obj,
			corev1.EventTypeNormal,
			eventconst.EventReasonStatusUpdateSucceeded,
			"Status successfully updated",
		)
		log.Trace(logger, "Status updated, requeueing")
		return ctrl.Result{Requeue: true}, nil
	}

	// Phase 2: Translation.
	resourceCount, err := translate(conv, ctx, logger)
	if err != nil {
		// Record translation failure event.
		r.eventRecorder.Event(
			obj,
			corev1.EventTypeWarning,
			eventconst.EventReasonTranslationFailed,
			fmt.Sprintf("Translation failed: %v", err),
		)
		return ctrl.Result{}, err
	}

	// Record translation success event.
	r.eventRecorder.Event(
		obj,
		corev1.EventTypeNormal,
		eventconst.EventReasonTranslationSucceeded,
		fmt.Sprintf("Successfully translated to %d Kong resources", resourceCount),
	)

	// Phase 3: State Enforcement.
	stateChanged, err := enforceState(ctx, r.Client, logger, conv)
	if err != nil {
		// Record state enforcement failure event.
		r.eventRecorder.Event(
			obj,
			corev1.EventTypeWarning,
			eventconst.EventReasonStateEnforcementFailed,
			fmt.Sprintf("State enforcement failed: %v", err),
		)
		return ctrl.Result{}, err
	}

	// Only emit success event if state was actually changed.
	if stateChanged {
		r.eventRecorder.Event(
			obj,
			corev1.EventTypeNormal,
			eventconst.EventReasonStateEnforcementSucceeded,
			fmt.Sprintf("Kong resources successfully enforced: %d total", resourceCount),
		)
		log.Trace(logger, "State changed, requeueing")
		return ctrl.Result{Requeue: true}, nil
	}

	// Phase 4: Orphan Cleanup.
	orphansDeleted, err := cleanOrphanedResources[t, tPtr](ctx, r.Client, logger, conv)
	if err != nil {
		// Record orphan cleanup failure event.
		r.eventRecorder.Event(
			obj,
			corev1.EventTypeWarning,
			eventconst.EventReasonOrphanCleanupFailed,
			fmt.Sprintf("Orphan cleanup failed: %v", err),
		)
		return ctrl.Result{}, err
	}

	// If we need to requeue to continue the multi-step deletion process, do it now.
	// This is critical for security: we process one resource type at a time, waiting for each
	// type to be fully deleted before moving to the next type (e.g., delete KongRoute before
	// KongPluginBinding to prevent routes from being active without security plugins).
	if orphansDeleted {
		log.Debug(logger, "Orphan cleanup in progress, requeueing to continue multi-step deletion")
		r.eventRecorder.Event(
			obj,
			corev1.EventTypeNormal,
			eventconst.EventReasonOrphanCleanupSucceeded,
			"Orphan cleanup in progress",
		)
		return ctrl.Result{Requeue: true}, nil
	}

	// Remove finalizer if it is no longer needed.
	// This is done after all processing to ensure that if the object is still
	// being managed by us, the finalizer remains.
	removed, err := removeFinalizerIfNotManaged[t](ctx, r.Client, obj, logger)
	if err != nil {
		return ctrl.Result{}, err
	}

	if removed {
		log.Debug(logger, "Finalizer removed, from object no longer managed by us")
	}

	log.Debug(logger, "Object reconciliation completed", "Group", gvk.Group, "Kind", gvk.Kind)

	return ctrl.Result{}, nil
}

// handleDeletion handles the deletion of a resource object by cleaning up generated resources
// and removing the finalizer. This ensures that all Kong resources generated from the resource
// are properly cleaned up before the resource is deleted from the cluster.
func (r *HybridGatewayReconciler[t, tPtr]) handleDeletion(ctx context.Context, logger logr.Logger, obj tPtr, rootObj t) (ctrl.Result, error) {
	log.Debug(logger, "Handling resource deletion")

	// Create converter to get the cleanup logic
	conv, err := converter.NewConverter(rootObj, r.Client, r.fqdnMode, r.clusterDomain)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create converter for cleanup: %w", err)
	}

	// Clean up all generated resources by calling the same cleanup logic as orphan cleanup
	// but with no desired resources (simulating what cleanOrphanedResources does when desiredObjects is empty)
	orphansDeleted, err := r.cleanupGeneratedResources(ctx, logger, conv)
	if err != nil {
		// Record cleanup failure event
		r.eventRecorder.Event(
			obj,
			corev1.EventTypeWarning,
			eventconst.EventReasonOrphanCleanupFailed,
			fmt.Sprintf("Deletion cleanup failed: %v", err),
		)
		return ctrl.Result{}, fmt.Errorf("failed to cleanup generated resources: %w", err)
	}

	// If resources are still being deleted, requeue to continue the multi-step deletion process.
	// We must wait for all resources to be fully deleted before removing the finalizer.
	if orphansDeleted {
		log.Debug(logger, "Resource deletion cleanup in progress, requeueing to continue")
		r.eventRecorder.Event(
			obj,
			corev1.EventTypeNormal,
			eventconst.EventReasonOrphanCleanupSucceeded,
			"Deletion cleanup in progress",
		)
		return ctrl.Result{Requeue: true}, nil
	}

	// All resources have been deleted, now we can remove the finalizer.
	log.Debug(logger, "All generated resources deleted, removing finalizer")

	// Remove finalizer using patch for safer concurrent updates.
	finalizerName := finalizerconst.GetFinalizerForType(rootObj)
	old := obj.DeepCopyObject().(tPtr)
	if controllerutil.RemoveFinalizer(obj, finalizerName) {
		log.Debug(logger, "Removing finalizer", "finalizer", finalizerName)
		if err := r.Patch(ctx, obj, client.MergeFrom(old)); err != nil {
			return finalizer.HandlePatchOrUpdateError(err, logger)
		}
	}

	log.Debug(logger, "Resource deletion completed successfully")
	return ctrl.Result{}, nil
}

// cleanupGeneratedResources deletes all resources generated by the converter.
// This is similar to cleanOrphanedResources but treats all owned resources as orphans
// since we want to delete everything when the Route is being deleted.
// Returns:
//   - bool: true if any resources were deleted and a requeue is needed
//   - error: any error that occurred during cleanup
func (r *HybridGatewayReconciler[t, tPtr]) cleanupGeneratedResources(
	ctx context.Context,
	logger logr.Logger,
	conv converter.APIConverter[t],
) (bool, error) {
	// Use the existing cleanup logic but with an empty desired set,
	// which will cause all owned resources to be considered orphans and deleted
	return cleanOrphanedResources[t, tPtr](ctx, r.Client, logger, conv)
}
