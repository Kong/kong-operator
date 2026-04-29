package konnect

// TODO: this file contains manually maintained reference handling for generated Konnect types.
// This is a temporary solution until we have a more generic way of handling
// references for generated types, e.g. by generating reference handling code in the future with crd-from-oas.

import (
	"context"
	"errors"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

type generatedReferenceHandler func(context.Context, client.Client, k8sutils.ConditionsAwareObject) (ctrl.Result, error)

var _generatedHandlers []generatedReferenceHandler

func init() {
	_generatedHandlers = []generatedReferenceHandler{
		handleEventGatewayRef,
		handlePortalRef,
	}
}

func _generatedTypeReferenceHandlers() []generatedReferenceHandler {
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
		res, err := handler(ctx, r.Client, ent)
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
