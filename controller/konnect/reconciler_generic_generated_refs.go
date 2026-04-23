package konnect

// TODO: this file contains manually maintained reference handling for generated Konnect types.
// This is a temporary solution until we have a more generic way of handling
// references for generated types, e.g. by generating reference handling code in the future with crd-from-oas.

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
)

type generatedReferenceHandler[T any] func(context.Context, T) (bool, ctrl.Result, error)

// handleGeneratedTypeReferences runs reference handling that is specific to
// generated Konnect types.
func (r *KonnectEntityReconciler[T, TEnt]) handleGeneratedTypeReferences(
	ctx context.Context,
	ent TEnt,
) (bool, ctrl.Result, error) {
	// if the generated reference handlers are nil for some reason we re-generate them.
	// e.g. reconciler created using struct literal instead of NewKonnectEntityReconciler.
	if r.generatedRefHandlers == nil {
		r.generatedRefHandlers = r.generatedTypeReferenceHandlers()
	}

	for _, handler := range r.generatedRefHandlers {
		if stop, res, err := handler(ctx, ent); stop {
			return true, res, err
		}
	}

	return false, ctrl.Result{}, nil
}

func (r *KonnectEntityReconciler[T, TEnt]) generatedTypeReferenceHandlers() []generatedReferenceHandler[TEnt] {
	return []generatedReferenceHandler[TEnt]{
		r.handleGeneratedEventGatewayRef,
		r.handleGeneratedPortalRef,
	}
}
