package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// testCase is a test case related to KongService validation.
type testCase struct {
	Name                       string
	KongService                configurationv1alpha1.KongService
	KongServiceStatus          *configurationv1alpha1.KongServiceStatus
	Update                     func(*configurationv1alpha1.KongService)
	ExpectedErrorMessage       *string
	ExpectedUpdateErrorMessage *string
}

// testCasesGroup is a group of test cases related to KongService validation.
// The grouping is done by a common name.
type testCasesGroup struct {
	Name      string
	TestCases []testCase
}

// TestCases is a collection of all test cases groups related to KongService validation.
var TestCases = []testCasesGroup{}

func init() {
	TestCases = append(TestCases,
		cpRef,
	)
}

var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-kongservice-",
	Namespace:    "default",
}
