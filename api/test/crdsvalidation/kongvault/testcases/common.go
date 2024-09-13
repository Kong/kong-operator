package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// testCase is a test case related to KongTarget validation.
type testCase struct {
	Name                       string
	KongVault                  configurationv1alpha1.KongVault
	KongVaultStatus            *configurationv1alpha1.KongVaultStatus
	Update                     func(*configurationv1alpha1.KongVault)
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
	TestCases = append(TestCases, controlPlaneRef, vaultSpec)
}

var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-kongvault-",
	Namespace:    "default",
}
