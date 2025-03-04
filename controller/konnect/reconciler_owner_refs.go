package konnect

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kong/gateway-operator/controller/konnect/constraints"
)

// RemoveOwnerRefIfSet removes the owner reference from the given entity if it is set.
// It returns a requeue result if there was a conflict during the patch operation.
//
// Such references may have been added by older versions of KGO (< 1.5).
// TODO: remove this after a couple of minor versions.
func RemoveOwnerRefIfSet[
	T constraints.SupportedKonnectEntityType,
	TEnt constraints.EntityType[T],
	TOwner constraints.SupportedKonnectEntityType,
	TEntOwner constraints.EntityType[TOwner],
](
	ctx context.Context,
	cl client.Client,
	ent TEnt,
	owner TEntOwner,
) (ctrl.Result, error) {
	ownerRefs := ent.GetOwnerReferences()

	ok, err := controllerutil.HasOwnerReference(ownerRefs, owner, cl.Scheme())
	if err != nil {
		ctrllog.FromContext(ctx).Info("failed to check if object has owner reference",
			"error", err,
			"object", ent,
			"owner", owner,
		)
		return ctrl.Result{}, nil
	}
	if !ok {
		return ctrl.Result{}, nil
	}

	old := ent.DeepCopyObject().(TEnt)
	if err := controllerutil.RemoveOwnerReference(owner, ent, cl.Scheme()); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to delete owner reference: %w", err)
	}
	if err := cl.Patch(ctx, ent, client.MergeFrom(old)); err != nil {
		if k8serrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to patch the object to remove owner reference: %w", err)
	}

	return ctrl.Result{}, nil
}
