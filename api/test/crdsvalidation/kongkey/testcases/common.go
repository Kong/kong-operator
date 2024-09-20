package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// testCase is a test case related to KongKey validation.
type testCase struct {
	Name                       string
	KongKey                    configurationv1alpha1.KongKey
	KongKeyStatus              *configurationv1alpha1.KongKeyStatus
	Update                     func(*configurationv1alpha1.KongKey)
	ExpectedErrorMessage       *string
	ExpectedUpdateErrorMessage *string
}

type testCasesGroup struct {
	Name      string
	TestCases []testCase
}

// TestCases is a collection of all test cases groups related to KongKey validation.
var TestCases = []testCasesGroup{}

func init() {
	TestCases = append(TestCases, keySetAPISpec, keySetRef, cpRef)
}

var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-kongkey-",
	Namespace:    "default",
}
