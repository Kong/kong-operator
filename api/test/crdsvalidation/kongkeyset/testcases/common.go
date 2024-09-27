package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// testCase is a test case related to KongKeySet validation.
type testCase struct {
	Name                       string
	KongKeySet                 configurationv1alpha1.KongKeySet
	KongKeySetStatus           *configurationv1alpha1.KongKeySetStatus
	Update                     func(*configurationv1alpha1.KongKeySet)
	ExpectedErrorMessage       *string
	ExpectedUpdateErrorMessage *string
}

type testCasesGroup struct {
	Name      string
	TestCases []testCase
}

// TestCases is a collection of all test cases groups related to KongKeySet validation.
var TestCases = []testCasesGroup{}

func init() {
	TestCases = append(TestCases, keySetAPISpec, cpRef)
}

var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-kongkeyset-",
	Namespace:    "default",
}
