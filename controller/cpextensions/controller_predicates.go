package cpextensions

import (
	"github.com/google/go-cmp/cmp"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	commonv1alpha1 "github.com/kong/kong-operator/v2/api/common/v1alpha1"
	operatorv1alpha1 "github.com/kong/kong-operator/v2/api/gateway-operator/v1alpha1"
	gwtypes "github.com/kong/kong-operator/v2/internal/types"
)

// ControlPlaneDataPlanePluginsSpecChangedPredicate is a predicate that checks if the
// ControlPlane's DataPlane metrics extensions have changed.
type ControlPlaneDataPlanePluginsSpecChangedPredicate struct {
	predicate.Funcs
}

// Create returns true if at least one DataPlane metrics extensions is set on
// the ControlPlane.
func (ControlPlaneDataPlanePluginsSpecChangedPredicate) Create(e event.CreateEvent) bool {
	if e.Object == nil {
		return false
	}
	return dataplaneMetricsExtensionIsAttachedToControlPlane(e.Object)
}

// Update returns true if the ControlPlane's DataPlane metrics extensions have changed.
func (ControlPlaneDataPlanePluginsSpecChangedPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil {
		return false
	}
	cpOld, ok := e.ObjectOld.(*gwtypes.ControlPlane)
	if !ok {
		return false
	}

	if e.ObjectNew == nil {
		return false
	}
	cpNew, ok := e.ObjectNew.(*gwtypes.ControlPlane)
	if !ok {
		return false
	}

	newExts := make([]commonv1alpha1.ExtensionRef, 0, len(cpNew.Spec.Extensions))
	for _, ext := range cpNew.Spec.Extensions {
		if ext.Kind == operatorv1alpha1.DataPlaneMetricsExtensionKind &&
			ext.Group == operatorv1alpha1.SchemeGroupVersion.Group {
			newExts = append(newExts, ext)
		}
	}

	oldExts := make([]commonv1alpha1.ExtensionRef, 0, len(cpNew.Spec.Extensions))
	for _, ext := range cpOld.Spec.Extensions {
		if ext.Kind == operatorv1alpha1.DataPlaneMetricsExtensionKind &&
			ext.Group == operatorv1alpha1.SchemeGroupVersion.Group {
			oldExts = append(oldExts, ext)
		}
	}

	return !cmp.Equal(newExts, oldExts)
}

// Delete returns true if the ControlPlane's DataPlanePluginOptions is set.
func (ControlPlaneDataPlanePluginsSpecChangedPredicate) Delete(e event.DeleteEvent) bool {
	if e.Object == nil {
		return false
	}
	return dataplaneMetricsExtensionIsAttachedToControlPlane(e.Object)
}

func dataplaneMetricsExtensionIsAttachedToControlPlane(obj client.Object) bool {
	controlplane, ok := obj.(*gwtypes.ControlPlane)
	if !ok {
		return false
	}
	for _, ext := range controlplane.Spec.Extensions {
		if ext.Kind == operatorv1alpha1.DataPlaneMetricsExtensionKind ||
			ext.Group == operatorv1alpha1.SchemeGroupVersion.Group {
			return true
		}
	}
	return false
}
