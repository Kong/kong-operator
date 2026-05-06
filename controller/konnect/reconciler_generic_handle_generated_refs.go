package konnect

// TODO: this file contains manually maintained reference handling for generated Konnect types.
// This is a temporary solution until we have a more generic way of handling
// references for generated types, e.g. by generating reference handling code in the future with crd-from-oas.

import (
	"context"
	"errors"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

type parentT interface {
	GetTypeName() string
}

type parentTPtr[T parentT] interface {
	*T
	k8sutils.ConditionsAwareObject
	GetKonnectID() string
	GetTypeName() string
}

type generatedParentRefHandler interface {
	handleParentRef(context.Context, client.Client, objectWithParentRef) (ctrl.Result, error)
}

var _generatedHandlers []generatedParentRefHandler

func init() {
	_generatedHandlers = []generatedParentRefHandler{
		parentRefHandler[konnectv1alpha1.KonnectEventGateway, *konnectv1alpha1.KonnectEventGateway]{},
		parentRefHandler[konnectv1alpha1.Portal, *konnectv1alpha1.Portal]{},
	}
}

func _generatedTypeReferenceHandlers() []generatedParentRefHandler {
	return _generatedHandlers
}

// UnsupportedGeneratedReferenceTypeError is returned by generated reference handlers
// when they encounter a reference type that they do not support.
type UnsupportedGeneratedReferenceTypeError struct {
	TypeName string
}

// Error implements the error interface.
func (e *UnsupportedGeneratedReferenceTypeError) Error() string {
	return "unsupported generated reference type: " + e.TypeName
}

// handleGeneratedTypeReferences runs reference handling that is specific to
// generated Konnect types.
func (r *KonnectEntityReconciler[T, TEnt]) handleGeneratedTypeReferences(
	ctx context.Context,
	ent TEnt,
) (bool, ctrl.Result, error) {
	for _, handler := range _generatedTypeReferenceHandlers() {
		obj, ok := any(ent).(objectWithParentRef)
		if !ok {
			continue
		}
		res, err := handler.handleParentRef(ctx, r.Client, obj)
		if err != nil {
			// Only UnsupportedGeneratedReferenceTypeError are handled here
			// to continue to the next handler.
			// All other errors should be handled in handleRefResult.
			if _, ok := errors.AsType[*UnsupportedGeneratedReferenceTypeError](err); ok {
				// This handler is not applicable to the given type, continue to the next handler.
				continue
			}
		}

		stop, res, err := handleRefResult(ctx, r.Client, ent, res, err)
		if stop || err != nil {
			return true, res, err
		}
	}

	return false, ctrl.Result{}, nil
}
