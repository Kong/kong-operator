package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// testCase is a test case related to KongTarget validation.
type testCase struct {
	Name                       string
	KongTarget                 configurationv1alpha1.KongTarget
	KongTargetStatus           *configurationv1alpha1.KongTargetStatus
	Update                     func(*configurationv1alpha1.KongTarget)
	ExpectedErrorMessage       *string
	ExpectedUpdateErrorMessage *string
}

type testCasesGroup struct {
	Name      string
	TestCases []testCase
}

// TestCases is a collection of all test cases groups related to KongTarget validation.
var TestCases = []testCasesGroup{}

func init() {
	TestCases = append(TestCases, upstreamRef, kongTargetAPISpec)
}

var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-kongtarget-",
	Namespace:    "default",
}
