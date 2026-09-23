package konnect

// TODO: this file contains manually maintained reference handling for generated Konnect types.
// This is a temporary solution until we have a more generic way of handling
// references for generated types, e.g. by generating reference handling code in the future with crd-from-oas.

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	aiconfigurationv1alpha1 "github.com/kong/kong-operator/v2/api/aiconfiguration/v1alpha1"
	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
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
	_generatedHandlersPerGVK map[schema.GroupVersionKind]generatedParentRefHandler
)

func init() {
	// _generatedHandlers contains the list of generated reference handlers to
	// be used in handleGeneratedTypeReferences.
	// Each handler is responsible for handling references to a specific parent type, for example:
	// - parentRefHandler[konnectv1alpha1.Portal, *konnectv1alpha1.Portal] for handling references to Portal parents.
	// This list is manually maintained for now, but in the future we may want to
	// generate this list based on the generated types and their reference configurations.
	type generatedParentRefHandlerEntry struct {
		gvk     schema.GroupVersionKind
		handler generatedParentRefHandler
	}

	_generatedHandlers := []generatedParentRefHandlerEntry{
		{
			gvk:     konnectv1alpha2.GroupVersion.WithKind("KonnectGatewayControlPlane"),
			handler: parentRefHandler[konnectv1alpha2.KonnectGatewayControlPlane, *konnectv1alpha2.KonnectGatewayControlPlane]{},
		},
		{
			gvk:     configurationv1alpha1.GroupVersion.WithKind("EventGatewayVirtualCluster"),
			handler: parentRefHandler[configurationv1alpha1.EventGatewayVirtualCluster, *configurationv1alpha1.EventGatewayVirtualCluster]{},
		},
		{
			gvk:     configurationv1alpha1.GroupVersion.WithKind("EventGatewayBackendCluster"),
			handler: parentRefHandler[configurationv1alpha1.EventGatewayBackendCluster, *configurationv1alpha1.EventGatewayBackendCluster]{},
		},
		{
			gvk:     configurationv1alpha1.GroupVersion.WithKind("EventGatewayListener"),
			handler: parentRefHandler[configurationv1alpha1.EventGatewayListener, *configurationv1alpha1.EventGatewayListener]{},
		},
		{
			gvk:     konnectv1alpha1.GroupVersion.WithKind("KonnectEventGateway"),
			handler: parentRefHandler[konnectv1alpha1.KonnectEventGateway, *konnectv1alpha1.KonnectEventGateway]{},
		},
		{
			gvk:     konnectv1alpha1.GroupVersion.WithKind("Portal"),
			handler: parentRefHandler[konnectv1alpha1.Portal, *konnectv1alpha1.Portal]{},
		},
		{
			gvk:     konnectv1alpha1.GroupVersion.WithKind("KonnectAIGateway"),
			handler: parentRefHandler[konnectv1alpha1.KonnectAIGateway, *konnectv1alpha1.KonnectAIGateway]{},
		},
		{
			gvk:     aiconfigurationv1alpha1.GroupVersion.WithKind("AIGatewayConsumer"),
			handler: parentRefHandler[aiconfigurationv1alpha1.AIGatewayConsumer, *aiconfigurationv1alpha1.AIGatewayConsumer]{},
		},
	}
	_generatedHandlersPerGVK = make(map[schema.GroupVersionKind]generatedParentRefHandler, len(_generatedHandlers))
	for _, entry := range _generatedHandlers {
		_generatedHandlersPerGVK[entry.gvk] = entry.handler
	}

}

// _generatedTypeReferenceHandlers returns a map of generated reference handlers
// keyed by the full GVK of the parent type they handle.
func _generatedTypeReferenceHandlers() map[schema.GroupVersionKind]generatedParentRefHandler {
	return _generatedHandlersPerGVK
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

// konnectReconciliationSkipper is implemented by generated entities whose
// parent reference can resolve to a parent the Konnect reconciler does not
// manage: an AIGatewayRef targeting an OnPremAIGateway is owned by the on-prem
// reconciliation machinery, so reconciling it here would be wrong.
type konnectReconciliationSkipper interface {
	SkipKonnectReconciliation() bool
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

	parentGVK := obj.GetParentGVK()
	handler, ok := _generatedTypeReferenceHandlers()[parentGVK]
	if !ok || handler.parentTypeName() != parentGVK.Kind {
		// The parent reference resolves to a parent this reconciler does not
		// manage. Entities that can target such parents (AI Gateway
		// configuration entities referencing an OnPremAIGateway) are skipped
		// silently: the on-prem machinery owns them.
		if skipper, isSkipper := any(ent).(konnectReconciliationSkipper); isSkipper && skipper.SkipKonnectReconciliation() {
			// Drop the Konnect cleanup finalizer from a deleting entity: the
			// on-prem machinery owns it now and never removes this finalizer,
			// so keeping it would wedge the entity in Terminating. Same
			// tradeoff as the missing-parent paths: a Konnect entity created
			// before the repoint may leak.
			if !ent.GetDeletionTimestamp().IsZero() && controllerutil.RemoveFinalizer(ent, KonnectCleanupFinalizer) {
				if err := r.Client.Update(ctx, ent); err != nil {
					if apierrors.IsConflict(err) {
						return true, ctrl.Result{Requeue: true}, nil
					}
					// in case the finalizer removal fails because the resource does not exist, ignore the error.
					if apierrors.IsNotFound(err) {
						return true, ctrl.Result{}, nil
					}
					return true, ctrl.Result{}, fmt.Errorf("failed to remove finalizer %s: %w", KonnectCleanupFinalizer, err)
				}
			}
			return true, ctrl.Result{}, nil
		}
		return false, ctrl.Result{}, &UnsupportedGeneratedReferenceTypeError{
			TypeName: parentGVK.String(),
		}
	}

	res, err := handler.handleParentRef(ctx, r.Client, obj)

	stop, res, err := handleRefResult(ctx, r.Client, ent, res, err)
	if stop || err != nil {
		return true, res, err
	}

	return false, ctrl.Result{}, nil
}
