package konnect

// TODO: this file contains manually maintained reference handling for generated Konnect types.
// This is a temporary solution until we have a more generic way of handling
// references for generated types, e.g. by generating reference handling code in the future with crd-from-oas.

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
	konnectv1alpha2 "github.com/kong/kong-operator/v2/api/konnect/v1alpha2"
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
	GetNamespace() string
}

type parentWithAPIAuthTPtr[T parentT] interface {
	parentTPtr[T]
	GetKonnectAPIAuthConfigurationRef() konnectv1alpha2.ControlPlaneKonnectAPIAuthConfigurationRef
}

type generatedParentRefHandler interface {
	handleParentRef(context.Context, client.Client, objectWithParentRef) (ctrl.Result, error)
	parentTypeName() string
}

var (
	_generatedHandlersPerKind map[string]generatedParentRefHandler
)

func init() {
	// _generatedHandlers contains the list of generated reference handlers to
	// be used in handleGeneratedTypeReferences.
	// Each handler is responsible for handling references to a specific parent type, for example:
	// - parentRefHandler[konnectv1alpha1.Portal, *konnectv1alpha1.Portal] for handling references to Portal parents.
	// This list is manually maintained for now, but in the future we may want to
	// generate this list based on the generated types and their reference configurations.
	_generatedHandlers := []generatedParentRefHandler{
		parentRefHandler[konnectv1alpha1.EventGatewayListener, *konnectv1alpha1.EventGatewayListener]{},
		parentRefHandler[konnectv1alpha1.KonnectEventGateway, *konnectv1alpha1.KonnectEventGateway]{},
		parentRefHandler[konnectv1alpha1.Portal, *konnectv1alpha1.Portal]{},
	}
	_generatedHandlersPerKind = make(map[string]generatedParentRefHandler)
	for _, handler := range _generatedHandlers {
		_generatedHandlersPerKind[handler.parentTypeName()] = handler
	}

}

// _generatedTypeReferenceHandlers returns a map of generated reference handlers
// keyed by the Kind of the parent type they handle, for example:
// "Portal" for handlers that handle references to Portal parents.
func _generatedTypeReferenceHandlers() map[string]generatedParentRefHandler {
	return _generatedHandlersPerKind
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

// handleGeneratedTypeParentReferences runs reference handling that is specific to
// generated Konnect types.
func (r *KonnectEntityReconciler[T, TEnt]) handleGeneratedTypeParentReferences(
	ctx context.Context,
	ent TEnt,
) (bool, ctrl.Result, error) {
	obj, ok := any(ent).(objectWithParentRef)
	if !ok {
		return false, ctrl.Result{}, nil
	}

	// TODO: This only compares the Kind and doesn't consider the Group or API Version.
	handler, ok := _generatedTypeReferenceHandlers()[obj.GetParentGVK().Kind]
	if !ok || handler.parentTypeName() != obj.GetParentGVK().Kind {
		return false, ctrl.Result{}, &UnsupportedGeneratedReferenceTypeError{
			TypeName: obj.GetParentGVK().Kind,
		}
	}

	res, err := handler.handleParentRef(ctx, r.Client, obj)

	stop, res, err := handleRefResult(ctx, r.Client, ent, res, err)
	if stop || err != nil {
		return true, res, err
	}

	return false, ctrl.Result{}, nil
}
