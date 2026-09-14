package utils

import (
	"errors"
	"fmt"

	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	configurationv1alpha1 "github.com/kong/kong-operator/v2/api/configuration/v1alpha1"
	"github.com/kong/kong-operator/v2/ingress-controller/internal/annotations"
	k8sutils "github.com/kong/kong-operator/v2/pkg/utils/kubernetes"
)

const defaultIngressClassAnnotation = "ingressclass.kubernetes.io/is-default-class"

// IsDefaultIngressClass returns whether an IngressClass is the default IngressClass.
func IsDefaultIngressClass(obj client.Object) bool {
	if ingressClass, ok := obj.(*netv1.IngressClass); ok {
		return ingressClass.Annotations[defaultIngressClassAnnotation] == "true"
	}
	return false
}

// MatchesIngressClass indicates whether or not an object belongs to a given ingress class.
func MatchesIngressClass(obj client.Object, controllerIngressClass string, isDefault bool) bool {
	objectIngressClass := obj.GetAnnotations()[annotations.IngressClassKey]
	if isDefault && IsIngressClassEmpty(obj) {
		return true
	}
	if ing, isV1Ingress := obj.(*netv1.Ingress); isV1Ingress {
		if ing.Spec.IngressClassName != nil && *ing.Spec.IngressClassName == controllerIngressClass {
			return true
		}
	}
	// For KongCustomEntities, we check whether the `spec.ControllerName` matches.
	if customEntity, isKongCustomEntity := obj.(*configurationv1alpha1.KongCustomEntity); isKongCustomEntity {
		if customEntity.Spec.ControllerName == controllerIngressClass {
			return true
		}
	}
	return objectIngressClass == controllerIngressClass
}

// GeneratePredicateFuncsForIngressClassFilter builds a controller-runtime reconciliation predicate function which filters out objects
// which have their ingress class set to the a value other than the controller class.
func GeneratePredicateFuncsForIngressClassFilter(name string) predicate.Funcs {
	preds := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		// we assume true for isDefault here because the predicates have no client and cannot check if the class is
		// default. classless and are filtered out by Reconcile() if the configured class is not the default class
		return MatchesIngressClass(obj, name, true)
	})
	preds.UpdateFunc = func(e event.UpdateEvent) bool {
		return MatchesIngressClass(e.ObjectOld, name, true) || MatchesIngressClass(e.ObjectNew, name, true)
	}
	return preds
}

// IsIngressClassEmpty returns true if an object has no ingress class information or false otherwise.
func IsIngressClassEmpty(obj client.Object) bool {
	switch obj := obj.(type) {
	case *netv1.Ingress:
		// netv1.Ingress is the only kind with an explicit IngressClassName field. All other resources use annotations
		// the annotation is deprecated for netv1.Ingress, and the older Ingress versions are themselves deprecated
		// our CRDs use the annotation, but should probably transition to a field eventually to align with Ingress
		if _, ok := obj.GetAnnotations()[annotations.IngressClassKey]; !ok {
			return obj.Spec.IngressClassName == nil
		}
		return false
	default:
		if _, ok := obj.GetAnnotations()[annotations.IngressClassKey]; ok {
			return false
		}
		return true
	}
}

// ErrReferenceGrantCRDNotFound is returned by DetectReferenceGrantVersion when the
// cluster serves neither the v1 nor the v1beta1 ReferenceGrant CRD.
var ErrReferenceGrantCRDNotFound = errors.New("neither v1 nor v1beta1 ReferenceGrant CRD found")

// DetectReferenceGrantVersion returns the GroupVersion of whichever ReferenceGrant
// API version is served by the cluster, preferring v1 and falling back to v1beta1
// (ReferenceGrant was promoted from v1beta1 to v1 in gateway-api v1.5.0; older
// clusters only serve v1beta1). It returns ErrReferenceGrantCRDNotFound if neither
// is installed, and a wrapped lookup error if the lookup itself failed.
func DetectReferenceGrantVersion(restMapper meta.RESTMapper) (schema.GroupVersion, error) {
	for _, gv := range []schema.GroupVersion{
		schema.GroupVersion(gatewayv1.GroupVersion),
		schema.GroupVersion(gatewayv1beta1.GroupVersion),
	} {
		exists, err := k8sutils.CRDExists(restMapper, gv.WithResource("referencegrants"))
		if err != nil {
			return schema.GroupVersion{}, fmt.Errorf("failed to detect the ReferenceGrant API version: %w", err)
		}
		if exists {
			return gv, nil
		}
	}
	return schema.GroupVersion{}, ErrReferenceGrantCRDNotFound
}
