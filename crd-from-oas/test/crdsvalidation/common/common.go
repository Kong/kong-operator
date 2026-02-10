package common

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// CommonObjectMeta returns a common ObjectMeta for test objects in the given namespace.
func CommonObjectMeta(ns string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		GenerateName: "test-",
		Namespace:    ns,
	}
}
