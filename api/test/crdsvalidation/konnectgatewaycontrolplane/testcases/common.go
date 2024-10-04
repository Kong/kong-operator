package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	konnectv1alpha1 "github.com/kong/kubernetes-configuration/api/konnect/v1alpha1"
)

// testCase is a test case related to KonnectGatewayControlPlane validation.
type testCase struct {
	Name                       string
	KonnectGatewayControlPlane konnectv1alpha1.KonnectGatewayControlPlane
	Update                     func(*konnectv1alpha1.KonnectGatewayControlPlane)
	ExpectedErrorMessage       *string
	ExpectedUpdateErrorMessage *string
}

// testCasesGroup is a group of test cases related to KonnectGatewayControlPlane validation.
// The grouping is done by a common name.
type testCasesGroup struct {
	Name      string
	TestCases []testCase
}

// TestCases is a collection of all test cases groups related to KonnectGatewayControlPlane validation.
var TestCases = []testCasesGroup{}

func init() {
	TestCases = append(TestCases,
		updatesNotAllowedForStatus,
		membersCanOnlyBeSetForControlPlaneGroups,
	)
}

var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-konnect-control-plane",
	Namespace:    "default",
}
