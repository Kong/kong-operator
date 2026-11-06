package konnect

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	konnectv1alpha1 "github.com/kong/kong-operator/v2/api/konnect/v1alpha1"
)

// referrerChanged lets through the changes of an object referencing others
// (see the reverseWatch references) that may change whether they are in use:
// a change of its spec, its creation and its deletion, and a change of its
// Programmed condition, which tells its Konnect state changed, e.g. it stopped
// using an entity Konnect refused to delete. Its other status writes don't.
func referrerChanged() predicate.Predicate {
	return predicate.Or[client.Object](
		predicate.GenerationChangedPredicate{},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				return programmedCondition(e.ObjectOld) != programmedCondition(e.ObjectNew)
			},
			CreateFunc:  func(event.CreateEvent) bool { return false },
			DeleteFunc:  func(event.DeleteEvent) bool { return false },
			GenericFunc: func(event.GenericEvent) bool { return false },
		},
	)
}

// programmedCondition returns the status and reason of the Programmed
// condition of the object, or "" when it has none.
func programmedCondition(obj any) string {
	withConditions, ok := obj.(interface{ GetConditions() []metav1.Condition })
	if !ok {
		return ""
	}
	cond := apimeta.FindStatusCondition(withConditions.GetConditions(), konnectv1alpha1.KonnectEntityProgrammedConditionType)
	if cond == nil {
		return ""
	}
	return string(cond.Status) + "/" + cond.Reason
}
