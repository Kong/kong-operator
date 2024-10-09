package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// testCase is a test case related to KonnectAPIAuthConfiguration validation.
type testCase struct {
	Name                        string
	KonnectAPIAuthConfiguration konnectv1alpha1.KonnectAPIAuthConfiguration
	Update                      func(*konnectv1alpha1.KonnectAPIAuthConfiguration)
	ExpectedErrorMessage        *string
	ExpectedUpdateErrorMessage  *string
}

// testCasesGroup is a group of test cases related to KonnectAPIAuthConfiguration validation.
// The grouping is done by a common name.
type testCasesGroup struct {
	Name      string
	TestCases []testCase
}

// TestCases is a collection of all test cases groups related to KonnectAPIAuthConfiguration validation.
var TestCases = []testCasesGroup{}

func init() {
	TestCases = append(TestCases,
		specTestCases,
	)
}

var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-konnectapiauthconfiguration-",
	Namespace:    "default",
}
