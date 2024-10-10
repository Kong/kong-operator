package crdsvalidation_test

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// commonObjectMeta is a common object meta used in tests.
var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-",
	Namespace:    "default",
}
