package utils

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/v2/api/konnect/v1alpha1"
)

// ReduceFunc defines a function type that takes a slice of unstructured.Unstructured objects
// and returns a (possibly modified or filtered) slice of unstructured.Unstructured objects.
type ReduceFunc func([]unstructured.Unstructured) []unstructured.Unstructured

// KeepYoungest returns a new slice with all elements except the youngest (most recently created) object removed from the input slice.
// If the input slice is empty, it returns nil.
func KeepYoungest(objs []unstructured.Unstructured) []unstructured.Unstructured {
	if len(objs) == 0 {
		return []unstructured.Unstructured{}
	}

	index := 0
	youngest := objs[index]
	for i := 1; i < len(objs); i++ {
		if objs[i].GetCreationTimestamp().After(youngest.GetCreationTimestamp().Time) {
			youngest = objs[i]
			index = i
		}
	}

	return append(objs[0:index], objs[index+1:]...)
}

// KeepProgrammed filters the given slice of unstructured.Unstructured objects,
// returning only those that are not in the "Programmed" condition.
// If all objects are programmed, it returns the original slice.
func KeepProgrammed(objs []unstructured.Unstructured) []unstructured.Unstructured {
	isProgrammed := func(obj unstructured.Unstructured) bool {
		conditions, found, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
		if !found || err != nil {
			return false
		}
		for _, c := range conditions {
			cond, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if cond["type"] == konnectv1alpha1.KonnectEntityProgrammedConditionType && cond["status"] == "True" {
				return true
			}
		}
		return false
	}

	var notProgrammedObjects []unstructured.Unstructured
	for _, obj := range objs {
		if !isProgrammed(obj) {
			notProgrammedObjects = append(notProgrammedObjects, obj)
		}
	}
	if len(notProgrammedObjects) > 0 && len(notProgrammedObjects) < len(objs) {
		return notProgrammedObjects
	}
	return []unstructured.Unstructured{}
}
