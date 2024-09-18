package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// testCase is a test case related to KongUpstream validation.
type testCase struct {
	Name                       string
	KongCACertificate          configurationv1alpha1.KongCACertificate
	KongCACertificateStatus    *configurationv1alpha1.KongCACertificateStatus
	Update                     func(*configurationv1alpha1.KongCACertificate)
	ExpectedErrorMessage       *string
	ExpectedUpdateErrorMessage *string
}

// testCasesGroup is a group of test cases related to KongCACertificates validation.
// The grouping is done by a common name.
type testCasesGroup struct {
	Name      string
	TestCases []testCase
}

// TestCases is a collection of all test cases groups related to KongCACertificates validation.
var TestCases = []testCasesGroup{}

func init() {
	TestCases = append(TestCases,
		cpRef,
		requiredFields,
	)
}

var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-kongupstream-",
	Namespace:    "default",
}
