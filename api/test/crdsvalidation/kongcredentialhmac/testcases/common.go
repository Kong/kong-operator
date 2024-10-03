package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// testCase is a test case related to KongCredentialHMAC validation.
type testCase struct {
	Name                       string
	KongCredentialHMAC         configurationv1alpha1.KongCredentialHMAC
	KongCredentialHMACStatus   *configurationv1alpha1.KongCredentialHMACStatus
	Update                     func(*configurationv1alpha1.KongCredentialHMAC)
	ExpectedErrorMessage       *string
	ExpectedUpdateErrorMessage *string
}

// testCasesGroup is a group of test cases related to KongCredentialHMAC validation.
// The grouping is done by a common name.
type testCasesGroup struct {
	Name      string
	TestCases []testCase
}

// TestCases is a collection of all test cases groups related to KongCredentialHMAC validation.
var TestCases = []testCasesGroup{}

func init() {
	TestCases = append(TestCases,
		updatesNotAllowedForStatus,
		fieldsValidation,
	)
}

var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-kongcredential-hmac-",
	Namespace:    "default",
}
