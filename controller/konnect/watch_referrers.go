package konnect

import (
	"strconv"

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
// Programmed condition's status, reason or observed generation. The latter
// tells its Konnect state changed: e.g. once a referrer dropped a reference,
// its spec change comes before its Konnect update, and Konnect may still
// refuse to delete the entity it used; the Programmed condition observing the
// new generation tells the Konnect update is done, so the deletion is retried
// then. That happens once per generation. Its other status writes don't pass.
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

// programmedCondition returns the status, reason and observed generation of
// the Programmed condition of the object, or "" when it has none.
func programmedCondition(obj any) string {
	withConditions, ok := obj.(interface{ GetConditions() []metav1.Condition })
	if !ok {
		return ""
	}
	cond := apimeta.FindStatusCondition(withConditions.GetConditions(), konnectv1alpha1.KonnectEntityProgrammedConditionType)
	if cond == nil {
		return ""
	}
	return string(cond.Status) + "/" + cond.Reason + "/" + strconv.FormatInt(cond.ObservedGeneration, 10)
}
