package common

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// CommonObjectMeta is a common object meta used in tests.
var CommonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-",
	Namespace:    "default",
}
