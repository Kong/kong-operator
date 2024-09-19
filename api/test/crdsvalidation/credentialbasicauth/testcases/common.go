package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// testCase is a test case related to KongConsumer validation.
type testCase struct {
	Name                       string
	CredentialBasicAuth        configurationv1alpha1.CredentialBasicAuth
	CredentialBasicAuthStatus  *configurationv1alpha1.CredentialBasicAuthStatus
	Update                     func(*configurationv1alpha1.CredentialBasicAuth)
	ExpectedErrorMessage       *string
	ExpectedUpdateErrorMessage *string
}

// testCasesGroup is a group of test cases related to CredentialBasicAuth validation.
// The grouping is done by a common name.
type testCasesGroup struct {
	Name      string
	TestCases []testCase
}

// TestCases is a collection of all test cases groups related to CredentialBasicAuth validation.
var TestCases = []testCasesGroup{}

func init() {
	TestCases = append(TestCases,
		requiredFields,
		updatesNotAllowedForStatus,
	)
}

var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-credentialbasicauth-",
	Namespace:    "default",
}
