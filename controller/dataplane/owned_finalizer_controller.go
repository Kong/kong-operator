package dataplane

import (
	"context"
	"fmt"
	"time"

	"github.com/samber/lo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	operatorv1beta1 "github.com/kong/gateway-operator/api/v1beta1"
	"github.com/kong/gateway-operator/controller/pkg/log"
	"github.com/kong/gateway-operator/pkg/consts"
	k8sutils "github.com/kong/gateway-operator/pkg/utils/kubernetes"
)

// DataPlaneOwnedResource is a type that represents a Kubernetes resource that is owned by a DataPlane.
type DataPlaneOwnedResource interface {
	corev1.Service | appsv1.Deployment | corev1.Secret
}

// DataPlaneOwnedResourcePointer is a type that represents a pointer to a DataPlaneOwnedResource that
// implements client.Object interface. It allows us to use it to create a new instance of the object using
// DataPlaneOwnedResource type param and cast it in compile time to DataPlaneOwnedResourcePointer.
// See: https://stackoverflow.com/a/69575720/7958339
type DataPlaneOwnedResourcePointer[T DataPlaneOwnedResource, PT ClientObjectPointer[T]] interface {
	// We need DeepCopier part of the type constraint to ensure that we can use
	// DeepCopy() *T on DataPlaneOwnedResourcePointer objects.
	DeepCopier[T, PT]
	// ClientObjectPointer is needed to ensure that we get access to the methods
	// of T with pointer receivers and to enforce fulfilling the client.Object
	// interface.
	ClientObjectPointer[T]
}

// ClientObjectPointer is a type contraint which enforces client.Object interface
// and holds *T.
type ClientObjectPointer[T DataPlaneOwnedResource] interface {
	*T
	client.Object
}

// DeepCopier is a type contraint which allows enforcing DeepCopy() *T method
// on objects.
type DeepCopier[T DataPlaneOwnedResource, PT ClientObjectPointer[T]] interface {
	DeepCopy() PT
}

// DataPlaneOwnedResourceFinalizerReconciler reconciles DataPlaneOwnedResource objects.
// It removes the finalizer from the object only when the parent DataPlane is deleted to prevent accidental
// deletion of the DataPlane owned resources.
// This is a stop gap solution until we implement proper self-healing for the DataPlane resources, see:
// https://github.com/Kong/gateway-operator/issues/1028
type DataPlaneOwnedResourceFinalizerReconciler[T DataPlaneOwnedResource, PT DataPlaneOwnedResourcePointer[T, PT]] struct {
	Client          client.Client
	DevelopmentMode bool
}

// NewDataPlaneOwnedResourceFinalizerReconciler returns a new DataPlaneOwnedResourceFinalizerReconciler for a type passed
// as the first parameter.
// The PT param is used only to allow inferring the type of the object so that we can write:
//
//	NewDataPlaneOwnedResourceFinalizerReconciler(&corev1.Service{}, ...)
//
// instead of repeating the type twice as follows:
//
//	NewDataPlaneOwnedResourceFinalizerReconciler[corev1.Service, *corev1.Service](...).
func NewDataPlaneOwnedResourceFinalizerReconciler[T DataPlaneOwnedResource, PT DataPlaneOwnedResourcePointer[T, PT]](
	client client.Client,
	developmentMode bool,
) *DataPlaneOwnedResourceFinalizerReconciler[T, PT] {
	return &DataPlaneOwnedResourceFinalizerReconciler[T, PT]{
		Client:          client,
		DevelopmentMode: developmentMode,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataPlaneOwnedResourceFinalizerReconciler[T, PT]) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	objectIsBeingDeleted := func(obj client.Object) bool {
		return obj.GetDeletionTimestamp() != nil
	}
	objectIsOwnedByDataPlane := func(obj client.Object) bool {
		ownerRef := metav1.GetControllerOf(obj)
		if ownerRef == nil || ownerRef.Kind != "DataPlane" {
			return false
		}
		return obj.GetLabels()[consts.GatewayOperatorManagedByLabel] == consts.DataPlaneManagedLabelValue
	}

	ownedObj := PT(new(T))
	or := reconcile.AsReconciler[PT](mgr.GetClient(), r)
	return ctrl.NewControllerManagedBy(mgr).
		For(ownedObj, builder.WithPredicates(
			predicate.NewPredicateFuncs(objectIsBeingDeleted),
			predicate.NewPredicateFuncs(objectIsOwnedByDataPlane),
		)).
		Watches(&operatorv1beta1.DataPlane{}, handler.EnqueueRequestsFromMapFunc(requestsForDataPlaneOwnedObjects[T](r.Client))).
		Complete(or)
}

// Reconcile reconciles the DataPlaneOwnedResource object.
func (r DataPlaneOwnedResourceFinalizerReconciler[T, PT]) Reconcile(ctx context.Context, obj PT) (ctrl.Result, error) {
	logger := log.GetLogger(ctx, obj.GetObjectKind().GroupVersionKind().Kind, r.DevelopmentMode)

	// If the object is not being deleted, we don't need to do anything.
	if obj.GetDeletionTimestamp().IsZero() {
		return ctrl.Result{}, nil
	}

	// Respect grace period.
	if obj.GetDeletionTimestamp().After(time.Now()) {
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: time.Until(obj.GetDeletionTimestamp().Time),
		}, nil
	}

	// If the object does not have the finalizer, we don't need to do anything.
	hasDataPlaneOwnedFinalizer := lo.Contains(obj.GetFinalizers(), consts.DataPlaneOwnedWaitForOwnerFinalizer)
	if !hasDataPlaneOwnedFinalizer {
		log.Debug(logger, "object has no finalizer",
			"finalizer", consts.DataPlaneOwnedWaitForOwnerFinalizer,
		)
		return ctrl.Result{}, nil
	}

	// Check if the parent DataPlane still exists.
	ownerRef := metav1.GetControllerOf(obj)
	ownerIsGone, err := func() (bool, error) {
		ownerDataPlane := &operatorv1beta1.DataPlane{}
		getOwnerErr := r.Client.Get(ctx, types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      ownerRef.Name,
		}, ownerDataPlane)
		if getOwnerErr != nil {
			// If the DataPlane is not found or gone, we consider it gone.
			if k8serrors.IsNotFound(getOwnerErr) || k8serrors.IsGone(getOwnerErr) {
				return true, nil
			}
			return false, getOwnerErr
		}

		// If the DataPlane is being deleted, we also consider it gone.
		ownerDpIsBeingDeleted := ownerDataPlane.DeletionTimestamp != nil
		return ownerDpIsBeingDeleted, nil
	}()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get DataPlane %s/%s: %w", obj.GetNamespace(), ownerRef.Name, err)
	}

	// If the DataPlane still exists, we wait for it to be deleted.
	if !ownerIsGone {
		log.Debug(logger, "not deleting, owner dataplane still exists", "dataplane", ownerRef.Name)
		return ctrl.Result{}, nil
	}

	// Given all above conditions were satisfied, we can remove the finalizer from the object.
	finalizers := obj.GetFinalizers()
	old := obj.DeepCopy()
	obj.SetFinalizers(lo.Reject(finalizers, func(f string, _ int) bool {
		return f == consts.DataPlaneOwnedWaitForOwnerFinalizer
	}))
	if err := r.Client.Patch(ctx, obj, client.MergeFrom(old)); err != nil {
		if k8serrors.IsNotFound(err) {
			// If the object is already gone, we don't need to do anything.
			log.Debug(logger, "object is already gone")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from %s %s: %w", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), err)
	}

	log.Debug(logger, "removed finalizer",
		"finalizer", consts.DataPlaneOwnedWaitForOwnerFinalizer,
	)
	return ctrl.Result{}, nil
}

// requestsForDataPlaneOwnedObjects returns a handler.Map func returning a list of requests for DataPlaneOwnedResource
// objects owned by the given DataPlane.
func requestsForDataPlaneOwnedObjects[T DataPlaneOwnedResource](cl client.Client) handler.MapFunc {
	return func(ctx context.Context, dp client.Object) []ctrl.Request {
		logger := ctrllog.FromContext(ctx, "dataplane", dp.GetNamespace()+"/"+dp.GetName())

		switch any(*new(T)).(type) {
		case corev1.Service:
			svcs, err := k8sutils.ListServicesForOwner(ctx, cl, dp.GetNamespace(), dp.GetUID())
			if err != nil {
				logger.Error(err, "failed to list services for dataplane")
				return nil
			}
			return objectsListToRequests(lo.ToSlicePtr(svcs))
		case appsv1.Deployment:
			dps, err := k8sutils.ListDeploymentsForOwner(ctx, cl, dp.GetNamespace(), dp.GetUID())
			if err != nil {
				logger.Error(err, "failed to list deployments for dataplane")
				return nil
			}
			return objectsListToRequests(lo.ToSlicePtr(dps))
		case corev1.Secret:
			secrets, err := k8sutils.ListSecretsForOwner(ctx, cl, dp.GetUID(), client.InNamespace(dp.GetNamespace()))
			if err != nil {
				logger.Error(err, "failed to list secrets for dataplane")
				return nil
			}
			return objectsListToRequests(lo.ToSlicePtr(secrets))
		}
		return nil
	}
}

func objectsListToRequests[T metav1.Object](objs []T) []ctrl.Request {
	reqs := make([]ctrl.Request, 0, len(objs))
	for _, obj := range objs {
		reqs = append(reqs, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: obj.GetNamespace(),
				Name:      obj.GetName(),
			},
		})
	}
	return reqs
}
