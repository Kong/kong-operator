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
// intentionally omit this annotation (see stripHybridRouteAnnotations) so that server-side apply
// never owns or overwrites it.
//
// It is a no-op when the object does not exist yet, when the route kind is not tracked via the
// hybrid-routes annotation, or when the route is already present.
func (am *AnnotationManager) EnsureRouteInAnnotation(
	ctx context.Context,
	cl client.Client,
	gvk schema.GroupVersionKind,
	key client.ObjectKey,
	route client.Object,
) error {
	// Skip kinds that are not tracked via the hybrid-routes annotation (e.g. Gateway-owned
	// resources) to avoid an unnecessary GET.
	if am.RouteAnnotationKeyForKind(route.GetObjectKind().GroupVersionKind().Kind) == "" {
		return nil
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	if err := cl.Get(ctx, key, obj); err != nil {
		if apierrors.IsNotFound(err) {
			// The resource has not been created yet; it will be tracked on a later reconcile.
			return nil
		}
		return err
	}

	base := obj.DeepCopy()
	if !am.AppendRouteToAnnotation(obj, route) {
		// Route already present, nothing to do.
		return nil
	}

	log.Debug(am.logger, "Atomically adding route to hybrid-routes annotation",
		"gvk", gvk.String(), "obj", key.String())
	return cl.Patch(ctx, obj, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}
