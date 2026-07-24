package metadata

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kong/kong-operator/v2/controller/pkg/log"
)

// EnsureRouteInAnnotation atomically adds the given route to the hybrid-routes annotation of the
// cluster object identified by gvk and key.
//
// The hybrid-routes annotation accumulates references from every Route that shares a Kong resource
// (for example a KongService/KongUpstream derived from a backend that several Routes, or several
// rules of the same Route, reference). Because the value is a single comma-separated string,
// server-side apply cannot merge concurrent writers: two Routes reconciling at the same time would
// each apply their own (stale) value and the last writer would clobber the other's entry. A Route
// that loses its entry is then considered not Programmed and gets removed from the data plane,
// which is the root cause of the flaky conformance failures.
//
// To avoid that, the annotation is reconciled here with an optimistic-lock patch instead of
// through server-side apply. Conflicts are surfaced to the reconciler, which requeues and
// re-evaluates against the latest object. The desired objects applied by the reconciler
// include this annotation with the merged value so that server-side apply owns and persists it atomically.
//
// It is a no-op when the route kind is not tracked via the hybrid-routes annotation or when the
// route is already present.
//
// The returned missing flag is true when the target object does not exist. The caller uses it to
// distinguish "not created yet" (expected on early reconciles) from "concurrently deleted": if a
// resource the reconciler just enforced is already gone here, another Route removed it in the
// window before this Route recorded itself, and the caller must requeue to recreate it because no
// watch event will re-trigger this Route (the delete event maps via the annotation, which no longer
// lists this Route).
func (am *AnnotationManager) EnsureRouteInAnnotation(
	ctx context.Context,
	cl client.Client,
	gvk schema.GroupVersionKind,
	key client.ObjectKey,
	route client.Object,
) (missing bool, err error) {
	// Skip kinds that are not tracked via the hybrid-routes annotation (e.g. Gateway-owned
	// resources) to avoid an unnecessary GET.
	if am.RouteAnnotationKeyForKind(route.GetObjectKind().GroupVersionKind().Kind) == "" {
		return false, nil
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	if err := cl.Get(ctx, key, obj); err != nil {
		if apierrors.IsNotFound(err) {
			// The resource is gone; report it as missing so the caller can decide whether this
			// is an early reconcile (resource not created yet) or a concurrent delete to recover from.
			return true, nil
		}
		return false, err
	}

	base := obj.DeepCopy()
	if !am.AppendRouteToAnnotation(obj, route) {
		// Route already present, nothing to do.
		return false, nil
	}

	log.Debug(am.logger, "Atomically adding route to hybrid-routes annotation",
		"gvk", gvk.String(), "obj", key.String())
	return false, cl.Patch(ctx, obj, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}
