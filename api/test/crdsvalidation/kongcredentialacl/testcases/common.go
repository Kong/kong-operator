package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// testCase is a test case related to KongConsumer validation.
type testCase struct {
	Name                       string
	KongCredentialACL          configurationv1alpha1.KongCredentialACL
	KongCredentialACLStatus    *configurationv1alpha1.KongCredentialACLStatus
	Update                     func(*configurationv1alpha1.KongCredentialACL)
	ExpectedErrorMessage       *string
	ExpectedUpdateErrorMessage *string
}

// testCasesGroup is a group of test cases related to KongCredentialACL validation.
// The grouping is done by a common name.
type testCasesGroup struct {
	Name      string
	TestCases []testCase
}

// TestCases is a collection of all test cases groups related to KongCredentialACL validation.
var TestCases = []testCasesGroup{}

func init() {
	TestCases = append(TestCases,
		requiredFields,
		updatesNotAllowedForStatus,
	)
}

var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-kongcredential-acl-",
	Namespace:    "default",
}
