package testcases

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configurationv1alpha1 "github.com/kong/kubernetes-configuration/api/configuration/v1alpha1"
)

// testCase is a test case related to KongDataplaneCertificate validation.
type testCase struct {
	Name                           string
	KongDataplaneCertificate       configurationv1alpha1.KongDataplaneCertificate
	KongDataplaneCertificateStatus *configurationv1alpha1.KongDataplaneCertificateStatus
	Update                         func(*configurationv1alpha1.KongDataplaneCertificate)
	ExpectedErrorMessage           *string
	ExpectedUpdateErrorMessage     *string
}

type testCasesGroup struct {
	Name      string
	TestCases []testCase
}

// TestCases is a collection of all test cases groups related to KongDataplaneCertificate validation.
var TestCases = []testCasesGroup{}

func init() {
	TestCases = append(TestCases, spec, cpRef)
}

var commonObjectMeta = metav1.ObjectMeta{
	GenerateName: "test-kongdataplanecertificate-",
	Namespace:    "default",
}
