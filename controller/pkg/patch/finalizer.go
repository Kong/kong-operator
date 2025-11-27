package patch

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// WithFinalizer patches the provided object with the provided finalizer.
// It returns a ctrl.Result and an error.
func WithFinalizer[
	T client.Object,
](
	ctx context.Context,
	cl client.Client,
	ent T,
	finalizer string,
) (bool, ctrl.Result, error) {
	// This check prevents a deep copy of the object when the finalizer is already added.
	// Since finalizers are typically set on an object in small numbers,
	// looping over them twice is not a performance issue.
	if controllerutil.ContainsFinalizer(ent, finalizer) {
		return false, ctrl.Result{}, nil
	}

	objWithFinalizer := ent.DeepCopyObject().(client.Object)
	if !controllerutil.AddFinalizer(objWithFinalizer, finalizer) {
		return false, ctrl.Result{}, nil
	}

	if errUpd := cl.Patch(ctx, objWithFinalizer, client.MergeFrom(ent)); errUpd != nil {
		if k8serrors.IsConflict(errUpd) {
			return false, ctrl.Result{Requeue: true}, nil
		}
		return false, ctrl.Result{}, fmt.Errorf(
			"failed to update finalizer %s: %w",
			finalizer, errUpd,
		)
	}
	return true, ctrl.Result{}, nil
}

// WithoutFinalizer patches the provided object to remove the provided finalizer.
// It returns a bool indicating whether the finalizer was removed, a ctrl.Result, and an error.
func WithoutFinalizer[
	T client.Object,
](
	ctx context.Context,
	cl client.Client,
	ent T,
	finalizer string,
) (bool, ctrl.Result, error) {
	// This check prevents a deep copy of the object when the finalizer is already added.
	// Since finalizers are typically set on an object in small numbers,
	// looping over them twice is not a performance issue.
	if !controllerutil.ContainsFinalizer(ent, finalizer) {
		return false, ctrl.Result{}, nil
	}

	objWithFinalizer := ent.DeepCopyObject().(client.Object)
	if !controllerutil.RemoveFinalizer(objWithFinalizer, finalizer) {
		return false, ctrl.Result{}, nil
	}

	if errUpd := cl.Patch(ctx, objWithFinalizer, client.MergeFrom(ent)); errUpd != nil {
		if k8serrors.IsConflict(errUpd) {
			return false, ctrl.Result{Requeue: true}, nil
		}
		return false, ctrl.Result{}, fmt.Errorf(
			"failed to update finalizer %s: %w",
			finalizer, errUpd,
		)
	}
	return true, ctrl.Result{}, nil
}
