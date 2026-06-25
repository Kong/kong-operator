package hybridgateway

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	finalizerconst "github.com/kong/kong-operator/v2/controller/hybridgateway/const/finalizers"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/converter"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/managedfields"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/metadata"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/refs"
	"github.com/kong/kong-operator/v2/controller/hybridgateway/utils"
	"github.com/kong/kong-operator/v2/controller/pkg/log"
	controllerpkgssa "github.com/kong/kong-operator/v2/controller/pkg/ssa"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
	"github.com/kong/kong-operator/v2/pkg/consts"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

// hybridGatewayStateFieldManager is intentionally distinct from the historical
// gateway-operator manager so omitting hybrid-routes annotations from apply
// payloads does not delete annotation fields that older reconciles owned.
const hybridGatewayStateFieldManager = controllerpkgssa.FieldManager + "-hybridgateway"

// hybridRouteAnnotationKeys are the annotation keys whose value accumulates Route references
// across multiple owners (multiple Routes, or multiple rules of the same Route, that share a Kong
// resource). They are reconciled out-of-band with an optimistic-lock read-modify-write (see
// metadata.AnnotationManager.EnsureRouteInAnnotation) instead of through server-side apply, which
// cannot merge concurrent writers of a single comma-separated value. They must therefore be
// stripped from the desired object before applying so SSA never owns or clobbers them.
var hybridRouteAnnotationKeys = []string{
	consts.GatewayOperatorHybridRoutesHTTPRouteAnnotation,
	consts.GatewayOperatorHybridRoutesTLSRouteAnnotation,
}

// stripHybridRouteAnnotations removes the accumulated hybrid-routes annotations from obj so that
// server-side apply does not manage them. See hybridRouteAnnotationKeys for the rationale.
func stripHybridRouteAnnotations(obj *unstructured.Unstructured) {
	anns := obj.GetAnnotations()
	if len(anns) == 0 {
		return
	}
	changed := false
	for _, k := range hybridRouteAnnotationKeys {
		if _, ok := anns[k]; ok {
			delete(anns, k)
			changed = true
		}
	}
	if changed {
		obj.SetAnnotations(anns)
	}
}

// translate performs the full translation process using the provided APIConverter.
// Returns an integer representing the number of translated resources, and an error if the translation fails.
func translate[t converter.RootObject](conv converter.APIConverter[t], ctx context.Context, logger logr.Logger) (int, error) {
	return conv.Translate(ctx, logger)
}

// enforceState ensures that the desired state of Kubernetes resources, as provided by the APIConverter,
// is reflected in the cluster. It attempts to create or update resources using server-side apply and
// structured merge. The function returns a boolean indicating if any changes were made and an error
// for any unrecoverable or transient issues. Resources marked for deletion are skipped. Conflict errors
// are returned as errors. All other errors are wrapped with resource kind and name for context.
//
// The function performs the following operations:
// 1. Retrieves the desired state from the converter's output store
// 2. For each desired resource, checks if it exists in the cluster
// 3. Creates new resources using server-side apply if they don't exist
// 4. Skips resources that are marked for deletion
// 5. Updates existing resources if changes are detected using managed fields comparison
// 6. Handles conflicts by returning an error for proper error handling
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - cl: The Kubernetes client for CRUD operations
//   - logger: Logger for structured logging with state-enforcement phase
//   - conv: The APIConverter that provides the desired state
//
// Returns:
//   - bool: true if any resources were created or updated in the cluster
//   - error: Any error that occurred during state enforcement
//
// The function uses server-side apply with the "gateway-operator" field manager to ensure
// proper ownership and conflict resolution when multiple controllers manage the same resources.
func enforceState[t converter.RootObject](ctx context.Context, cl client.Client, logger logr.Logger, conv converter.APIConverter[t]) (applied bool, waiting bool, err error) {
	logger = logger.WithValues("phase", "state-enforcement")
	log.Debug(logger, "Starting state enforcement")

	// Get the desired state from the converter.
	desiredObjects, err := conv.GetOutputStore(ctx, logger)
	if err != nil {
		return false, false, fmt.Errorf("failed to get desired objects from converter: %w", err)
	}
	if len(desiredObjects) == 0 {
		log.Debug(logger, "No desired objects to enforce")
		return false, false, nil
	}

	log.Debug(logger, "Retrieved desired objects for enforcement", "objectCount", len(desiredObjects))

	// Build lookup maps once so that per-object gating checks are O(1) instead of O(n).
	desiredUpstreamNames := make(map[string]struct{}, len(desiredObjects))
	desiredTargetsByUpstream := make(map[string][]unstructured.Unstructured, len(desiredObjects))
	for _, obj := range desiredObjects {
		switch obj.GetKind() {
		case "KongUpstream":
			desiredUpstreamNames[obj.GetName()] = struct{}{}
		case "KongTarget":
			if upName, _, _ := unstructured.NestedString(obj.Object, "spec", "upstreamRef", "name"); upName != "" {
				desiredTargetsByUpstream[upName] = append(desiredTargetsByUpstream[upName], obj)
			}
		}
	}

	var (
		objectsCreated = 0
		objectsUpdated = 0
		objectsSkipped = 0
	)

	// In order to ensure proper ordering of resource creation/update, track the kind of the last created resource in the
	// loop and skip further processing if we move to a different desired kind which may dependend on the just generated
	// resource.
	//
	// Intentional: when dependency gating sets a specific kind in stopAtKind (e.g., waiting for
	// KongService/KongUpstream to become Programmed), we conservatively skip all other kinds until
	// that prerequisite kind is enforced. This avoids out-of-order application that could cause Konnect
	// rejects (e.g., creating Routes/Targets before Services/Upstreams are ready).
	stopAtKind := ""
	for i, desired := range desiredObjects {
		if stopAtKind != "" && desired.GetKind() != stopAtKind {
			log.Debug(logger, "Waiting for previous resource kind to be fully created/updated before processing next kind", "waitingForKind", stopAtKind, "currentKind", desired.GetKind())
			objectsSkipped++
			continue
		}

		// Best-effort dependency gating: avoid creating dependent resources before
		// their prerequisites are Programmed in Konnect. This reduces transient
		// 404s during conformance and prevents noisy reconciliation errors like
		// "can't create target without a Konnect Upstream ID".
		switch desired.GetKind() {
		case "KongService":
			// KongService.Spec.Host is the KongUpstream name. Do not create/program the service before its
			// upstream exists and is Programmed in Konnect. Otherwise Konnect can hold a service whose host
			// has no matching upstream, and once a request hits it the dataplane falls back to DNS-resolving
			// the (hashed) upstream name -> NXDOMAIN. Only gate when the host actually refers to a desired
			// KongUpstream (in hybrid gateway it always does; the guard avoids waiting forever on a service
			// that legitimately points at an external hostname).
			if host, _, _ := unstructured.NestedString(desired.Object, "spec", "host"); host != "" && desiredHasUpstreamNamed(desiredUpstreamNames, host) {
				var up configurationv1alpha1.KongUpstream
				if err := cl.Get(ctx, client.ObjectKey{Namespace: desired.GetNamespace(), Name: host}, &up); err != nil {
					log.Debug(logger, "Upstream not found yet for service, waiting", "upstream", host)
					objectsSkipped++
					stopAtKind = "KongUpstream"
					continue
				}
				if !k8sutils.HasConditionTrue(konnectv1alpha1.KonnectEntityProgrammedConditionType, &up) {
					log.Debug(logger, "Upstream not Programmed yet for service, waiting", "upstream", host)
					objectsSkipped++
					stopAtKind = "KongUpstream"
					continue
				}
				// Also wait until the upstream's targets are Programmed before creating the service, so the
				// service is only ever live once it can actually serve traffic. Anything that depends on the
				// service (the KongRoute) then transitively waits for a servable backend.
				targetsReady, err := upstreamTargetsProgrammed(ctx, cl, desiredTargetsByUpstream[host])
				if err != nil {
					return false, false, fmt.Errorf("failed to check upstream targets readiness for service %s: %w", desired.GetName(), err)
				}
				if !targetsReady {
					log.Debug(logger, "Upstream targets not Programmed yet for service, waiting", "service", desired.GetName(), "upstream", host)
					objectsSkipped++
					stopAtKind = "KongTarget"
					continue
				}
			}
			// KongService with a clientCertificateRef depends on the referenced KongCertificate being Programmed.
			certName, _, _ := unstructured.NestedString(desired.Object, "spec", "clientCertificateRef", "name")
			if certName != "" {
				var cert configurationv1alpha1.KongCertificate
				if err := cl.Get(ctx, client.ObjectKey{Namespace: desired.GetNamespace(), Name: certName}, &cert); err != nil {
					log.Debug(logger, "Certificate not found yet for service, waiting", "certificate", certName)
					objectsSkipped++
					stopAtKind = "KongCertificate"
					continue
				}
				if !k8sutils.HasConditionTrue(konnectv1alpha1.KonnectEntityProgrammedConditionType, &cert) {
					log.Debug(logger, "Certificate not Programmed yet for service, waiting", "certificate", certName)
					objectsSkipped++
					stopAtKind = "KongCertificate"
					continue
				}
			}
		case "KongTarget":
			// KongTarget depends on KongUpstream being Programmed.
			upstreamName, _, _ := unstructured.NestedString(desired.Object, "spec", "upstreamRef", "name")
			if upstreamName != "" {
				var up configurationv1alpha1.KongUpstream
				if err := cl.Get(ctx, client.ObjectKey{Namespace: desired.GetNamespace(), Name: upstreamName}, &up); err != nil {
					// Upstream not present yet, wait.
					log.Debug(logger, "Upstream not found yet for target, waiting", "upstream", upstreamName)
					objectsSkipped++
					stopAtKind = "KongUpstream"
					continue
				}
				if !k8sutils.HasConditionTrue(konnectv1alpha1.KonnectEntityProgrammedConditionType, &up) {
					log.Debug(logger, "Upstream not Programmed yet for target, waiting", "upstream", upstreamName)
					objectsSkipped++
					stopAtKind = "KongUpstream"
					continue
				}
			}
		case "KongRoute":
			// KongRoute (serviceful) depends on KongService being Programmed.
			svcName, _, _ := unstructured.NestedString(desired.Object, "spec", "serviceRef", "namespacedRef", "name")
			if svcName != "" {
				var svc configurationv1alpha1.KongService
				if err := cl.Get(ctx, client.ObjectKey{Namespace: desired.GetNamespace(), Name: svcName}, &svc); err != nil {
					log.Debug(logger, "Service not found yet for route, waiting", "service", svcName)
					objectsSkipped++
					stopAtKind = "KongService"
					continue
				}
				if !k8sutils.HasConditionTrue(konnectv1alpha1.KonnectEntityProgrammedConditionType, &svc) {
					log.Debug(logger, "Service not Programmed yet for route, waiting", "service", svcName)
					objectsSkipped++
					stopAtKind = "KongService"
					continue
				}
			}
		case "KongPluginBinding":
			// Gate on referenced KongRoute/KongService readiness before creating a binding.
			if routeName, _, _ := unstructured.NestedString(desired.Object, "spec", "targets", "routeRef", "name"); routeName != "" {
				var route configurationv1alpha1.KongRoute
				if err := cl.Get(ctx, client.ObjectKey{Namespace: desired.GetNamespace(), Name: routeName}, &route); err != nil {
					log.Debug(logger, "Route not found yet for plugin binding, waiting", "route", routeName)
					objectsSkipped++
					stopAtKind = "KongRoute"
					continue
				}
				if !k8sutils.HasConditionTrue(konnectv1alpha1.KonnectEntityProgrammedConditionType, &route) {
					log.Debug(logger, "Route not Programmed yet for plugin binding, waiting", "route", routeName)
					objectsSkipped++
					stopAtKind = "KongRoute"
					continue
				}
			}
			if svcName, _, _ := unstructured.NestedString(desired.Object, "spec", "targets", "serviceRef", "name"); svcName != "" {
				if kind, _, _ := unstructured.NestedString(desired.Object, "spec", "targets", "serviceRef", "kind"); kind == "KongService" {
					var svc configurationv1alpha1.KongService
					if err := cl.Get(ctx, client.ObjectKey{Namespace: desired.GetNamespace(), Name: svcName}, &svc); err != nil {
						log.Debug(logger, "Service not found yet for plugin binding, waiting", "service", svcName)
						objectsSkipped++
						stopAtKind = "KongService"
						continue
					}
					if !k8sutils.HasConditionTrue(konnectv1alpha1.KonnectEntityProgrammedConditionType, &svc) {
						log.Debug(logger, "Service not Programmed yet for plugin binding, waiting", "service", svcName)
						objectsSkipped++
						stopAtKind = "KongService"
						continue
					}
				}
			}
		}
		log.Debug(logger, "Processing desired object", "index", i, "kind", desired.GetKind(), "name", desired.GetName())

		// The hybrid-routes annotation is reconciled out-of-band with an optimistic-lock
		// read-modify-write (see reconcileSharedRouteAnnotations); strip it here so server-side
		// apply never owns or overwrites the shared, accumulated value.
		stripHybridRouteAnnotations(&desired)

		// Get the existing object by name from the API server.
		existing := &unstructured.Unstructured{}
		existing.SetGroupVersionKind(desired.GetObjectKind().GroupVersionKind())

		err := cl.Get(ctx, client.ObjectKey{
			Namespace: desired.GetNamespace(),
			Name:      desired.GetName(),
		}, existing)

		namespacedNameDesired := client.ObjectKeyFromObject(&desired)
		namespacedNameExisting := client.ObjectKeyFromObject(existing)

		if err != nil {
			if apierrors.IsNotFound(err) {
				// Object doesn't exist, create it using server-side apply.
				log.Debug(logger, "Creating new object", "kind", desired.GetKind(), "obj", namespacedNameDesired)
				// Set field manager for server-side apply
				if err := cl.Apply(ctx, client.ApplyConfigurationFromUnstructured(&desired), client.FieldOwner(hybridGatewayStateFieldManager), client.ForceOwnership); err != nil {
					if apierrors.IsConflict(err) {
						return false, false, fmt.Errorf("conflict during create of object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
					}
					return false, false, fmt.Errorf("failed to create object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
				}
				objectsCreated++
				log.Debug(logger, "Successfully created object", "kind", desired.GetKind(), "obj", namespacedNameDesired)
				stopAtKind = desired.GetKind()
				continue
			} else {
				// Other error getting the object.
				return false, false, fmt.Errorf("failed to get object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
			}
		}

		// Handle the case when resource are marked for deletion.
		if !existing.GetDeletionTimestamp().IsZero() {
			log.Debug(logger, "Existing object is marked for deletion, will not enforce state", "kind", existing.GetKind(), "obj", namespacedNameDesired)
			objectsSkipped++
			stopAtKind = existing.GetKind()
			continue
		}

		// Object exists, check if we need to update it.
		managedFieldsObj, err := managedfields.ExtractAsUnstructured(existing, hybridGatewayStateFieldManager, "")
		if err != nil {
			return false, false, fmt.Errorf("failed to extract managed fields for kind %s obj %s: %w", existing.GetKind(), namespacedNameExisting, err)
		}
		if managedFieldsObj == nil {
			// No managed fields for our field manager, we should update.
			log.Debug(logger, "No managed fields found for our field manager, will apply desired state", "kind", existing.GetKind(), "obj", namespacedNameExisting)
			if err := cl.Apply(ctx, client.ApplyConfigurationFromUnstructured(&desired), client.FieldOwner(hybridGatewayStateFieldManager), client.ForceOwnership); err != nil {
				if apierrors.IsConflict(err) {
					return false, false, fmt.Errorf("conflict during create of object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
				}
				return false, false, fmt.Errorf("failed to create object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
			}
			objectsUpdated++
			log.Debug(logger, "Successfully applied desired state (no managed fields)", "kind", existing.GetKind(), "obj", namespacedNameExisting)
			continue
		}

		// Convert desired resource to unstructured.
		desiredU, err := utils.ToUnstructured(&desired, cl.Scheme())
		if err != nil {
			return false, false, fmt.Errorf("failed to convert to unstructured desired obj for kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
		}

		// Compare the two states.
		compare, err := managedfields.Compare(managedFieldsObj, pruneDesiredObj(desiredU))
		if err != nil {
			return false, false, fmt.Errorf("failed to compare managed fields for kind %s obj %s: %w", existing.GetKind(), namespacedNameExisting, err)
		}

		if compare.IsSame() {
			log.Trace(logger, "No changes detected for obj", "kind", existing.GetKind(), "obj", namespacedNameExisting)
		} else {
			log.Info(logger, "Changes detected for obj, applying desired state", "kind", existing.GetKind(), "obj", namespacedNameExisting, "changes", compare.String())
			// Changes detected, apply the desired state using server-side apply.
			if err := cl.Apply(ctx, client.ApplyConfigurationFromUnstructured(&desired), client.FieldOwner(hybridGatewayStateFieldManager), client.ForceOwnership); err != nil {
				if apierrors.IsConflict(err) {
					return false, false, fmt.Errorf("conflict during create of object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
				}
				return false, false, fmt.Errorf("failed to update object kind %s obj %s: %w", desired.GetKind(), namespacedNameDesired, err)
			}
			objectsUpdated++
			log.Debug(logger, "Successfully applied changes to object", "kind", existing.GetKind(), "obj", namespacedNameExisting)
		}
	}

	log.Debug(logger, "Finished state enforcement",
		"totalObjects", len(desiredObjects),
		"created", objectsCreated,
		"updated", objectsUpdated,
		"skipped", objectsSkipped)

	// Report whether we applied anything or are waiting for prerequisites.
	applied = (objectsCreated + objectsUpdated) > 0
	waiting = objectsSkipped > 0
	return applied, waiting, nil
}

// desiredHasUpstreamNamed reports whether the pre-built upstream name set contains the given name.
// Used to decide whether a KongService's host refers to a managed upstream (and should therefore be gated
// on it) versus an external hostname (which must not gate).
func desiredHasUpstreamNamed(desiredUpstreamNames map[string]struct{}, name string) bool {
	_, ok := desiredUpstreamNames[name]
	return ok
}

// upstreamTargetsProgrammed reports whether every KongTarget in the pre-filtered targets slice is present
// in the cluster and Programmed. The caller is responsible for passing only the targets that belong to the
// upstream being checked (use the desiredTargetsByUpstream map built in enforceState). An empty/nil slice
// means no targets exist for that upstream and is considered ready.
func upstreamTargetsProgrammed(ctx context.Context, cl client.Client, targets []unstructured.Unstructured) (bool, error) {
	for i := range targets {
		d := &targets[i]
		var tgt configurationv1alpha1.KongTarget
		if err := cl.Get(ctx, client.ObjectKey{Namespace: d.GetNamespace(), Name: d.GetName()}, &tgt); err != nil {
			if apierrors.IsNotFound(err) {
				// Not created yet (targets are appended after the route): not ready.
				return false, nil
			}
			return false, fmt.Errorf("failed to get KongTarget %s/%s: %w", d.GetNamespace(), d.GetName(), err)
		}
		if !k8sutils.HasConditionTrue(konnectv1alpha1.KonnectEntityProgrammedConditionType, &tgt) {
			return false, nil
		}
	}
	return true, nil
}

// reconcileSharedRouteAnnotations atomically ensures the root Route is recorded in the
// hybrid-routes annotation of every Kong resource the converter currently desires.
//
// These annotations are intentionally not applied via server-side apply (see
// stripHybridRouteAnnotations) because a single comma-separated value cannot be merged across
// concurrent writers. Instead each entry is added here with an optimistic-lock read-modify-write
// that is safe against Routes (or rules) sharing the same Kong resource reconciling concurrently.
//
// A desired resource that does not exist yet is reported back via the missing return value rather
// than failing: on early reconciles enforceState has not created it yet, but in steady state
// (enforceState applied nothing and is not waiting) a missing resource means another Route deleted
// it concurrently before this Route recorded itself. The caller requeues in that case to recreate
// it, because no watch event will re-trigger this Route on its own.
func reconcileSharedRouteAnnotations[t converter.RootObject, tPtr converter.RootObjectPtr[t]](
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	conv converter.APIConverter[t],
) (missing bool, err error) {
	logger = logger.WithValues("phase", "route-annotation-sync")

	rootObj := conv.GetRootObject()
	var rootObjPtr tPtr
	switch v := any(&rootObj).(type) {
	case tPtr:
		rootObjPtr = v
	default:
		return false, fmt.Errorf("failed to convert root object to pointer type: got %T, expected %T", &rootObj, rootObjPtr)
	}

	desiredObjects, err := conv.GetOutputStore(ctx, logger)
	if err != nil {
		return false, fmt.Errorf("failed to get desired objects from converter for annotation sync: %w", err)
	}

	am := metadata.NewAnnotationManager(logger)
	for _, desired := range desiredObjects {
		objMissing, err := am.EnsureRouteInAnnotation(
			ctx, cl, desired.GroupVersionKind(), client.ObjectKeyFromObject(&desired), rootObjPtr,
		)
		if err != nil {
			return false, fmt.Errorf("failed to ensure hybrid-routes annotation on %s %s: %w",
				desired.GetKind(), client.ObjectKeyFromObject(&desired), err)
		}
		missing = missing || objMissing
	}
	return missing, nil
}

// enforceStatus updates the status of the root object managed by the provided APIConverter.
// This function delegates to the converter's UpdateRootObjectStatus method to handle
// status condition management and cluster updates.
//
// Parameters:
//   - ctx: The context for API calls
//   - logger: Logger for debugging information
//   - conv: The APIConverter that manages the root object and its status
//
// Returns:
//   - bool: true if the status was actually updated in the cluster
//   - bool: true if the reconciliation loop should stop further processing
//   - error: Any error that occurred during status processing
//
// This is a generic wrapper function that works with any converter implementing
// the APIConverter interface, providing a consistent interface for status enforcement
// across different resource types.
func enforceStatus[t converter.RootObject](ctx context.Context, logger logr.Logger, conv converter.APIConverter[t]) (updated bool, stop bool, err error) {
	return conv.UpdateRootObjectStatus(ctx, logger)
}

// orphanCleanupOptions controls how cleanOrphanedResources handles
// in-flight deletions while pruning orphaned resources.
type orphanCleanupOptions struct {
	// waitForDeletes, when true, makes cleanup process one GVK at a time and
	// requeue until every orphan of that type is fully gone before moving on,
	// enforcing deletion ordering across resource types (e.g. delete KongRoute
	// before KongPluginBinding). When false, all GVKs are processed in a single
	// pass and resources already being deleted are not waited on.
	waitForDeletes bool
}

// cleanOrphanedResources deletes resources previously managed by the converter but no longer present in the desired output.
//
// The function performs the following operations:
// 1. Retrieves the current desired state from the converter's output store
// 2. Builds a set of desired resource keys for quick lookup
// 3. For each expected GroupVersionKind, lists existing resources owned by the root object
// 4. Compares existing resources against the desired set and deletes orphans
// 5. Handles deletion errors gracefully, ignoring NotFound errors
//
// This cleanup process ensures that resources that were previously created by the converter
// but are no longer needed (due to configuration changes) are properly removed from the cluster.
//
// When opts.waitForDeletes is true, deletion is performed in a multi-step process ensuring resources are
// deleted in the order defined by conv.GetExpectedGVKs(): the function returns (true, nil) to requeue as
// soon as a GVK has any orphan deleted or still in deletion, so each type is fully removed before the next
// one is processed. When false, all GVKs are processed in a single pass and in-flight deletions are not
// waited on.
//
// Parameters:
//   - ctx: The context for API calls and cancellation
//   - cl: The Kubernetes client for listing and deleting resources
//   - logger: Logger for debugging and status information
//   - conv: The APIConverter that manages the root object and its desired state
//   - opts: Options controlling whether to wait for in-flight deletions
//
// Returns:
//   - bool: true if a requeue is needed to continue/complete the cleanup
//   - error: Any error that occurred during the cleanup process
//
// The function uses ownership labels to identify resources managed by the root object
// and only deletes resources that are no longer present in the converter's desired output.
func cleanOrphanedResources[t converter.RootObject, tPtr converter.RootObjectPtr[t]](
	ctx context.Context,
	cl client.Client,
	logger logr.Logger,
	conv converter.APIConverter[t],
	opts orphanCleanupOptions,
) (bool, error) {
	logger = logger.WithValues("phase", "orphan-cleanup")
	log.Debug(logger, "Starting orphaned resource cleanup", "waitForDeletes", opts.waitForDeletes)

	desiredObjects, err := conv.GetOutputStore(ctx, logger)
	if err != nil {
		return false, fmt.Errorf("failed to get desired objects from converter for cleanup: %w", err)
	}

	desiredSet := make(map[string]struct{})
	expectedGVKs := conv.GetExpectedGVKs()

	log.Debug(logger, "Retrieved desired objects and expected GVKs",
		"desiredObjectCount", len(desiredObjects),
		"expectedGVKCount", len(expectedGVKs))

	// Extract the root object for label selector.
	rootObj := conv.GetRootObject()
	var rootObjPtr tPtr
	switch v := any(&rootObj).(type) {
	case tPtr:
		rootObjPtr = v
	default:
		return false, fmt.Errorf("failed to convert root object to pointer type: got %T, expected %T", &rootObj, rootObjPtr)
	}

	// Build a set of desired resource keys.
	log.Debug(logger, "Building desired resource key set")
	for _, obj := range desiredObjects {
		key := fmt.Sprintf("%s/%s/%s", obj.GetNamespace(), obj.GetName(), obj.GetObjectKind().GroupVersionKind().String())
		desiredSet[key] = struct{}{}
		log.Trace(logger, "Added desired resource key", "key", key, "kind", obj.GetKind(), "name", obj.GetName())
	}
	log.Debug(logger, "Finished building desired resource key set", "totalKeys", len(desiredSet))

	// For each expected GVK, list resources and delete orphans. When waiting for deletes, reconciliation
	// processes one GVK at a time and waits for resources to be fully deleted before moving to the next type
	// or releasing the root finalizer.
	orphansDeleted := 0
	for _, gvk := range expectedGVKs {
		log.Debug(logger, "Processing GVK for orphan cleanup", "gvk", gvk.String())

		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk)
		selector := metadata.LabelSelectorForOwnedResources(rootObjPtr, nil)

		if err := cl.List(ctx, list, selector); err != nil {
			return false, fmt.Errorf("unable to list objects with gvk %s: %w", gvk.String(), err)
		}

		log.Debug(logger, "Found existing resources for GVK", "gvk", gvk.String(), "resourceCount", len(list.Items))

		orphansDeletedForGVK := 0
		orphansInDeletionForGVK := 0

		for _, item := range list.Items {
			key := fmt.Sprintf("%s/%s/%s", item.GetNamespace(), item.GetName(), gvk.String())
			if _, found := desiredSet[key]; found {
				log.Trace(logger, "Resource still desired, keeping", "kind", item.GetKind(), "obj", client.ObjectKeyFromObject(&item))
				continue
			}

			// If the converter implements OrphanedResourceHandler, give it a chance to perform custom cleanup logic.
			if customHandler, ok := conv.(converter.OrphanedResourceHandler); ok {
				skipDelete, err := customHandler.HandleOrphanedResource(ctx, logger, &item)
				if err != nil {
					return false, fmt.Errorf("failed to handle orphaned resource kind %s obj %s: %w", item.GetKind(), client.ObjectKeyFromObject(&item), err)
				}
				if skipDelete {
					log.Trace(logger, "Skipping orphaned resource as per converter decision", "kind", item.GetKind(), "obj", client.ObjectKeyFromObject(&item))
					continue
				}
			}

			// Check if the resource is already being deleted (has deletionTimestamp set).
			if !item.GetDeletionTimestamp().IsZero() {
				if opts.waitForDeletes {
					log.Debug(logger, "Resource is already being deleted, will requeue to wait for deletion", "kind", item.GetKind(), "obj", client.ObjectKeyFromObject(&item))
					orphansInDeletionForGVK++
				} else {
					log.Debug(logger, "Resource is already being deleted, not waiting for deletion", "kind", item.GetKind(), "obj", client.ObjectKeyFromObject(&item))
				}
				continue
			}

			log.Info(logger, "Deleting orphaned resource", "kind", item.GetKind(), "obj", client.ObjectKeyFromObject(&item))
			// Delete with an optimistic-lock precondition on the resourceVersion that the orphan
			// decision was made against. This closes the race where another Route attaches to a
			// shared resource (adding itself to the hybrid-routes annotation) between our read and
			// the delete: such a change bumps the resourceVersion and the delete fails with a
			// conflict, so we requeue and re-evaluate instead of deleting a resource still in use.
			deleteOpts := []client.DeleteOption{}
			if rv := item.GetResourceVersion(); rv != "" {
				deleteOpts = append(deleteOpts, client.Preconditions{ResourceVersion: &rv})
			}
			if err := cl.Delete(ctx, &item, deleteOpts...); err != nil {
				switch {
				case apierrors.IsNotFound(err):
					// Already gone; treat as deleted.
				case apierrors.IsConflict(err):
					if opts.waitForDeletes {
						log.Debug(logger, "Orphaned resource changed before deletion, requeuing to re-evaluate",
							"kind", item.GetKind(), "obj", client.ObjectKeyFromObject(&item))
						orphansInDeletionForGVK++
						continue
					}
					log.Debug(logger, "Orphaned resource changed before deletion, requeueing cleanup",
						"kind", item.GetKind(), "obj", client.ObjectKeyFromObject(&item))
					return true, nil
				default:
					return false, fmt.Errorf("failed to delete orphaned resource kind %s obj %s: %w", item.GetKind(), client.ObjectKeyFromObject(&item), err)
				}
			}
			orphansDeletedForGVK++
		}
		orphansDeleted += orphansDeletedForGVK

		// When waiting for deletes, if we deleted any orphan resource or found any that is currently being
		// deleted, return true to trigger a requeue. This ensures we wait for the current GVK's resources to
		// be fully deleted before moving to the next GVK, enforcing deletion order among resource types.
		if opts.waitForDeletes && (orphansDeletedForGVK > 0 || orphansInDeletionForGVK > 0) {
			log.Debug(logger, "Requeuing to wait for orphaned resources deletion for GVK", "gvk", gvk.String(), "orphansDeleted", orphansDeletedForGVK)
			return true, nil
		}
		log.Debug(logger, "Finished processing GVK for orphan cleanup", "gvk", gvk.String())
	}

	if opts.waitForDeletes && orphansDeleted > 0 {
		log.Debug(logger, "Requeuing after deleting orphaned resources", "orphansDeleted", orphansDeleted)
		return true, nil
	}

	log.Debug(logger, "Finished orphaned resource cleanup")
	return false, nil
}

// pruneDesiredObj removes fields that should not be compared when checking for differences.
func pruneDesiredObj(obj unstructured.Unstructured) *unstructured.Unstructured {
	u := obj.DeepCopy()
	// Remove metadata fields such as name and namespace from the desired object that are not managed by the controller.
	unstructured.RemoveNestedField(u.Object, "metadata", "name")
	unstructured.RemoveNestedField(u.Object, "metadata", "namespace")
	managedfields.PruneEmptyFields(u)
	return u
}

// shouldProcessObject determines if an object should be processed in the reconcile loop.
// It filters objects based on finalizer presence and Gateway references to handle three scenarios:
//
// 1. Objects that have our finalizer - always process them (we own/owned them, need to continue/cleanup)
// 2. Objects without our finalizer but referencing our Gateway - process them (new objects we should manage)
// 3. Objects without our finalizer and not referencing our Gateway - skip them (not meant for us)
//
// This filtering is necessary because watch-level predicates may pass objects that:
// - Were owned by us previously but ownership was transferred to another controller
// - Match watch criteria but were never managed by this controller
// - An error occurred during predicate evaluation
//
// The presence of our finalizer indicates that we have processed the object before and are
// responsible for its cleanup. Objects without our finalizer are checked for Gateway references
// to determine if they should be newly managed by us.
//
// Parameters:
//   - ctx: The context for API calls
//   - cl: The Kubernetes client for API operations
//   - obj: The object to check (must be a converter.RootObject)
//   - logger: Logger for debugging information
//
// Returns:
//   - bool: true if the object should be processed, false if it should be skipped
func shouldProcessObject[t converter.RootObject](ctx context.Context, cl client.Client, obj client.Object, logger logr.Logger) bool {
	// Check if the object has our finalizer, which indicates we've processed it before
	// and are responsible for its lifecycle management.
	var rootObj t
	finalizerName := finalizerconst.GetFinalizerForType(rootObj)
	if controllerutil.ContainsFinalizer(obj, finalizerName) {
		log.Trace(logger, "Object has our finalizer, will process", "finalizer", finalizerName)
		return true
	}

	// Object doesn't have our finalizer. Check if it references a supported Gateway
	// This determines if we should start managing this object.
	if hasSupportedGateway := referencesSupportedGateway(ctx, cl, obj, logger); hasSupportedGateway {
		log.Debug(logger, "Object references supported Gateway, will process")
		return true
	}

	// Object doesn't have our finalizer and doesn't reference our Gateway then skip it.
	log.Debug(logger, "Skipping object reconciliation", "reason", "object does not have our finalizer and does not reference a supported gateway")
	return false
}

// removeFinalizerIfNotManaged removes our finalizer from the object if it's present
// but the object is not (or is no longer) managed by our controller.
//
// This function should be called when an object that was previously managed by us
// is no longer under our control (e.g., GatewayClass changed to a different controller).
// It ensures proper cleanup by removing our finalizer so the object can be deleted
// or managed by another controller without being blocked.
//
// Parameters:
//   - ctx: The context for API calls
//   - cl: The Kubernetes client for update operations
//   - obj: The object to check and potentially update (must be a converter.RootObject)
//   - logger: Logger for debugging information
//
// Returns:
//   - bool: true if the finalizer was removed (object was updated)
//   - error: Any error that occurred during the update
func removeFinalizerIfNotManaged[t converter.RootObject](ctx context.Context, cl client.Client, obj client.Object, logger logr.Logger) (bool, error) {
	var rootObj t
	finalizerName := finalizerconst.GetFinalizerForType(rootObj)

	// Check if our finalizer is present.
	if !controllerutil.ContainsFinalizer(obj, finalizerName) {
		// No finalizer present, nothing to do
		log.Trace(logger, "Object does not have our finalizer, no cleanup needed", "finalizer", finalizerName)
		return false, nil
	}

	// Check if the object is managed by us.
	if hasSupportedGateway := referencesSupportedGateway(ctx, cl, obj, logger); hasSupportedGateway {
		// Object is managed by us, don't remove the finalizer
		log.Trace(logger, "Object is managed by us, keeping finalizer", "finalizer", finalizerName)
		return false, nil
	}

	// Finalizer is present but object is not managed by us, remove it.
	log.Debug(logger, "Removing finalizer from object no longer managed by us",
		"obj", client.ObjectKeyFromObject(obj),
		"finalizer", finalizerName)

	// Create a patch from the original object.
	patch := client.MergeFrom(obj.DeepCopyObject().(client.Object))

	// Remove the finalizer.
	controllerutil.RemoveFinalizer(obj, finalizerName)

	// Patch the object.
	if err := cl.Patch(ctx, obj, patch); err != nil {
		if apierrors.IsNotFound(err) {
			// Object was already deleted, this is fine.
			log.Trace(logger, "Object already deleted, finalizer removal not needed")
			return false, nil
		}
		return false, fmt.Errorf("failed to remove finalizer from object: %w", err)
	}

	log.Debug(logger, "Successfully removed finalizer from unmanaged object",
		"obj", client.ObjectKeyFromObject(obj),
		"finalizer", finalizerName)
	return true, nil
}

// referencesSupportedGateway checks if the given object references at least one Gateway
// that is supported by this controller (has a GatewayClass controlled by us).
func referencesSupportedGateway(ctx context.Context, cl client.Client, obj client.Object, logger logr.Logger) bool {
	switch o := obj.(type) {
	case *gwtypes.HTTPRoute:
		// Check if any of the ParentRefs reference a supported Gateway.
		for _, pRef := range o.Spec.ParentRefs {
			gw, found, err := refs.GetSupportedGatewayForParentRef(ctx, logger, cl, pRef, o.Namespace)
			if err != nil {
				// Log the error but continue checking other ParentRefs.
				log.Trace(logger, "Error checking ParentRef", "parentRef", pRef, "error", err)
				continue
			}
			if found {
				// Found at least one supported Gateway reference.
				log.Trace(logger, "Found supported Gateway reference", "gateway", client.ObjectKeyFromObject(gw))
				return true
			}
		}
		return false

	case *gwtypes.TLSRoute:
		for _, pRef := range o.Spec.ParentRefs {
			gw, found, err := refs.GetSupportedGatewayForParentRef(ctx, logger, cl, pRef, o.Namespace)
			if err != nil {
				// Log the error but continue checking other ParentRefs.
				log.Trace(logger, "Error checking ParentRef", "parentRef", pRef, "error", err)
				continue
			}
			if found {
				// Found at least one supported Gateway reference.
				log.Trace(logger, "Found supported Gateway reference", "gateway", client.ObjectKeyFromObject(gw))
				return true
			}
		}
		return false

	case *gwtypes.Gateway:
		// For Gateway objects, check if they are supported by checking their GatewayClass.
		supported, err := refs.IsGatewayInKonnect(ctx, cl, o)
		if err != nil {
			log.Debug(logger, "Error checking if Gateway is supported", "error", err)
			return false
		}
		return supported
	}

	// This should never be reached due to type constraints on RootObject.
	return false
}
